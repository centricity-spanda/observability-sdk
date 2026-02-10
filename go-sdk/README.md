# Platform Observability SDK - Go

Production-grade observability library for Go microservices with Kafka-based telemetry.

## Features

- **Logging**: Structured JSON logs via Zap → Kafka
- **Metrics**: Prometheus metrics → Kafka pusher
- **Tracing**: OpenTelemetry spans → Kafka

## Quick Start

```bash
go get github.com/centricitywealthtech/platform-observability-sdk/go-sdk
```

```go
package main

import (
    "context"
    "net/http"

    "go.uber.org/zap"
    obs "github.com/centricitywealthtech/platform-observability-sdk/go-sdk/pkg/observability"
)

func main() {
    // Initialize logger
    logger, _ := obs.NewLogger("my-service")
    defer logger.Sync()

    // Start metrics pusher
    obs.StartMetricsPusher("my-service")

    // Initialize tracer
    tp, _ := obs.NewTracer("my-service")
    defer tp.Shutdown(context.Background())

    // Apply middleware
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

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `KAFKA_BROKERS` | Yes | - | Comma-separated Kafka brokers |
| `SERVICE_NAME` | Yes | - | Service identifier |
| `ENVIRONMENT` | No | `production` | `production`/`development` |
| `LOG_LEVEL` | No | `info` | Log level |
| `TRACE_SAMPLING_RATE` | No | `0.1` | Trace sampling (0.0-1.0) |

## Development Mode

Set `ENVIRONMENT=development` to disable Kafka and use stdout only.
