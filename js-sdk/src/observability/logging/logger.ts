/**
 * EnvelopeLogger — structured logger that emits the unified Centricity observability
 * log envelope on every write:
 *
 *  {
 *    "timestamp":    "...",
 *    "severity":     "INFO",
 *    "severity_num": 9,
 *    "message":      "...",
 *    "service":      { "service.name": "...", ... },
 *    "attributes":   { "trace_id": "...", "span_id": "...", "parent_span_id": "...", ... },
 *    "error":        {}
 *  }
 *
 * OTel trace context (trace_id, span_id, parent_span_id, trace_flags) is auto-injected
 * from the active span at log time. Caller-supplied values always take precedence.
 */

import fs from 'fs';
import path from 'path';
import { context, trace, TraceFlags } from '@opentelemetry/api';
import { newLogConfig, isDevelopment, LogConfig } from './config';
import { redactLogEvent } from './pii-redactor';
import { initKafkaWriter, writeLogToKafka } from './kafka-writer';

// ── Severity mapping ────────────────────────────────────────────────────────

const SEVERITY_NUM: Record<string, number> = {
  TRACE: 1,
  DEBUG: 5,
  INFO: 9,
  WARN: 13,
  WARNING: 13,
  ERROR: 17,
  FATAL: 21,
};

function toSeverityNum(level: string): number {
  return SEVERITY_NUM[level.toUpperCase()] ?? 9;
}

// ── Attribute types ──────────────────────────────────────────────────────────

export type LogAttributes = Record<string, unknown>;

export interface LogError {
  message?: string;
  type?: string;
  stack?: string;
  [key: string]: unknown;
}

// ── OTel trace context injection ─────────────────────────────────────────────

function getOtelTraceContext(): Partial<LogAttributes> {
  const ctx: Partial<LogAttributes> = {};
  try {
    const span = trace.getSpan(context.active());
    if (!span) return ctx;
    const sc = span.spanContext();
    if (!sc || !trace.isSpanContextValid(sc)) return ctx;

    ctx['trace_id'] = sc.traceId;
    ctx['span_id'] = sc.spanId;
    ctx['trace_flags'] = (sc.traceFlags ?? TraceFlags.NONE)
      .toString(16)
      .padStart(2, '0');

    // parent_span_id: the SDK exposes it via the internal _parentSpanId field
    const parentSpanId: string | undefined =
      (span as unknown as { parentSpanId?: string }).parentSpanId ??
      (span as unknown as { _parentSpanId?: string })._parentSpanId;
    if (parentSpanId) ctx['parent_span_id'] = parentSpanId;
  } catch {
    // never throw from trace extraction
  }
  return ctx;
}

// ── EnvelopeLogger ───────────────────────────────────────────────────────────

export class EnvelopeLogger {
  private readonly cfg: LogConfig;
  private fileStream: fs.WriteStream | null = null;
  private kafkaReady = false;

  constructor(serviceName: string) {
    this.cfg = newLogConfig(serviceName);
    this._initFile();
    this._initKafka();
  }

  // ── Public log methods ────────────────────────────────────────────────────

  debug(message: string, attributes?: LogAttributes, error?: LogError): void {
    this._write('DEBUG', message, attributes, error);
  }

  info(message: string, attributes?: LogAttributes, error?: LogError): void {
    this._write('INFO', message, attributes, error);
  }

  warn(message: string, attributes?: LogAttributes, error?: LogError): void {
    this._write('WARN', message, attributes, error);
  }

  error(message: string, attributes?: LogAttributes, error?: LogError): void {
    this._write('ERROR', message, attributes, error);
  }

  fatal(message: string, attributes?: LogAttributes, error?: LogError): void {
    this._write('FATAL', message, attributes, error);
  }

  // ── Envelope builder ──────────────────────────────────────────────────────

