import { register, collectDefaultMetrics } from 'prom-client';
import { IncomingMessage, ServerResponse } from 'http';
import { KafkaMetricsPusher } from './kafka-pusher';
import { getEnv, getEnvBool, getEnvNumber } from '../env';

let pusher: KafkaMetricsPusher | null = null;

export function startMetricsPusher(serviceName: string) {
    if (pusher) {
        console.warn('Metrics pusher already started');
        return;
    }

    // Skip Kafka push in development mode (matching Go/Python SDKs)
    if (getEnv('ENVIRONMENT', 'production') === 'development') {
        console.log('Metrics Kafka pusher disabled in development mode');
        collectDefaultMetrics({ prefix: '' });
        return;
    }

    // Default metrics
    collectDefaultMetrics({ prefix: '' });

    // Config
    const brokers = getEnv('KAFKA_BROKERS', '').split(',').filter(b => b.length > 0);
    const topic = getEnv('KAFKA_METRICS_TOPIC', 'metrics.application');
    const interval = getEnvNumber('METRICS_PUSH_INTERVAL_MS', 15000);
    const enabled = getEnvBool('METRICS_KAFKA_ENABLED', true);

    if (!enabled) {
        console.log('Metrics Kafka pusher disabled by config');
        return;
    }

    pusher = new KafkaMetricsPusher(register, serviceName, topic, brokers, interval);
    pusher.start();
}

export function stopMetricsPusher() {
    if (pusher) {
        pusher.stop();
        pusher = null;
    }
}

// Simple middleware to track HTTP metrics
// We will follow the Go SDK implementation:
// - http_requests_total (counter) [service, method, path, status]
// - http_request_duration_seconds (histogram) [service, method, path]
// - http_requests_in_flight (gauge) [service]

import { Counter, Histogram, Gauge } from 'prom-client';

const httpRequestsTotal = new Counter({
    name: 'http_requests_total',
    help: 'Total number of HTTP requests',
    labelNames: ['service', 'method', 'path', 'status']
});

const httpRequestDuration = new Histogram({
    name: 'http_request_duration_seconds',
    help: 'Duration of HTTP requests in seconds',
    labelNames: ['service', 'method', 'path'],
    buckets: [0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10]
});

const httpRequestsInFlight = new Gauge({
    name: 'http_requests_in_flight',
    help: 'Current number of in-flight HTTP requests',
    labelNames: ['service']
});

export function HTTPMetricsMiddleware(serviceName: string) {
    return (req: IncomingMessage, res: ServerResponse, next: () => void) => {
        const start = process.hrtime();
        const url = req.url || '/';
        const path = normalizePath(url.split('?')[0]);
        const method = req.method || 'GET';

        httpRequestsInFlight.inc({ service: serviceName });

        const originalEnd = res.end.bind(res);
        // @ts-ignore - Monkey patching end
        res.end = function (...args: any[]) {
            // @ts-ignore
            const result = originalEnd.apply(this, args);

            // In Node http, statusCode is on the response object
            const statusCode = res.statusCode.toString();

            httpRequestsInFlight.dec({ service: serviceName });

            const diff = process.hrtime(start);
            const durationInSeconds = diff[0] + diff[1] / 1e9;

            httpRequestsTotal.inc({
                service: serviceName,
                method: method,
                path: path,
                status: statusCode
            });

            httpRequestDuration.observe({
                service: serviceName,
                method: method,
                path: path
            }, durationInSeconds);

            return result;
        };

        next();
    };
}

function normalizePath(path: string): string {
    // Basic normalization to avoid cardinality explosion
    // Replace IDs with :id or similar if possible.
    // For now, simple length check as in Go SDK.
    if (path.length > 50) {
        return path.substring(0, 50) + '...';
    }
    return path;
}
