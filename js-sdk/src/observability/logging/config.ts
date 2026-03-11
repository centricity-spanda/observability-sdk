/**
 * Logging configuration from environment variables.
 * Aligns with go-sdk and python-sdk env var names.
 */

import { getEnv, getEnvBool } from '../env';

export interface LogConfig {
  serviceName: string;
  serviceVersion: string;
  serviceNamespace: string;
  environment: string;
  team: string;
  hostName: string;
  k8sPodName: string;
  k8sNamespaceName: string;
  k8sNodeName: string;
  kafkaBrokers: string[];
  logTopic: string;
  logLevel: string;
  logType: string;
  bufferSize: number;
  enableKafka: boolean;
  enableFile: boolean;
  logFilePath: string;
  enableConsole: boolean;
  enablePiiRedaction: boolean;
  enableFallback?: boolean;
}

/**
 * Create LogConfig from environment variables.
 * Uses same env var names as Go and Python SDKs.
 */
export function newLogConfig(serviceName: string): LogConfig {
  const brokers = getEnv('KAFKA_BROKERS', '');
  return {
    serviceName,
    serviceVersion: getEnv('SERVICE_VERSION', 'unknown'),
    serviceNamespace: getEnv('SERVICE_NAMESPACE', 'default'),
    environment: getEnv('ENVIRONMENT', 'production'),
    team: getEnv('SERVICE_TEAM', ''),
    hostName: getEnv('HOST_NAME', process.env['HOSTNAME'] || ''),
    k8sPodName: getEnv('K8S_POD_NAME', ''),
    k8sNamespaceName: getEnv('K8S_NAMESPACE_NAME', ''),
    k8sNodeName: getEnv('K8S_NODE_NAME', ''),
    kafkaBrokers: brokers ? brokers.split(',').map((b) => b.trim()) : [],
    logTopic: getEnv('KAFKA_LOG_TOPIC', 'logs.application'),
    logLevel: getEnv('LOG_LEVEL', 'info'),
    logType: getEnv('LOG_TYPE', 'app'),
    bufferSize: 4096,
    enableKafka: getEnvBool('LOG_KAFKA_ENABLED', true),
    enableFile: getEnvBool('LOG_FILE_ENABLED', false),
    logFilePath: getEnv('LOG_FILE_PATH', './logs/app.log'),
    enableConsole: getEnvBool('LOG_CONSOLE_ENABLED', true),
    enablePiiRedaction: getEnvBool('LOG_PII_REDACTION_ENABLED', true),
    enableFallback: getEnvBool('LOG_KAFKA_FALLBACK_ENABLED', true),
  };
}

/** Check if running in development (Kafka export disabled in other modules). */
export function isDevelopment(config: LogConfig): boolean {
  return config.environment === 'development';
}
