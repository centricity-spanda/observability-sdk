# Platform Observability SDK

Multi-language observability SDK for microservices. Drop-in structured logging, Prometheus metrics, and OpenTelemetry tracing — all with Kafka-based telemetry export and built-in PII redaction.

## Architecture

```
┌──────────────────────────────────────────────────────────┐
│  Your Service                                            │
│  ┌─────────┐  ┌──────────┐  ┌─────────┐                │
│  │ Logging  │  │ Metrics  │  │ Tracing │                │
│  │ (Zap /   │  │ (Prom    │  │ (OTel   │                │
│  │ structlog│  │  client) │  │  SDK)   │                │
│  │ / pino)  │  │          │  │         │                │
│  └───┬──┬──┘  └───┬──┬───┘  └───┬──┬──┘                │
│      │  │         │  │           │  │                    │
│  stdout file   /metrics  Kafka   │  Kafka               │
│      │            │  │           │  │                    │
│      │  ┌─────────┘  │           │  │                    │
│      │  │  ┌─────────┘     ┌─────┘  │                    │
└──────┼──┼──┼───────────────┼────────┼────────────────────┘
       │  │  │               │        │
       ▼  ▼  ▼               ▼        ▼
      ELK  Prometheus   Kafka (7-day buffer)   Jaeger
           Grafana       └──▶ OTel Collector
```

**Key design principles:**
- **Zero external agents** — all instrumentation lives in your app process
- **Kafka as durable buffer** — decouples your service from telemetry backends with 7-day retention
- **Industry-standard wire formats** — OTLP protobuf for metrics and traces, JSON for logs
- **FinTech-compliant** — built-in PII redaction with regex pattern matching

## SDKs

| Language | Directory | Libraries | Status |
|----------|-----------|-----------|--------|
| **Go** | [`/go-sdk`](go-sdk/) | Zap, Prometheus, OpenTelemetry, Sarama | ✅ Stable |
| **Python** | [`/python-sdk`](python-sdk/) | structlog, prometheus-client, OpenTelemetry, kafka-python | ✅ Stable |
| **Node.js** | [`/js-sdk`](js-sdk/) | pino, prom-client, OpenTelemetry, KafkaJS | ✅ Stable |

All three SDKs are **feature-equivalent** and share the same:
- Environment variable names
- Kafka topic names and wire formats
- Metric names and label sets
- HTTP middleware behavior (span attributes, error marking)
- PII redaction patterns

## Quick Start

### Go
```go
import obs "github.com/centricitywealthtech/platform-observability-sdk/go-sdk/pkg/observability"

logger, _ := obs.NewLogger("my-service")
obs.StartMetricsPusher("my-service")
tp, _ := obs.NewTracer("my-service")
```

### Python
```python
from observability import new_logger, start_metrics_pusher, new_tracer

logger = new_logger("my-service")
start_metrics_pusher("my-service")
tracer = new_tracer("my-service")
```

### Node.js
```typescript
import { newLogger, startMetricsPusher, newTracer, HTTPTracingMiddleware, HTTPMetricsMiddleware } from 'centricity-observability';

const logger = newLogger('my-service');
await startMetricsPusher('my-service');
const { shutdown } = newTracer('my-service');
```

## Environment Variables

All SDKs read the same environment variables:

### Core

| Variable | Default | Description |
|----------|---------|-------------|
| `KAFKA_BROKERS` | *(required)* | Comma-separated Kafka broker addresses |
| `ENVIRONMENT` | `production` | Set to `development` to disable Kafka export |
| `SERVICE_VERSION` | `unknown` | Appears in log fields and trace resource |

### Logging

| Variable | Default | Description |
|----------|---------|-------------|
| `LOG_LEVEL` | `info` | Minimum level: `debug`, `info`, `warn`, `error` |
| `LOG_KAFKA_ENABLED` | `true` | Send logs to Kafka |
| `LOG_FILE_ENABLED` | `false` | Write logs to file |
| `LOG_FILE_PATH` | `./logs/app.log` | File path when file logging is enabled |
| `LOG_CONSOLE_ENABLED` | `true` | Write logs to stdout |
| `LOG_PII_REDACTION_ENABLED` | `true` | Enable automatic PII masking |
| `KAFKA_LOG_TOPIC` | `logs.application` | Kafka topic for logs |

### Metrics

| Variable | Default | Description |
|----------|---------|-------------|
| `METRICS_KAFKA_ENABLED` | `true` | Push metrics to Kafka in OTLP format |
| `KAFKA_METRICS_TOPIC` | `metrics.application` | Kafka topic for metrics |
| `METRICS_PUSH_INTERVAL_MS` | `15000` | Push interval in milliseconds |

### Tracing

| Variable | Default | Description |
|----------|---------|-------------|
| `TRACE_SAMPLING_RATE` | `1.0` | Sampling ratio (0.0 to 1.0) |
| `TRACES_KAFKA_ENABLED` | `true` | Export traces to Kafka |
| `KAFKA_TRACES_TOPIC` | `traces.application` | Kafka topic for traces |

## Development Mode

Set `ENVIRONMENT=development` to:
- Disable all Kafka export (logs, metrics, traces go to stdout only)
- Keep Prometheus `/metrics` endpoint active for local scraping
- Keep console logging active

## Built-in HTTP Metrics

All SDKs automatically record these Prometheus metrics via `HTTPMetricsMiddleware`:

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `http_requests_total` | Counter | `service`, `method`, `path`, `status` | Total HTTP requests |
| `http_request_duration_seconds` | Histogram | `service`, `method`, `path` | Request duration |
| `http_requests_in_flight` | Gauge | `service` | Active requests |
| `kafka_producer_messages_total` | Counter | `service`, `topic`, `status` | Kafka messages sent |
| `kafka_producer_buffer_usage` | Gauge | `service` | Kafka producer buffer (0–1) |

## PII Redaction

When `LOG_PII_REDACTION_ENABLED=true` (default), the SDK automatically masks sensitive data in log messages and fields:

**Redacted patterns:** emails, phone numbers, PAN cards, Aadhaar numbers, credit card numbers, bank account numbers, IFSC codes, passport numbers, SSNs

**Sensitive field names:** `password`, `secret`, `token`, `api_key`, `authorization`, `credential`

**Safe patterns (never redacted):** URLs, file paths, UUIDs, timestamps, version strings, order IDs

## Examples

Working example services are provided in [`/examples`](examples/):

| Example | Port | Description |
|---------|------|-------------|
| [`go-http-service`](examples/go-http-service/) | 8088 | Go + net/http |
| [`node-http-service`](examples/node-http-service/) | 8088 | Node.js + Express |

## Local Development

```bash
# Start infrastructure (Kafka, Prometheus, OTel Collector)
docker-compose up -d

# View Kafka UI
open http://localhost:8090

# View Prometheus
open http://localhost:9090
```

## Documentation

- [Go SDK README](go-sdk/README.md) — Installation, API reference, and examples
- [Python SDK README](python-sdk/README.md) — Installation, FastAPI/Flask integration
- [Node.js SDK README](js-sdk/README.md) — Installation, Express integration
- [Architecture Deep Dive](docs/Observability-stack-0602.md) — Full system design
