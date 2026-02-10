package metrics

import (
	"net/http"
	"strconv"
	"time"
)

// HTTPMetricsMiddleware creates middleware that records HTTP metrics
func HTTPMetricsMiddleware(serviceName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Track in-flight requests
			HTTPRequestsInFlight.WithLabelValues(serviceName).Inc()
			defer HTTPRequestsInFlight.WithLabelValues(serviceName).Dec()

			// Start timer
			start := time.Now()

			// Wrap response writer to capture status code
			rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			// Call next handler
			next.ServeHTTP(rw, r)

			// Record metrics
			duration := time.Since(start).Seconds()
			path := normalizePath(r.URL.Path)
			status := strconv.Itoa(rw.statusCode)

			HTTPRequestsTotal.WithLabelValues(serviceName, r.Method, path, status).Inc()
			HTTPRequestDuration.WithLabelValues(serviceName, r.Method, path).Observe(duration)
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// normalizePath reduces path cardinality by removing IDs
func normalizePath(path string) string {
	// Simple normalization - in production, use a router-aware approach
	if len(path) > 50 {
		return path[:50] + "..."
	}
	return path
}
