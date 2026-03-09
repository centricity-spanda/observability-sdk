package tracing

import (
	"context"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	apitrace "go.opentelemetry.io/otel/trace"
)

// TracerProvider wraps the OpenTelemetry TracerProvider
type TracerProvider struct {
	*trace.TracerProvider
}

// NewTracer creates a new OpenTelemetry tracer with OTLP gRPC exporter (to the OTEL agent).
func NewTracer(serviceName string) (apitrace.Tracer, *TracerProvider, error) {
	// Build resource without merging resource.Default() to avoid schema URL
	// conflicts (SDK ships v1.40.0 schema, semconv/v1.21.0 has a different URL).
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(serviceName),
		semconv.ServiceVersion(getEnv("SERVICE_VERSION", "unknown")),
		semconv.DeploymentEnvironment(getEnv("ENVIRONMENT", "production")),
	)

	samplingRate := 1.0
	if rate := getEnv("TRACE_SAMPLING_RATE", ""); rate != "" {
		if parsed, err := strconv.ParseFloat(rate, 64); err == nil {
			samplingRate = parsed
		}
	}

	opts := []trace.TracerProviderOption{
		trace.WithResource(res),
		trace.WithSampler(trace.ParentBased(trace.TraceIDRatioBased(samplingRate))),
	}

	// Set up the OTLP gRPC exporter using the library's own options.
	// WithInsecure() is the otlptracegrpc-native flag for plaintext connections.
	endpoint := getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	if endpoint != "" {
		target := endpoint
		if idx := strings.Index(endpoint, "://"); idx >= 0 {
			target = endpoint[idx+3:]
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		exporter, expErr := otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(target),
			otlptracegrpc.WithInsecure(),
		)
		if expErr == nil {
			opts = append(opts, trace.WithBatcher(exporter))
		}
	}

	tp := trace.NewTracerProvider(opts...)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	tr := tp.Tracer(serviceName)
	return tr, &TracerProvider{tp}, nil
}

// Shutdown gracefully shuts down the tracer provider
func (tp *TracerProvider) Shutdown(ctx context.Context) error {
	return tp.TracerProvider.Shutdown(ctx)
}