  private _write(
    severity: string,
    message: string,
    attributes: LogAttributes = {},
    error: LogError = {},
  ): void {
    const level = severity.toUpperCase();
    const minLevel = toSeverityNum(this.cfg.logLevel);
    if (toSeverityNum(level) < minLevel) return;

    // Merge caller attributes with OTel trace context
    // Caller-supplied values win over auto-injected trace fields
    const traceCtx = getOtelTraceContext();
    const mergedAttrs: LogAttributes = { ...traceCtx, ...attributes };

    // Inject standard defaults
    if (!('log.type' in mergedAttrs)) mergedAttrs['log.type'] = this.cfg.logType || 'app';
    if (this.cfg.team && !('team' in mergedAttrs)) mergedAttrs['team'] = this.cfg.team;

    // Apply PII redaction to attributes
    const finalAttrs = this.cfg.enablePiiRedaction
      ? (redactLogEvent(mergedAttrs as Record<string, unknown>) as LogAttributes)
      : mergedAttrs;

    // Build service block
    const serviceBlock: Record<string, string> = {
      'service.name': this.cfg.serviceName,
      'service.version': this.cfg.serviceVersion,
      'service.namespace': this.cfg.serviceNamespace,
      'deployment.environment': this.cfg.environment,
    };
    if (this.cfg.hostName) serviceBlock['host.name'] = this.cfg.hostName;
    if (this.cfg.k8sPodName) serviceBlock['k8s.pod.name'] = this.cfg.k8sPodName;
    if (this.cfg.k8sNamespaceName) serviceBlock['k8s.namespace.name'] = this.cfg.k8sNamespaceName;
    if (this.cfg.k8sNodeName) serviceBlock['k8s.node.name'] = this.cfg.k8sNodeName;

    const envelope = {
      timestamp: new Date().toISOString(),
      severity: level,
      severity_num: toSeverityNum(level),
      message,
      service: serviceBlock,
      attributes: finalAttrs,
      error: error ?? {},
    };

    const line = JSON.stringify(envelope) + '\n';

    // Write to configured outputs
    if (this.cfg.enableConsole) process.stdout.write(line);
    if (this.fileStream) this.fileStream.write(line);
    if (this.kafkaReady && !isDevelopment(this.cfg) && this.cfg.enableKafka) {
      writeLogToKafka(this.cfg.logTopic, line);
    }
  }

  // ── Transports ────────────────────────────────────────────────────────────

  private _initFile(): void {
    if (!this.cfg.enableFile) return;
    try {
      const dir = path.dirname(this.cfg.logFilePath);
      fs.mkdirSync(dir, { recursive: true });
      this.fileStream = fs.createWriteStream(this.cfg.logFilePath, { flags: 'a' });
      this.fileStream.on('error', () => {});
    } catch {
      process.stderr.write(`Warning: Failed to open log file: ${this.cfg.logFilePath}\n`);
    }
  }

  private _initKafka(): void {
    if (isDevelopment(this.cfg) || !this.cfg.enableKafka || !this.cfg.kafkaBrokers.length) return;
    initKafkaWriter({
      serviceName: this.cfg.serviceName,
      kafkaBrokers: this.cfg.kafkaBrokers,
      topic: this.cfg.logTopic,
    })
      .then(() => { this.kafkaReady = true; })
      .catch(() => {});
  }

  // ── Graceful shutdown ──────────────────────────────────────────────────

  /** Flush all transports and close connections. Call on app shutdown. */
  async shutdown(): Promise<void> {
    this.info('Logger shutting down');

    // Close Kafka producer
    if (this.kafkaReady) {
      try {
        await closeKafkaWriter();
        this.kafkaReady = false;
      } catch { /* best effort */ }
    }

    // Close file stream
    if (this.fileStream) {
      await new Promise<void>((resolve) => {
        this.fileStream!.end(() => resolve());
      });
      this.fileStream = null;
    }
  }
}

// ── Logger instance management ──────────────────────────────────────────────

let _defaultLogger: EnvelopeLogger | null = null;

/** Create a new logger and optionally register as default + auto-shutdown. */
export function newLogger(serviceName: string, autoShutdown = true): EnvelopeLogger {
  const logger = new EnvelopeLogger(serviceName);
  _defaultLogger = logger;

  // Auto-register shutdown handlers if requested
  if (autoShutdown) {
    const onShutdown = async () => {
      await logger.shutdown();
      process.exit(0);
    };
    process.once('SIGINT',  onShutdown);
    process.once('SIGTERM', onShutdown);
  }

  return logger;
}

/** Get the default logger instance (created by last newLogger call). */
export function getLogger(): EnvelopeLogger | null {
  return _defaultLogger;
}

export type Logger = EnvelopeLogger;

