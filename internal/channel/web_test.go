package channel

import (
	"context"
	"testing"
	"time"
)

func TestWebAdapter_PushAndRecv(t *testing.T) {
	adapter := NewWebAdapter(WebConfig{})

	ok := adapter.PushMessage(InboundMessage{ID: "1", Content: "hello"})
	if !ok {
		t.Fatal("push should succeed")
	}

	msg, err := adapter.Recv(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if msg == nil || msg.Content != "hello" {
		t.Fatal("expected 'hello' message")
	}

	// Second recv should return nil.
	msg, _ = adapter.Recv(context.Background())
	if msg != nil {
		t.Fatal("expected nil on empty buffer")
	}
}

func TestWebAdapter_Broadcast(t *testing.T) {
	adapter := NewWebAdapter(WebConfig{})

	sub1 := adapter.Subscribe(10)
	sub2 := adapter.Subscribe(10)

	err := adapter.Send(context.Background(), OutboundMessage{Content: "broadcast"})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case msg := <-sub1:
		if msg.Content != "broadcast" {
			t.Errorf("sub1 got %q", msg.Content)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("sub1 timeout")
	}

	select {
	case msg := <-sub2:
		if msg.Content != "broadcast" {
			t.Errorf("sub2 got %q", msg.Content)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("sub2 timeout")
	}

	adapter.Unsubscribe(sub1)
}

func TestWebAdapter_BufferFull(t *testing.T) {
	adapter := NewWebAdapter(WebConfig{InboundBufferSize: 1})

	ok := adapter.PushMessage(InboundMessage{ID: "1"})
	if !ok {
		t.Fatal("first push should succeed")
	}

	ok = adapter.PushMessage(InboundMessage{ID: "2"})
	if ok {
		t.Fatal("second push should fail (buffer full)")
	}
}
