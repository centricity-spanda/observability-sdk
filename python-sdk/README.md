# Observability SDK — Python

Drop-in observability for Python microservices. Structured logging, Prometheus metrics, and OpenTelemetry tracing — all with automatic Kafka export and PII redaction.

Works with **FastAPI**, **Flask**, and any ASGI/WSGI framework.

## Installation

```bash
pip install centricity-observability
```

## Quick Start

```python
from observability import new_logger, start_metrics_pusher, new_tracer

# 1. Initialize logger
logger = new_logger("my-service")

# 2. Start metrics pusher (pushes to Kafka every 15s)
start_metrics_pusher("my-service")

# 3. Initialize tracer
tracer = new_tracer("my-service")
```

That's it. Your service now has:
- ✅ Structured JSON logs → stdout + Kafka
- ✅ HTTP request metrics → Prometheus + Kafka (OTLP)
- ✅ Distributed traces → Kafka → Jaeger
- ✅ Automatic PII redaction in log messages

---

## Framework Integration

### FastAPI

```python
from fastapi import FastAPI
from observability import new_logger, start_metrics_pusher, new_tracer
from observability.metrics.middleware import HTTPMetricsMiddleware
from observability.tracing.middleware import HTTPTracingMiddleware

app = FastAPI()

# Initialize observability
logger = new_logger("my-service")
start_metrics_pusher("my-service")
tracer = new_tracer("my-service")

# Add middleware (tracing first, then metrics)
app.add_middleware(HTTPTracingMiddleware, service_name="my-service")
app.add_middleware(HTTPMetricsMiddleware, service_name="my-service")

@app.get("/health")
async def health():
    logger.info("health check")
    return {"status": "ok"}

@app.post("/api/payment")
async def process_payment(payment_id: str, amount: float):
    logger.info("processing payment", payment_id=payment_id, amount=amount)
    return {"status": "completed"}
```

### Flask

```python
from flask import Flask
from observability import new_logger, start_metrics_pusher, new_tracer
from observability.tracing.middleware import flask_tracing_middleware

app = Flask(__name__)

# Initialize observability
logger = new_logger("my-service")
start_metrics_pusher("my-service")
tracer = new_tracer("my-service")

# Add tracing middleware
before, after, teardown = flask_tracing_middleware("my-service")
app.before_request(before)
app.after_request(after)
app.teardown_request(teardown)

@app.route("/health")
def health():
    logger.info("health check")
    return {"status": "ok"}
```

---

## How It Works

### Logging

The SDK creates a [structlog](https://www.structlog.org/) logger with JSON output:

```python
logger = new_logger("my-service")

# Standard structured logging
logger.info("processing payment", payment_id="PAY-12345", amount=1500.50)

# PII is automatically redacted when LOG_PII_REDACTION_ENABLED=true
logger.info("user data",
    email="john@example.com",    # → j***@***.com
    password="secret123",         # → [REDACTED]
)
```

Every log entry automatically includes `service`, `environment`, and `version` fields.

**Output destinations** (all configurable via env vars):
| Destination | Env Var | Default |
|-------------|---------|---------|
| Console (stdout) | `LOG_CONSOLE_ENABLED` | `true` |
| File | `LOG_FILE_ENABLED` | `false` |
| Kafka | `LOG_KAFKA_ENABLED` | `true` |

---

### Metrics

Standard [Prometheus](https://prometheus.io) metrics with Kafka push in **OTLP protobuf** format:

```python
# Automatic when you use the middleware:
app.add_middleware(HTTPMetricsMiddleware, service_name="my-service")
```

**Built-in HTTP metrics:**

| Metric | Type | Labels |
|--------|------|--------|
| `http_requests_total` | Counter | `service`, `method`, `path`, `status` |
| `http_request_duration_seconds` | Histogram | `service`, `method`, `path` |
| `http_requests_in_flight` | Gauge | `service` |

**Custom metrics** — use the Prometheus registry:

```python
from prometheus_client import Counter

payments_total = Counter(
    "payments_total",
    "Total payments processed",
    ["currency", "status"],
)

# Increment
payments_total.labels(currency="USD", status="success").inc()
```

Custom metrics are automatically picked up by the Kafka pusher.

---

### Tracing

[OpenTelemetry](https://opentelemetry.io) tracing with Kafka export:

```python
tracer = new_tracer("my-service")

# Automatic spans for HTTP requests via middleware (see Framework Integration)

# Manual spans:
with tracer.start_as_current_span("process-payment") as span:
    span.set_attribute("payment.id", "PAY-12345")
    # ... business logic ...

# Get trace ID for log correlation:
from observability.tracing import get_trace_id
trace_id = get_trace_id()
logger.info("processing", trace_id=trace_id)
```

**HTTP middleware span attributes:**
`http.method`, `http.route`, `http.scheme`, `http.host`, `http.user_agent`, `http.status_code`

Requests returning **400+** are automatically marked as error spans.

---

### PII Redaction

When `LOG_PII_REDACTION_ENABLED=true` (default), the SDK masks sensitive data:

| Data Type | Example Input | Redacted Output |
|-----------|---------------|-----------------|
| Email | `john.doe@example.com` | `j***@***.com` |
| Phone | `+919876543210` | `+91****3210` |
| PAN Card | `ABCDE1234F` | `ABCD****4F` |
| Aadhaar | `8561 0272 7756` | `****_****_7756` |
| Credit Card | `4111-1111-1111-1111` | `****-****-****-1111` |
| Sensitive fields | `password`, `secret`, `token` | `[REDACTED]` |

**Safe patterns** (never redacted): URLs, file paths, UUIDs, order IDs.

---

## Graceful Shutdown

```python
from observability import stop_metrics_pusher

# Call during shutdown
stop_metrics_pusher()
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

See the [root README](../README.md) for the full environment variable reference.
