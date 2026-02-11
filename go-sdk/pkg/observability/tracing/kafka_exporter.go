package tracing

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
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

// ExportSpans exports spans to Kafka in OTLP protobuf format
func (e *KafkaExporter) ExportSpans(ctx context.Context, spans []trace.ReadOnlySpan) error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.mu.Unlock()

	// Convert to OTLP format
	otlpSpans := e.convertToOTLP(spans)
	
	// Marshal to protobuf
	data, err := proto.Marshal(otlpSpans)
	if err != nil {
		os.Stderr.WriteString("Failed to marshal OTLP traces: " + err.Error() + "\n")
		return err
	}

	// Use first span's trace ID as key
	var key string
	if len(spans) > 0 {
		key = spans[0].SpanContext().TraceID().String()
	}

	msg := &sarama.ProducerMessage{
		Topic:     e.topic,
		Key:       sarama.StringEncoder(key),
		Value:     sarama.ByteEncoder(data),
		Timestamp: time.Now(),
	}

	select {
	case e.producer.Input() <- msg:
	default:
		// Channel full, drop span
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

// convertToOTLP converts OpenTelemetry spans to OTLP format
func (e *KafkaExporter) convertToOTLP(spans []trace.ReadOnlySpan) *v1.ExportTraceServiceRequest {
	var otlpSpans []*tracepb.Span

	for _, span := range spans {
		sc := span.SpanContext()
		traceID := sc.TraceID()
		spanID := sc.SpanID()
		
		var parentSpanID []byte
		if span.Parent().IsValid() {
			pid := span.Parent().SpanID()
			parentSpanID = pid[:]
		}
		
		// Convert attributes
		var attributes []*commonpb.KeyValue
		for _, attr := range span.Attributes() {
			attributes = append(attributes, &commonpb.KeyValue{
				Key:   string(attr.Key),
				Value: attributeValueToOTLP(attr.Value),
			})
		}

		// Convert events
		var events []*tracepb.Span_Event
		for _, event := range span.Events() {
			var eventAttrs []*commonpb.KeyValue
			for _, attr := range event.Attributes {
				eventAttrs = append(eventAttrs, &commonpb.KeyValue{
					Key:   string(attr.Key),
					Value: attributeValueToOTLP(attr.Value),
				})
			}
			events = append(events, &tracepb.Span_Event{
				TimeUnixNano: uint64(event.Time.UnixNano()),
				Name:         event.Name,
				Attributes:   eventAttrs,
			})
		}

		// Convert status
		status := &tracepb.Status{
			Code: tracepb.Status_StatusCode(span.Status().Code),
			Message: span.Status().Description,
		}

		otlpSpan := &tracepb.Span{
			TraceId:           traceID[:],
			SpanId:            spanID[:],
			ParentSpanId:      parentSpanID,
			Name:              span.Name(),
			Kind:              tracepb.Span_SpanKind(span.SpanKind()),
			StartTimeUnixNano: uint64(span.StartTime().UnixNano()),
			EndTimeUnixNano:   uint64(span.EndTime().UnixNano()),
			Attributes:        attributes,
			Events:            events,
			Status:            status,
		}

		otlpSpans = append(otlpSpans, otlpSpan)
	}

	return &v1.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{
						{
							Key: "service.name",
							Value: &commonpb.AnyValue{
								Value: &commonpb.AnyValue_StringValue{
									StringValue: e.serviceName,
								},
							},
						},
					},
				},
				ScopeSpans: []*tracepb.ScopeSpans{
					{
						Spans: otlpSpans,
					},
				},
			},
		},
	}
}

// attributeValueToOTLP converts an attribute value to OTLP format
func attributeValueToOTLP(value attribute.Value) *commonpb.AnyValue {
	switch value.Type() {
	case attribute.BOOL:
		return &commonpb.AnyValue{
			Value: &commonpb.AnyValue_BoolValue{BoolValue: value.AsBool()},
		}
	case attribute.INT64:
		return &commonpb.AnyValue{
			Value: &commonpb.AnyValue_IntValue{IntValue: value.AsInt64()},
		}
	case attribute.FLOAT64:
		return &commonpb.AnyValue{
			Value: &commonpb.AnyValue_DoubleValue{DoubleValue: value.AsFloat64()},
		}
	case attribute.STRING:
		return &commonpb.AnyValue{
			Value: &commonpb.AnyValue_StringValue{StringValue: value.AsString()},
		}
	default:
		return &commonpb.AnyValue{
			Value: &commonpb.AnyValue_StringValue{StringValue: value.AsString()},
		}
	}
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
