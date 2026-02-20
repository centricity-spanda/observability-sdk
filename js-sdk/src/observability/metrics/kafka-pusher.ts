import { Kafka, Producer, ProducerRecord } from 'kafkajs';
import { Registry, Metric } from 'prom-client';
import { ProtobufMetricsSerializer } from '@opentelemetry/otlp-transformer';
import { resourceFromAttributes } from '@opentelemetry/resources';

// Kafka producer metrics (matching Go/Python SDKs)
import { Counter, Gauge } from 'prom-client';

const kafkaProducerMessagesTotal = new Counter({
    name: 'kafka_producer_messages_total',
    help: 'Total Kafka messages sent',
    labelNames: ['service', 'topic', 'status']
});

const kafkaProducerBufferUsage = new Gauge({
    name: 'kafka_producer_buffer_usage',
    help: 'Kafka producer buffer usage (0-1)',
    labelNames: ['service']
});

export class KafkaMetricsPusher {
    private producer: Producer | null = null;
    private registry: Registry;
    private serviceName: string;
    private topic: string;
    private intervalMs: number;
    private intervalId: NodeJS.Timeout | null = null;
    private isConnected = false;
    private resource: any;
    private processStartTime: [number, number];

    constructor(
        registry: Registry,
        serviceName: string,
        topic: string,
        kafkaBrokers: string[],
        intervalMs: number = 15000
    ) {
        this.registry = registry;
        this.serviceName = serviceName;
        this.topic = topic;
        this.intervalMs = intervalMs;

        const now = Date.now();
        this.processStartTime = [Math.floor(now / 1000), (now % 1000) * 1000000];

        this.resource = resourceFromAttributes({
            'service.name': serviceName
        });

        if (kafkaBrokers.length > 0) {
            const kafka = new Kafka({
                clientId: `observability-metrics-${serviceName}`,
                brokers: kafkaBrokers,
            });
            this.producer = kafka.producer();
        }
    }

    async start() {
        if (!this.producer) {
            console.warn('Metrics Kafka producer not configured (no brokers). dedicated metrics push disabled.');
            return;
        }

        try {
            await this.producer.connect();
            this.isConnected = true;
            console.log(`Metrics Kafka producer connected. Pushing to ${this.topic} every ${this.intervalMs}ms`);

            this.intervalId = setInterval(() => {
                this.push();
            }, this.intervalMs);
        } catch (error) {
            console.error('Failed to connect metrics Kafka producer', error);
        }
    }

    async stop() {
        if (this.intervalId) {
            clearInterval(this.intervalId);
            this.intervalId = null;
        }
        if (this.isConnected && this.producer) {
            await this.push(); // Final push
            await this.producer.disconnect();
            this.isConnected = false;
        }
    }

    private async push() {
        if (!this.isConnected || !this.producer) return;

        try {
            // Cast to any[] because prom-client types might miss getMetricsAsJSON
            const promMetrics = await this.registry.getMetricsAsJSON() as any[];
            console.log(`Pushing ${promMetrics.length} metrics to Kafka`);

            const resourceMetrics = this.convertToResourceMetrics(promMetrics);
            const payload = ProtobufMetricsSerializer.serializeRequest(resourceMetrics);

            if (!payload) return;

            const record: ProducerRecord = {
                topic: this.topic,
                messages: [
                    {
                        key: this.serviceName,
                        value: Buffer.from(payload)
                    }
                ]
            };

            await this.producer.send(record);
            kafkaProducerMessagesTotal.inc({ service: this.serviceName, topic: this.topic, status: 'success' });
        } catch (error) {
            console.error('Failed to push metrics to Kafka', error);
            kafkaProducerMessagesTotal.inc({ service: this.serviceName, topic: this.topic, status: 'error' });
        }
    }

    private convertToResourceMetrics(promMetrics: any[]): any {
        // Construct ResourceMetrics object matching @opentelemetry/sdk-metrics interface

        const timestamp = Date.now();
        const hrTime: [number, number] = [Math.floor(timestamp / 1000), (timestamp % 1000) * 1000000];

        const metricData = promMetrics.map(pm => this.mapPrometheusToMetricData(pm, hrTime)).filter(m => m !== null);

        return {
            resource: this.resource,
            scopeMetrics: [{
                scope: { name: 'centricity-observability' },
                metrics: metricData
            }]
        };
    }

