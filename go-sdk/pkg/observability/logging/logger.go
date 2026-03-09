package logging

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var piiRedactor *PIIRedactor

// NewLogger creates a new structured logger with configurable outputs
// and enforces the standard envelope log format.
func NewLogger(serviceName string) (*zap.Logger, error) {
	cfg := NewConfig(serviceName)

	// Initialize PII redactor if enabled
	if cfg.EnablePIIRedaction {
		piiRedactor = NewPIIRedactor()
	}

	// Parse log level
	level := parseLogLevel(cfg.LogLevel)

	// Create cores based on configuration
	var cores []zapcore.Core

	// Console output (stdout)
	if cfg.EnableConsole {
		core := NewEnvelopeCore(zapcore.AddSync(os.Stdout), level, cfg)
		cores = append(cores, core)
	}

	// File output with rotation
	if cfg.EnableFile {
		fileWriterCfg := NewFileWriterConfig()
		fileWriter, err := NewFileWriter(fileWriterCfg)
		if err != nil {
			os.Stderr.WriteString("Warning: Failed to create log file: " + err.Error() + "\n")
		} else {
			core := NewEnvelopeCore(zapcore.AddSync(fileWriter), level, cfg)
			cores = append(cores, core)
		}
	}

	// Kafka output (only in non-development mode and if enabled)
	if cfg.EnableKafka {
		var kafkaWriter zapcore.WriteSyncer

		// Use resilient writer with fallback if enabled
		if cfg.EnableFallback {
			resilientWriter, err := NewResilientKafkaWriter(cfg)
			if err != nil {
				os.Stderr.WriteString("Warning: Failed to initialize resilient Kafka writer: " + err.Error() + "\n")
			} else if resilientWriter != nil {
				kafkaWriter = zapcore.AddSync(resilientWriter)
			}
		} else {
			// Use standard Kafka writer
			stdWriter, err := NewKafkaWriter(cfg)
			if err != nil {
				os.Stderr.WriteString("Warning: Failed to initialize Kafka writer: " + err.Error() + "\n")
			} else if stdWriter != nil {
				kafkaWriter = zapcore.AddSync(stdWriter)
			}
		}

		if kafkaWriter != nil {
			core := NewEnvelopeCore(kafkaWriter, level, cfg)
			cores = append(cores, core)
		}
	}

	// Ensure at least one core exists
	if len(cores) == 0 {
		// Fallback to stdout
		cores = append(cores, NewEnvelopeCore(zapcore.AddSync(os.Stdout), level, cfg))
	}

	// Combine cores
	core := zapcore.NewTee(cores...)

	// Create logger with caller and stacktrace; service metadata is part of the envelope
	logger := zap.New(core,
		zap.AddCaller(),
		zap.AddStacktrace(zapcore.ErrorLevel),
	)

	return logger, nil
}

// EnvelopeCore is a zapcore.Core implementation that writes logs
// in the standard envelope JSON format expected by the observability stack.
type EnvelopeCore struct {
	ws     zapcore.WriteSyncer
	level  zapcore.LevelEnabler
	cfg    *Config
	fields []zapcore.Field
}

// NewEnvelopeCore creates a new EnvelopeCore for a given sink.
func NewEnvelopeCore(ws zapcore.WriteSyncer, level zapcore.LevelEnabler, cfg *Config) zapcore.Core {
	return &EnvelopeCore{
		ws:    ws,
		level: level,
		cfg:   cfg,
	}
}

// Enabled implements zapcore.Core.
func (c *EnvelopeCore) Enabled(lvl zapcore.Level) bool {
	return c.level.Enabled(lvl)
}

// With implements zapcore.Core.
func (c *EnvelopeCore) With(fields []zapcore.Field) zapcore.Core {
	clone := *c
	clone.fields = append(clone.fields, fields...)
	return &clone
}

// Check implements zapcore.Core.
func (c *EnvelopeCore) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return ce.AddCore(entry, c)
	}
	return ce
}

