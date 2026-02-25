/**
 * Example Node HTTP service with observability SDK.
 * Single init: one logger, one tracer, one metrics pusher; use getLogger()/getTracer().
 * Mirrors examples/go-http-service and examples/python-fastapi-service.
 */

import 'dotenv/config';
import express, { Request, Response } from 'express';
import { HTTPTracingMiddleware, HTTPMetricsMiddleware } from 'centricity-observability';
import {
  initializeObservability,
  getLogger,
  getTraceId,
  shutdown as observabilityShutdown,
} from './observability';

const SERVICE_NAME = process.env.SERVICE_NAME ?? 'example-service-node';
const PORT = process.env.PORT ?? '8088';

async function main() {
  await initializeObservability(SERVICE_NAME);

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
    getLogger().info(
      { trace_id: traceId, payment_id: 'PAY-12345', amount: 1500.5 },
      'processing payment'
    );

    // Simulate work
    setTimeout(() => {
      getLogger().info({ payment_id: 'PAY-12345' }, 'payment completed');
      res.status(200).json({ status: 'completed', payment_id: 'PAY-12345' });
    }, 100);
  });

  app.get('/api/users', (req: Request, res: Response) => {
    const traceId = getTraceId();
    // Log with PII-like fields to test redaction (when LOG_PII_REDACTION_ENABLED=true)
    getLogger().info(
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
    getLogger().info({ addr: `:${PORT}` }, 'starting server');
  });

  const shutdown = async () => {
    getLogger().info('shutting down server');
    await observabilityShutdown();
    server.close(() => process.exit(0));
  };

  process.on('SIGINT', shutdown);
  process.on('SIGTERM', shutdown);
}

main().catch((err) => {
  try {
    getLogger().error({ err }, 'failed to start');
  } catch {
    console.error('failed to start', err);
  }
  process.exit(1);
});
