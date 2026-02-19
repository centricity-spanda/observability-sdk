/**
 * Example Node HTTP service with observability SDK.
 * Mirrors examples/go-http-service and examples/python-fastapi-service.
 */

import 'dotenv/config';
import express, { Request, Response } from 'express';
import {
  newLogger,
  newTracer,
  getTraceId,
  startMetricsPusher,
  stopMetricsPusher,
  HTTPTracingMiddleware,
  HTTPMetricsMiddleware,
} from 'centricity-observability';

const SERVICE_NAME = process.env.SERVICE_NAME ?? 'example-service-node';
const PORT = process.env.PORT ?? '8088';

// Initialize observability (sync where possible)
const logger = newLogger(SERVICE_NAME);

async function main() {
  // Start metrics pusher (no-op in development or when Kafka disabled; non-fatal if Kafka unreachable)
  try {
    await startMetricsPusher(SERVICE_NAME);
  } catch (err) {
    logger.warn({ err }, 'metrics pusher failed to start; continuing without Kafka metrics');
  }

  // Initialize tracer and set global provider
  const { shutdown: shutdownTracer } = newTracer(SERVICE_NAME);

  const app = express();
  app.use(express.json());

  // Middleware: tracing first, then metrics (same order as Go/Python examples)
  app.use(HTTPTracingMiddleware(SERVICE_NAME));
  app.use(HTTPMetricsMiddleware(SERVICE_NAME));

  app.get('/health', (_req: Request, res: Response) => {
    res.status(200).json({ status: 'healthy' });
  });

  app.post('/api/payment', (req: Request, res: Response) => {
    const traceId = getTraceId();
    console.log('traceId', traceId);
    logger.info(
      { trace_id: traceId, payment_id: 'PAY-12345', amount: 1500.5 },
      'processing payment'
    );

    // Simulate work
    setTimeout(() => {
      logger.info({ payment_id: 'PAY-12345' }, 'payment completed');
      res.status(200).json({ status: 'completed', payment_id: 'PAY-12345' });
    }, 100);
  });

  app.get('/api/users', (req: Request, res: Response) => {
    const traceId = getTraceId();
    // Log with PII-like fields to test redaction (when LOG_PII_REDACTION_ENABLED=true)
    logger.info(
      {
        trace_id: traceId,
        password: 'secret123',
        secret: 'my-secret-key',
        token: 'bearer-token-xyz',
        api_key: 'sk_test_123456',
        email: 'john.doe@example.com',
        order_id: 'ORD-123456789',
        url: 'https://example.com/user/12345',
      },
      'user profile accessed'
    );

    res.status(200).json({ user_id: 'USR-001', name: 'John Doe' });
  });

  const server = app.listen(PORT, () => {
    logger.info({ addr: `:${PORT}` }, 'starting server');
  });

  const shutdown = async () => {
    logger.info('shutting down server');
    stopMetricsPusher();
    await shutdownTracer();
    server.close(() => process.exit(0));
  };

  process.on('SIGINT', shutdown);
  process.on('SIGTERM', shutdown);
}

main().catch((err) => {
  logger.error({ err }, 'failed to start');
  process.exit(1);
});
