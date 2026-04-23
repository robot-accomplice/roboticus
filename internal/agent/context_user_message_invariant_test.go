// context_user_message_invariant_test.go pins the v1.0.6 critical
// invariant for ContextBuilder.BuildRequest: the latest user message
// is ALWAYS included in the LLM request, even when the system prompt
// + memory + tool definitions exhaust the token budget.
//
// Why this matters: pre-v1.0.6, when (sysTokCount + memTokCount +
// toolTokCount) >= budget, the message-loading loop blindly broke at
// the first iteration because `usedTokens(0) + tokens(N) > remaining
// (negative)` is true. historyMessages stayed empty, the LLM never
// saw the user's prompt, and the agent replied "the user has not
// provided instructions" — exactly the v1.0.6 cache-cleared soak's
// failure mode for 6 of 10 scenarios.
//
// The fix in context.go's BuildRequest:
//   * identifies the index of the latest user message
//   * includes that message UNCONDITIONALLY (even if over budget),
//     with a loud warning log explaining the diagnostic
//   * drops OLDER history messages first when budget is tight
//
// This test MUST fail loudly if a future refactor reintroduces the
// blind-break behavior.

package agent

import (
	"strings"
	"testing"

	"roboticus/internal/llm"
)

// TestBuildRequest_UserMessageSurvivesNegativeBudget is the
// regression for the v1.0.6 empty-prompt bug. Construct a
// ContextBuilder whose system prompt + memory + tool defs alone
// exceed the configured budget, then assert the latest user
// message is still present in the resulting LLM request.
//
// Without the fix, `historyMessages` stays empty and the test's
// assertion fails — operators see exactly the failure mode the
// soak surfaced.
func TestBuildRequest_UserMessageSurvivesNegativeBudget(t *testing.T) {
	cfg := DefaultContextConfig()
	cfg.MaxTokens = 1000 // small budget
	cb := NewContextBuilder(cfg)

	// System prompt eats most of the budget on its own (~1500 tok
	// at 4 chars/token).
	cb.SetSystemPrompt(strings.Repeat("x", 6000))
	// Memory adds another ~250 tokens.
	cb.SetMemory(strings.Repeat("y", 1000))

	// Tool defs add more on top — push into negative-remaining
	// territory.
	defs := make([]llm.ToolDef, 5)
	for i := range defs {
		defs[i] = llm.ToolDef{
			Type: "function",
			Function: llm.ToolFuncDef{
				Name:        strings.Repeat("z", 50),
				Description: strings.Repeat("d", 200),
				Parameters:  []byte(`{"type":"object","properties":{}}`),
			},
		}
	}
	cb.SetTools(defs)

	sess := NewSession("s1", "agent-id", "Test")
	const userPrompt = "Count markdown files in /Users/jmachen/code"
	sess.AddUserMessage(userPrompt)

	req := cb.BuildRequest(sess)

	// The latest user message MUST be in req.Messages — anywhere.
	// (Order can vary depending on how the builder interleaves
	// system / memory / topic-summary messages around the
	// historical block.)
	found := false
	for _, m := range req.Messages {
		if m.Role == "user" && strings.Contains(m.Content, userPrompt) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("latest user message dropped from LLM request despite v1.0.6 invariant; req.Messages: %d entries, none containing user prompt", len(req.Messages))
	}
}

// TestBuildRequest_OldHistoryDroppedFirst verifies the secondary
// invariant: when budget is tight, the OLDEST messages get dropped
// first — not the latest user message. Without this discipline a
// well-meaning refactor could "fix" the over-budget case by
// dropping the latest user message and end up back at the original
// bug.
func TestBuildRequest_OldHistoryDroppedFirst(t *testing.T) {
	cfg := DefaultContextConfig()
	cfg.MaxTokens = 1000
	cb := NewContextBuilder(cfg)
	cb.SetSystemPrompt(strings.Repeat("x", 3000)) // ~750 tokens

	sess := NewSession("s1", "agent-id", "Test")
	// Several historical exchanges plus a final user prompt.
	sess.AddUserMessage("first old question that should be drop-eligible " + strings.Repeat("a", 800))
	sess.AddAssistantMessage("first old response "+strings.Repeat("b", 800), nil)
	sess.AddUserMessage("second old question " + strings.Repeat("c", 800))
	sess.AddAssistantMessage("second old response "+strings.Repeat("d", 800), nil)
	const finalPrompt = "FINAL_USER_PROMPT_MARKER what's happening"
	sess.AddUserMessage(finalPrompt)

	req := cb.BuildRequest(sess)

	// Assert the FINAL user message survives.
	finalFound := false
	for _, m := range req.Messages {
		if m.Role == "user" && strings.Contains(m.Content, "FINAL_USER_PROMPT_MARKER") {
			finalFound = true
			break
		}
	}
	if !finalFound {
		t.Fatalf("final user message dropped under tight budget; req.Messages had %d entries but none contained FINAL_USER_PROMPT_MARKER", len(req.Messages))
	}

	// Assert that under tight budget, the original old-message
	// CONTENT (the long padding strings) doesn't survive verbatim.
	// The compaction stages compress older messages to skeletons
	// or topic extracts rather than dropping them entirely (the
	// LLM still gets a "[user message]" placeholder so it knows
	// there was a prior turn). What MUST get compacted is the
	// 800-character padding — if that survives in full, budget
	// enforcement isn't running.
	for _, padding := range []string{strings.Repeat("a", 800), strings.Repeat("b", 800)} {
		for _, m := range req.Messages {
			if strings.Contains(m.Content, padding) {
				t.Fatalf("budget did not compact old-message padding — either the test isn't tight enough or compaction isn't running; req.Messages count=%d", len(req.Messages))
			}
		}
	}
}

