package logging

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

// FileWriter provides file-based logging with rotation and compression
type FileWriter struct {
	logger *lumberjack.Logger
	mu     sync.RWMutex
	closed bool
}

// FileWriterConfig holds file writer configuration
type FileWriterConfig struct {
	// FilePath is the path to the log file
	FilePath string
	// MaxSizeMB is the maximum size in megabytes before rotation
	MaxSizeMB int
	// MaxBackups is the maximum number of old log files to retain
	MaxBackups int
	// MaxAgeDays is the maximum number of days to retain old log files
	MaxAgeDays int
	// Compress determines if rotated files should be compressed
	Compress bool
	// LocalTime uses local time for backup file names (default is UTC)
	LocalTime bool
}

// NewFileWriterConfig creates config from environment variables
func NewFileWriterConfig() *FileWriterConfig {
	return &FileWriterConfig{
		FilePath:   getEnv("LOG_FILE_PATH", "./logs/app.log"),
		MaxSizeMB:  getEnvInt("LOG_FILE_MAX_SIZE_MB", 100),
		MaxBackups: getEnvInt("LOG_FILE_MAX_BACKUPS", 5),
		MaxAgeDays: getEnvInt("LOG_FILE_MAX_AGE_DAYS", 30),
		Compress:   getEnvBool("LOG_FILE_COMPRESS", true),
		LocalTime:  getEnvBool("LOG_FILE_LOCAL_TIME", true),
	}
}

// NewFileWriter creates a new file writer with rotation support
func NewFileWriter(cfg *FileWriterConfig) (*FileWriter, error) {
	// Create directory if needed
	dir := filepath.Dir(cfg.FilePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	logger := &lumberjack.Logger{
		Filename:   cfg.FilePath,
		MaxSize:    cfg.MaxSizeMB,
		MaxBackups: cfg.MaxBackups,
		MaxAge:     cfg.MaxAgeDays,
		Compress:   cfg.Compress,
		LocalTime:  cfg.LocalTime,
	}

	return &FileWriter{
		logger: logger,
	}, nil
}

// Write implements io.Writer
func (fw *FileWriter) Write(p []byte) (n int, err error) {
	fw.mu.RLock()
	if fw.closed {
		fw.mu.RUnlock()
		return 0, nil
	}
	fw.mu.RUnlock()

	return fw.logger.Write(p)
}

// Sync flushes any buffered data
func (fw *FileWriter) Sync() error {
	// lumberjack handles sync internally
	return nil
}

// Close closes the file writer
func (fw *FileWriter) Close() error {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	if fw.closed {
		return nil
	}
	fw.closed = true

	return fw.logger.Close()
}

// Rotate forces a log rotation
func (fw *FileWriter) Rotate() error {
	return fw.logger.Rotate()
}

// FallbackBuffer provides buffered file storage for Kafka failures
type FallbackBuffer struct {
	writer      *FileWriter
	bufferDir   string
	mu          sync.Mutex
	currentFile *os.File
	lineCount   int
}

// FallbackBufferConfig holds fallback buffer configuration
type FallbackBufferConfig struct {
	BufferDir  string
	MaxSizeMB  int
	MaxFiles   int
	MaxAgeDays int
	Compress   bool
}

// NewFallbackBufferConfig creates config from environment
func NewFallbackBufferConfig() *FallbackBufferConfig {
	return &FallbackBufferConfig{
		BufferDir:  getEnv("LOG_FALLBACK_DIR", "./logs/buffer"),
		MaxSizeMB:  getEnvInt("LOG_FALLBACK_MAX_SIZE_MB", 100),
		MaxFiles:   getEnvInt("LOG_FALLBACK_MAX_FILES", 10),
		MaxAgeDays: getEnvInt("LOG_FALLBACK_MAX_AGE_DAYS", 7),
		Compress:   getEnvBool("LOG_FALLBACK_COMPRESS", true),
	}
}

// NewFallbackBuffer creates a new fallback buffer
func NewFallbackBuffer(cfg *FallbackBufferConfig) (*FallbackBuffer, error) {
	// Create buffer directory
	if err := os.MkdirAll(cfg.BufferDir, 0755); err != nil {
		return nil, err
	}

	writerCfg := &FileWriterConfig{
		FilePath:   filepath.Join(cfg.BufferDir, "buffer.log"),
		MaxSizeMB:  cfg.MaxSizeMB,
		MaxBackups: cfg.MaxFiles,
		MaxAgeDays: cfg.MaxAgeDays,
		Compress:   cfg.Compress,
		LocalTime:  true,
	}

	writer, err := NewFileWriter(writerCfg)
	if err != nil {
		return nil, err
	}

	return &FallbackBuffer{
		writer:    writer,
		bufferDir: cfg.BufferDir,
	}, nil
}

// Write writes data to the fallback buffer
func (fb *FallbackBuffer) Write(p []byte) (n int, err error) {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	fb.lineCount++
	return fb.writer.Write(p)
}

// Close closes the fallback buffer
func (fb *FallbackBuffer) Close() error {
	return fb.writer.Close()
}

// HasPendingData checks if there are buffered logs to replay
func (fb *FallbackBuffer) HasPendingData() bool {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	return fb.lineCount > 0
}

// ReplayTo replays buffered logs to a writer (e.g., Kafka)
func (fb *FallbackBuffer) ReplayTo(ctx context.Context, writer func([]byte) error) error {
	fb.mu.Lock()
	defer fb.mu.Unlock()

	// Find all buffer files
	files, err := filepath.Glob(filepath.Join(fb.bufferDir, "buffer*.log"))
	if err != nil {
		return err
	}

	for _, path := range files {
		if err := fb.replayFile(ctx, path, writer); err != nil {
			return err
		}
		// Remove replayed file
		os.Remove(path)
	}

	fb.lineCount = 0
	return nil
}

func (fb *FallbackBuffer) replayFile(ctx context.Context, path string, writer func([]byte) error) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// Increase buffer size for long log lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			line := scanner.Bytes()
			if len(line) > 0 {
				if err := writer(line); err != nil {
					return err
				}
				// Small delay to avoid overwhelming Kafka
				time.Sleep(1 * time.Millisecond)
			}
		}
	}

	return scanner.Err()
}

func getEnvInt(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	var result int
	if _, err := parseIntString(value, &result); err != nil {
		return defaultValue
	}
	return result
}

func parseIntString(s string, result *int) (bool, error) {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false, nil
		}
		*result = *result*10 + int(c-'0')
	}
	return true, nil
}
