# Observability SDK — Go

Drop-in observability for Go microservices. Structured logging, Prometheus metrics, and OpenTelemetry tracing — all with automatic Kafka export and PII redaction.

## Installation

```bash
go get github.com/centricitywealthtech/platform-observability-sdk/go-sdk
```

## Quick Start

```go
package main

import (
    "context"
    "net/http"

    "go.uber.org/zap"
    obs "github.com/centricitywealthtech/platform-observability-sdk/go-sdk/pkg/observability"
)

func main() {
    // 1. Initialize logger
    logger, _ := obs.NewLogger("my-service")
    defer logger.Sync()

    // 2. Start metrics pusher (pushes to Kafka every 15s)
    obs.StartMetricsPusher("my-service")

    // 3. Initialize tracer (one per process; use returned tracer for spans)
    tracer, tp, _ := obs.NewTracer("my-service")
    defer tp.Shutdown(context.Background())

    // 4. Apply middleware
    mux := http.NewServeMux()
    mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        logger.Info("health check")
        w.WriteHeader(http.StatusOK)
    })

    handler := obs.HTTPTracingMiddleware("my-service")(
        obs.HTTPMetricsMiddleware("my-service")(mux),
    )

    logger.Info("starting server", zap.Int("port", 8080))
    http.ListenAndServe(":8080", handler)
}
```

That's it. Your service now has:
- ✅ Structured JSON logs → stdout + Kafka
- ✅ HTTP request metrics → Prometheus + Kafka (OTLP)
- ✅ Distributed traces → Kafka → Jaeger
- ✅ Automatic PII redaction in log messages

## How It Works

### Logging

The SDK creates a [Zap](https://github.com/uber-go/zap) logger with JSON encoding and configurable outputs:

```go
logger, _ := obs.NewLogger("my-service")

// Standard structured logging
logger.Info("processing payment",
    zap.String("payment_id", "PAY-12345"),
    zap.Float64("amount", 1500.50),
)

// PII is automatically redacted when LOG_PII_REDACTION_ENABLED=true
logger.Info("user data",
    zap.String("email", "john@example.com"),    // → j***@***.com
    zap.String("password", "secret123"),          // → [REDACTED]
)
```

Every log entry automatically includes `service`, `environment`, and `version` fields.

**Output destinations** (all configurable via env vars):
| Destination | Env Var | Default |
|-------------|---------|---------|
| Console (stdout) | `LOG_CONSOLE_ENABLED` | `true` |
| File | `LOG_FILE_ENABLED` | `false` |
| Kafka | `LOG_KAFKA_ENABLED` | `true` |

When Kafka is enabled, the SDK uses a **resilient writer** that falls back to a local file if Kafka is temporarily unavailable, then replays buffered logs when Kafka recovers.

---

### Metrics

The SDK registers standard [Prometheus](https://prometheus.io) metrics and pushes them to Kafka in **OTLP protobuf** format:

```go
// Metrics are automatic when you use the middleware:
handler := obs.HTTPMetricsMiddleware("my-service")(yourHandler)
```

**Built-in HTTP metrics:**

| Metric | Type | Labels |
|--------|------|--------|
| `http_requests_total` | Counter | `service`, `method`, `path`, `status` |
| `http_request_duration_seconds` | Histogram | `service`, `method`, `path` |
| `http_requests_in_flight` | Gauge | `service` |

**Custom metrics** — use the Prometheus registry directly:

```go
import (
    "github.com/prometheus/client_golang/prometheus"
    obs "github.com/centricitywealthtech/platform-observability-sdk/go-sdk/pkg/observability"
)

paymentTotal := prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "payments_total",
        Help: "Total payments processed",
    },
    []string{"currency", "status"},
)
prometheus.MustRegister(paymentTotal)

// Increment
paymentTotal.WithLabelValues("USD", "success").Inc()
```

Custom metrics are automatically picked up by the Kafka pusher — no additional configuration needed.

---

### Tracing

The SDK creates an [OpenTelemetry](https://opentelemetry.io) tracer with Kafka export. Create one tracer per process and use the instance returned by `NewTracer` (or store it and provide a getter):

```go
tracer, tp, _ := obs.NewTracer("my-service")
defer tp.Shutdown(context.Background())

// Automatic spans for HTTP requests:
handler := obs.HTTPTracingMiddleware("my-service")(yourHandler)

// Manual spans inside your handlers (use the tracer returned above):
ctx, span := tracer.Start(r.Context(), "process-payment")
defer span.End()

// Get trace ID for log correlation:
import "github.com/centricitywealthtech/platform-observability-sdk/go-sdk/pkg/observability/tracing"
traceID := tracing.GetTraceIDFromContext(ctx)
logger.Info("processing", zap.String("trace_id", traceID))
```

**HTTP middleware span attributes:**
`http.method`, `http.route`, `http.scheme`, `http.host`, `http.user_agent`, `http.status_code`

Requests returning **400+** are automatically marked as error spans.

---

### PII Redaction

When `LOG_PII_REDACTION_ENABLED=true` (default), the SDK masks sensitive data in all log fields:

| Data Type | Example Input | Redacted Output |
|-----------|---------------|-----------------|
| Email | `john.doe@example.com` | `j***@***.com` |
| Phone | `+919876543210` | `+91****3210` |
| PAN Card | `ABCDE1234F` | `ABCD****4F` |
| Aadhaar | `8561 0272 7756` | `****_****_7756` |
| Credit Card | `4111-1111-1111-1111` | `****-****-****-1111` |
| SSN | `123-45-6789` | `***-**-6789` |
| Sensitive fields | `password`, `secret`, `token`, `api_key` | `[REDACTED]` |

**Safe patterns** (never redacted): URLs, file paths, UUIDs, timestamps, order IDs.

---

## Middleware Order

Apply tracing first, then metrics — this ensures trace context is available for metric labels:

```go
handler := obs.HTTPTracingMiddleware("my-service")(
    obs.HTTPMetricsMiddleware("my-service")(mux),
)
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `KAFKA_BROKERS` | *(required)* | Comma-separated Kafka brokers |
| `ENVIRONMENT` | `production` | `development` disables Kafka |
| `SERVICE_VERSION` | `unknown` | Service version tag |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `LOG_PII_REDACTION_ENABLED` | `true` | PII masking in logs |
| `TRACE_SAMPLING_RATE` | `1.0` | 0.0–1.0 |
| `METRICS_KAFKA_ENABLED` | `true` | Push metrics to Kafka |
| `KAFKA_METRICS_TOPIC` | `metrics.application` | Kafka topic for metrics |

See the [root README](../README.md) for the full environment variable reference.

## Example

See [`examples/go-http-service`](../examples/go-http-service/) for a complete working service.
