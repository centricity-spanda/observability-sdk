// Example Go HTTP service with observability.
// Single init via observability.Initialize(); use observability.GetLogger() and observability.GetTracer().
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
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
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy"}`))
	})

	mux.HandleFunc("/api/payment", func(w http.ResponseWriter, r *http.Request) {
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
