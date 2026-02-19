import { Kafka, Producer, ProducerRecord } from 'kafkajs';
import { Registry, Metric } from 'prom-client';
import { ProtobufMetricsSerializer } from '@opentelemetry/otlp-transformer';
import { resourceFromAttributes } from '@opentelemetry/resources';

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
        } catch (error) {
            console.error('Failed to push metrics to Kafka', error);
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

        // TODO: Handle Histogram (requires bucketing logic matching SDK)

        return null;
    }
}
