package api

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestTicketStore_IssueAndValidate(t *testing.T) {
	ts := NewTicketStore(context.Background(), 60*time.Second)

	ticket := ts.Issue()
	if ticket == "" {
		t.Fatal("Issue returned empty ticket")
	}
	if !strings.HasPrefix(ticket, "wst_") {
		t.Errorf("ticket should start with wst_, got %q", ticket)
	}

	// Validate should succeed the first time.
	if !ts.Validate(ticket) {
		t.Error("valid ticket should pass validation")
	}

	// Single-use: second validation should fail.
	if ts.Validate(ticket) {
		t.Error("ticket should be consumed after first validation")
	}
}

func TestTicketStore_InvalidTicket(t *testing.T) {
	ts := NewTicketStore(context.Background(), 60*time.Second)

	if ts.Validate("wst_nonexistent") {
		t.Error("non-existent ticket should fail validation")
	}
}

func TestTicketStore_ExpiredTicket(t *testing.T) {
	ts := NewTicketStore(context.Background(), 1*time.Millisecond) // very short TTL

	ticket := ts.Issue()
	time.Sleep(5 * time.Millisecond)

	if ts.Validate(ticket) {
		t.Error("expired ticket should fail validation")
	}
}

func TestTicketStore_MultipleTickets(t *testing.T) {
	ts := NewTicketStore(context.Background(), 60*time.Second)

	t1 := ts.Issue()
	t2 := ts.Issue()

	if t1 == t2 {
		t.Error("two tickets should be different")
	}

	if !ts.Validate(t1) {
		t.Error("ticket 1 should be valid")
	}
	if !ts.Validate(t2) {
		t.Error("ticket 2 should be valid")
	}
}

func TestTicketStore_Cleanup(t *testing.T) {
	ts := NewTicketStore(context.Background(), 1*time.Millisecond)

	ts.Issue()
	ts.Issue()
	time.Sleep(5 * time.Millisecond)

	ts.cleanup()

	ts.mu.Lock()
	remaining := len(ts.tickets)
	ts.mu.Unlock()

	if remaining != 0 {
		t.Errorf("cleanup should remove expired tickets, %d remaining", remaining)
	}
}

func TestTicketStore_CleanupKeepsValid(t *testing.T) {
	ts := NewTicketStore(context.Background(), 60*time.Second)

	ticket := ts.Issue()
	ts.cleanup() // should not remove the valid ticket

	if !ts.Validate(ticket) {
		t.Error("cleanup should not remove non-expired tickets")
	}
}

func TestTicketStore_EntropyLength(t *testing.T) {
	ts := NewTicketStore(context.Background(), 60*time.Second)
	ticket := ts.Issue()
	// "wst_" prefix + 64 hex chars (32 bytes of entropy).
	expectedLen := 4 + 64
	if len(ticket) != expectedLen {
		t.Errorf("ticket length = %d, want %d (32 bytes entropy)", len(ticket), expectedLen)
	}
}

func TestTicketStore_UniqueTokens(t *testing.T) {
	ts := NewTicketStore(context.Background(), 60*time.Second)
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		token := ts.Issue()
		if seen[token] {
			t.Fatalf("duplicate token generated: %s", token)
		}
		seen[token] = true
	}
}
