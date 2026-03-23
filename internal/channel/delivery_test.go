package channel

import (
	"testing"
	"time"
)

func TestBackoffDelay(t *testing.T) {
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 0},
		{1, 1 * time.Second},
		{2, 5 * time.Second},
		{3, 30 * time.Second},
		{4, 5 * time.Minute},
		{5, 15 * time.Minute},
		{99, 15 * time.Minute},
	}
	for _, tc := range cases {
		got := backoffDelay(tc.attempt)
		if got != tc.want {
			t.Errorf("backoffDelay(%d) = %v, want %v", tc.attempt, got, tc.want)
		}
	}
}

func TestIsPermanentError(t *testing.T) {
	permanent := []string{
		"403 Forbidden",
		"401 unauthorized",
		"400 bad request: invalid recipient",
		"blocked by the user",
		"bot was kicked from the group",
		"peer_id_invalid",
	}
	for _, msg := range permanent {
		if !isPermanentError(msg) {
			t.Errorf("expected permanent error: %q", msg)
		}
	}

	transient := []string{
		"connection timeout",
		"500 internal server error",
		"network unreachable",
	}
	for _, msg := range transient {
		if isPermanentError(msg) {
			t.Errorf("expected transient error: %q", msg)
		}
	}
}

func TestDeliveryQueue_EnqueueDrain(t *testing.T) {
	dq := NewDeliveryQueue(nil)

	dq.Enqueue("telegram", "user123", "hello")
	dq.Enqueue("discord", "chan456", "world")

	if dq.PendingCount() != 2 {
		t.Fatalf("expected 2 pending, got %d", dq.PendingCount())
	}

	items := dq.DrainReady()
	if len(items) != 2 {
		t.Fatalf("expected 2 ready, got %d", len(items))
	}

	if dq.PendingCount() != 0 {
		t.Fatalf("expected 0 pending after drain, got %d", dq.PendingCount())
	}
}

func TestDeliveryQueue_RequeueFailed(t *testing.T) {
	dq := NewDeliveryQueue(nil)
	dq.Enqueue("telegram", "user1", "msg")

	items := dq.DrainReady()
	if len(items) != 1 {
		t.Fatal("expected 1 item")
	}

	// Requeue with transient error.
	dq.RequeueFailed(items[0], "connection timeout")

	if dq.PendingCount() != 1 {
		t.Fatalf("expected 1 pending after requeue, got %d", dq.PendingCount())
	}
	if len(dq.DeadLetters()) != 0 {
		t.Fatal("should not dead-letter on first transient failure")
	}
}

func TestDeliveryQueue_PermanentErrorDeadLetters(t *testing.T) {
	dq := NewDeliveryQueue(nil)
	dq.Enqueue("telegram", "user1", "msg")

	items := dq.DrainReady()
	dq.RequeueFailed(items[0], "403 Forbidden")

	if dq.PendingCount() != 0 {
		t.Fatal("permanent error should not requeue")
	}
	if len(dq.DeadLetters()) != 1 {
		t.Fatal("expected 1 dead letter")
	}
}

func TestDeliveryQueue_MaxAttemptsDeadLetters(t *testing.T) {
	dq := NewDeliveryQueue(nil)
	dq.Enqueue("telegram", "user1", "msg")

	// Exhaust all attempts.
	for i := 0; i < 5; i++ {
		items := dq.DrainReady()
		if len(items) == 0 {
			// Future retry — manually drain by setting NextRetryAt to past.
			dq.mu.Lock()
			for _, item := range dq.pending {
				item.NextRetryAt = time.Now().Add(-time.Second)
			}
			dq.mu.Unlock()
			items = dq.DrainReady()
		}
		if len(items) != 1 {
			t.Fatalf("attempt %d: expected 1 item, got %d", i, len(items))
		}
		dq.RequeueFailed(items[0], "timeout")
	}

	if len(dq.DeadLetters()) != 1 {
		t.Fatalf("expected 1 dead letter after max attempts, got %d", len(dq.DeadLetters()))
	}
}

func TestDeliveryQueue_ReplayDeadLetter(t *testing.T) {
	dq := NewDeliveryQueue(nil)
	dq.Enqueue("telegram", "user1", "msg")

	items := dq.DrainReady()
	dq.RequeueFailed(items[0], "403 Forbidden")

	dl := dq.DeadLetters()
	if len(dl) != 1 {
		t.Fatal("expected 1 dead letter")
	}

	ok := dq.ReplayDeadLetter(dl[0].ID)
	if !ok {
		t.Fatal("replay should succeed")
	}

	if dq.PendingCount() != 1 {
		t.Fatal("replayed item should be pending")
	}
	if len(dq.DeadLetters()) != 0 {
		t.Fatal("dead letter should be removed after replay")
	}
}
