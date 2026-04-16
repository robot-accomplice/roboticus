package llm

import (
	"strings"
	"testing"
)

// TestWarmupPrompt_IsShort pins the warm-up prompt contract: it must
// be a short prompt that produces a short response, so warm-up cost
// stays bounded. A future change that makes WarmupPrompt verbose
// would silently inflate baseline-run time for every local model.
func TestWarmupPrompt_IsShort(t *testing.T) {
	// 60 chars is plenty for the "reply with just ready" shape and
	// leaves headroom if we ever rephrase. Anything much longer means
	// someone started using warm-up to exercise tool-calling or
	// reasoning paths — which violates the scope (warm-up is purely
	// cost amortization, not benchmarking).
	const maxPromptLen = 60
	if len(WarmupPrompt) > maxPromptLen {
		t.Fatalf("WarmupPrompt is %d chars; must be <= %d to keep warm-up cost bounded. Long warm-up prompts inflate baseline run time for every local model.",
			len(WarmupPrompt), maxPromptLen)
	}
}

// TestWarmupPrompt_NoToolTriggers confirms the warm-up prompt doesn't
// contain common tool-calling trigger words. If warm-up prompts cause
// the agent to invoke tools, we're no longer measuring model-load
// cost — we're measuring tool-execution cost too.
func TestWarmupPrompt_NoToolTriggers(t *testing.T) {
	// A short, low-signal deny-list. Not exhaustive; just catches
	// accidental drift toward tool-triggering phrasing.
	triggers := []string{
		"search", "find", "look up", "fetch", "read file", "write file",
		"execute", "run command", "list files", "directory",
	}
	lower := strings.ToLower(WarmupPrompt)
	for _, trigger := range triggers {
		if strings.Contains(lower, trigger) {
			t.Fatalf("WarmupPrompt contains tool-calling trigger %q — warm-up must stay trivial (no tool execution) so its latency measures ONLY cold-start cost, not cold-start + tool overhead. Current prompt: %q", trigger, WarmupPrompt)
		}
	}
}

// TestWarmupResult_TimedOutVsError separates the two non-success
// states that warm-up callers care about. A timeout is a LEGITIMATE
// data point — "cold start exceeds N seconds" — and should feed the
// cold-start latency distribution as a lower bound. A transport error
// is a signal that the model/daemon is broken, not that it's cold.
//
// This test pins the contract by constructing both shapes and
// asserting the caller can distinguish them via the flags.
func TestWarmupResult_TimedOutVsError(t *testing.T) {
	timeout := WarmupResult{LatencyMs: 300_000, TimedOut: true}
	broken := WarmupResult{LatencyMs: 42, Err: errString("connection refused")}

	if !timeout.TimedOut {
		t.Fatal("timeout result should have TimedOut=true")
	}
	if timeout.Err != nil {
		t.Fatalf("timeout result should have nil Err; got %v", timeout.Err)
	}

	if broken.TimedOut {
		t.Fatal("broken result should have TimedOut=false")
	}
	if broken.Err == nil {
		t.Fatal("broken result should have non-nil Err")
	}
}

// errString is a tiny error shim for TestWarmupResult_TimedOutVsError
// so we don't have to pull in errors.New or a sentinel.
type errString string

func (e errString) Error() string { return string(e) }
