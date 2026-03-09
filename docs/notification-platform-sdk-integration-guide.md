## Notification Platform SDK – Integration Guide

This guide explains how to integrate the Platform Observability SDK into a Go-based notification service similar to the `notification-platform` in this repository. It assumes no prior knowledge of the platform or SDK.

---

### 1. Complete Integration Steps

#### 1.1. Installation

Add the Go SDK to your service:

```bash
go get github.com/centricitywealthtech/platform-observability-sdk/go-sdk
```

````

#### 1.2. Environment Setup

The SDK is configured via environment variables (either OS env or a `.env` loaded by your app). At minimum:

| Variable            | Required | Example                   | Description                                      |
|---------------------|----------|---------------------------|--------------------------------------------------|
| `KAFKA_BROKERS`     | Yes      | `127.0.0.1:9092`          | Comma-separated Kafka brokers                    |
| `ENVIRONMENT`       | No       | `development` / `prod`    | Controls dev vs prod behavior                    |
| `SERVICE_VERSION`   | No       | `v1`                      | Service version added to traces/logs             |
| `LOG_LEVEL`         | No       | `info`                    | `debug`, `info`, `warn`, `error`                 |
| `TRACE_SAMPLING_RATE` | No    | `1.0`                     | 0.0–1.0 sample rate for traces                   |

In `notification-platform`, configuration is centralized in `pkg/config` and a `.env` at the service root. For a clean integration:

1. Create a `.env` with at least:
   ```env
   KAFKA_BROKERS=127.0.0.1:9092
   ENVIRONMENT=development
   SERVICE_VERSION=v1
   LOG_LEVEL=info
````

2. Load it in `main.go` (optional but recommended for local dev) using `github.com/joho/godotenv`.

#### 1.3. Configuration & Authentication

The observability SDK itself has **no authentication**; it only needs connectivity to:

- **Kafka** (for logs, metrics, traces)
- Optional downstream services you already configure (DB, providers, etc.)

In `notification-platform`, non-observability config is loaded by:

```go
import pkgconfig "notification-platform/pkg/config"

cfg := pkgconfig.Load()
```

This pulls DB, Kafka, logging, and server settings from `.env` + env vars.

#### 1.4. SDK Initialization

To make SDK usage repeatable and safe, `notification-platform` wraps initialization in `internal/app/observability`:

```go
package observability

func Initialize(serviceName string, cfg *pkgconfig.Config) (*zap.Logger, Tracer, error)
func GetLogger() *zap.Logger
func GetTracer() trace.Tracer
```

**Recommended pattern (from `cmd/api-gateway/main.go`):**

```go
cfg := pkgconfig.Load()

zapLogger, tracer, err := appobs.Initialize("api-gateway", cfg)
if err != nil {
    log.Fatalf("Failed to initialize observability: %v", err)
}
defer zapLogger.Sync()
if tracer != nil {
    defer tracer.Shutdown(context.Background())
}
```

`Initialize`:

- Exposes config to the SDK via env vars (`SERVICE_VERSION`, `ENVIRONMENT`, `LOG_LEVEL`, `KAFKA_BROKERS`, etc.).
- Creates a singleton Zap logger via `obs.NewLogger`.
- Creates and registers an OpenTelemetry `TracerProvider` via `obs.NewTracer`.
- Starts the metrics pusher via `obs.StartMetricsPusher`.

`GetLogger` / `GetTracer` are then used throughout the app to ensure a single, consistent observability setup.

#### 1.5. HTTP Middleware Wiring

Wrap your HTTP stack with tracing and metrics middleware from the Go SDK:

```go
import obs "github.com/centricitywealthtech/platform-observability-sdk/go-sdk/pkg/observability"

handler := obs.HTTPTracingMiddleware("api-gateway")(
    obs.HTTPMetricsMiddleware("api-gateway")(routerEngine),
)

