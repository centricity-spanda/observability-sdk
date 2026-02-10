package tracing

import (
	"context"
	"os"
	"strconv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
)

// TracerProvider wraps the OpenTelemetry TracerProvider
type TracerProvider struct {
	*trace.TracerProvider
}

// NewTracer creates a new OpenTelemetry tracer with Kafka exporter
func NewTracer(serviceName string) (*TracerProvider, error) {
	cfg := NewExporterConfig(serviceName)

	// Create resource with service info
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(getEnv("SERVICE_VERSION", "unknown")),
			semconv.DeploymentEnvironment(getEnv("ENVIRONMENT", "production")),
		),
	)
	if err != nil {
		return nil, err
	}

	// Parse sampling rate
	samplingRate := 0.1
	if rate := getEnv("TRACE_SAMPLING_RATE", ""); rate != "" {
		if parsed, err := strconv.ParseFloat(rate, 64); err == nil {
			samplingRate = parsed
		}
	}

	// Create TracerProvider options
	opts := []trace.TracerProviderOption{
		trace.WithResource(res),
		trace.WithSampler(trace.ParentBased(trace.TraceIDRatioBased(samplingRate))),
	}

	// In development mode, use stdout exporter
	if getEnv("ENVIRONMENT", "production") == "development" {
		// For development, we just create a basic provider without Kafka
		tp := trace.NewTracerProvider(opts...)
		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))
		return &TracerProvider{tp}, nil
	}

	// Create Kafka exporter if enabled
	if !cfg.EnableKafka {
		os.Stderr.WriteString("Trace Kafka export disabled\n")
	} else {
		exporter, err := NewKafkaExporter(cfg)
		if err != nil {
			return nil, err
		}

		if exporter != nil {
			opts = append(opts, trace.WithBatcher(exporter))
		} else {
			os.Stderr.WriteString("Warning: KAFKA_BROKERS not set, trace export disabled\n")
		}
	}

	// Create TracerProvider
	tp := trace.NewTracerProvider(opts...)

	// Set global TracerProvider and propagator
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return &TracerProvider{tp}, nil
}

// Shutdown gracefully shuts down the tracer provider
func (tp *TracerProvider) Shutdown(ctx context.Context) error {
	return tp.TracerProvider.Shutdown(ctx)
}
