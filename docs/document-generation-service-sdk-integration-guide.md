## Document Generation Service SDK – Integration Guide

This guide explains how to integrate the Platform Observability SDK into a Python-based document generation microservice similar to `platform-be-document-generation-service-python`. It assumes the reader has never used the SDK or this system before.

---

### 1. Complete Integration Steps

#### 1.1. Installation

Install the Python SDK:

```bash
pip install centricity-observability
```

In a typical FastAPI document generation service you will also have:

```bash
pip install fastapi uvicorn httpx structlog
```

#### 1.2. Environment Setup

The Python SDK reads configuration from environment variables:

| Variable              | Required | Example                | Description                      |
| --------------------- | -------- | ---------------------- | -------------------------------- |
| `KAFKA_BROKERS`       | Yes      | `127.0.0.1:9092`       | Comma-separated Kafka brokers    |
| `ENVIRONMENT`         | No       | `development` / `prod` | Controls dev vs prod behavior    |
| `LOG_LEVEL`           | No       | `info`                 | `debug`, `info`, `warn`, `error` |
| `TRACE_SAMPLING_RATE` | No       | `1.0`                  | 0.0–1.0 sample rate for traces   |

In `platform-be-document-generation-service-python`, these values are typically provided via `.env` or a deployment environment, then consumed by `app.config.settings`.

Minimum recommended `.env` for local development:

```env
KAFKA_BROKERS=127.0.0.1:9092
ENVIRONMENT=development
LOG_LEVEL=debug
TRACE_SAMPLING_RATE=1.0
```

#### 1.3. Configuration & Authentication

The observability SDK itself does not introduce new authentication. Your service is still responsible for:

- Authenticating to external APIs (file storage, template engines, Kafka, etc.).
- Securing HTTP endpoints exposed by FastAPI.

Configuration in the sample service comes from `app.config.settings`, which in turn reads from environment variables and `.env`.

#### 1.4. SDK Initialization

The document generation service centralizes observability in `app/observability.py`:

```python
from typing import Optional
import structlog
from opentelemetry.trace import Tracer
from observability import new_logger, new_tracer, start_metrics_pusher, Status, StatusCode

_logger: Optional[structlog.BoundLogger] = None
_tracer: Optional[Tracer] = None
_metrics_initialized: bool = False
```

Initialization entry point:

```python
def initialize_observability(service_name: str) -> None:
    global _logger, _tracer, _metrics_initialized

    _logger = new_logger(service_name)
    _tracer = new_tracer(service_name)
    start_metrics_pusher(service_name)
    _metrics_initialized = True
```

Accessors:

```python
def get_logger() -> structlog.BoundLogger: ...
def get_tracer() -> Tracer: ...
def is_metrics_initialized() -> bool: ...
```

These functions are exposed via `__all__`, along with `Status` and `StatusCode`:

```python
__all__ = [
    "initialize_observability",
    "get_logger",
    "get_tracer",
    "is_metrics_initialized",
    "Status",
    "StatusCode",
]
```

#### 1.5. Application Startup Integration

Observability is initialized exactly once during FastAPI startup via the lifespan context in `app/lifecycle.py`:

```python
from contextlib import asynccontextmanager
from fastapi import FastAPI
from app.config.settings import settings
from app.monitoring.health import health_checker
from app.logging import get_logger
from app.observability import initialize_observability

logger = get_logger(__name__)

@asynccontextmanager
async def lifespan(app: FastAPI):
    logger.info("Starting PDF service...",
               app_name=settings.app_name,
               app_version=settings.app_version,
               environment=settings.environment)

    logger.debug("Configuring observability system")
    initialize_observability(settings.app_name)

    logger.info(
        "Observability configured",
        log_level=settings.log_level,
        log_format=settings.log_format,
    )
    # ... Kafka init, health checks, shutdown logic ...
    yield
```

The FastAPI application is built in `app/bootstrap/app_factory.py`:

```python
from observability.tracing.middleware import HTTPTracingMiddleware
from observability.metrics.middleware import HTTPMetricsMiddleware

def create_app() -> FastAPI:
    app = FastAPI(
        title=settings.app_name,
        version=settings.app_version,
        description="Template-driven PDF generation service",
        lifespan=lifespan,
        debug=settings.debug,
    )

    # Observability middleware
    app.add_middleware(HTTPTracingMiddleware, service_name=settings.app_name)
    app.add_middleware(HTTPMetricsMiddleware, service_name=settings.app_name)

    # Routers and other middleware...
    return app
```

