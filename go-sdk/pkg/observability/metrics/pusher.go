package metrics

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"
)

// Pusher pushes metrics to Kafka periodically
type Pusher struct {
	producer    sarama.SyncProducer
	registry    *prometheus.Registry
	serviceName string
	topic       string
	interval    time.Duration
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

// PusherConfig holds pusher configuration
type PusherConfig struct {
	ServiceName  string
	KafkaBrokers []string
	Topic        string
	Interval     time.Duration
	EnableKafka  bool
}

// NewPusherConfig creates config from environment
func NewPusherConfig(serviceName string) *PusherConfig {
	interval, _ := time.ParseDuration(getEnv("METRICS_PUSH_INTERVAL", "15s"))

	cfg := &PusherConfig{
		ServiceName: serviceName,
		Topic:       getEnv("KAFKA_METRICS_TOPIC", "metrics.application"),
		Interval:    interval,
		EnableKafka: getEnvBool("METRICS_KAFKA_ENABLED", true),
	}

	brokers := getEnv("KAFKA_BROKERS", "")
	if brokers != "" {
		cfg.KafkaBrokers = strings.Split(brokers, ",")
	}

	return cfg
}

// NewPusher creates a new metrics pusher
func NewPusher(cfg *PusherConfig) (*Pusher, error) {
	if len(cfg.KafkaBrokers) == 0 {
		return nil, nil // No Kafka, metrics exposed via /metrics only
	}

	saramaConfig := sarama.NewConfig()
	saramaConfig.Producer.RequiredAcks = sarama.WaitForLocal
	saramaConfig.Producer.Compression = sarama.CompressionSnappy
	saramaConfig.Producer.Return.Successes = true

	producer, err := sarama.NewSyncProducer(cfg.KafkaBrokers, saramaConfig)
	if err != nil {
		return nil, err
	}

	return &Pusher{
		producer:    producer,
		registry:    Registry,
		serviceName: cfg.ServiceName,
		topic:       cfg.Topic,
		interval:    cfg.Interval,
	}, nil
}

// Start begins the background push loop
func (p *Pusher) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	p.wg.Add(1)
	go p.pushLoop(ctx)
}

// Stop gracefully stops the pusher
func (p *Pusher) Stop() error {
	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
	return p.producer.Close()
}

func (p *Pusher) pushLoop(ctx context.Context) {
	defer p.wg.Done()

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Final push before exit
			p.push()
			return
		case <-ticker.C:
			p.push()
		}
	}
}

func (p *Pusher) push() {
	// Gather metrics from Prometheus registry
	mfs, err := p.registry.Gather()
	if err != nil {
		os.Stderr.WriteString("Failed to gather metrics: " + err.Error() + "\n")
		return
	}

	// Convert to OTLP format
	otlpMetrics := p.convertToOTLP(mfs)
	if otlpMetrics == nil {
		return
	}

	// Marshal to protobuf
	data, err := proto.Marshal(otlpMetrics)
	if err != nil {
		os.Stderr.WriteString("Failed to marshal OTLP metrics: " + err.Error() + "\n")
		return
	}

	// Send to Kafka
	msg := &sarama.ProducerMessage{
		Topic:     p.topic,
		Key:       sarama.StringEncoder(p.serviceName),
		Value:     sarama.ByteEncoder(data),
		Timestamp: time.Now(),
	}

	if _, _, err := p.producer.SendMessage(msg); err != nil {
		os.Stderr.WriteString("Failed to push metrics to Kafka: " + err.Error() + "\n")
		KafkaProducerMessagesTotal.WithLabelValues(p.serviceName, p.topic, "error").Inc()
	} else {
		KafkaProducerMessagesTotal.WithLabelValues(p.serviceName, p.topic, "success").Inc()
	}
}

