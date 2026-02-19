/**
 * Optional Kafka writer for log records.
 * Used when LOG_KAFKA_ENABLED=true and not in development.
 */

import { Kafka } from 'kafkajs';

export interface KafkaWriterConfig {
  serviceName: string;
  kafkaBrokers: string[];
  topic: string;
}

let producer: ReturnType<Kafka['producer']> | null = null;

export async function initKafkaWriter(config: KafkaWriterConfig): Promise<void> {
  if (producer) return;
  const kafka = new Kafka({
    clientId: `observability-log-${config.serviceName}`,
    brokers: config.kafkaBrokers,
  });
  const p = kafka.producer();
  try {
    await p.connect();
    producer = p;
  } catch (err) {
    process.stderr.write(`Log Kafka writer: connect failed (logs to stdout only): ${err}\n`);
  }
}

export function writeLogToKafka(topic: string, value: string): void {
  if (!producer) return;
  producer.send({
    topic,
    messages: [{ value }],
  }).catch((err) => {
    process.stderr.write(`Log Kafka write error: ${err}\n`);
  });
}

export function closeKafkaWriter(): Promise<void> {
  if (producer) {
    const p = producer;
    producer = null;
    return p.disconnect();
  }
  return Promise.resolve();
}