// Write implements zapcore.Core.
func (c *EnvelopeCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	// Merge context fields and entry fields
	allFields := make([]zapcore.Field, 0, len(c.fields)+len(fields))
	allFields = append(allFields, c.fields...)
	allFields = append(allFields, fields...)

	// Convert fields to a map
	attrMap := fieldsToMap(allFields)

	// Apply PII redaction if enabled
	if piiRedactor != nil && c.cfg.EnablePIIRedaction {
		attrMap = redactAttributes(attrMap, piiRedactor)
		entry.Message = piiRedactor.Redact(entry.Message)
	}

	// Extract error block if present
	errorBlock := make(map[string]interface{})
	if errVal, ok := attrMap["error"]; ok {
		if m, ok := errVal.(map[string]interface{}); ok {
			errorBlock = m
			delete(attrMap, "error")
		}
	}

	// Build service block (k8s fields are optional)
	serviceBlock := map[string]interface{}{
		"service.name":           c.cfg.ServiceName,
		"service.version":        c.cfg.ServiceVersion,
		"service.namespace":      c.cfg.ServiceNamespace,
		"deployment.environment": c.cfg.Environment,
	}
	if c.cfg.HostName != "" {
		serviceBlock["host.name"] = c.cfg.HostName
	}
	if c.cfg.K8sPodName != "" {
		serviceBlock["k8s.pod.name"] = c.cfg.K8sPodName
	}
	if c.cfg.K8sNamespaceName != "" {
		serviceBlock["k8s.namespace.name"] = c.cfg.K8sNamespaceName
	}
	if c.cfg.K8sNodeName != "" {
		serviceBlock["k8s.node.name"] = c.cfg.K8sNodeName
	}

	// Inject standard attributes
	if _, ok := attrMap["log.type"]; !ok {
		logType := c.cfg.LogType
		if logType == "" {
			logType = "app"
		}
		attrMap["log.type"] = logType
	}
	if c.cfg.Team != "" {
		if _, ok := attrMap["team"]; !ok {
			attrMap["team"] = c.cfg.Team
		}
	}

	// Build envelope
	timestamp := entry.Time.UTC().Format(time.RFC3339Nano)

	envelope := map[string]interface{}{
		"timestamp":    timestamp,
		"severity":     toSeverityString(entry.Level),
		"severity_num": severityToNumber(entry.Level),
		"message":      entry.Message,
		"service":      serviceBlock,
		"attributes":   attrMap,
		"error":        errorBlock,
	}

	// Marshal and write
	data, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	if _, err := c.ws.Write(append(data, '\n')); err != nil {
		return err
	}

	if entry.Level >= zapcore.ErrorLevel {
		_ = c.Sync()
	}

	return nil
}

// Sync implements zapcore.Core.
func (c *EnvelopeCore) Sync() error {
	return c.ws.Sync()
}

// fieldsToMap converts zap fields to a map[string]interface{}.
func fieldsToMap(fields []zapcore.Field) map[string]interface{} {
	enc := zapcore.NewMapObjectEncoder()
	for i := range fields {
		fields[i].AddTo(enc)
	}
	return enc.Fields
}

// redactAttributes applies PII redaction to the attributes map.
func redactAttributes(attrs map[string]interface{}, redactor *PIIRedactor) map[string]interface{} {
	if redactor == nil || attrs == nil {
		return attrs
	}
	redacted := redactor.RedactValue(attrs)
	if m, ok := redacted.(map[string]interface{}); ok {
		return m
	}
	return attrs
}

// toSeverityString converts zap level to upper-case severity string.
func toSeverityString(level zapcore.Level) string {
	switch level {
	case zapcore.DebugLevel:
		return "DEBUG"
	case zapcore.InfoLevel:
		return "INFO"
	case zapcore.WarnLevel:
		return "WARN"
	case zapcore.ErrorLevel:
		return "ERROR"
	case zapcore.DPanicLevel, zapcore.PanicLevel, zapcore.FatalLevel:
		return "FATAL"
	default:
		return "INFO"
	}
}

// severityToNumber maps zap levels to OpenTelemetry-style numeric severity.
func severityToNumber(level zapcore.Level) int {
	switch level {
	case zapcore.DebugLevel:
		return 5
	case zapcore.InfoLevel:
		return 9
	case zapcore.WarnLevel:
		return 13
	case zapcore.ErrorLevel:
		return 17
	case zapcore.DPanicLevel, zapcore.PanicLevel, zapcore.FatalLevel:
		return 21
	default:
		return 9
	}
}

func parseLogLevel(level string) zapcore.Level {
	switch level {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

func createFileWriter(path string) (*os.File, error) {
	// Create directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	// Open file with append mode
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
}
