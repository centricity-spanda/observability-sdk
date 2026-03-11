# Centricity .NET Observability SDK

Unified observability SDK for .NET 8+ services.

| Signal  | Library               |
|---------|-----------------------|
| Logs    | **Serilog** + custom `EnvelopeSink` |
| Traces  | **OpenTelemetry** (OTLP gRPC / HTTP) |
| Metrics | **OpenTelemetry** + **Prometheus** scrape `/metrics` |

---

## Log Envelope

Every log call emits the shared Centricity JSON envelope:

```json
{
  "timestamp":   "2026-02-23T10:15:30.123456789Z",
  "severity":    "INFO",
  "severity_num": 9,
  "message":     "Order created successfully",
  "service": {
    "service.name":           "order-service",
    "service.version":        "1.4.2",
    "service.namespace":      "broking",
    "deployment.environment": "production",
    "host.name":              "node-3",
    "k8s.pod.name":           "order-svc-7d9",
    "k8s.namespace.name":     "platform",
    "k8s.node.name":          "ip-10-0-1-45"
  },
  "attributes": {
    "trace_id":      "4bf92f3577b34da6a3ce929d0e0e4736",
    "span_id":       "00f067aa0ba902b7",
    "parent_span_id":"bbb222",
    "trace_flags":   "01",
    "log.type":      "app",
    "team":          "broking",
    "order_id":      "ORD-001"
  },
  "error": {}
}
```

`trace_id`, `span_id`, `parent_span_id`, and `trace_flags` are **auto-injected** from `Activity.Current` — no manual threading required.

---

## Quick Start

### 1 — Add NuGet reference

```xml
<ProjectReference Include="..\dotnet-sdk\Centricity.Observability.csproj" />
```

### 2 — ASP.NET Core (`Program.cs`)

```csharp
using Centricity.Observability.Logging;
using Centricity.Observability.Tracing;
using Centricity.Observability.Metrics;

var builder = WebApplication.CreateBuilder(args);

// ── Logging ─────────────────────────────────────────────────────────────────
var (logger, sink) = ObservabilityLogger.CreateWithSink("order-service");
builder.Host.UseSerilog(Log.Logger);

// ── Tracing ──────────────────────────────────────────────────────────────────
builder.Services.AddOpenTelemetry()
    .WithTracing(b  => TracerFactory.Configure(b,  "order-service"))
    .WithMetrics(b  => MetricsFactory.Configure(b, "order-service"));

var app = builder.Build();

// Expose /metrics for Prometheus scraping
app.MapPrometheusScrapingEndpoint();

app.Run();
```

### 3 — Logging

```csharp
// Simple
Log.Information("Order created");

// With structured attributes
Log.Information("Order created {order_id} {user_id}", "ORD-001", "USR-42");

// With error
try { /* ... */ }
catch (Exception ex)
{
    Log.Error(ex, "Failed to process order {order_id}", "ORD-001");
}
```

### 4 — Tracing (manual spans)

```csharp
var tracer = TracerFactory.GetTracer("order-service");
using var activity = tracer.StartActivity("ProcessOrder");
activity?.SetTag("order_id", "ORD-001");
```

### 5 — Custom metrics

```csharp
var meter   = MetricsFactory.GetMeter("order-service");
var counter = meter.CreateCounter<long>("orders_created_total");
counter.Add(1, new TagList { { "status", "success" } });
```

---

## Environment Variables

| Variable                     | Default              | Description                                      |
|------------------------------|----------------------|--------------------------------------------------|
| `SERVICE_VERSION`            | `unknown`            | Semantic version of the service                  |
| `SERVICE_NAMESPACE`          | `default`            | Logical namespace (e.g. `broking`)               |
| `ENVIRONMENT` / `ENV`        | `production`         | Deployment environment                           |
| `SERVICE_TEAM`               | _(empty)_            | Owning team                                      |
| `HOST_NAME`                  | `MachineName`        | Host name override                               |
| `K8S_POD_NAME`               | _(empty)_            | Kubernetes pod name                              |
| `K8S_NAMESPACE_NAME`         | _(empty)_            | Kubernetes namespace                             |
| `K8S_NODE_NAME`              | _(empty)_            | Kubernetes node name                             |
| `LOG_LEVEL`                  | `info`               | Min log level (`trace`/`debug`/`info`/`warn`...) |
| `LOG_TYPE`                   | `app`                | Log type tag in attributes                       |
| `LOG_CONSOLE_ENABLED`        | `true`               | Write to stdout                                  |
| `LOG_FILE_ENABLED`           | `false`              | Write to file                                    |
| `LOG_FILE_PATH`              | `./logs/app.log`     | Log file path                                    |
| `LOG_KAFKA_ENABLED`          | `true`               | Publish logs to Kafka                            |
| `KAFKA_BROKERS`              | _(empty)_            | Comma-separated Kafka broker list                |
| `KAFKA_LOG_TOPIC`            | `logs.application`   | Kafka topic for logs                             |
| `LOG_PII_REDACTION_ENABLED`  | `true`               | Enable PII field redaction                       |
| `OTEL_EXPORTER_OTLP_ENDPOINT`| _(empty)_            | OTLP endpoint for traces + metrics               |
| `OTEL_EXPORTER_OTLP_PROTOCOL`| `grpc`               | `grpc` or `http/protobuf`                        |
| `TRACE_SAMPLING_RATE`        | `1.0`                | Trace sampling ratio (0.0–1.0)                   |
