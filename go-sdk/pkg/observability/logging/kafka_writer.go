package logging

import (
	"os"
	"sync"
	"time"

	"github.com/IBM/sarama"
)

// KafkaWriter implements io.Writer interface for Zap to write logs to Kafka
type KafkaWriter struct {
	producer sarama.AsyncProducer
	topic    string
	config   *Config
	mu       sync.Mutex
	closed   bool
}

// NewKafkaWriter creates a new Kafka writer for logging
func NewKafkaWriter(cfg *Config) (*KafkaWriter, error) {
	if len(cfg.KafkaBrokers) == 0 {
		return nil, nil // No Kafka configured, will use stdout only
	}

	saramaConfig := sarama.NewConfig()
	saramaConfig.Producer.Return.Successes = false
	saramaConfig.Producer.Return.Errors = true
	saramaConfig.Producer.Compression = sarama.CompressionSnappy
	saramaConfig.Producer.Flush.Frequency = cfg.FlushInterval
	saramaConfig.Producer.Flush.Messages = 1000
	saramaConfig.Producer.Flush.Bytes = 1048576 // 1MB
	saramaConfig.ChannelBufferSize = cfg.ChannelBufferSize

	// Audit logs need stricter durability
	if cfg.IsAuditLog() {
		saramaConfig.Producer.RequiredAcks = sarama.WaitForAll
		saramaConfig.Producer.Idempotent = true
		saramaConfig.Producer.Retry.Max = 10
		saramaConfig.Net.MaxOpenRequests = 1 // Required for idempotent
	} else {
		saramaConfig.Producer.RequiredAcks = sarama.WaitForLocal
	}

	producer, err := sarama.NewAsyncProducer(cfg.KafkaBrokers, saramaConfig)
	if err != nil {
		return nil, err
	}

	kw := &KafkaWriter{
		producer: producer,
		topic:    cfg.LogTopic,
		config:   cfg,
	}

	// Handle errors in background
	go kw.handleErrors()

	return kw, nil
}

// Write implements io.Writer - sends log data to Kafka
func (kw *KafkaWriter) Write(p []byte) (n int, err error) {
	kw.mu.Lock()
	if kw.closed {
		kw.mu.Unlock()
		return os.Stderr.Write(p) // Fallback to stderr if closed
	}
	kw.mu.Unlock()

	msg := &sarama.ProducerMessage{
		Topic:     kw.topic,
		Value:     sarama.ByteEncoder(p),
		Timestamp: time.Now(),
	}

	// Non-blocking send with fallback
	select {
	case kw.producer.Input() <- msg:
		return len(p), nil
	default:
		// Channel full - fallback to stderr
		return os.Stderr.Write(p)
	}
}

// Sync flushes any buffered logs (required by Zap)
func (kw *KafkaWriter) Sync() error {
	// AsyncProducer doesn't have explicit flush, it flushes based on config
	return nil
}

// Close shuts down the Kafka producer
func (kw *KafkaWriter) Close() error {
	kw.mu.Lock()
	defer kw.mu.Unlock()

	if kw.closed {
		return nil
	}
	kw.closed = true

	return kw.producer.Close()
}

func (kw *KafkaWriter) handleErrors() {
	for err := range kw.producer.Errors() {
		// Log to stderr as fallback
		os.Stderr.WriteString("Kafka producer error: " + err.Error() + "\n")
	}
}
