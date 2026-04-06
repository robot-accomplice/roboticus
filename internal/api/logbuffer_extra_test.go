package api

import (
	"testing"
)

func TestNewLogRingBuffer_MinSize(t *testing.T) {
	// Sizes less than 100 should be clamped to 100.
	buf := NewLogRingBuffer(10)
	if buf.maxSize != 100 {
		t.Errorf("maxSize = %d, want 100 (minimum)", buf.maxSize)
	}
}

func TestNewLogRingBuffer_ExactMinSize(t *testing.T) {
	buf := NewLogRingBuffer(100)
	if buf.maxSize != 100 {
		t.Errorf("maxSize = %d, want 100", buf.maxSize)
	}
}

func TestNewLogRingBuffer_LargerSize(t *testing.T) {
	buf := NewLogRingBuffer(500)
	if buf.maxSize != 500 {
		t.Errorf("maxSize = %d, want 500", buf.maxSize)
	}
}

func TestLogRingBuffer_WriteJSONWithExtraFields(t *testing.T) {
	buf := NewLogRingBuffer(100)
	_, _ = buf.Write([]byte(`{"time":"2024-01-01","level":"error","message":"fail","component":"api","request_id":"abc123"}`))

	entries := buf.Tail(1, "")
	if len(entries) != 1 {
		t.Fatal("should have 1 entry")
	}
	e := entries[0]
	if e.Level != "error" {
		t.Errorf("level = %s", e.Level)
	}
	if e.Message != "fail" {
		t.Errorf("message = %s", e.Message)
	}
	if e.Fields == nil {
		t.Fatal("fields should not be nil")
	}
	if e.Fields["component"] != "api" {
		t.Errorf("fields[component] = %v", e.Fields["component"])
	}
	if e.Fields["request_id"] != "abc123" {
		t.Errorf("fields[request_id] = %v", e.Fields["request_id"])
	}
}

func TestLogRingBuffer_WriteJSONNoExtraFields(t *testing.T) {
	buf := NewLogRingBuffer(100)
	_, _ = buf.Write([]byte(`{"time":"2024-01-01","level":"info","message":"simple"}`))

	entries := buf.Tail(1, "")
	if len(entries) != 1 {
		t.Fatal("should have 1 entry")
	}
	if entries[0].Fields != nil {
		t.Errorf("fields should be nil for JSON with only standard fields, got %v", entries[0].Fields)
	}
}

func TestLogRingBuffer_Tail_ZeroN(t *testing.T) {
	buf := NewLogRingBuffer(100)
	buf.push(LogEntry{Level: "info", Message: "a"})
	buf.push(LogEntry{Level: "info", Message: "b"})
	buf.push(LogEntry{Level: "info", Message: "c"})

	// n=0 should return all entries.
	entries := buf.Tail(0, "")
	if len(entries) != 3 {
		t.Errorf("Tail(0) returned %d entries, want 3", len(entries))
	}
}

func TestLogRingBuffer_Tail_NegativeN(t *testing.T) {
	buf := NewLogRingBuffer(100)
	buf.push(LogEntry{Level: "info", Message: "a"})

	entries := buf.Tail(-1, "")
	if len(entries) != 1 {
		t.Errorf("Tail(-1) returned %d entries, want 1", len(entries))
	}
}

func TestLogRingBuffer_Tail_ExceedsCount(t *testing.T) {
	buf := NewLogRingBuffer(100)
	buf.push(LogEntry{Level: "info", Message: "only"})

	entries := buf.Tail(999, "")
	if len(entries) != 1 {
		t.Errorf("Tail(999) returned %d entries, want 1", len(entries))
	}
}

func TestLogRingBuffer_Tail_FilterReducesCount(t *testing.T) {
	buf := NewLogRingBuffer(100)
	for i := 0; i < 10; i++ {
		level := "info"
		if i%3 == 0 {
			level = "error"
		}
		buf.push(LogEntry{Level: level, Message: "msg"})
	}

	// Ask for 2 error entries.
	entries := buf.Tail(2, "error")
	if len(entries) != 2 {
		t.Errorf("got %d error entries, want 2", len(entries))
	}
	for _, e := range entries {
		if e.Level != "error" {
			t.Errorf("entry level = %s, want error", e.Level)
		}
	}
}

func TestLogRingBuffer_Empty(t *testing.T) {
	buf := NewLogRingBuffer(100)
	entries := buf.Tail(10, "")
	if len(entries) != 0 {
		t.Errorf("empty buffer should return 0 entries, got %d", len(entries))
	}
}

func TestStringFromMap_Missing(t *testing.T) {
	m := map[string]any{"other": "value"}
	got := stringFromMap(m, "missing", "default")
	if got != "default" {
		t.Errorf("stringFromMap missing key = %q, want default", got)
	}
}

func TestStringFromMap_NonString(t *testing.T) {
	m := map[string]any{"key": 42}
	got := stringFromMap(m, "key", "fallback")
	if got != "fallback" {
		t.Errorf("stringFromMap non-string = %q, want fallback", got)
	}
}

func TestStringFromMap_Present(t *testing.T) {
	m := map[string]any{"key": "value"}
	got := stringFromMap(m, "key", "fallback")
	if got != "value" {
		t.Errorf("stringFromMap present = %q, want value", got)
	}
}

func TestLogRingBuffer_WriteReturnValue(t *testing.T) {
	buf := NewLogRingBuffer(100)
	data := []byte(`{"level":"info","message":"test"}`)
	n, err := buf.Write(data)
	if err != nil {
		t.Errorf("Write error: %v", err)
	}
	if n != len(data) {
		t.Errorf("Write returned %d, want %d", n, len(data))
	}
}

func TestLogRingBuffer_WritePlainTextReturnValue(t *testing.T) {
	buf := NewLogRingBuffer(100)
	data := []byte("plain text log line\n")
	n, err := buf.Write(data)
	if err != nil {
		t.Errorf("Write error: %v", err)
	}
	if n != len(data) {
		t.Errorf("Write returned %d, want %d", n, len(data))
	}
}
