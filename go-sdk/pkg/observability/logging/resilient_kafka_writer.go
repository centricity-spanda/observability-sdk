package logging

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/IBM/sarama"
)

// ResilientKafkaWriter provides Kafka logging with file fallback and auto-replay
type ResilientKafkaWriter struct {
	// Kafka producer
	producer sarama.AsyncProducer
	topic    string
	config   *Config

	// Fallback buffer
	fallback *FallbackBuffer

	// Checkpoint tracking
	checkpoint *CheckpointManager

	// Health state
	healthy     atomic.Bool
	healthCheck *time.Ticker

	// Replay control
	replayCtx    context.Context
	replayCancel context.CancelFunc
	replaying    atomic.Bool

	// Synchronization
	mu     sync.RWMutex
	closed bool
	wg     sync.WaitGroup
}

// NewResilientKafkaWriter creates a Kafka writer with file fallback
func NewResilientKafkaWriter(cfg *Config) (*ResilientKafkaWriter, error) {
	if len(cfg.KafkaBrokers) == 0 {
		return nil, nil
	}

	// Configure Kafka
	saramaConfig := sarama.NewConfig()
	saramaConfig.Producer.RequiredAcks = sarama.WaitForLocal
	if cfg.IsAuditLog() {
		saramaConfig.Producer.RequiredAcks = sarama.WaitForAll
	}
	saramaConfig.Producer.Compression = sarama.CompressionSnappy
	saramaConfig.Producer.Flush.Frequency = cfg.FlushInterval
	saramaConfig.Producer.Flush.Messages = cfg.ChannelBufferSize / 10
	saramaConfig.Producer.Return.Successes = false
	saramaConfig.Producer.Return.Errors = true
	saramaConfig.Producer.Retry.Max = 3

	producer, err := sarama.NewAsyncProducer(cfg.KafkaBrokers, saramaConfig)
	if err != nil {
		return nil, err
	}

	// Create fallback buffer
	fallbackCfg := NewFallbackBufferConfig()
	fallback, err := NewFallbackBuffer(fallbackCfg)
	if err != nil {
		producer.Close()
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create checkpoint manager
	checkpointDir := getEnv("LOG_FALLBACK_DIR", "./logs/buffer")
	checkpointMgr, err := NewCheckpointManager(checkpointDir)
	if err != nil {
		os.Stderr.WriteString("Warning: Failed to create checkpoint manager: " + err.Error() + "\n")
	}

	rw := &ResilientKafkaWriter{
		producer:     producer,
		topic:        cfg.LogTopic,
		config:       cfg,
		fallback:     fallback,
		checkpoint:   checkpointMgr,
		replayCtx:    ctx,
		replayCancel: cancel,
	}

	// Start healthy
	rw.healthy.Store(true)

	// Start error handler
	rw.wg.Add(1)
	go rw.handleErrors()

	// Start health checker
	checkInterval := time.Duration(getEnvInt("LOG_KAFKA_HEALTH_CHECK_INTERVAL_S", 10)) * time.Second
	rw.healthCheck = time.NewTicker(checkInterval)
	rw.wg.Add(1)
	go rw.runHealthCheck()

	return rw, nil
}

// Write implements io.Writer - sends to Kafka or fallback
func (rw *ResilientKafkaWriter) Write(p []byte) (n int, err error) {
	rw.mu.RLock()
	if rw.closed {
		rw.mu.RUnlock()
		return 0, nil
	}
	rw.mu.RUnlock()

	// If healthy, try Kafka
	if rw.healthy.Load() {
		msg := &sarama.ProducerMessage{
			Topic:     rw.topic,
			Value:     sarama.ByteEncoder(p),
			Timestamp: time.Now(),
		}

		select {
		case rw.producer.Input() <- msg:
			if rw.checkpoint != nil {
				rw.checkpoint.RecordSent(1)
			}
			return len(p), nil
		default:
			// Channel full - use fallback
			rw.healthy.Store(false)
			if rw.checkpoint != nil {
				rw.checkpoint.SetKafkaHealth(false)
			}
		}
	}

	// Use fallback
	if rw.checkpoint != nil {
		rw.checkpoint.RecordBuffered(1)
	}
	return rw.fallback.Write(p)
}

// Sync ensures all pending messages are flushed
func (rw *ResilientKafkaWriter) Sync() error {
	return nil
}

// Close shuts down the writer gracefully
func (rw *ResilientKafkaWriter) Close() error {
	rw.mu.Lock()
	if rw.closed {
		rw.mu.Unlock()
		return nil
	}
	rw.closed = true
	rw.mu.Unlock()

	// Stop health check
	rw.healthCheck.Stop()
	rw.replayCancel()

	// Wait for goroutines
	rw.wg.Wait()

	// Close resources
	if rw.checkpoint != nil {
		rw.checkpoint.Close()
	}
	rw.fallback.Close()
	return rw.producer.Close()
}

func (rw *ResilientKafkaWriter) handleErrors() {
	defer rw.wg.Done()

	errorCount := 0
	for err := range rw.producer.Errors() {
		errorCount++
		os.Stderr.WriteString("Kafka log error: " + err.Error() + "\n")
		if rw.checkpoint != nil {
			rw.checkpoint.RecordError(err.Error())
		}

		// After multiple errors, mark unhealthy
		if errorCount >= 3 {
			rw.healthy.Store(false)
			if rw.checkpoint != nil {
				rw.checkpoint.SetKafkaHealth(false)
			}
			errorCount = 0
		}
	}
}

func (rw *ResilientKafkaWriter) runHealthCheck() {
	defer rw.wg.Done()

	for {
		select {
		case <-rw.replayCtx.Done():
			return
		case <-rw.healthCheck.C:
			rw.checkHealthAndReplay()
		}
	}
}

func (rw *ResilientKafkaWriter) checkHealthAndReplay() {
	// Skip if already replaying
	if rw.replaying.Load() {
		return
	}

	// Try a health check by creating a new sync producer temporarily
	cfg := sarama.NewConfig()
	cfg.Producer.Return.Successes = true
	cfg.Producer.Timeout = 5 * time.Second
	cfg.Net.DialTimeout = 5 * time.Second

	testProducer, err := sarama.NewSyncProducer(rw.config.KafkaBrokers, cfg)
	if err != nil {
		// Still unhealthy
		os.Stderr.WriteString("Kafka health check failed: " + err.Error() + "\n")
		rw.healthy.Store(false)
		return
	}
	testProducer.Close()

	// Kafka is healthy
	wasHealthy := rw.healthy.Swap(true)
	if rw.checkpoint != nil {
		rw.checkpoint.SetKafkaHealth(true)
	}

	// If just became healthy and have pending data, replay
	if !wasHealthy && rw.fallback.HasPendingData() {
		rw.startReplay()
	}
}

func (rw *ResilientKafkaWriter) startReplay() {
	if !rw.replaying.CompareAndSwap(false, true) {
		return // Already replaying
	}

	rw.wg.Add(1)
	go func() {
		defer rw.wg.Done()
		defer rw.replaying.Store(false)

		os.Stderr.WriteString("Starting Kafka log replay from fallback buffer...\n")

		replayedCount := int64(0)
		err := rw.fallback.ReplayTo(rw.replayCtx, func(data []byte) error {
			msg := &sarama.ProducerMessage{
				Topic:     rw.topic,
				Value:     sarama.ByteEncoder(data),
				Timestamp: time.Now(),
			}

			select {
			case rw.producer.Input() <- msg:
				replayedCount++
				return nil
			case <-time.After(5 * time.Second):
				// Kafka became unhealthy again
				rw.healthy.Store(false)
				return context.DeadlineExceeded
			}
		})

		// Record replayed count
		if rw.checkpoint != nil && replayedCount > 0 {
			rw.checkpoint.RecordReplayed(replayedCount)
		}

		if err != nil {
			os.Stderr.WriteString("Kafka replay failed: " + err.Error() + "\n")
		} else {
			os.Stderr.WriteString("Kafka log replay completed\n")
		}
	}()
}

// IsHealthy returns the current Kafka health status
func (rw *ResilientKafkaWriter) IsHealthy() bool {
	return rw.healthy.Load()
}

// GetCheckpointStatus returns the current log checkpoint status
func (rw *ResilientKafkaWriter) GetCheckpointStatus() *LogCheckpoint {
	if rw.checkpoint == nil {
		return nil
	}
	status := rw.checkpoint.GetStatus()
	return &status
}

// GetPendingCount returns the number of logs pending to be sent
func (rw *ResilientKafkaWriter) GetPendingCount() int64 {
	if rw.checkpoint == nil {
		return 0
	}
	return rw.checkpoint.GetPendingCount()
}