// TestBuildRequest_UserMessagePresentInGenerousBudget is the happy-
// path control: with plenty of budget, the user message AND all
// history is included. Guards against a future refactor that
// over-aggressively drops history under non-tight budgets.
func TestBuildRequest_UserMessagePresentInGenerousBudget(t *testing.T) {
	cfg := DefaultContextConfig()
	cfg.MaxTokens = 100000 // generous
	cb := NewContextBuilder(cfg)
	cb.SetSystemPrompt("You are a helpful assistant.")

	sess := NewSession("s1", "agent-id", "Test")
	sess.AddUserMessage("first question")
	sess.AddAssistantMessage("first answer", nil)
	sess.AddUserMessage("second question")

	req := cb.BuildRequest(sess)

	for _, marker := range []string{"first question", "first answer", "second question"} {
		found := false
		for _, m := range req.Messages {
			if strings.Contains(m.Content, marker) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("under generous budget, expected %q to be in req.Messages; got %d entries", marker, len(req.Messages))
		}
	}
}

// TestBuildRequest_DropsEmptyCompactedHistoryMessages pins the ownership
// boundary for empty-message removal. Social filler that compacts to ""
// must be dropped by ContextBuilder itself, not carried forward and cleaned
// up later by llm.Service.
func TestBuildRequest_DropsEmptyCompactedHistoryMessages(t *testing.T) {
	cfg := DefaultContextConfig()
	cfg.MaxTokens = 10 // force selective compaction
	cb := NewContextBuilder(cfg)
	cb.SetSystemPrompt(strings.Repeat("x", 20))

	sess := NewSession("s1", "agent-id", "Test")
	sess.AddUserMessage("hello")
	sess.AddAssistantMessage("okay", nil)
	sess.AddUserMessage("FINAL_PROMPT")

	req := cb.BuildRequest(sess)

	for _, m := range req.Messages {
		if m.Role == "user" && m.Content == "" {
			t.Fatal("context builder emitted empty user message")
		}
		if m.Role == "assistant" && m.Content == "" {
			t.Fatal("context builder emitted empty assistant message")
		}
	}
	for _, m := range req.Messages {
		if m.Role == "user" && strings.Contains(m.Content, "FINAL_PROMPT") {
			return
		}
	}
	t.Fatal("latest user prompt missing after dropping empty compacted history")
}

func TestBuildRequest_SkipsAntiFadeReminderWhenItDoesNotFitBudget(t *testing.T) {
	cfg := DefaultContextConfig()
	cfg.MaxTokens = 20
	cfg.AntiFadeAfter = 1

	cb := NewContextBuilder(cfg)
	cb.SetSystemPrompt("system prompt fits")

	sess := NewSession("s1", "agent-id", "Test")
	sess.AddUserMessage("first question")
	sess.AddAssistantMessage("first answer", nil)
	sess.AddUserMessage("FINAL_PROMPT")

	req := cb.BuildRequest(sess)

	for _, m := range req.Messages {
		if m.Role == "system" && strings.Contains(m.Content, "Reminder: Follow your instructions carefully.") {
			t.Fatal("anti-fade reminder should be skipped when it would exceed the remaining request budget")
		}
	}

	foundFinal := false
	for _, m := range req.Messages {
		if m.Role == "user" && strings.Contains(m.Content, "FINAL_PROMPT") {
			foundFinal = true
			break
		}
	}
	if !foundFinal {
		t.Fatal("final user prompt missing after anti-fade budget check")
	}
}

func TestBuildRequest_PreservesToolExchangeAtomicallyUnderBudgetPressure(t *testing.T) {
	cfg := DefaultContextConfig()
	cfg.MaxTokens = 120
	cb := NewContextBuilder(cfg)
	cb.SetSystemPrompt(strings.Repeat("x", 200))

	sess := NewSession("s1", "agent-id", "Test")
	sess.AddUserMessage("build the report")
	sess.AddAssistantMessage("", []llm.ToolCall{{
		ID: "call-1",
		Function: llm.ToolCallFunc{
			Name:      "write_file",
			Arguments: `{"path":"report.md","content":"hello"}`,
		},
	}})
	sess.AddToolResult("call-1", "write_file", strings.Repeat("tool result payload ", 40), false)
	sess.AddUserMessage("FINAL_PROMPT")

	req := cb.BuildRequest(sess)

	foundAssistantCall := false
	foundToolReply := false
	assistantIdx := -1
	toolIdx := -1
	for i, m := range req.Messages {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 && m.ToolCalls[0].ID == "call-1" {
			foundAssistantCall = true
			assistantIdx = i
		}
		if m.Role == "tool" && m.ToolCallID == "call-1" {
			foundToolReply = true
			toolIdx = i
		}
	}
	if foundAssistantCall != foundToolReply {
		t.Fatalf("tool exchange was compacted non-atomically: assistant_call=%v tool_reply=%v", foundAssistantCall, foundToolReply)
	}
	if foundAssistantCall && assistantIdx > toolIdx {
		t.Fatalf("tool exchange order corrupted: assistant_idx=%d tool_idx=%d", assistantIdx, toolIdx)
	}
}
