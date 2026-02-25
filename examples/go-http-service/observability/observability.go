// Package observability provides a single init and getters for logger, tracer, and metrics.
// Use Initialize once at startup, then GetLogger() and GetTracer() everywhere.
package observability

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	obs "github.com/centricitywealthtech/platform-observability-sdk/go-sdk/pkg/observability"
	"github.com/centricitywealthtech/platform-observability-sdk/go-sdk/pkg/observability/tracing"
)

var (
	initOnce       sync.Once
	initErr        error
	logger         *zap.Logger
	tracer         trace.Tracer
	tracerProvider *tracing.TracerProvider
	serviceName    string
)

// Initialize sets up logger, tracer, and metrics pusher once per process.
// Call this at application startup before using GetLogger or GetTracer.
func Initialize(svcName string) error {
	initOnce.Do(func() {
		serviceName = svcName
		logger, initErr = obs.NewLogger(svcName)
		if initErr != nil {
			return
		}
		if err := obs.StartMetricsPusher(svcName); err != nil {
			logger.Warn("failed to start metrics pusher", zap.Error(err))
		}
		tracer, tracerProvider, initErr = obs.NewTracer(svcName)
		if initErr != nil {
			logger.Warn("failed to initialize tracer", zap.Error(initErr))
			initErr = nil // non-fatal; continue without tracer
		}
	})
	return initErr
}

// GetLogger returns the singleton logger. Panics if Initialize has not been called successfully.
func GetLogger() *zap.Logger {
	if logger == nil {
		panic("observability not initialized: call observability.Initialize(...) first")
	}
	return logger
}

// GetTracer returns the singleton tracer. Panics if Initialize has not been called successfully.
func GetTracer() trace.Tracer {
	if tracer == nil || tracerProvider == nil {
		panic("observability not initialized: call observability.Initialize(...) first")
	}
	return tracer
}

// ServiceName returns the service name passed to Initialize.
func ServiceName() string {
	return serviceName
}

// Shutdown flushes the logger and shuts down the tracer provider. Call on process exit.
func Shutdown(ctx context.Context) {
	if logger != nil {
		_ = logger.Sync()
	}
	if tracerProvider != nil {
		_ = tracerProvider.Shutdown(ctx)
	}
}

// GetTraceIDFromContext returns the trace ID from the context for log correlation.
func GetTraceIDFromContext(ctx context.Context) string {
	return tracing.GetTraceIDFromContext(ctx)
}
