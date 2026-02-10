package metrics

import (
	"os"
)

var pusher *Pusher

// StartMetricsPusher initializes and starts the metrics pusher
func StartMetricsPusher(serviceName string) error {
	cfg := NewPusherConfig(serviceName)

	// In development mode, don't push to Kafka
	if getEnv("ENVIRONMENT", "production") == "development" {
		return nil
	}

	// Check if Kafka export is enabled
	if !cfg.EnableKafka {
		return nil
	}

	p, err := NewPusher(cfg)
	if err != nil {
		return err
	}

	if p == nil {
		// No Kafka configured
		os.Stderr.WriteString("Warning: KAFKA_BROKERS not set, metrics push disabled\n")
		return nil
	}

	pusher = p
	pusher.Start()

	return nil
}

// StopMetricsPusher gracefully stops the metrics pusher
func StopMetricsPusher() error {
	if pusher != nil {
		return pusher.Stop()
	}
	return nil
}

// RecordPayment is a helper to record payment metrics
func RecordPayment(amount float64, currency, status string) {
	// This would be extended with actual payment metrics
	// For now, we just track in the http metrics
}
