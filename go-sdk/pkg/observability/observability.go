// Package observability provides unified telemetry collection for the Centricity platform.
// It includes logging (Zap + Kafka), metrics (Prometheus + Kafka), and tracing (OpenTelemetry + Kafka).
package observability

import (
	"github.com/centricitywealthtech/platform-observability-sdk/go-sdk/pkg/observability/logging"
	"github.com/centricitywealthtech/platform-observability-sdk/go-sdk/pkg/observability/metrics"
	"github.com/centricitywealthtech/platform-observability-sdk/go-sdk/pkg/observability/tracing"
	"go.opentelemetry.io/otel/codes"
)

// Re-export main functions for convenient access
var (
	// NewLogger creates a new structured logger with Kafka integration
	NewLogger = logging.NewLogger

	// StartMetricsPusher starts the background metrics pusher to Kafka
	StartMetricsPusher = metrics.StartMetricsPusher

	// NewTracer creates a new OpenTelemetry tracer with Kafka exporter
	NewTracer = tracing.NewTracer

	// Registry is the Prometheus registry for custom metrics
	Registry = metrics.Registry

	// HTTPMetricsMiddleware wraps HTTP handlers with metrics collection
	HTTPMetricsMiddleware = metrics.HTTPMetricsMiddleware

	// HTTPTracingMiddleware wraps HTTP handlers with distributed tracing
	HTTPTracingMiddleware = tracing.HTTPTracingMiddleware
)

// Re-export OpenTelemetry codes for span status
const (
	// StatusCodeUnset is the default status code
	StatusCodeUnset = codes.Unset
	// StatusCodeOK indicates the operation completed successfully
	StatusCodeOK = codes.Ok
	// StatusCodeError indicates the operation encountered an error
	StatusCodeError = codes.Error
)