This ensures:

- Global logger, tracer, and metrics pusher are ready before serving traffic.
- Every HTTP request is traced and measured automatically.

#### 1.6. First Working Request

With dependencies installed and environment variables configured:

1. Start Kafka locally.
2. Run the service:
   ```bash
   cd platform-be-document-generation-service-python
   uvicorn app.main:app --reload
   ```
3. Call a document generation endpoint (example path may differ in your service):
   ```bash
   curl -X POST "http://localhost:8000/api/documents/generate" \
     -H "Content-Type: application/json" \
     -d '{
       "template_url": "s3://templates/invoice.typ",
       "data": {"invoice_id": "INV-001", "amount": 1000},
       "mode": "async"
     }'
   ```

Expected behavior:

- A new trace representing the HTTP request (`POST /api/documents/generate`).
- Downstream spans for:
  - Kafka interactions (if generation is queued).
  - External file upload (see `FileUploader.upload`).
  - Template rendering and PDF generation.

#### 1.7. Error Handling

Use the SDK’s status helpers to mark span outcomes:

```python
from app.observability import get_tracer, Status, StatusCode

tracer = get_tracer()
with tracer.start_as_current_span("upload_to_external_service") as span:
    try:
        # upload file via HTTPX
        ...
    except Exception as e:
        span.record_exception(e)
        span.set_status(Status(StatusCode.ERROR, str(e)))
        raise
```

In `app/utils/file_uploader.py`, error branches (DNS issues, timeouts, HTTP errors) follow this pattern, ensuring spans are marked as failed and contain diagnostics.

#### 1.8. Production Readiness Considerations

- Set `ENVIRONMENT=production` and ensure Kafka is reachable from your deployment.
- Consider lowering `TRACE_SAMPLING_RATE` in high throughput environments.
- Be careful not to log full PDF contents or sensitive data; prefer metadata (file size, bucket, template name).
- Properly shut down the HTTPX client (`file_uploader.close()`) and any async resources in the FastAPI lifespan shutdown phase.

---

### 2. Usage & Use-Cases

#### 2.1. When to Use This SDK

Use the Python observability SDK in document generation or similar backend services when you want:

- Structured, correlated logs tied to specific document jobs.
- Metrics for generation counts, durations, and DLQ behavior.
- Traces that connect:
  - API ingress
  - Template rendering
  - External storage / file upload
  - Kafka-based orchestration

#### 2.2. Typical Real-World Scenarios

- **Async document generation from Kafka messages**:
  - Downstream service consumes a Kafka message representing a PDF job.
  - `handle_kafka_message` validates and transforms the message into a `PDFJob`.
  - `_process_pdf_in_background` generates the document with retries and publishes a completion event.
  - Traces and logs highlight failures and DLQ decisions.

- **Sync HTTP-driven document generation**:
  - Client calls an HTTP endpoint to generate and directly download a PDF.
  - The SDK traces the full path through validation, rendering, and file upload.

#### 2.3. Best Practices vs Bad Practices

- **Best practices**
  - Initialize observability exactly once using a module like `app.observability`.
  - Use `get_tracer()` lazily inside functions when you need spans, rather than passing tracer objects everywhere.
  - Attach meaningful attributes to spans:
    - File name, size, bucket.
    - Template URL.
    - `platform_id` / tenant identifiers (non-PII).
  - Record exceptions and set `Status(StatusCode.ERROR, "...")` on failure.

- **Bad practices**
  - Creating new tracers directly from OpenTelemetry instead of the SDK-integrated one.
  - Logging request bodies containing PII or full PDF content.
  - Swallowing errors without recording them on spans.

#### 2.4. Expected Request / Response Flow

For an HTTP-driven PDF generation:

1. Client sends `POST /api/documents/generate`.
2. FastAPI + observability middleware create a server span and HTTP metrics.
3. Application logic:
   - Validates the request.
   - Builds a `PDFJob`.
   - Starts an async background task or synchronous generation.
4. Background processing:
   - Renders the PDF.
   - Records metrics (`pdf_jobs_total`, `pdf_generation_duration_seconds`).
   - Publishes completion events to Kafka.
5. Optional external file upload (e.g., S3-like storage) creates additional spans via `FileUploader.upload`.

#### 2.5. Lifecycle of an Operation

End-to-end lifecycle of an async PDF job:

