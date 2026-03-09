package logging

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds logging configuration
type Config struct {
	// ServiceName identifies the service in logs
	ServiceName string

	// ServiceNamespace is the logical namespace of the service (e.g. "broking")
	ServiceNamespace string

	// Environment is the deployment environment (production, staging, development)
	Environment string

	// ServiceVersion is the version of the service
	ServiceVersion string

	// HostName is the host name where the service is running (optional)
	HostName string

	// K8sPodName is the Kubernetes pod name (optional)
	K8sPodName string

	// K8sNamespaceName is the Kubernetes namespace name (optional)
	K8sNamespaceName string

	// K8sNodeName is the Kubernetes node name (optional)
	K8sNodeName string

	// KafkaBrokers is a comma-separated list of Kafka broker addresses
	KafkaBrokers []string

	// LogTopic is the Kafka topic for logs
	LogTopic string

	// LogLevel is the minimum log level (debug, info, warn, error)
	LogLevel string

	// LogType is either "standard" or "audit" (audit has stricter durability)
	LogType string

	// ChannelBufferSize is the size of the async producer channel
	ChannelBufferSize int

	// FlushInterval is how often to flush buffered logs
	FlushInterval time.Duration

	// EnableKafka enables/disables Kafka logging export
	EnableKafka bool

	// EnableFile enables/disables file logging
	EnableFile bool

	// LogFilePath is the path for file logging (if enabled)
	LogFilePath string

	// EnablePIIRedaction enables/disables PII redaction in logs
	EnablePIIRedaction bool

	// EnableConsole enables/disables console (stdout) logging
	EnableConsole bool

	// EnableFallback enables file fallback when Kafka fails (with auto-replay)
	EnableFallback bool

	// Team is the owning team for the service (used in log attributes)
	Team string
}

// NewConfig creates a Config from environment variables
func NewConfig(serviceName string) *Config {
	cfg := &Config{
		ServiceName:        serviceName,
		ServiceNamespace:   getEnv("SERVICE_NAMESPACE", "default"),
		Environment:        getEnv("ENVIRONMENT", "production"),
		ServiceVersion:     getEnv("SERVICE_VERSION", "unknown"),
		HostName:           getEnv("HOST_NAME", os.Getenv("HOSTNAME")),
		K8sPodName:         getEnv("K8S_POD_NAME", ""),
		K8sNamespaceName:   getEnv("K8S_NAMESPACE_NAME", ""),
		K8sNodeName:        getEnv("K8S_NODE_NAME", ""),
		LogTopic:           getEnv("KAFKA_LOG_TOPIC", "logs.application"),
		LogLevel:           getEnv("LOG_LEVEL", "info"),
		LogType:            getEnv("LOG_TYPE", "standard"),
		ChannelBufferSize:  4096,
		FlushInterval:      100 * time.Millisecond,
		EnableKafka:        getEnvBool("LOG_KAFKA_ENABLED", true),
		EnableFile:         getEnvBool("LOG_FILE_ENABLED", false),
		LogFilePath:        getEnv("LOG_FILE_PATH", "./logs/app.log"),
		EnablePIIRedaction: getEnvBool("LOG_PII_REDACTION_ENABLED", true),
		EnableConsole:      getEnvBool("LOG_CONSOLE_ENABLED", true),
		EnableFallback:     getEnvBool("LOG_KAFKA_FALLBACK_ENABLED", true),
		Team:               getEnv("SERVICE_TEAM", ""),
	}

	// Parse Kafka brokers
	brokers := getEnv("KAFKA_BROKERS", "")
	if brokers != "" {
		cfg.KafkaBrokers = strings.Split(brokers, ",")
	}

	return cfg
}

// IsDevelopment returns true if running in development mode
func (c *Config) IsDevelopment() bool {
	return c.Environment == "development"
}

// IsAuditLog returns true if this is configured for audit logging
func (c *Config) IsAuditLog() bool {
	return c.LogType == "audit"
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
	b, err := strconv.ParseBool(value)
	if err != nil {
		return defaultValue
	}
	return b
}
