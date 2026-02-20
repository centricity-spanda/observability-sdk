/**
 * HTTP tracing middleware for Express/Connect (Node) and Next.js backend.
 * Creates a server span per request and propagates context.
 */

import { trace, context, propagation, defaultTextMapGetter, SpanKind } from '@opentelemetry/api';
import type { IncomingMessage, ServerResponse } from 'http';

export interface RequestHandler {
  (req: IncomingMessage, res: ServerResponse, next: () => void): void;
}

/**
 * Express/Connect-style HTTP tracing middleware.
 * Usage: app.use(HTTPTracingMiddleware('my-service'))
 */
export function HTTPTracingMiddleware(serviceName: string): RequestHandler {
  const tracer = trace.getTracer(serviceName);

  return function middleware(
    req: IncomingMessage,
    res: ServerResponse,
    next: () => void
  ): void {
    const method = req.method ?? 'GET';
    const path = (req.url ?? '/').split('?')[0];
    const spanName = `${method} ${path}`;

    const headers = req.headers as Record<string, string | string[] | undefined>;
    const parentContext = propagation.extract(context.active(), headers, defaultTextMapGetter);

    const span = tracer.startSpan(spanName, {
      kind: SpanKind.SERVER,
      attributes: {
        'http.method': method,
        'http.route': path,
        'http.scheme': (req as unknown as { protocol?: string }).protocol?.replace(':', '') ?? 'http',
        'http.host': req.headers?.host ?? '',
        'http.user_agent': req.headers?.['user-agent'] ?? '',
      },
    }, parentContext);

    let statusCode = 500;
    const originalWriteHead = res.writeHead.bind(res);
    res.writeHead = function (this: ServerResponse, code: number, ...args: unknown[]): ServerResponse {
      statusCode = code ?? 500;
      return (originalWriteHead as (code: number, ...a: unknown[]) => ServerResponse)(code, ...args);
    };

    const originalEnd = res.end.bind(res);
    res.end = function (...args: unknown[]): ServerResponse {
      // Prefer res.statusCode (Express sets it via res.status(); Node sets it in writeHead)
      const code = res.statusCode ?? statusCode;
      span.setAttribute('http.status_code', code);
      if (code >= 400) {
        span.setStatus({ code: 2, message: `HTTP ${code}` });
      }
      span.end();
      return (originalEnd as (...a: unknown[]) => ServerResponse)(...args);
    };

    const spanCtx = trace.setSpan(parentContext, span);
    context.with(spanCtx, (n: () => void): void => { n(); }, undefined, next as () => void);
  };
}
