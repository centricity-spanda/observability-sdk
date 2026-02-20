# Observability SDK — Node.js

Drop-in observability for Node.js microservices. Structured logging via [pino](https://getpino.io), Prometheus metrics via [prom-client](https://github.com/siimon/prom-client), and OpenTelemetry tracing — all with automatic Kafka export and PII redaction.

Works with **Express**, **Fastify**, and any Node.js HTTP framework.

## Installation

```bash
npm install centricity-observability
```

## Quick Start

```typescript
import {
  newLogger,
  startMetricsPusher,
  stopMetricsPusher,
  newTracer,
  HTTPTracingMiddleware,
  HTTPMetricsMiddleware,
} from 'centricity-observability';

const SERVICE_NAME = 'my-service';

// 1. Initialize logger
const logger = newLogger(SERVICE_NAME);

// 2. Start metrics pusher (pushes to Kafka every 15s)
await startMetricsPusher(SERVICE_NAME);

// 3. Initialize tracer
const { shutdown: shutdownTracer } = newTracer(SERVICE_NAME);
```

That's it. Your service now has:
- ✅ Structured JSON logs → stdout + Kafka
- ✅ HTTP request metrics → Prometheus + Kafka (OTLP)
- ✅ Distributed traces → Kafka → Jaeger
- ✅ Automatic PII redaction in log messages

---

## Express Integration

```typescript
import 'dotenv/config';
import express from 'express';
import {
  newLogger,
  startMetricsPusher,
  stopMetricsPusher,
  newTracer,
  getTraceId,
  HTTPTracingMiddleware,
  HTTPMetricsMiddleware,
} from 'centricity-observability';

const SERVICE_NAME = 'my-service';
const logger = newLogger(SERVICE_NAME);

async function main() {
  // Start metrics pusher
  await startMetricsPusher(SERVICE_NAME);

  // Initialize tracer
  const { shutdown: shutdownTracer } = newTracer(SERVICE_NAME);

  const app = express();
  app.use(express.json());

  // Apply middleware (tracing first, then metrics)
  app.use(HTTPTracingMiddleware(SERVICE_NAME));
  app.use(HTTPMetricsMiddleware(SERVICE_NAME));

  // Routes
  app.get('/health', (_req, res) => {
    res.json({ status: 'healthy' });
  });

  app.post('/api/payment', (req, res) => {
    const traceId = getTraceId();
    logger.info({ trace_id: traceId, payment_id: 'PAY-12345' }, 'processing payment');
    res.json({ status: 'completed' });
  });

  // Start server
  const server = app.listen(8080, () => {
    logger.info({ addr: ':8080' }, 'server started');
  });

  // Graceful shutdown
  const shutdown = async () => {
    stopMetricsPusher();
    await shutdownTracer();
    server.close(() => process.exit(0));
  };

  process.on('SIGINT', shutdown);
  process.on('SIGTERM', shutdown);
}

main().catch(err => {
  logger.error({ err }, 'failed to start');
  process.exit(1);
});
```

---

## How It Works

### Logging

The SDK creates a [pino](https://getpino.io) logger with JSON output:

```typescript
const logger = newLogger('my-service');

// Standard structured logging
logger.info({ payment_id: 'PAY-12345', amount: 1500.50 }, 'processing payment');

// PII is automatically redacted when LOG_PII_REDACTION_ENABLED=true
logger.info({ email: 'john@example.com', password: 'secret' }, 'user data');
// → email: "j***@***.com", password: "[REDACTED]"
```

Every log entry automatically includes `service`, `environment`, and `version` fields.

**Output destinations** (all configurable via env vars):
| Destination | Env Var | Default |
|-------------|---------|---------|
| Console (stdout) | `LOG_CONSOLE_ENABLED` | `true` |
| Kafka | `LOG_KAFKA_ENABLED` | `true` |

---

### Metrics

Standard [Prometheus](https://prometheus.io) metrics with Kafka push in **OTLP protobuf** format:

```typescript
// Automatic when you use the middleware:
app.use(HTTPMetricsMiddleware('my-service'));
```

**Built-in HTTP metrics:**

| Metric | Type | Labels |
|--------|------|--------|
| `http_requests_total` | Counter | `service`, `method`, `path`, `status` |
| `http_request_duration_seconds` | Histogram | `service`, `method`, `path` |
| `http_requests_in_flight` | Gauge | `service` |

**Custom metrics** — use prom-client directly:

```typescript
import { Counter } from 'prom-client';

const paymentsTotal = new Counter({
  name: 'payments_total',
  help: 'Total payments processed',
  labelNames: ['currency', 'status'],
});

// Increment
paymentsTotal.inc({ currency: 'USD', status: 'success' });
```

Custom metrics are automatically picked up by the Kafka pusher — no additional setup needed.

**Kafka push details:**
- Converts counters, gauges, and histograms to OTLP protobuf
- Pushes every 15s (configurable via `METRICS_PUSH_INTERVAL_MS`)
- Skipped in `development` mode

---

### Tracing

[OpenTelemetry](https://opentelemetry.io) tracing with Kafka export:

```typescript
const { shutdown } = newTracer('my-service');

// Automatic spans for HTTP requests via middleware:
app.use(HTTPTracingMiddleware('my-service'));

// Manual spans:
import { trace } from '@opentelemetry/api';
const tracer = trace.getTracer('my-service');
const span = tracer.startSpan('process-payment');
span.setAttribute('payment.id', 'PAY-12345');
// ... business logic ...
span.end();

// Get trace ID for log correlation:
import { getTraceId } from 'centricity-observability';
const traceId = getTraceId();
logger.info({ trace_id: traceId }, 'processing');
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
| Credit Card | `4111-1111-1111-1111` | `****-****-****-1111` |
| Sensitive fields | `password`, `secret`, `token` | `[REDACTED]` |

**Safe patterns** (never redacted): URLs, file paths, UUIDs, order IDs.

---

## Middleware Order

Apply tracing first, then metrics:

```typescript
app.use(HTTPTracingMiddleware('my-service'));   // ← first
app.use(HTTPMetricsMiddleware('my-service'));   // ← second
```

This ensures trace context is available before metrics are recorded.

## Graceful Shutdown

```typescript
// On SIGINT / SIGTERM:
stopMetricsPusher();            // Flushes remaining metrics to Kafka
await shutdownTracer();          // Flushes remaining spans
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
| `METRICS_PUSH_INTERVAL_MS` | `15000` | Metrics push interval |

See the [root README](../README.md) for the full environment variable reference.

## Example

See [`examples/node-http-service`](../examples/node-http-service/) for a complete working Express service.