1. **Ingress**: HTTP request or Kafka message ingestion.
2. **Job creation**: Message parsing and `PDFJob` instantiation.
3. **Processing**: Template rendering and PDF generation (possibly with retries).
4. **Output**:
   - File upload to external storage.
   - Completion event emitted to Kafka.
5. **Failure path**:
   - Metrics incremented for failures.
   - Message routed to DLQ with reason and context.
   - Spans marked with `StatusCode.ERROR`.

---

### 3. SDK API Reference (Used in Document Generation Service)

#### 3.1. Python SDK – Core Observability Functions

Imported from:

```python
from observability import new_logger, new_tracer, start_metrics_pusher, Status, StatusCode
from observability.tracing.middleware import HTTPTracingMiddleware
from observability.metrics.middleware import HTTPMetricsMiddleware
```

- **`new_logger(service_name: str) -> structlog.BoundLogger`**
  - Creates a structured logger configured from environment variables.
  - Side effects: May configure sink(s) that ship logs to Kafka.
  - Failure cases: Raises error on invalid configuration or sink failure.

- **`start_metrics_pusher(service_name: str) -> None`**
  - Starts background metrics pushing to Kafka.
  - Side effects: Background task and network usage.

- **`new_tracer(service_name: str) -> opentelemetry.trace.Tracer`**
  - Creates and configures an OpenTelemetry tracer.
  - Side effects: Registers global tracer provider and propagator.

- **`Status` / `StatusCode`**
  - Thin wrappers over OpenTelemetry span status utilities.
  - Usage:
    ```python
    span.set_status(Status(StatusCode.ERROR, "reason"))
    ```

- **`HTTPTracingMiddleware(service_name: str)`**
  - FastAPI middleware that:
    - Creates a span per request.
    - Extracts/injects trace context.
    - Attaches HTTP method, path, status code, and error status.

- **`HTTPMetricsMiddleware(service_name: str)`**
  - FastAPI middleware that records HTTP metrics (latency, status, etc.).

#### 3.2. Service-Level Observability Module: `app.observability`

- **Module purpose**
  - Provide **singleton** instances of logger, tracer, and metrics initialization for the service.
  - Hide the details of SDK configuration behind a very small interface.

- **`initialize_observability(service_name: str) -> None`**
  - Parameters:
    - `service_name`: A human-readable identifier for your service (e.g., `"pdf-generation-service"`).
  - Side effects:
    - Calls `new_logger`, `new_tracer`, and `start_metrics_pusher`.
    - Stores references to these objects in module-level variables.
  - Failure cases:
    - Propagates exceptions from the SDK initializers (e.g., invalid Kafka config).

- **`get_logger() -> structlog.BoundLogger`**
  - Returns the singleton logger.
  - Raises `RuntimeError` if `initialize_observability` has not been called.

- **`get_tracer() -> opentelemetry.trace.Tracer`**
  - Returns the singleton tracer.
  - Raises `RuntimeError` if `initialize_observability` has not been called.

- **`is_metrics_initialized() -> bool`**
  - Returns `True` once `start_metrics_pusher` has been called successfully.

#### 3.3. Example Usage – File Upload Spans

From `app/utils/file_uploader.py`:

```python
from app.observability import get_tracer, Status, StatusCode

class FileUploader:
    async def upload(...):
        tracer = get_tracer()

        with tracer.start_as_current_span("upload_to_external_service") as span:
            try:
                # prepare headers, data, and files
                client = await self._get_client()
                response = await client.post(...)

                span.set_attribute("http.status_code", response.status_code)
                # handle success...
            except httpx.ConnectError as conn_error:
                span.record_exception(conn_error)
                span.set_status(Status(StatusCode.ERROR, str(conn_error)))
                # log and raise...
            except httpx.TimeoutException as e:
                span.record_exception(e)
                span.set_status(Status(StatusCode.ERROR, "Timeout"))
                # log and raise...
            except httpx.HTTPStatusError as e:
                span.record_exception(e)
                span.set_status(Status(StatusCode.ERROR, f"HTTP {e.response.status_code}"))
                # log and raise...
            except Exception as e:
                span.record_exception(e)
                span.set_status(Status(StatusCode.ERROR, str(e)))
                # log and raise...
```

This pattern can be reused in any IO-heavy or failure-prone part of your application (template rendering, external data fetches, etc.) to get rich, queryable traces in your observability backend.
