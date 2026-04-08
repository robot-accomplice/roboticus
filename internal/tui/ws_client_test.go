package tui

import (
	"context"
	"testing"
)

func TestWSClient_NewAndClose(t *testing.T) {
	wc := NewWSClient("ws://localhost:9999/ws")
	if wc.url != "ws://localhost:9999/ws" {
		t.Fatalf("unexpected url: %s", wc.url)
	}

	// Close without connecting should not error.
	if err := wc.Close(); err != nil {
		t.Fatalf("unexpected close error: %v", err)
	}
}

func TestWSClient_SendWithoutConnect(t *testing.T) {
	wc := NewWSClient("ws://localhost:9999/ws")
	err := wc.Send(context.Background(), []byte("hello"))
	if err == nil {
		t.Fatal("expected error sending without connection")
	}
}

func TestWSClient_ReceiveWithoutConnect(t *testing.T) {
	wc := NewWSClient("ws://localhost:9999/ws")
	_, err := wc.Receive(context.Background())
	if err == nil {
		t.Fatal("expected error receiving without connection")
	}
}

func TestWSClient_ConnectBadURL(t *testing.T) {
	wc := NewWSClient("ws://127.0.0.1:1/nonexistent")
	err := wc.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error connecting to bad URL")
	}
}

func TestWSClient_DoneChannel(t *testing.T) {
	wc := NewWSClient("ws://localhost:9999/ws")
	done := wc.Done()

	select {
	case <-done:
		t.Fatal("done channel should not be closed yet")
	default:
	}

	_ = wc.Close()

	select {
	case <-done:
		// Good, done was closed.
	default:
		t.Fatal("done channel should be closed after Close()")
	}
}
