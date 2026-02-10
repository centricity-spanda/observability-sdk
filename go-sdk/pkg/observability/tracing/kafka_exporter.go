package tracing

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// KafkaExporter exports OpenTelemetry spans to Kafka
type KafkaExporter struct {
	producer    sarama.AsyncProducer
	topic       string
	serviceName string
	mu          sync.Mutex
	closed      bool
}

// ExporterConfig holds exporter configuration
type ExporterConfig struct {
	ServiceName  string
	KafkaBrokers []string
	Topic        string
	EnableKafka  bool
}

// NewExporterConfig creates config from environment
func NewExporterConfig(serviceName string) *ExporterConfig {
	cfg := &ExporterConfig{
		ServiceName: serviceName,
		Topic:       getEnv("KAFKA_TRACES_TOPIC", "traces.application"),
		EnableKafka: getEnvBool("TRACES_KAFKA_ENABLED", true),
	}

	brokers := getEnv("KAFKA_BROKERS", "")
	if brokers != "" {
		cfg.KafkaBrokers = strings.Split(brokers, ",")
	}

	return cfg
}

// NewKafkaExporter creates a new Kafka span exporter
func NewKafkaExporter(cfg *ExporterConfig) (*KafkaExporter, error) {
	if len(cfg.KafkaBrokers) == 0 {
		return nil, nil
	}

	saramaConfig := sarama.NewConfig()
	saramaConfig.Producer.RequiredAcks = sarama.WaitForLocal
	saramaConfig.Producer.Compression = sarama.CompressionSnappy
	saramaConfig.Producer.Flush.Frequency = 100 * time.Millisecond
	saramaConfig.Producer.Flush.Messages = 100
	saramaConfig.Producer.Return.Errors = true

	producer, err := sarama.NewAsyncProducer(cfg.KafkaBrokers, saramaConfig)
	if err != nil {
		return nil, err
	}

	exp := &KafkaExporter{
		producer:    producer,
		topic:       cfg.Topic,
		serviceName: cfg.ServiceName,
	}

	go exp.handleErrors()

	return exp, nil
}

// ExportSpans exports spans to Kafka
func (e *KafkaExporter) ExportSpans(ctx context.Context, spans []trace.ReadOnlySpan) error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.mu.Unlock()

	for _, span := range spans {
		data, err := e.spanToJSON(span)
		if err != nil {
			continue
		}

		msg := &sarama.ProducerMessage{
			Topic:     e.topic,
			Key:       sarama.StringEncoder(span.SpanContext().TraceID().String()),
			Value:     sarama.ByteEncoder(data),
			Timestamp: time.Now(),
		}

		select {
		case e.producer.Input() <- msg:
		default:
			// Channel full, drop span
		}
	}

	return nil
}

// Shutdown gracefully shuts down the exporter
func (e *KafkaExporter) Shutdown(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil
	}
	e.closed = true

	return e.producer.Close()
}

// spanToJSON converts a span to JSON format
func (e *KafkaExporter) spanToJSON(span trace.ReadOnlySpan) ([]byte, error) {
	sc := span.SpanContext()

	// Convert attributes to map
	attrs := make(map[string]interface{})
	for _, attr := range span.Attributes() {
		attrs[string(attr.Key)] = attr.Value.AsInterface()
	}

	// Convert events
	events := make([]map[string]interface{}, 0, len(span.Events()))
	for _, event := range span.Events() {
		eventAttrs := make(map[string]interface{})
		for _, attr := range event.Attributes {
			eventAttrs[string(attr.Key)] = attr.Value.AsInterface()
		}
		events = append(events, map[string]interface{}{
			"name":       event.Name,
			"timestamp":  event.Time.UnixNano(),
			"attributes": eventAttrs,
		})
	}

	spanData := map[string]interface{}{
		"trace_id":    sc.TraceID().String(),
		"span_id":     sc.SpanID().String(),
		"parent_id":   span.Parent().SpanID().String(),
		"name":        span.Name(),
		"kind":        span.SpanKind().String(),
		"start_time":  span.StartTime().UnixNano(),
		"end_time":    span.EndTime().UnixNano(),
		"duration_ms": span.EndTime().Sub(span.StartTime()).Milliseconds(),
		"status":      span.Status().Code.String(),
		"service":     e.serviceName,
		"attributes":  attrs,
		"events":      events,
	}

	return json.Marshal(spanData)
}

func (e *KafkaExporter) handleErrors() {
	for err := range e.producer.Errors() {
		os.Stderr.WriteString("Kafka trace exporter error: " + err.Error() + "\n")
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

// GetTraceID extracts trace ID from context
func GetTraceID(ctx context.Context) string {
	spanCtx := oteltrace.SpanContextFromContext(ctx)
	if spanCtx.HasTraceID() {
		return spanCtx.TraceID().String()
	}
	return ""
}
