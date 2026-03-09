// Example Go HTTP service with observability.
// Single init via observability.Initialize(); use observability.GetLogger() and observability.GetTracer().
package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"go.uber.org/zap"

	"example-service/observability"

	obs "github.com/centricitywealthtech/platform-observability-sdk/go-sdk/pkg/observability"
	"github.com/joho/godotenv"
)

const serviceName = "example-service"

func main() {
	_ = godotenv.Load()

	if err := observability.Initialize(serviceName); err != nil {
		panic(err)
	}
	defer observability.Shutdown(context.Background())

	logger := observability.GetLogger()
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy"}`))
	})

	mux.HandleFunc("/api/payment", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		ctx := r.Context()
		logger.Info("processing payment",
			zap.String("trace_id", observability.GetTraceIDFromContext(ctx)),
			zap.String("payment_id", "PAY-12345"),
			zap.Float64("amount", 1500.50),
		)
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"completed","payment_id":"PAY-12345"}`))
	})

	mux.HandleFunc("/api/users", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		ctx := r.Context()
		logger.Info("user profile accessed",
			zap.String("trace_id", observability.GetTraceIDFromContext(ctx)),
			zap.String("password", "secret123"),
			zap.String("secret", "my-secret-key"),
			zap.String("token", "bearer-token-xyz"),
			zap.String("api_key", "sk_test_123456"),
			zap.String("authorization", "Bearer xyz"),
			zap.Any("user_details", map[string]interface{}{
				"email_address": "john.doe@example.com",
				"phone_number":  "+919876543210",
				"pan_card":      "ABCDE1234F",
				"aadhaar_no":    "8561 0272 7756",
				"credit_card":   "4111-1111-1111-1111",
				"bank_account":  "123456789012",
				"ifsc_code":     "SBIN0123456",
				"passport_no":   "A1234567",
				"ssn":           "123-45-6789",
			}),
			zap.Any("safe_data", map[string]interface{}{
				"url":         "https://example.com/user/12345",
				"file_url":   "https://centricity-oms-vault.s3.ap-south-1.amazonaws.com/testgenerated/pdf/1768383784596-2da72e2a-6218-4049-bfe6-c41142e2e088_20260114_094303.pdf",
				"file_path":   "/var/log/app/12345.log",
				"windows_path": `C:\Users\John\12345.txt`,
				"request_uuid": "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
				"timestamp":   "20230101120000",
				"version":    "1.2.3",
				"order_id":   "ORD-123456789",
			}),
			zap.Any("structural_data", map[string]interface{}{
				"zipcode": "12345",
				"count":  "100",
				"page":   "1",
				"total":  "500",
			}),
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"user_id":"USR-001","name":"John Doe"}`))
	})

	// Large payload endpoint - mirrors Python /api/large-payload
	mux.HandleFunc("/api/large-payload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			logger.Error("failed to read request body", zap.Error(err))
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		size := len(body)
		ctx := r.Context()
		logger.Info("large_payload received",
			zap.Int("size_bytes", size),
			zap.String("trace_id", observability.GetTraceIDFromContext(ctx)),
		)

		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"received_bytes": size,
			"message":        "payload accepted",
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	// Error endpoint - mirrors Python /api/error
	mux.HandleFunc("/api/error", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		countStr := r.URL.Query().Get("count")
		count := 1
		if countStr != "" {
			if v, err := strconv.Atoi(countStr); err == nil && v > 0 {
				count = v
			}
		}
		ctx := r.Context()
		logger.Warn("trigger_error invoked",
			zap.Int("count", count),
			zap.String("trace_id", observability.GetTraceIDFromContext(ctx)),
		)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		resp := map[string]interface{}{
			"error": "demo error",
			"count": count,
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	// Stress endpoint - mirrors Python /api/stress
	mux.HandleFunc("/api/stress", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// Query params: duration_ms (default 2000), memory_mb (default 50)
		durationMS := 2000
		memoryMB := 50

		if v := r.URL.Query().Get("duration_ms"); v != "" {
			if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
				durationMS = parsed
			}
		}
		if v := r.URL.Query().Get("memory_mb"); v != "" {
			if parsed, err := strconv.Atoi(v); err == nil && parsed >= 0 {
				memoryMB = parsed
			}
		}

		ctx := r.Context()
		logger.Info("stress started",
			zap.Int("duration_ms", durationMS),
			zap.Int("memory_mb", memoryMB),
			zap.String("trace_id", observability.GetTraceIDFromContext(ctx)),
		)

		start := time.Now()

		// CPU burn: busy-wait for the specified duration
		deadline := start.Add(time.Duration(durationMS) * time.Millisecond)
		for time.Now().Before(deadline) {
		}

		// Optional memory allocation (hold briefly)
		var chunk []byte
		if memoryMB > 0 {
			chunk = make([]byte, memoryMB*1024*1024)
			_ = chunk
		}

		elapsed := time.Since(start).Seconds()
		logger.Info("stress completed",
			zap.Float64("elapsed_seconds", elapsed),
			zap.String("trace_id", observability.GetTraceIDFromContext(ctx)),
		)

		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"duration_ms":     durationMS,
			"memory_mb":       memoryMB,
			"elapsed_seconds": elapsed,
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	handler := obs.HTTPTracingMiddleware(observability.ServiceName())(
		obs.HTTPMetricsMiddleware(observability.ServiceName())(mux),
	)

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

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down server")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	server.Shutdown(ctx)
}
