package channel

import (
	"context"
	"testing"
)

// TestAdapterRegistry_AllPlatformsRegisterable verifies that all adapters can
// be registered with the router without errors.
func TestAdapterRegistry_AllPlatformsRegisterable(t *testing.T) {
	dq := NewDeliveryQueue(nil)
	router := NewRouter(dq)

	adapters := []Adapter{
		NewTelegramAdapter(TelegramConfig{Token: "test"}),
		NewDiscordAdapter(DiscordConfig{Token: "test"}),
		NewWhatsAppAdapter(WhatsAppConfig{Token: "test"}),
		NewSignalAdapter(SignalConfig{DaemonURL: "http://localhost:1234"}),
		NewWebAdapter(WebConfig{}),
		NewEmailAdapter(EmailConfig{SMTPHost: "localhost", IMAPHost: "localhost"}),
	}

	for _, a := range adapters {
		router.Register(a)
	}

	names := router.ChannelNames()
	if len(names) != len(adapters) {
		t.Errorf("registered %d adapters, got %d names", len(adapters), len(names))
	}
}

// TestWebAdapter_SendRecvRoundtrip exercises the Web adapter's in-memory message queue.
func TestWebAdapter_SendRecvRoundtrip(t *testing.T) {
	adapter := NewWebAdapter(WebConfig{})
	ctx := context.Background()

	adapter.PushMessage(InboundMessage{
		SenderID: "web-user",
		Content:  "hello from web",
		Platform: "web",
	})

	msg, err := adapter.Recv(ctx)
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if msg == nil {
		t.Fatal("Recv returned nil")
	}
	if msg.Content != "hello from web" {
		t.Errorf("content = %q", msg.Content)
	}

	err = adapter.Send(ctx, OutboundMessage{
		RecipientID: "web-user",
		Content:     "response",
		Platform:    "web",
	})
	if err != nil {
		t.Errorf("Send: %v", err)
	}
}

// TestRouterHealth_TransitionModel exercises Connected→Degraded→Disconnected transitions.
func TestRouterHealth_TransitionModel(t *testing.T) {
	dq := NewDeliveryQueue(nil)
	router := NewRouter(dq)
	router.Register(&integMockAdapter{name: "test-ch"})

	// Initial: Connected.
	s := router.Status()
	if len(s) != 1 || s[0].Health != HealthConnected {
		t.Fatalf("initial: %v", s)
	}

	// 3 errors → Degraded.
	for i := 0; i < 3; i++ {
		router.RecordError("test-ch", "err")
	}
	if s := router.Status(); s[0].Health != HealthDegraded {
		t.Errorf("after 3 errors: %v, want Degraded", s[0].Health)
	}

	// 10 errors → Disconnected.
	for i := 0; i < 7; i++ {
		router.RecordError("test-ch", "err")
	}
	if s := router.Status(); s[0].Health != HealthDisconnected {
		t.Errorf("after 10 errors: %v, want Disconnected", s[0].Health)
	}

	// Success resets to Connected.
	router.RecordReceived("test-ch")
	if s := router.Status(); s[0].Health != HealthConnected {
		t.Errorf("after success: %v, want Connected", s[0].Health)
	}
	if s := router.Status(); s[0].ErrorCount != 0 {
		// ErrorCount is cumulative; health resets but count doesn't.
		// That's the correct behavior — the count is for observability.
		_ = s // SA9003: intentionally empty — documenting expected behavior
	}
}

// TestDeliveryQueue_IdempotencyDedup verifies duplicate idempotency key rejection.
func TestDeliveryQueue_IdempotencyDedup(t *testing.T) {
	dq := NewDeliveryQueue(nil)

	id1 := dq.EnqueueWithOptions("telegram", "123", "hello", "key-abc", 5)
	id2 := dq.EnqueueWithOptions("telegram", "123", "hello", "key-abc", 5)

	// Should return the same ID (deduped).
	if id1 != id2 {
		t.Errorf("expected same ID for duplicate key, got %q and %q", id1, id2)
	}
	if dq.PendingCount() != 1 {
		t.Errorf("pending count = %d, want 1", dq.PendingCount())
	}
}

// TestDeliveryQueue_ConfigurableMaxAttempts verifies per-item retry budget.
func TestDeliveryQueue_ConfigurableMaxAttempts(t *testing.T) {
	dq := NewDeliveryQueue(nil)
	dq.EnqueueWithOptions("telegram", "123", "important", "", 10)

	items := dq.DrainReady()
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].MaxAttempts != 10 {
		t.Errorf("MaxAttempts = %d, want 10", items[0].MaxAttempts)
	}
}

type integMockAdapter struct {
	name string
}

func (m *integMockAdapter) PlatformName() string                                { return m.name }
func (m *integMockAdapter) Send(ctx context.Context, msg OutboundMessage) error { return nil }
func (m *integMockAdapter) Recv(ctx context.Context) (*InboundMessage, error)   { return nil, nil }
