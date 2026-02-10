package logging

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LogCheckpoint tracks the state of log delivery
type LogCheckpoint struct {
	// LastSentOffset is the byte offset of the last successfully sent log
	LastSentOffset int64 `json:"last_sent_offset"`
	// LastSentTime is when the last log was sent
	LastSentTime time.Time `json:"last_sent_time"`
	// PendingCount is the number of logs waiting to be sent
	PendingCount int64 `json:"pending_count"`
	// TotalSent is the total number of logs sent to Kafka
	TotalSent int64 `json:"total_sent"`
	// TotalBuffered is the total number of logs written to fallback
	TotalBuffered int64 `json:"total_buffered"`
	// TotalReplayed is the total number of logs replayed from fallback
	TotalReplayed int64 `json:"total_replayed"`
	// LastError is the last error message (if any)
	LastError string `json:"last_error,omitempty"`
	// LastErrorTime is when the last error occurred
	LastErrorTime time.Time `json:"last_error_time,omitempty"`
	// KafkaHealthy indicates current Kafka health status
	KafkaHealthy bool `json:"kafka_healthy"`
}

// CheckpointManager manages log delivery checkpoints
type CheckpointManager struct {
	filePath   string
	checkpoint *LogCheckpoint
	mu         sync.RWMutex
	dirty      bool
	ticker     *time.Ticker
	done       chan struct{}
}

// NewCheckpointManager creates a new checkpoint manager
func NewCheckpointManager(dir string) (*CheckpointManager, error) {
	filePath := filepath.Join(dir, ".checkpoint.json")

	cm := &CheckpointManager{
		filePath: filePath,
		checkpoint: &LogCheckpoint{
			KafkaHealthy: true,
		},
		done: make(chan struct{}),
	}

	// Load existing checkpoint if exists
	if err := cm.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	// Start periodic save
	cm.ticker = time.NewTicker(5 * time.Second)
	go cm.periodicSave()

	return cm, nil
}

// RecordSent records a successful Kafka send
func (cm *CheckpointManager) RecordSent(count int64) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.checkpoint.TotalSent += count
	cm.checkpoint.LastSentTime = time.Now()
	if cm.checkpoint.PendingCount > 0 {
		cm.checkpoint.PendingCount -= count
		if cm.checkpoint.PendingCount < 0 {
			cm.checkpoint.PendingCount = 0
		}
	}
	cm.dirty = true
}

// RecordBuffered records logs written to fallback buffer
func (cm *CheckpointManager) RecordBuffered(count int64) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.checkpoint.TotalBuffered += count
	cm.checkpoint.PendingCount += count
	cm.dirty = true
}

// RecordReplayed records logs replayed from fallback
func (cm *CheckpointManager) RecordReplayed(count int64) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.checkpoint.TotalReplayed += count
	cm.checkpoint.PendingCount -= count
	if cm.checkpoint.PendingCount < 0 {
		cm.checkpoint.PendingCount = 0
	}
	cm.dirty = true
}

// RecordError records a Kafka error
func (cm *CheckpointManager) RecordError(err string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.checkpoint.LastError = err
	cm.checkpoint.LastErrorTime = time.Now()
	cm.dirty = true
}

// SetKafkaHealth updates Kafka health status
func (cm *CheckpointManager) SetKafkaHealth(healthy bool) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.checkpoint.KafkaHealthy = healthy
	cm.dirty = true
}

// GetStatus returns current checkpoint status
func (cm *CheckpointManager) GetStatus() LogCheckpoint {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return *cm.checkpoint
}

// GetPendingCount returns number of pending logs
func (cm *CheckpointManager) GetPendingCount() int64 {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return cm.checkpoint.PendingCount
}

// Close saves and closes the checkpoint manager
func (cm *CheckpointManager) Close() error {
	close(cm.done)
	cm.ticker.Stop()
	return cm.save()
}

func (cm *CheckpointManager) load() error {
	data, err := os.ReadFile(cm.filePath)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, cm.checkpoint)
}

func (cm *CheckpointManager) save() error {
	cm.mu.RLock()
	if !cm.dirty {
		cm.mu.RUnlock()
		return nil
	}
	data, err := json.MarshalIndent(cm.checkpoint, "", "  ")
	cm.mu.RUnlock()

	if err != nil {
		return err
	}

	// Ensure directory exists
	dir := filepath.Dir(cm.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	if err := os.WriteFile(cm.filePath, data, 0644); err != nil {
		return err
	}

	cm.mu.Lock()
	cm.dirty = false
	cm.mu.Unlock()

	return nil
}

func (cm *CheckpointManager) periodicSave() {
	for {
		select {
		case <-cm.done:
			return
		case <-cm.ticker.C:
			cm.save()
		}
	}
}