// convertToOTLP converts Prometheus metrics to OTLP format
func (p *Pusher) convertToOTLP(mfs []*dto.MetricFamily) *v1.ExportMetricsServiceRequest {
	now := time.Now()
	timeNanos := uint64(now.UnixNano())

	var metrics []*metricspb.Metric

	for _, mf := range mfs {
		for _, m := range mf.GetMetric() {
			metric := &metricspb.Metric{
				Name:        mf.GetName(),
				Description: mf.GetHelp(),
			}

			// Convert labels to attributes
			var attributes []*commonpb.KeyValue
			for _, label := range m.GetLabel() {
				attributes = append(attributes, &commonpb.KeyValue{
					Key: label.GetName(),
					Value: &commonpb.AnyValue{
						Value: &commonpb.AnyValue_StringValue{
							StringValue: label.GetValue(),
						},
					},
				})
			}

			// Convert based on metric type
			switch mf.GetType() {
			case dto.MetricType_COUNTER:
				metric.Data = &metricspb.Metric_Sum{
					Sum: &metricspb.Sum{
						AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE,
						IsMonotonic:            true,
						DataPoints: []*metricspb.NumberDataPoint{
							{
								Attributes:        attributes,
								TimeUnixNano:      timeNanos,
								Value:             &metricspb.NumberDataPoint_AsDouble{AsDouble: m.GetCounter().GetValue()},
							},
						},
					},
				}

			case dto.MetricType_GAUGE:
				metric.Data = &metricspb.Metric_Gauge{
					Gauge: &metricspb.Gauge{
						DataPoints: []*metricspb.NumberDataPoint{
							{
								Attributes:   attributes,
								TimeUnixNano: timeNanos,
								Value:        &metricspb.NumberDataPoint_AsDouble{AsDouble: m.GetGauge().GetValue()},
							},
						},
					},
				}

			case dto.MetricType_HISTOGRAM:
				hist := m.GetHistogram()
				var bucketCounts []uint64
				var explicitBounds []float64

				for _, bucket := range hist.GetBucket() {
					bucketCounts = append(bucketCounts, bucket.GetCumulativeCount())
					explicitBounds = append(explicitBounds, bucket.GetUpperBound())
				}

				metric.Data = &metricspb.Metric_Histogram{
					Histogram: &metricspb.Histogram{
						AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE,
						DataPoints: []*metricspb.HistogramDataPoint{
							{
								Attributes:     attributes,
								TimeUnixNano:   timeNanos,
								Count:          hist.GetSampleCount(),
								Sum:            func() *float64 { v := hist.GetSampleSum(); return &v }(),
								BucketCounts:   bucketCounts,
								ExplicitBounds: explicitBounds,
							},
						},
					},
				}

			case dto.MetricType_SUMMARY:
				summary := m.GetSummary()
				var quantiles []*metricspb.SummaryDataPoint_ValueAtQuantile

				for _, q := range summary.GetQuantile() {
					quantiles = append(quantiles, &metricspb.SummaryDataPoint_ValueAtQuantile{
						Quantile: q.GetQuantile(),
						Value:    q.GetValue(),
					})
				}

				metric.Data = &metricspb.Metric_Summary{
					Summary: &metricspb.Summary{
						DataPoints: []*metricspb.SummaryDataPoint{
							{
								Attributes:     attributes,
								TimeUnixNano:   timeNanos,
								Count:          summary.GetSampleCount(),
								Sum:            summary.GetSampleSum(),
								QuantileValues: quantiles,
							},
						},
					},
				}
			}

			metrics = append(metrics, metric)
		}
	}

	return &v1.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{
						{
							Key: "service.name",
							Value: &commonpb.AnyValue{
								Value: &commonpb.AnyValue_StringValue{
									StringValue: p.serviceName,
								},
							},
						},
					},
				},
				ScopeMetrics: []*metricspb.ScopeMetrics{
					{
						Metrics: metrics,
					},
				},
			},
		},
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value == "true" || value == "1" || value == "yes"
}
