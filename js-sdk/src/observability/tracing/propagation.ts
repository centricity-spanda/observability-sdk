/**
 * Trace context propagation and span access.
 * Aligns with Go ExtractContext, SpanFromContext, GetTraceIDFromContext, etc.
 */

import { context, trace, INVALID_SPAN_CONTEXT } from '@opentelemetry/api';
import type { Span } from '@opentelemetry/api';

/**
 * Get the current span from the active context.
 */
export function spanFromContext(ctx: ReturnType<typeof context.active>): Span {
  return trace.getSpan(ctx) ?? trace.wrapSpanContext(INVALID_SPAN_CONTEXT);
}

/**
 * Get the current trace ID from the active span context.
 * Returns hex string or empty if no valid span.
 */
export function getTraceId(): string {
  const span = trace.getActiveSpan();
  if (!span) return '';
  const spanContext = span.spanContext();
  if (!spanContext.traceId) return '';
  return spanContext.traceId;
}

/**
 * Get trace ID from a given context.
 */
export function getTraceIdFromContext(ctx: ReturnType<typeof context.active>): string {
  const span = trace.getSpan(ctx);
  if (!span) return '';
  const spanContext = span.spanContext();
  return spanContext.traceId ?? '';
}

/**
 * Get span ID from a given context.
 */
export function getSpanIdFromContext(ctx: ReturnType<typeof context.active>): string {
  const span = trace.getSpan(ctx);
  if (!span) return '';
  const spanContext = span.spanContext();
  return spanContext.spanId ?? '';
}

export { context };
