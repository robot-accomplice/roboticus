package api

import (
	"fmt"
	"sync"
	"testing"
)

func TestLogRingBuffer_WriteAndTail(t *testing.T) {
	buf := NewLogRingBuffer(100)
	buf.push(LogEntry{Level: "info", Message: "hello"})
	buf.push(LogEntry{Level: "error", Message: "oops"})

	entries := buf.Tail(10, "")
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Message != "hello" {
		t.Errorf("first message = %s", entries[0].Message)
	}
}

func TestLogRingBuffer_LevelFilter(t *testing.T) {
	buf := NewLogRingBuffer(100)
	buf.push(LogEntry{Level: "info", Message: "info msg"})
	buf.push(LogEntry{Level: "error", Message: "error msg"})
	buf.push(LogEntry{Level: "info", Message: "another info"})

	entries := buf.Tail(10, "error")
	if len(entries) != 1 {
		t.Fatalf("got %d error entries, want 1", len(entries))
	}
	if entries[0].Message != "error msg" {
		t.Errorf("message = %s", entries[0].Message)
	}
}

func TestLogRingBuffer_Overflow(t *testing.T) {
	buf := NewLogRingBuffer(100) // min size is 100
	for i := 0; i < 150; i++ {
		buf.push(LogEntry{Level: "info", Message: fmt.Sprintf("msg-%d", i)})
	}
	entries := buf.Tail(200, "")
	if len(entries) != 100 {
		t.Fatalf("got %d entries, want 100 (buffer size)", len(entries))
	}
	// Should have the last 100 messages (msg-50 through msg-149).
	if entries[0].Message != "msg-50" {
		t.Errorf("oldest visible = %s, want msg-50", entries[0].Message)
	}
	if entries[99].Message != "msg-149" {
		t.Errorf("newest = %s, want msg-149", entries[99].Message)
	}
}

func TestLogRingBuffer_WriteJSON(t *testing.T) {
	buf := NewLogRingBuffer(100)
	_, _ = buf.Write([]byte(`{"time":"2024-01-01","level":"warn","message":"test warning"}`))

	entries := buf.Tail(1, "")
	if len(entries) != 1 {
		t.Fatal("should have 1 entry")
	}
	if entries[0].Level != "warn" {
		t.Errorf("level = %s, want warn", entries[0].Level)
	}
	if entries[0].Message != "test warning" {
		t.Errorf("message = %s", entries[0].Message)
	}
}

func TestLogRingBuffer_WritePlainText(t *testing.T) {
	buf := NewLogRingBuffer(100)
	_, _ = buf.Write([]byte("not json\n"))

	entries := buf.Tail(1, "")
	if len(entries) != 1 {
		t.Fatal("should have 1 entry")
	}
	if entries[0].Message != "not json" {
		t.Errorf("message = %q", entries[0].Message)
	}
}

func TestLogRingBuffer_ConcurrentWrite(t *testing.T) {
	buf := NewLogRingBuffer(1000)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			buf.push(LogEntry{Level: "info", Message: fmt.Sprintf("msg-%d", n)})
		}(i)
	}
	wg.Wait()
	entries := buf.Tail(1000, "")
	if len(entries) != 100 {
		t.Errorf("got %d entries, want 100", len(entries))
	}
}
