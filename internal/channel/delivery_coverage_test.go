package channel

import (
	"testing"

	"goboticus/testutil"
)

func TestDeliveryQueue_EnqueueAndPending(t *testing.T) {
	store := testutil.TempStore(t)
	dq := NewDeliveryQueue(store)

	dq.Enqueue("telegram", "user1", "test message")

	if dq.pending.Len() != 1 {
		t.Errorf("pending count = %d, want 1", dq.pending.Len())
	}
}

func TestDeliveryQueue_MultipleEnqueueCoverage(t *testing.T) {
	store := testutil.TempStore(t)
	dq := NewDeliveryQueue(store)

	dq.Enqueue("telegram", "user1", "msg1")
	dq.Enqueue("discord", "user2", "msg2")
	dq.Enqueue("signal", "user3", "msg3")

	if dq.pending.Len() != 3 {
		t.Errorf("pending count = %d, want 3", dq.pending.Len())
	}
}

func TestRouter_CreationCoverage(t *testing.T) {
	store := testutil.TempStore(t)
	dq := NewDeliveryQueue(store)
	router := NewRouter(dq)
	if router == nil {
		t.Fatal("router should not be nil")
	}
}
