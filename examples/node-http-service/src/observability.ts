/**
 * Centralized observability singleton for the example service.
 * Single init: one logger, one tracer, one metrics pusher; use getLogger()/getTracer().
 */

import {
  newLogger,
  newTracer,
  getTraceId,
  startMetricsPusher,
  stopMetricsPusher,
} from 'centricity-observability';

let logger: ReturnType<typeof newLogger> | null = null;
let tracer: ReturnType<typeof newTracer>['tracer'] | null = null;
let shutdownTracer: (() => Promise<void>) | null = null;
let initialized = false;

/**
 * Initialize observability once at startup: logger, tracer, metrics pusher.
 */
export async function initializeObservability(serviceName: string): Promise<void> {
  if (initialized) {
    return;
  }
  logger = newLogger(serviceName);
  try {
    await startMetricsPusher(serviceName);
  } catch (err) {
    logger.warn({ err }, 'metrics pusher failed to start; continuing without Kafka metrics');
  }
  const result = newTracer(serviceName);
  tracer = result.tracer;
  shutdownTracer = result.shutdown;
  initialized = true;
}

export function getLogger(): ReturnType<typeof newLogger> {
  if (logger === null) {
    throw new Error('Observability not initialized. Call initializeObservability() first.');
  }
  return logger;
}

export function getTracer(): NonNullable<typeof tracer> {
  if (tracer === null) {
    throw new Error('Observability not initialized. Call initializeObservability() first.');
  }
  return tracer;
}

export { getTraceId };

/**
 * Shutdown observability (metrics pusher and tracer provider). Call on process exit.
 */
export async function shutdown(): Promise<void> {
  stopMetricsPusher();
  if (shutdownTracer) {
    await shutdownTracer();
    shutdownTracer = null;
  }
  tracer = null;
  logger = null;
  initialized = false;
}
