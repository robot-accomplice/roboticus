package api

import (
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// LogEntry represents a single captured log entry.
type LogEntry struct {
	Timestamp string         `json:"timestamp"`
	Level     string         `json:"level"`
	Message   string         `json:"message"`
	Fields    map[string]any `json:"fields,omitempty"`
}

// LogRingBuffer is a thread-safe fixed-size ring buffer for structured log capture.
// Go improvement over roboticus: instead of reading log files from disk (fragile,
// file rotation issues), we capture logs directly from the zerolog pipeline via
// the io.Writer interface. This is simpler and more reliable.
type LogRingBuffer struct {
	mu      sync.RWMutex
	entries []LogEntry
	maxSize int
	head    int
	count   int
}

// NewLogRingBuffer creates a ring buffer with the given capacity.
func NewLogRingBuffer(maxSize int) *LogRingBuffer {
	if maxSize < 100 {
		maxSize = 100
	}
	return &LogRingBuffer{
		entries: make([]LogEntry, maxSize),
		maxSize: maxSize,
	}
}

// Write implements io.Writer for zerolog integration.
// It parses JSON log lines from zerolog and stores them in the ring buffer.
func (b *LogRingBuffer) Write(p []byte) (n int, err error) {
	var raw map[string]any
	if err := json.Unmarshal(p, &raw); err != nil {
		// Not JSON — store as plain text.
		b.push(LogEntry{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Level:     "info",
			Message:   strings.TrimSpace(string(p)),
		})
		return len(p), nil
	}

	entry := LogEntry{
		Timestamp: stringFromMap(raw, "time", time.Now().UTC().Format(time.RFC3339)),
		Level:     stringFromMap(raw, "level", "info"),
		Message:   stringFromMap(raw, "message", ""),
	}

	// Collect remaining fields.
	fields := make(map[string]any)
	for k, v := range raw {
		if k == "time" || k == "level" || k == "message" {
			continue
		}
		fields[k] = v
	}
	if len(fields) > 0 {
		entry.Fields = fields
	}

	b.push(entry)
	return len(p), nil
}

func (b *LogRingBuffer) push(entry LogEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries[b.head] = entry
	b.head = (b.head + 1) % b.maxSize
	if b.count < b.maxSize {
		b.count++
	}
}

// Tail returns the most recent n entries, optionally filtered by level.
func (b *LogRingBuffer) Tail(n int, level string) []LogEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if n <= 0 || n > b.count {
		n = b.count
	}

	result := make([]LogEntry, 0, n)
	// Start from oldest visible entry and walk forward.
	start := (b.head - b.count + b.maxSize) % b.maxSize
	for i := 0; i < b.count && len(result) < n; i++ {
		idx := (start + i) % b.maxSize
		e := b.entries[idx]
		if level != "" && e.Level != level {
			continue
		}
		result = append(result, e)
	}

	// We want the most recent n, so take the tail.
	if len(result) > n {
		result = result[len(result)-n:]
	}
	return result
}

func stringFromMap(m map[string]any, key, fallback string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return fallback
}
