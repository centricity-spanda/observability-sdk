export { newLogger } from './logger';
export type { Logger } from './logger';
export { newLogConfig, isDevelopment } from './config';
export type { LogConfig } from './config';
export { redactLogEvent } from './pii-redactor';
export { initKafkaWriter, writeLogToKafka, closeKafkaWriter } from './kafka-writer';