server := &http.Server{
    Addr:    ":" + port,
    Handler: handler,
}
```

This ensures every incoming HTTP request (e.g. `POST /api/notifications/send`) creates a top-level trace span plus metrics.

#### 1.6. First Working Request

With the observability SDK and service initialized:

1. Start Kafka (locally via Docker or your preferred method).
2. Run the API Gateway:
   ```bash
   cd notification-platform/cmd/api-gateway
   go run .
   ```
3. Send a notification:
   ```bash
   curl -X POST "http://localhost:9020/api/notifications/send" \
     -H "Content-Type: application/json" \
     -d '{
       "tenant_id": 1,
       "channel_id": 4,
       "send_to": "+1234567890",
       "template_code": "OTP_LOGIN",
       "payload": {"otp": "123456"}
     }'
   ```

Expected behavior:

- HTTP 202 `Notification accepted` from the API Gateway.
- A trace with:
  - `POST /api/notifications/send` (HTTP server span)
  - `db_create_notification_request` (DB insert span)
  - `kafka_publish_notification` (Kafka publish span)
- Logs emitted for validation, DB insert, Kafka publish, and final acceptance.

#### 1.7. Error Handling

Use the SDK’s span status codes to mark failures:

```go
dbSpan.RecordError(err)
dbSpan.SetStatus(obs.StatusCodeError, err.Error())
```

Patterns in `NotificationHandler.PostNotification`:

- On DB failure:
  - The `db_create_notification_request` span is marked with `StatusCodeError`.
  - The client receives a 500 error payload.
- On Kafka publish failure:
  - The `kafka_publish_notification` span is marked with `StatusCodeError`.
  - An error is logged, but the HTTP response may still be 202, depending on business rules.

#### 1.8. Production Readiness Considerations

- **Sampling**: Set `TRACE_SAMPLING_RATE` to a lower value (e.g. `0.1`) in high-volume environments.
- **Kafka availability**: Decide how the service behaves when Kafka is down:
  - For development: `ENVIRONMENT=development` → SDK falls back to stdout.
  - For production: ensure robust retry and monitoring for the Kafka telemetry topics.
- **PII and logging**:
  - Avoid logging full payloads or sensitive fields.
  - Prefer aggregated identifiers and masked values (e.g. `******0427` for phone numbers).
- **Resource shutdown**:
  - Always `defer tracer.Shutdown(context.Background())` and logger `Sync()` to flush data on exit.

---

### 2. Usage & Use-Cases

#### 2.1. When to Use This SDK

Use the Platform Observability SDK in notification services when you need:

- Consistent logging, metrics, and tracing across microservices.
- Kafka-based export of observability data for centralized processing.
- Automatic HTTP tracing + metrics with minimal boilerplate.

Typical consumers:

- Multi-channel notification services (SMS, email, push).
- API gateways that fan out to Kafka and downstream workers.

#### 2.2. Typical Real-World Scenarios

- **Transactional notifications**:
  - User performs an action → API Gateway receives `POST /api/notifications/send`.
  - Request is validated, persisted, and published to a Kafka topic.
  - A Kafka consumer (like `dispatch-consumer`) picks up the message and dispatches via providers.
  - Traces capture the end-to-end flow across gateway, Kafka, consumer, and outbound provider.

- **High-volume campaigns**:
  - Same architecture, but with more focus on sampling and metrics to avoid overwhelming the tracing backend.

#### 2.3. Best Practices vs Bad Practices

- **Best practices**
  - Initialize observability once at startup via a module like `internal/app/observability`.
  - Use `HTTPTracingMiddleware` and `HTTPMetricsMiddleware` as the outermost wrappers on your router.
  - Create **semantic child spans** for meaningful operations:
    - `db_create_notification_request`
    - `kafka_publish_notification`
  - Always record errors and set span status on failure.

- **Bad practices**
  - Creating a new tracer provider per request.
  - Ignoring errors from `NewLogger`, `NewTracer`, or `StartMetricsPusher`.
  - Logging sensitive data (raw tokens, passwords, full PII).
  - Swallowing errors without annotating spans or logs.

#### 2.4. Expected Request / Response Flow

1. Client calls `POST /api/notifications/send`.
2. Gin + observability middleware create a server span.
3. `ValidationMiddleware` validates and enriches the request.
4. `NotificationHandler.PostNotification`:
   - Creates DB span and persists a request row.
   - Creates Kafka span and publishes to `notification-transactional-topic`.
   - Logs INITIATED status.
5. Client receives HTTP 202 while background workers continue processing.

#### 2.5. Lifecycle of an Operation

End-to-end lifecycle for a single notification:

1. **Ingress**: HTTP request enters API Gateway (HTTP trace span + metrics event).
2. **Persistence**: DB span records write latency and any failures.
3. **Queueing**: Kafka span records time to publish and target topic.
4. **Dispatch (consumer)**:
   - Uses the same observability SDK to create spans for message consumption and provider calls.
5. **Delivery**: Provider integration logs and traces outbound calls.

---

### 3. SDK API Reference (Used in Notification Platform)

This section documents the developer-facing components involved in the integration.

#### 3.1. Go SDK – Core Observability Package

All symbols are under:

```go
import obs "github.com/centricitywealthtech/platform-observability-sdk/go-sdk/pkg/observability"
```

- **`func NewLogger(serviceName string) (*zap.Logger, error)`**
  - Creates a structured Zap logger configured from env vars.
  - **Side effects**: May open Kafka connections depending on env.
  - **Failure cases**: Returns error if config is invalid or sinks cannot be initialized.

- **`func StartMetricsPusher(serviceName string) error`**
  - Starts a background routine pushing Prometheus-like metrics to Kafka.
  - **Side effects**: Background goroutine, network connections to Kafka.
  - **Failure cases**: Returns error if Kafka configuration is invalid.

- **`func NewTracer(serviceName string) (*tracing.TracerProvider, error)`**
  - Allocates a new OpenTelemetry `TracerProvider` and registers it globally.
  - **Side effects**:
    - Calls `otel.SetTracerProvider`.
    - Configures global propagator.
  - **Failure cases**: Invalid exporter configuration.

- **`func HTTPTracingMiddleware(serviceName string) func(http.Handler) http.Handler`**
  - Wraps an `http.Handler` with tracing:
    - Creates a **server span** per request.
    - Extracts/injects context via HTTP headers.
    - Adds common HTTP attributes and marks spans as error when status ≥ 400.

- **`func HTTPMetricsMiddleware(serviceName string) func(http.Handler) http.Handler`**
  - Wraps an `http.Handler` with HTTP metrics (latency, status code, method, route).

- **Span status codes**
  - `const StatusCodeUnset`, `StatusCodeOK`, `StatusCodeError`
  - Used with `span.SetStatus(obs.StatusCodeError, "reason")`.

#### 3.2. Notification Platform Integration Helpers

##### Module: `internal/app/observability`

- **`func Initialize(serviceName string, cfg *pkgconfig.Config) (*zap.Logger, Tracer, error)`**
  - Purpose: One-shot initialization of logger, tracer provider, and metrics pusher.
  - Parameters:
    - `serviceName`: Logical service name (`"api-gateway"`, `"dispatch-consumer"`).
    - `cfg`: Strongly typed configuration from `pkg/config`.
  - Returns:
    - `*zap.Logger`: Shared service logger.
    - `Tracer`: Minimal interface wrapping the tracer provider (for shutdown).
    - `error`: Non-nil if logger setup fails.
  - Side effects:
    - Exposes env vars for the SDK.
    - Starts background metrics pusher.
    - Registers global tracer provider.

- **`func GetLogger() *zap.Logger`**
  - Returns the singleton logger.
  - Panics if `Initialize` has not been called.

- **`func GetTracer() trace.Tracer`**
  - Returns a named tracer from the global tracer provider.
  - Panics if `Initialize` has not been called.

##### Example Usage in Handlers

```go
func (h *NotificationHandler) PostNotification(c *gin.Context) {
    ctx := c.Request.Context()
    tracer := observability.GetTracer()

    dbCtx, dbSpan := tracer.Start(ctx, "db_create_notification_request",
        trace.WithAttributes(
            attribute.Int64("tenant_id", int64(n.TenantID)),
            attribute.Int64("channel_id", int64(n.ChannelID)),
        ),
    )
    defer dbSpan.End()

    requestID, err := h.repo.CreateRequest(dbCtx, n)
    if err != nil {
        dbSpan.RecordError(err)
        dbSpan.SetStatus(obs.StatusCodeError, err.Error())
        // handle error...
    }
}
```

This pattern (using `GetTracer` + explicit spans) should be reused for any IO-bound or critical section where visibility matters.
