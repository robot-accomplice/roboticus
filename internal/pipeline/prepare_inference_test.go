package pipeline

import (
	"context"
	"strings"
	"testing"

	"roboticus/internal/core"
	"roboticus/internal/session"
	"roboticus/testutil"
)

func TestPrepareForInference_CompactsSessionMessagesInPlace(t *testing.T) {
	pipe := New(PipelineDeps{})
	sess := session.New("s-prepare", "agent-1", "TestBot")

	sess.AddSystemMessage("System prompt")
	for i := 0; i < 20; i++ {
		sess.AddUserMessage(strings.Repeat("user message ", 80))
		sess.AddAssistantMessage(strings.Repeat("assistant reply ", 80), nil)
	}

	before := len(sess.Messages())
	if before <= 5 {
		t.Fatalf("test setup invalid: before=%d", before)
	}

	pipe.PrepareForInference(t.Context(), sess, "", 0, TurnEnvelopePolicy{})

	after := len(sess.Messages())
	if after >= before {
		t.Fatalf("expected compacted session history, before=%d after=%d", before, after)
	}
	if after >= 10 {
		t.Fatalf("expected compaction to materially reduce history, before=%d after=%d", before, after)
	}
	if got := sess.Messages()[0].Role; got != "system" {
		t.Fatalf("first compacted message role=%q, want system", got)
	}
}

type captureSessionSizeExecutor struct {
	seenMessages int
}

func (e *captureSessionSizeExecutor) RunLoop(_ context.Context, sess *session.Session) (string, int, error) {
	e.seenMessages = len(sess.Messages())
	sess.AddAssistantMessage("ok", nil)
	return "ok", 1, nil
}

func TestRunStandardInference_CompactsSessionMessagesInPlace(t *testing.T) {
	store := testutil.TempStore(t)
	bgw := core.NewBackgroundWorker(1)
	exec := &captureSessionSizeExecutor{}
	pipe := &Pipeline{
		store:    store,
		executor: exec,
		bgWorker: bgw,
	}
	sess := session.New("s-run", "agent-1", "TestBot")
	sess.AddSystemMessage("System prompt")
	for i := 0; i < 20; i++ {
		sess.AddUserMessage(strings.Repeat("user message ", 80))
		sess.AddAssistantMessage(strings.Repeat("assistant reply ", 80), nil)
	}
	if _, err := store.ExecContext(context.Background(),
		`INSERT INTO sessions (id, agent_id, scope_key) VALUES (?, ?, ?)`,
		sess.ID, sess.AgentID, "scope:test",
	); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if _, err := store.ExecContext(context.Background(),
		`INSERT INTO turns (id, session_id) VALUES (?, ?)`,
		"t1", sess.ID,
	); err != nil {
		t.Fatalf("insert turn: %v", err)
	}

	before := len(sess.Messages())
	if before <= 5 {
		t.Fatalf("test setup invalid: before=%d", before)
	}

	if _, err := pipe.runStandardInference(context.Background(), Config{}, sess, "m1", "t1"); err != nil {
		t.Fatalf("runStandardInference: %v", err)
	}

	after := len(sess.Messages())
	if after >= before {
		t.Fatalf("expected compacted session history, before=%d after=%d", before, after)
	}
	if exec.seenMessages >= before {
		t.Fatalf("executor saw uncompacted history: before=%d executor=%d", before, exec.seenMessages)
	}
	if after != exec.seenMessages+1 {
		t.Fatalf("expected assistant append after executor, executor=%d after=%d", exec.seenMessages, after)
	}
	if got := sess.Messages()[0].Role; got != "system" {
		t.Fatalf("first compacted message role=%q, want system", got)
	}
}

func TestPrepareForInference_AddsSocialTurnConversationModeInstruction(t *testing.T) {
	pipe := New(PipelineDeps{})
	sess := session.New("s-social", "agent-1", "TestBot")
	sess.AddUserMessage("What's going on?")
	sess.SetTaskVerificationHints("conversational", "simple", "execute_directly", nil)

	pipe.PrepareForInference(t.Context(), sess, "", 0, TurnEnvelopePolicy{
		Weight:                 TurnWeightLight,
		LightweightToolSurface: true,
	})

	found := false
	for _, msg := range sess.Messages() {
		if msg.Role == "system" && strings.Contains(msg.Content, "[Conversation Mode]") {
			found = true
			if !strings.Contains(msg.Content, "Do not mention sandbox state") {
				t.Fatalf("conversation mode note missing operational-status constraint: %q", msg.Content)
			}
		}
	}
	if !found {
		t.Fatal("expected conversation mode instruction to be injected")
	}
}

func TestBuildDelegationReportingContract_RequiresEvidenceAndGapReporting(t *testing.T) {
	msg := buildDelegationReportingContract("subagent ran ls and found 12 markdown files")
	if !strings.Contains(msg, "[Delegation Reporting Contract]") {
		t.Fatal("expected delegation reporting contract header")
	}
	if !strings.Contains(msg, "Treat delegated output as evidence") {
		t.Fatal("expected delegated output to be framed as evidence")
	}
	if !strings.Contains(msg, "Subagents report to you; they do not report directly to the operator") {
		t.Fatal("expected contract to enforce orchestrator-only operator reporting")
	}
	if !strings.Contains(msg, "cite the concrete evidence or artifacts") {
		t.Fatal("expected contract to require concrete evidence or artifacts")
	}
	if !strings.Contains(msg, "remaining gaps, uncertainty, or unverified assumptions") {
		t.Fatal("expected contract to require gap and uncertainty reporting")
	}
	if !strings.Contains(msg, "Do not claim the delegated task succeeded unless the attached result proves it") {
		t.Fatal("expected contract to forbid unsupported delegated success claims")
	}
	if !strings.Contains(msg, "Repackage delegated results for the operator in clear operator-facing language") {
		t.Fatal("expected contract to require operator-facing repackaging")
	}
}
