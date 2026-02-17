package logging

import (
	"os"
	"path/filepath"

	"go.uber.org/zap"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

var piiRedactor *PIIRedactor

// NewLogger creates a new structured logger with configurable outputs
func NewLogger(serviceName string) (*zap.Logger, error) {
	cfg := NewConfig(serviceName)

	// Initialize PII redactor if enabled
	if cfg.EnablePIIRedaction {
		piiRedactor = NewPIIRedactor()
	}

	// Configure encoder
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "message",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.MillisDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	// Parse log level
	level := parseLogLevel(cfg.LogLevel)

	// Create cores based on configuration
	var cores []zapcore.Core

	// Create encoder (with PII redaction if enabled)
	var encoder zapcore.Encoder
	if cfg.EnablePIIRedaction {
		encoder = NewRedactingEncoder(zapcore.NewJSONEncoder(encoderConfig), piiRedactor)
	} else {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	}

	// Console output (stdout)
	if cfg.EnableConsole {
		stdoutCore := zapcore.NewCore(
			encoder,
			zapcore.AddSync(os.Stdout),
			level,
		)
		cores = append(cores, stdoutCore)
	}

	// File output with rotation
	if cfg.EnableFile {
		fileWriterCfg := NewFileWriterConfig()
		fileWriter, err := NewFileWriter(fileWriterCfg)
		if err != nil {
			os.Stderr.WriteString("Warning: Failed to create log file: " + err.Error() + "\n")
		} else {
			fileCore := zapcore.NewCore(
				encoder,
				zapcore.AddSync(fileWriter),
				level,
			)
			cores = append(cores, fileCore)
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
			kafkaCore := zapcore.NewCore(
				encoder,
				kafkaWriter,
				level,
			)
			cores = append(cores, kafkaCore)
		}
	}

	// Ensure at least one core exists
	if len(cores) == 0 {
		// Fallback to stdout
		cores = append(cores, zapcore.NewCore(
			encoder,
			zapcore.AddSync(os.Stdout),
			level,
		))
	}

	// Combine cores
	core := zapcore.NewTee(cores...)

	// Create logger with default fields
	logger := zap.New(core,
		zap.AddCaller(),
		zap.AddStacktrace(zapcore.ErrorLevel),
	).With(
		zap.String("service", cfg.ServiceName),
		zap.String("environment", cfg.Environment),
		zap.String("version", cfg.ServiceVersion),
	)

	return logger, nil
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

// RedactingEncoder wraps a zapcore.Encoder to apply PII redaction
type RedactingEncoder struct {
	zapcore.Encoder
	redactor *PIIRedactor
}

// NewRedactingEncoder creates a new encoder with PII redaction
func NewRedactingEncoder(enc zapcore.Encoder, redactor *PIIRedactor) *RedactingEncoder {
	return &RedactingEncoder{
		Encoder:  enc,
		redactor: redactor,
	}
}

// Clone implements zapcore.Encoder
func (e *RedactingEncoder) Clone() zapcore.Encoder {
	return &RedactingEncoder{
		Encoder:  e.Encoder.Clone(),
		redactor: e.redactor,
	}
}

// EncodeEntry applies PII redaction to the log message
func (e *RedactingEncoder) EncodeEntry(entry zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	// Redact the message
	entry.Message = e.redactor.Redact(entry.Message)

	// Redact fields - delegate all logic to RedactValue
	for i := range fields {
		switch fields[i].Type {
		case zapcore.StringType:
			// Apply pattern matching to string values
			fields[i].String = e.redactor.Redact(fields[i].String)

		case zapcore.ReflectType:
			// Delegate complex types to RedactValue (handles maps, slices, etc.)
			if fields[i].Interface != nil {
				fields[i].Interface = e.redactor.RedactValue(fields[i].Interface)
			}
		
		case zapcore.StringerType:
			// Convert stringer to string and redact
			if fields[i].Interface != nil {
				if s, ok := fields[i].Interface.(interface{ String() string }); ok {
					fields[i].Type = zapcore.StringType
					fields[i].String = e.redactor.Redact(s.String())
					fields[i].Interface = nil
				}
			}

		case zapcore.ErrorType:
			// Redact error messages
			if fields[i].Interface != nil {
				if err, ok := fields[i].Interface.(error); ok {
					fields[i].Type = zapcore.StringType
					fields[i].String = e.redactor.Redact(err.Error())
					fields[i].Interface = nil
				}
			}
		}
	}

	return e.Encoder.EncodeEntry(entry, fields)
}
