/**
 * Tracer factory - creates OpenTelemetry tracer with optional Kafka exporter.
 * Aligns with Go NewTracer and Python new_tracer.
 */

import { trace } from '@opentelemetry/api';
import {
  NodeTracerProvider,
  BatchSpanProcessor,
  ParentBasedSampler,
  TraceIdRatioBasedSampler,
} from '@opentelemetry/sdk-trace-node';
import { resourceFromAttributes } from '@opentelemetry/resources';
import { W3CTraceContextPropagator } from '@opentelemetry/core';
import { getEnv, getEnvBool, getEnvNumber } from '../env';
import { KafkaSpanExporter } from './kafka-exporter';
import { getTraceId } from './propagation';

let tracerProvider: NodeTracerProvider | null = null;

export interface TracerResult {
  /** The OpenTelemetry Tracer instance */
  tracer: ReturnType<typeof trace.getTracer>;
  /** Shutdown the tracer provider (call on process exit) */
  shutdown: () => Promise<void>;
}

/**
 * Create a new OpenTelemetry tracer with optional Kafka exporter.
 * In development (ENVIRONMENT=development) or when TRACES_KAFKA_ENABLED=false, no Kafka exporter is added.
 */
export function newTracer(serviceName: string): TracerResult {
  const environment = getEnv('ENVIRONMENT', 'production');
  const serviceVersion = getEnv('SERVICE_VERSION', 'unknown');
  const samplingRate = getEnvNumber('TRACE_SAMPLING_RATE', 1.0);
  const enableKafka = getEnvBool('TRACES_KAFKA_ENABLED', true);
  const brokers = getEnv('KAFKA_BROKERS', '');
  const topic = getEnv('KAFKA_TRACES_TOPIC', 'traces.application');

  const resource = resourceFromAttributes({
    'service.name': serviceName,
    'service.version': serviceVersion,
    'deployment.environment': environment,
  });

  const sampler = new ParentBasedSampler({
    root: new TraceIdRatioBasedSampler(samplingRate),
  });

  const spanProcessors: InstanceType<typeof BatchSpanProcessor>[] = [];

  if (environment !== 'development' && enableKafka && brokers) {
    const kafkaBrokers = brokers.split(',').map((b) => b.trim());
    const exporter = new KafkaSpanExporter({
      serviceName,
      kafkaBrokers,
      topic,
    });
    spanProcessors.push(new BatchSpanProcessor(exporter));
  }

  const provider = new NodeTracerProvider({
    resource,
    sampler,
    spanProcessors: spanProcessors.length > 0 ? spanProcessors : undefined,
  });

  provider.register({
    propagator: new W3CTraceContextPropagator(),
  });

  tracerProvider = provider;

  const otelTracer = trace.getTracer(serviceName, serviceVersion);

  return {
    tracer: otelTracer,
    shutdown: async () => {
      if (tracerProvider) {
        await tracerProvider.shutdown();
        tracerProvider = null;
      }
    },
  };
}

export { getTraceId };