    private mapPrometheusToMetricData(pm: any, hrTime: [number, number]): any {
        const descriptor = {
            name: pm.name,
            description: pm.help,
            unit: '', // Prom client doesn't give unit
            valueType: 1 // ValueType.DOUBLE = 1
        };

        // For SDK objects, attributes are Record<string, AttributeValue>
        const attributes = (labels: any) => {
            return labels;
        };

        if (pm.type === 'counter') {
            return {
                descriptor,
                aggregationTemporality: 1, // CUMULATIVE (SDK enum: DELTA=0, CUMULATIVE=1)
                dataPointType: 3, // SUM
                isMonotonic: true,
                dataPoints: (pm as any).values.map((v: any) => ({
                    startTime: this.processStartTime, // Cumulative needs process start time
                    endTime: hrTime,
                    attributes: attributes(v.labels),
                    value: v.value
                }))
            };
        }

        if (pm.type === 'gauge') {
            return {
                descriptor,
                aggregationTemporality: 1, // CUMULATIVE (SDK enum: DELTA=0, CUMULATIVE=1)
                dataPointType: 2, // GAUGE
                dataPoints: (pm as any).values.map((v: any) => ({
                    startTime: hrTime, // Gauges are instantaneous
                    endTime: hrTime,
                    attributes: attributes(v.labels),
                    value: v.value
                }))
            };
        }

        if (pm.type === 'histogram') {
            return this.convertHistogram(pm, descriptor, hrTime);
        }

        if (pm.type === 'summary') {
            // Summary metrics from prom-client default metrics are rare;
            // skip gracefully for now (Go SDK handles them, but they are not
            // commonly encountered in typical prom-client usage)
            return null;
        }

        return null;
    }

    /**
     * Convert prom-client histogram to OTel SDK Histogram MetricData.
     *
     * prom-client getMetricsAsJSON() for a histogram returns:
     *   { name, help, type: 'histogram', values: [
     *       { labels: { ..., le: '0.005' }, value: 0, metricName: 'name_bucket' },
     *       ...
     *       { labels: { ..., le: '+Inf' }, value: N, metricName: 'name_bucket' },
     *       { labels: { ... }, value: N, metricName: 'name_sum' },
     *       { labels: { ... }, value: N, metricName: 'name_count' },
     *   ]}
     *
     * OTel transformer toHistogramDataPoints expects dataPoint.value = {
     *   buckets: { counts: number[], boundaries: number[] },
     *   count: number, sum: number, min: number, max: number
     * }
     * DataPointType.HISTOGRAM = 0
     */
    private convertHistogram(pm: any, descriptor: any, hrTime: [number, number]): any {
        const values: any[] = pm.values || [];

        // Group values by label set (excluding 'le' for buckets)
        // Each unique label combination (minus le) forms one HistogramDataPoint
        const groups = new Map<string, {
            bucketBounds: number[],
            bucketCounts: number[],
            count: number,
            sum: number,
            labels: Record<string, string>
        }>();

        for (const v of values) {
            const metricName: string = v.metricName || '';
            // Build a key from labels excluding 'le'
            const labelsCopy = { ...v.labels };
            delete labelsCopy.le;
            const key = JSON.stringify(labelsCopy);

            if (!groups.has(key)) {
                groups.set(key, {
                    bucketBounds: [],
                    bucketCounts: [],
                    count: 0,
                    sum: 0,
                    labels: labelsCopy
                });
            }
            const group = groups.get(key)!;

            if (metricName.endsWith('_bucket')) {
                const le = v.labels?.le;
                if (le !== undefined && le !== '+Inf') {
                    group.bucketBounds.push(parseFloat(le));
                    group.bucketCounts.push(v.value);
                } else if (le === '+Inf') {
                    // +Inf bucket count is the overflow bucket (total count)
                    // OTLP expects bucketCounts to have len(boundaries) + 1 entries
                    group.bucketCounts.push(v.value);
                }
            } else if (metricName.endsWith('_sum')) {
                group.sum = v.value;
            } else if (metricName.endsWith('_count')) {
                group.count = v.value;
            }
        }

        if (groups.size === 0) return null;

        const dataPoints = Array.from(groups.values()).map(group => ({
            startTime: this.processStartTime,
            endTime: hrTime,
            attributes: group.labels,
            value: {
                buckets: {
                    boundaries: group.bucketBounds,
                    counts: group.bucketCounts
                },
                count: group.count,
                sum: group.sum,
                min: undefined,
                max: undefined
            }
        }));

        return {
            descriptor,
            aggregationTemporality: 1, // CUMULATIVE (SDK enum)
            dataPointType: 0, // DataPointType.HISTOGRAM = 0
            dataPoints
        };
    }
}
