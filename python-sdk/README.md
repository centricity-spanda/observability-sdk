# Platform Observability SDK - Python

Production-grade observability library for Python microservices with Kafka-based telemetry.

## Features

- **Logging**: Structured JSON logs via structlog → Kafka
- **Metrics**: Prometheus metrics → Kafka pusher
- **Tracing**: OpenTelemetry spans → Kafka

## Quick Start

```bash
pip install centricity-observability
# Or for FastAPI support:
pip install centricity-observability[fastapi]
```

```python
from observability import new_logger, start_metrics_pusher, new_tracer

# Initialize
logger = new_logger("my-service")
start_metrics_pusher("my-service")
tracer = new_tracer("my-service")

# Use logger
logger.info("processing request", payment_id="PAY123", amount=1000.50)

# Use tracer
with tracer.start_as_current_span("process_payment"):
    # Business logic here
    pass
```

## FastAPI Integration

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

# Add middleware
app.add_middleware(HTTPTracingMiddleware, service_name="my-service")
app.add_middleware(HTTPMetricsMiddleware, service_name="my-service")

@app.get("/health")
async def health():
    logger.info("health check")
    return {"status": "ok"}
```

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `KAFKA_BROKERS` | Yes | - | Comma-separated Kafka brokers |
| `ENVIRONMENT` | No | `production` | `production`/`development` |
| `LOG_LEVEL` | No | `info` | Log level |
| `TRACE_SAMPLING_RATE` | No | `1.0` | Trace sampling (0.0-1.0) |

## Development Mode

Set `ENVIRONMENT=development` to disable Kafka and use stdout only.
