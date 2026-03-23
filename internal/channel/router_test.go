package channel

import (
	"context"
	"fmt"
	"testing"
)

type mockAdapter struct {
	name    string
	recvMsg *InboundMessage
	sendErr error
	sent    []OutboundMessage
}

func (m *mockAdapter) PlatformName() string { return m.name }
func (m *mockAdapter) Recv(ctx context.Context) (*InboundMessage, error) {
	msg := m.recvMsg
	m.recvMsg = nil
	return msg, nil
}
func (m *mockAdapter) Send(ctx context.Context, msg OutboundMessage) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.sent = append(m.sent, msg)
	return nil
}

func TestRouter_RegisterAndPollAll(t *testing.T) {
	dq := NewDeliveryQueue(nil)
	router := NewRouter(dq)

	adapter := &mockAdapter{
		name: "test",
		recvMsg: &InboundMessage{
			ID:      "1",
			Content: "hello",
		},
	}
	router.Register(adapter)

	msgs := router.PollAll(context.Background())
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Content != "hello" {
		t.Errorf("expected 'hello', got %q", msgs[0].Content)
	}
	if msgs[0].Platform != "test" {
		t.Errorf("expected platform 'test', got %q", msgs[0].Platform)
	}
}

func TestRouter_SendTo_Success(t *testing.T) {
	dq := NewDeliveryQueue(nil)
	router := NewRouter(dq)

	adapter := &mockAdapter{name: "test"}
	router.Register(adapter)

	err := router.SendTo(context.Background(), "test", OutboundMessage{
		Content:     "hi",
		RecipientID: "user1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(adapter.sent) != 1 {
		t.Fatal("expected 1 sent message")
	}
}

func TestRouter_SendTo_UnknownChannel(t *testing.T) {
	dq := NewDeliveryQueue(nil)
	router := NewRouter(dq)

	err := router.SendTo(context.Background(), "nonexistent", OutboundMessage{})
	if err == nil {
		t.Fatal("expected error for unknown channel")
	}
}

func TestRouter_SendTo_TransientFailure_Enqueues(t *testing.T) {
	dq := NewDeliveryQueue(nil)
	router := NewRouter(dq)

	adapter := &mockAdapter{
		name:    "test",
		sendErr: fmt.Errorf("connection timeout"),
	}
	router.Register(adapter)

	err := router.SendTo(context.Background(), "test", OutboundMessage{
		Content:     "retry me",
		RecipientID: "user1",
	})
	// Transient failures are enqueued, not returned as error.
	if err != nil {
		t.Fatalf("transient failure should return nil, got: %v", err)
	}
	if dq.PendingCount() != 1 {
		t.Fatal("should enqueue for retry")
	}
}

func TestRouter_SendTo_PermanentFailure_ReturnsError(t *testing.T) {
	dq := NewDeliveryQueue(nil)
	router := NewRouter(dq)

	adapter := &mockAdapter{
		name:    "test",
		sendErr: fmt.Errorf("403 Forbidden"),
	}
	router.Register(adapter)

	err := router.SendTo(context.Background(), "test", OutboundMessage{
		Content:     "will fail",
		RecipientID: "user1",
	})
	if err == nil {
		t.Fatal("permanent failure should return error")
	}
	if dq.PendingCount() != 0 {
		t.Fatal("permanent failure should not enqueue")
	}
}

func TestRouter_Status(t *testing.T) {
	dq := NewDeliveryQueue(nil)
	router := NewRouter(dq)

	router.Register(&mockAdapter{name: "telegram"})
	router.Register(&mockAdapter{name: "discord"})

	statuses := router.Status()
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}
}

func TestRouter_ChannelNames(t *testing.T) {
	dq := NewDeliveryQueue(nil)
	router := NewRouter(dq)

	router.Register(&mockAdapter{name: "telegram"})
	router.Register(&mockAdapter{name: "discord"})

	names := router.ChannelNames()
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}
}
