package memory

import (
	"context"
	"sync"
	"testing"
)

// TestIntentsContext_RoundTrip covers the happy-path contract: put
// intents on ctx via WithIntents, pull them back via intentsFromContext.
func TestIntentsContext_RoundTrip(t *testing.T) {
	ctx := context.Background()
	intents := []IntentSignal{
		{Label: "introspection", Confidence: 0.8},
		{Label: "tool_use", Confidence: 0.6},
	}
	got := intentsFromContext(WithIntents(ctx, intents))
	if len(got) != 2 {
		t.Fatalf("intents len = %d; want 2", len(got))
	}
	if got[0].Label != "introspection" || got[1].Label != "tool_use" {
		t.Fatalf("intents content mismatch: %+v", got)
	}
}

// TestIntentsContext_EmptyIsNoOp verifies that passing nil or empty
// intents returns the original ctx unchanged. This keeps ad-hoc callers
// (tests, smoke scripts, CLI) from having to branch on intent
// availability before calling WithIntents.
func TestIntentsContext_EmptyIsNoOp(t *testing.T) {
	base := context.Background()
	if WithIntents(base, nil) != base {
		t.Fatalf("nil intents should return the original context unchanged")
	}
	if WithIntents(base, []IntentSignal{}) != base {
		t.Fatalf("empty intents slice should return the original context unchanged")
	}
	if got := intentsFromContext(base); got != nil {
		t.Fatalf("unset ctx should return nil intents; got %v", got)
	}
}

// TestIntentsContext_NilCtxSafe verifies the reader tolerates a nil
// context gracefully. This matters because Retrieve is called from
// places that occasionally propagate nil in tests — the reader should
// degrade to "no intents" rather than panic.
func TestIntentsContext_NilCtxSafe(t *testing.T) {
	var nilCtx context.Context
	if got := intentsFromContext(nilCtx); got != nil {
		t.Fatalf("nil ctx should return nil intents; got %v", got)
	}
}

// TestIntentsContext_ConcurrentIsolation is the headline v1.0.6
// regression for P1-A. Pre-fix the Retriever had a shared `intents`
// field that was SetIntents'd on every call — under concurrent traffic
// two goroutines racing SetIntents → Retrieve could see each other's
// intents.
//
// This test runs N goroutines in parallel, each carrying a unique intent
// label in its own ctx, and asserts each goroutine reads back exactly
// the intent it put in. If the implementation regressed to shared state,
// intents would mix across goroutines and at least one read would find
// a label that didn't match its goroutine's id.
func TestIntentsContext_ConcurrentIsolation(t *testing.T) {
	const goroutines = 50
	const iters = 20
	var wg sync.WaitGroup
	errs := make(chan string, goroutines*iters)

	for g := 0; g < goroutines; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			myLabel := "goroutine_" + itoaPadded(g)
			for i := 0; i < iters; i++ {
				ctx := WithIntents(context.Background(), []IntentSignal{
					{Label: myLabel, Confidence: 0.9},
				})
				got := intentsFromContext(ctx)
				if len(got) != 1 {
					errs <- "goroutine " + myLabel + ": expected 1 intent, got " + itoaPadded(len(got))
					return
				}
				if got[0].Label != myLabel {
					errs <- "goroutine " + myLabel + ": expected label " + myLabel + ", got " + got[0].Label
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for msg := range errs {
		t.Errorf("concurrent intent isolation failed: %s", msg)
	}
}

// itoaPadded is a small no-import int-to-string for the test, to avoid
// pulling strconv into a test that should stay self-contained.
func itoaPadded(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	idx := len(buf)
	for i > 0 {
		idx--
		buf[idx] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		idx--
		buf[idx] = '-'
	}
	return string(buf[idx:])
}
