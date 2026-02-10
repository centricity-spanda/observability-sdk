package metrics

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
)

// Pusher pushes metrics to Kafka periodically
type Pusher struct {
	producer    sarama.SyncProducer
	registry    *prometheus.Registry
	serviceName string
	topic       string
	interval    time.Duration
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

// PusherConfig holds pusher configuration
type PusherConfig struct {
	ServiceName  string
	KafkaBrokers []string
	Topic        string
	Interval     time.Duration
	EnableKafka  bool
}

// NewPusherConfig creates config from environment
func NewPusherConfig(serviceName string) *PusherConfig {
	interval, _ := time.ParseDuration(getEnv("METRICS_PUSH_INTERVAL", "15s"))

	cfg := &PusherConfig{
		ServiceName: serviceName,
		Topic:       getEnv("KAFKA_METRICS_TOPIC", "metrics.application"),
		Interval:    interval,
		EnableKafka: getEnvBool("METRICS_KAFKA_ENABLED", true),
	}

	brokers := getEnv("KAFKA_BROKERS", "")
	if brokers != "" {
		cfg.KafkaBrokers = strings.Split(brokers, ",")
	}

	return cfg
}

// NewPusher creates a new metrics pusher
func NewPusher(cfg *PusherConfig) (*Pusher, error) {
	if len(cfg.KafkaBrokers) == 0 {
		return nil, nil // No Kafka, metrics exposed via /metrics only
	}

	saramaConfig := sarama.NewConfig()
	saramaConfig.Producer.RequiredAcks = sarama.WaitForLocal
	saramaConfig.Producer.Compression = sarama.CompressionSnappy
	saramaConfig.Producer.Return.Successes = true

	producer, err := sarama.NewSyncProducer(cfg.KafkaBrokers, saramaConfig)
	if err != nil {
		return nil, err
	}

	return &Pusher{
		producer:    producer,
		registry:    Registry,
		serviceName: cfg.ServiceName,
		topic:       cfg.Topic,
		interval:    cfg.Interval,
	}, nil
}

// Start begins the background push loop
func (p *Pusher) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	p.wg.Add(1)
	go p.pushLoop(ctx)
}

// Stop gracefully stops the pusher
func (p *Pusher) Stop() error {
	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
	return p.producer.Close()
}

func (p *Pusher) pushLoop(ctx context.Context) {
	defer p.wg.Done()

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Final push before exit
			p.push()
			return
		case <-ticker.C:
			p.push()
		}
	}
}

func (p *Pusher) push() {
	// Gather metrics
	mfs, err := p.registry.Gather()
	if err != nil {
		os.Stderr.WriteString("Failed to gather metrics: " + err.Error() + "\n")
		return
	}

	// Encode to Prometheus text format
	var buf bytes.Buffer
	for _, mf := range mfs {
		if _, err := expfmt.MetricFamilyToText(&buf, mf); err != nil {
			os.Stderr.WriteString("Failed to encode metrics: " + err.Error() + "\n")
			continue
		}
	}

	// Send to Kafka
	msg := &sarama.ProducerMessage{
		Topic:     p.topic,
		Key:       sarama.StringEncoder(p.serviceName),
		Value:     sarama.ByteEncoder(buf.Bytes()),
		Timestamp: time.Now(),
	}

	if _, _, err := p.producer.SendMessage(msg); err != nil {
		os.Stderr.WriteString("Failed to push metrics to Kafka: " + err.Error() + "\n")
		KafkaProducerMessagesTotal.WithLabelValues(p.serviceName, p.topic, "error").Inc()
	} else {
		KafkaProducerMessagesTotal.WithLabelValues(p.serviceName, p.topic, "success").Inc()
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value == "true" || value == "1" || value == "yes"
}
