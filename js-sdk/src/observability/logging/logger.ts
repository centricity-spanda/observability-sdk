/**
 * Structured logger (pino) with optional PII redaction and Kafka (when not in development).
 */

import { Writable } from 'stream';
import pino from 'pino';
import { newLogConfig, isDevelopment } from './config';
import { redactLogEvent } from './pii-redactor';
import { initKafkaWriter, writeLogToKafka } from './kafka-writer';

export type Logger = pino.Logger;

/**
 * Create a new structured logger. Config from env (same var names as Go/Python).
 * When LOG_KAFKA_ENABLED=true and not in development, logs are also sent to Kafka (best-effort).
 */
export function newLogger(serviceName: string): Logger {
  const config = newLogConfig(serviceName);
  const level = config.logLevel.toLowerCase();
  const base: Record<string, unknown> = {
    service: serviceName,
    version: config.serviceVersion,
    environment: config.environment,
  };

  const serializers = config.enablePiiRedaction
    ? {
        ...pino.stdSerializers,
        [Symbol.for('pino.*')]: (obj: Record<string, unknown>) => redactLogEvent(obj),
      }
    : pino.stdSerializers;

  let dest: Writable = process.stdout;
  if (!isDevelopment(config) && config.enableKafka && config.kafkaBrokers.length > 0) {
    let kafkaReady = false;
    const topic = config.logTopic;
    initKafkaWriter({
      serviceName: config.serviceName,
      kafkaBrokers: config.kafkaBrokers,
      topic,
    })
      .then(() => {
        kafkaReady = true;
      })
      .catch(() => {});
    dest = new Writable({
      write(chunk: Buffer | string, _enc, cb) {
        const line = chunk.toString();
        process.stdout.write(chunk, (err) => {
          if (kafkaReady) writeLogToKafka(topic, line);
          cb(err);
        });
      },
    });
    dest.on('error', () => {});
  }

  return pino(
    {
      level,
      base,
      serializers: typeof serializers === 'object' ? serializers : undefined,
      formatters: { level: (label) => ({ level: label }) },
      timestamp: pino.stdTimeFunctions.isoTime,
    },
    dest
  );
}
