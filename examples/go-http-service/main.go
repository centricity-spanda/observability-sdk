// Example Go HTTP service with observability
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	obs "github.com/centricitywealthtech/platform-observability-sdk/go-sdk/pkg/observability"
	"github.com/centricitywealthtech/platform-observability-sdk/go-sdk/pkg/observability/tracing"
	"github.com/joho/godotenv"
)

func main() {
	// Load environment variables from .env file
	_ = godotenv.Load()

	// Initialize logger
	logger, err := obs.NewLogger("example-service")
	if err != nil {
		panic(err)
	}
	defer logger.Sync()

	// Start metrics pusher
	if err := obs.StartMetricsPusher("example-service"); err != nil {
		logger.Warn("failed to start metrics pusher", zap.Error(err))
	}

	// Initialize tracer
	tp, err := obs.NewTracer("example-service")
	if err != nil {
		logger.Warn("failed to initialize tracer", zap.Error(err))
	}
	if tp != nil {
		defer tp.Shutdown(context.Background())
	}

	// Create router
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy"}`))
	})

	mux.HandleFunc("/api/payment", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Log with trace correlation
		logger.Info("processing payment",
			zap.String("trace_id", tracing.GetTraceIDFromContext(ctx)),
			zap.String("payment_id", "PAY-12345"),
			zap.Float64("amount", 1500.50),
		)

		// Simulate work
		time.Sleep(100 * time.Millisecond)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"completed","payment_id":"PAY-12345"}`))
	})

	// Endpoint with PII data for testing redaction
	mux.HandleFunc("/api/users", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Log with PII data - should be redacted
		logger.Info("user profile accessed",
			zap.String("trace_id", tracing.GetTraceIDFromContext(ctx)),
			zap.String("user_id", "USR-001"),
			zap.String("email", "john.doe@example.com"),      // Should be redacted
			zap.String("pan", "GNYPP4789A"),                  // Should be redacted
			zap.String("aadhaar", "856102727756"),          // Should be redacted
			zap.String("phone", "+919876543210"),            // Should be redacted
			zap.String("card_number", "4111111111111111"), // Should be redacted
		)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"user_id":"USR-001","name":"John Doe"}`))
	})

	// Apply middleware (tracing first, then metrics)
	handler := obs.HTTPTracingMiddleware("example-service")(
		obs.HTTPMetricsMiddleware("example-service")(mux),
	)

	// Start server on port 8088 (8080 used by Kafka UI)
	server := &http.Server{
		Addr:    ":8088",
		Handler: handler,
	}

	go func() {
		logger.Info("starting server", zap.String("addr", ":8088"))
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			logger.Fatal("server error", zap.Error(err))
		}
	}()

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down server")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	server.Shutdown(ctx)
}
