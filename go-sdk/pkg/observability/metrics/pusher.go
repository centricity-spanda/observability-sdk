package metrics

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
)

// Pusher pushes metrics to an OTEL collector via OTLP gRPC periodically.
type Pusher struct {
	conn        *grpc.ClientConn
	client      v1.MetricsServiceClient
	registry    *prometheus.Registry
	serviceName string
	endpoint    string
	interval    time.Duration
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

// PusherConfig holds pusher configuration.
type PusherConfig struct {
	ServiceName string
	Endpoint    string
	Interval    time.Duration
}

// NewPusherConfig creates config from environment (mirrors Python metrics pusher).
func NewPusherConfig(serviceName string) *PusherConfig {
	intervalStr := getEnv("METRICS_PUSH_INTERVAL", "15s")
	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		interval = 15 * time.Second
	}

	endpoint := getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4317")
	// Strip scheme if present (e.g. http://host:4317 -> host:4317)
	if strings.Contains(endpoint, "://") {
		parts := strings.SplitN(endpoint, "://", 2)
		if len(parts) == 2 {
			endpoint = parts[1]
		}
	}

	return &PusherConfig{
		ServiceName: serviceName,
		Endpoint:    endpoint,
		Interval:    interval,
	}
}

// NewPusher creates a new metrics pusher backed by OTLP gRPC.
func NewPusher(cfg *PusherConfig) (*Pusher, error) {
	if cfg.Endpoint == "" {
		return nil, nil
	}

	conn, err := grpc.NewClient(cfg.Endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}

	client := v1.NewMetricsServiceClient(conn)

	return &Pusher{
		conn:        conn,
		client:      client,
		registry:    Registry,
		serviceName: cfg.ServiceName,
		endpoint:    cfg.Endpoint,
		interval:    cfg.Interval,
	}, nil
}

// Start begins the background push loop.
func (p *Pusher) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	p.wg.Add(1)
	go p.pushLoop(ctx)
}

// Stop gracefully stops the pusher and closes the gRPC connection.
func (p *Pusher) Stop() error {
	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
	if p.conn != nil {
		return p.conn.Close()
	}
	return nil
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
	if p.client == nil {
		return
	}

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

	// Marshal for debugging/logging if needed (not required for gRPC call)
	_, err = proto.Marshal(otlpMetrics)
	if err != nil {
		os.Stderr.WriteString("Failed to marshal OTLP metrics: " + err.Error() + "\n")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := p.client.Export(ctx, otlpMetrics); err != nil {
		os.Stderr.WriteString("Failed to push metrics via OTLP: " + err.Error() + "\n")
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
	if key == "ENVIRONMENT" {
		for _, k := range []string{"ENVIRONMENT", "ENV", "environment", "env"} {
			if v := os.Getenv(k); v != "" {
				return v
			}
		}
	}
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
