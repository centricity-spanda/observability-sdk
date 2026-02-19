/**
 * Kafka span exporter - sends spans to Kafka in OTLP protobuf format.
 * Same format as Go and Python SDKs so Jaeger/OTLP collectors can consume.
 */

import type { ReadableSpan, SpanExporter } from '@opentelemetry/sdk-trace-base';
import type { ExportResult } from '@opentelemetry/core';
import { Kafka } from 'kafkajs';
import { ProtobufTraceSerializer } from '@opentelemetry/otlp-transformer';

export interface KafkaExporterConfig {
  serviceName: string;
  kafkaBrokers: string[];
  topic: string;
}

/**
 * Kafka span exporter. Sends each batch to Kafka as OTLP protobuf (same as Go/Python).
 * Waits for producer connection on first export.
 */
export class KafkaSpanExporter implements SpanExporter {
  private producer: ReturnType<Kafka['producer']> | null = null;
  private topic: string;
  private serviceName: string;
  private closed = false;
  private connectPromise: Promise<void> | null = null;

  constructor(config: KafkaExporterConfig) {
    this.topic = config.topic;
    this.serviceName = config.serviceName;
    const kafka = new Kafka({
      clientId: `observability-trace-${config.serviceName}`,
      brokers: config.kafkaBrokers,
    });
    this.producer = kafka.producer();
  }

  private ensureConnected(): Promise<void> {
    if (!this.producer) return Promise.reject(new Error('Trace exporter closed or connect failed'));
    if (this.connectPromise) return this.connectPromise;
    this.connectPromise = this.producer.connect();
    this.connectPromise.catch((err) => {
      process.stderr.write(`Trace Kafka producer connect error: ${err}\n`);
      this.producer = null;
      this.connectPromise = null;
    });
    return this.connectPromise;
  }

  export(spans: ReadableSpan[], resultCallback: (result: ExportResult) => void): void {
    if (this.closed || !this.producer) {
      resultCallback({ code: 0 });
      return;
    }
    if (spans.length === 0) {
      resultCallback({ code: 0 });
      return;
    }
    let payload: Uint8Array | undefined;
    try {
      payload = ProtobufTraceSerializer.serializeRequest(spans);
    } catch (err) {
      process.stderr.write(`Trace OTLP serialize error: ${err}\n`);
      resultCallback({ code: 1 });
      return;
    }
    if (!payload) {
      resultCallback({ code: 1 });
      return;
    }
    const key = spans[0].spanContext().traceId;
    const value = Buffer.from(payload);

    this.ensureConnected()
      .then(() =>
        this.producer!.send({
          topic: this.topic,
          messages: [{ key, value }],
        })
      )
      .then(() => resultCallback({ code: 0 }))
      .catch((err) => {
        if (this.producer) {
          process.stderr.write(`Trace Kafka export error: ${err}\n`);
        }
        resultCallback({ code: 1 });
      });
  }

  shutdown(): Promise<void> {
    this.closed = true;
    if (this.producer) {
      return this.producer.disconnect();
    }
    return Promise.resolve();
  }

  forceFlush(): Promise<void> {
    return Promise.resolve();
  }
}
