package api

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEventBus_PublishSubscribe(t *testing.T) {
	bus := NewEventBus(16)
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	bus.Publish("test-event")

	select {
	case msg := <-ch:
		if msg != "test-event" {
			t.Errorf("got %q, want test-event", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestEventBus_MultipleSubscribers(t *testing.T) {
	bus := NewEventBus(16)
	ch1 := bus.Subscribe()
	ch2 := bus.Subscribe()
	defer bus.Unsubscribe(ch1)
	defer bus.Unsubscribe(ch2)

	bus.Publish("broadcast")

	for _, ch := range []chan string{ch1, ch2} {
		select {
		case msg := <-ch:
			if msg != "broadcast" {
				t.Errorf("got %q, want broadcast", msg)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
	}
}

func TestEventBus_Unsubscribe(t *testing.T) {
	bus := NewEventBus(16)
	ch := bus.Subscribe()
	bus.Unsubscribe(ch)

	// After unsubscribe, channel should be closed.
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("channel should be closed after unsubscribe")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("channel should be closed immediately")
	}
}

func TestEventBus_DropOnFull(t *testing.T) {
	bus := NewEventBus(2) // tiny capacity
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	// Fill the buffer.
	bus.Publish("msg1")
	bus.Publish("msg2")
	// This should be dropped (non-blocking).
	bus.Publish("msg3")

	// Should only get the first two.
	count := 0
	for {
		select {
		case <-ch:
			count++
		case <-time.After(50 * time.Millisecond):
			goto done
		}
	}
done:
	if count != 2 {
		t.Errorf("received %d messages, want 2 (third should be dropped)", count)
	}
}

func TestEventBus_PublishEvent_JSON(t *testing.T) {
	bus := NewEventBus(16)
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	bus.PublishEvent("agent.reply", map[string]string{"content": "Hello"})

	select {
	case msg := <-ch:
		var parsed map[string]any
		if err := json.Unmarshal([]byte(msg), &parsed); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if parsed["type"] != "agent.reply" {
			t.Errorf("type = %v, want agent.reply", parsed["type"])
		}
		if parsed["timestamp"] == nil {
			t.Error("timestamp should be present")
		}
		data, ok := parsed["data"].(map[string]any)
		if !ok {
			t.Fatal("data should be an object")
		}
		if data["content"] != "Hello" {
			t.Errorf("data.content = %v, want Hello", data["content"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestEventBus_PublishF(t *testing.T) {
	bus := NewEventBus(16)
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	bus.PublishF("status", "agent %s is %s", "roboticus", "running")

	select {
	case msg := <-ch:
		var parsed map[string]any
		_ = json.Unmarshal([]byte(msg), &parsed)
		if parsed["type"] != "status" {
			t.Errorf("type = %v", parsed["type"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestEventBus_NoSubscribers(t *testing.T) {
	bus := NewEventBus(16)
	// Should not panic with no subscribers.
	bus.Publish("orphan")
	bus.PublishEvent("test", nil)
}
