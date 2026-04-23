package pipeline

import (
	"context"
	"strings"
	"testing"

	"roboticus/internal/agent"
	agentmemory "roboticus/internal/agent/memory"
	"roboticus/internal/llm"
	"roboticus/internal/session"
	"roboticus/testutil"
)

func procedureFromSteps(steps []string, count int) agent.Procedure {
	return agent.Procedure{Steps: append([]string(nil), steps...), Count: count}
}

func newFailingToolSession(t *testing.T, steps []string, errorBodies []string) *session.Session {
	t.Helper()
	sess := session.New("s-promo", "a1", "Bot")
	sess.AddUserMessage("Deploy the service safely.")
	sess.SetTaskVerificationHints("task", "complex", "execute_directly", []string{
		"drain traffic first", "flip feature flag", "verify canary health",
	})
	for i, step := range steps {
		calls := []llm.ToolCall{{
			ID:       step + "-call",
			Type:     "function",
			Function: llm.ToolCallFunc{Name: step, Arguments: `{}`},
		}}
		sess.AddAssistantMessage("using "+step, calls)
		body := "ok"
		isErr := false
		if i < len(errorBodies) && errorBodies[i] != "" {
			body = errorBodies[i]
			isErr = true
		}
		sess.AddToolResult(step+"-call", step, body, isErr)
	}
	return sess
}

func TestExtractErrorModesFromSession_PicksFirstLinePerStep(t *testing.T) {
	sess := newFailingToolSession(t,
		[]string{"shell", "deploy"},
		[]string{"Error: target not found\n  at line 5", ""},
	)
	modes := extractErrorModesFromSession(sess, []string{"shell", "deploy"})
	if len(modes) != 1 {
		t.Fatalf("expected 1 error mode, got %+v", modes)
	}
	if !strings.Contains(modes[0], "target not found") {
		t.Fatalf("expected first-line extraction to carry the root error text, got %q", modes[0])
	}
	if !strings.HasPrefix(modes[0], "shell: ") {
		t.Fatalf("expected tool-name prefix, got %q", modes[0])
	}
	if strings.Contains(modes[0], "\n") || strings.Contains(modes[0], "line 5") {
		t.Fatalf("expected continuation dropped, got %q", modes[0])
	}
}

func TestExtractErrorModesFromSession_Deduplicates(t *testing.T) {
	sess := newFailingToolSession(t,
		[]string{"shell", "shell", "shell"},
		[]string{"Error: same message", "Error: same message", "Error: same message"},
	)
	modes := extractErrorModesFromSession(sess, []string{"shell"})
	if len(modes) != 1 {
		t.Fatalf("expected dedup, got %+v", modes)
	}
}

func TestExtractPreconditionsFromSession_UsesTaskHintsAndSubgoals(t *testing.T) {
	sess := newFailingToolSession(t, []string{"shell"}, []string{""})
	pres := extractPreconditionsFromSession(sess)
	if len(pres) == 0 {
		t.Fatalf("expected preconditions, got empty")
	}
	joined := strings.Join(pres, " | ")
	if !strings.Contains(joined, "intent = task") {
		t.Fatalf("expected intent precondition, got %q", joined)
	}
	if !strings.Contains(joined, "complexity = complex") {
		t.Fatalf("expected complexity precondition, got %q", joined)
	}
	if !strings.Contains(joined, "subgoal: drain traffic first") {
		t.Fatalf("expected subgoal precondition, got %q", joined)
	}
}

func TestPromoteProcedureToWorkflow_PersistsRichMetadata(t *testing.T) {
	store := testutil.TempStore(t)
	p := &Pipeline{store: store}
	ctx := context.Background()

	// Build a session whose tool chain includes a failing step so the
	// promotion captures a real error mode, not just boilerplate tags.
	sess := newFailingToolSession(t,
		[]string{"shell", "deploy", "verify"},
		[]string{"", "Error: rollout blocked by legal review", ""},
	)
	if _, err := store.ExecContext(ctx,
		`INSERT INTO turn_diagnostics (id, turn_id, session_id, channel, status)
		 VALUES (?, ?, ?, 'api', 'ok')`,
		"td-promote", "turn-promote", sess.ID,
	); err != nil {
		t.Fatalf("insert turn diagnostics: %v", err)
	}

	proc := struct {
		Steps []string
		Count int
	}{Steps: []string{"shell", "deploy", "verify"}, Count: 3}
	// Convert anonymous struct to agent.Procedure via the exposed type.
	// We invoke promoteProcedureToWorkflow directly to avoid running the
	// full detector in this test.
	p.promoteProcedureToWorkflow(ctx, "turn-promote", sess, procedureFromSteps(proc.Steps, proc.Count))

	mm := agentmemory.NewManager(agentmemory.DefaultConfig(), store)
	got, err := mm.GetWorkflow(ctx, "shell → deploy → verify")
	if err != nil || got == nil {
		t.Fatalf("expected promoted workflow, got %v %v", got, err)
	}
	if len(got.ErrorModes) != 1 {
		t.Fatalf("expected error_modes captured from session, got %+v", got.ErrorModes)
	}
	if !strings.Contains(got.ErrorModes[0], "rollout blocked by legal review") {
		t.Fatalf("expected error mode to carry the failing tool output, got %q", got.ErrorModes[0])
	}
	if len(got.Preconditions) == 0 {
		t.Fatalf("expected preconditions populated, got empty")
	}
	// Context tags should carry both auto_promoted and the task intent.
	var sawIntent, sawTag bool
	for _, tag := range got.ContextTags {
		if tag == "auto_promoted" {
			sawTag = true
		}
		if strings.HasPrefix(tag, "intent:") {
			sawIntent = true
		}
	}
	if !sawIntent || !sawTag {
		t.Fatalf("expected auto_promoted + intent:* context tags, got %+v", got.ContextTags)
	}

	var detailsJSON string
	if err := store.QueryRowContext(ctx,
		`SELECT details_json
		   FROM turn_diagnostic_events
		  WHERE turn_id = ? AND event_type = 'procedural_knowledge_promoted'
		  ORDER BY seq DESC LIMIT 1`,
		"turn-promote",
	).Scan(&detailsJSON); err != nil {
		t.Fatalf("query promotion RCA event: %v", err)
	}
	if !strings.Contains(detailsJSON, `"promotion_target":"procedural_memory.workflow"`) {
		t.Fatalf("details_json = %q, want workflow promotion target", detailsJSON)
	}
	if !strings.Contains(detailsJSON, `"workflow_name":"shell → deploy → verify"`) {
		t.Fatalf("details_json = %q, want workflow name", detailsJSON)
	}
}
