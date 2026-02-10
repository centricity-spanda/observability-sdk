package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Registry is the global Prometheus registry for custom metrics
var Registry = prometheus.NewRegistry()

// Pre-registered HTTP metrics
var (
	// HTTPRequestsTotal counts HTTP requests by service, method, path, and status
	HTTPRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"service", "method", "path", "status"},
	)

	// HTTPRequestDuration tracks HTTP request duration in seconds
	HTTPRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"service", "method", "path"},
	)

	// HTTPRequestsInFlight tracks current in-flight requests
	HTTPRequestsInFlight = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "http_requests_in_flight",
			Help: "Current number of in-flight HTTP requests",
		},
		[]string{"service"},
	)

	// KafkaProducerMessagesTotal counts Kafka messages sent
	KafkaProducerMessagesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kafka_producer_messages_total",
			Help: "Total Kafka messages sent",
		},
		[]string{"service", "topic", "status"},
	)

	// KafkaProducerBufferUsage tracks producer buffer usage
	KafkaProducerBufferUsage = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_producer_buffer_usage",
			Help: "Kafka producer buffer usage (0-1)",
		},
		[]string{"service"},
	)
)

func init() {
	// Register all metrics with our registry
	Registry.MustRegister(
		HTTPRequestsTotal,
		HTTPRequestDuration,
		HTTPRequestsInFlight,
		KafkaProducerMessagesTotal,
		KafkaProducerBufferUsage,
	)

	// Also register Go runtime metrics
	Registry.MustRegister(prometheus.NewGoCollector())
	Registry.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
}
