package pipeline

import (
	"context"
	"testing"

	"roboticus/internal/db"
	"roboticus/internal/session"
	"roboticus/testutil"
)

func TestMaybeCheckpoint_UsesRepositoryShape(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		BGWorker: testutil.BGWorker(t, 2),
	})

	sess := session.New("sess-ckpt", "agent-1", "TestBot")
	if _, err := store.FindOrCreateSession(context.Background(), sess.AgentID, "scope:checkpoint"); err != nil {
		t.Fatalf("FindOrCreateSession: %v", err)
	}
	var storedID string
	if err := store.QueryRowContext(context.Background(),
		`SELECT id FROM sessions WHERE agent_id = ? ORDER BY created_at DESC LIMIT 1`,
		sess.AgentID,
	).Scan(&storedID); err != nil {
		t.Fatalf("load session id: %v", err)
	}
	sess.ID = storedID
	for i := 0; i < 10; i++ {
		sess.AddUserMessage("user turn")
	}
	sess.AddSystemMessage("memory block")
	sess.AddSystemMessage("hippocampus note")
	sess.AddAssistantMessage("latest assistant reply", nil)

	pipe.maybeCheckpoint(context.Background(), sess, "turn-10")

	var rec db.CheckpointRecord
	var activeTasks, digest string
	err := store.QueryRowContext(context.Background(),
		`SELECT session_id, system_prompt_hash, memory_summary, COALESCE(active_tasks, ''), COALESCE(conversation_digest, ''), turn_count
		   FROM context_checkpoints
		  WHERE session_id = ?
		  ORDER BY created_at DESC, rowid DESC
		  LIMIT 1`,
		sess.ID,
	).Scan(&rec.SessionID, &rec.SystemPromptHash, &rec.MemorySummary, &activeTasks, &digest, &rec.TurnCount)
	if err != nil {
		t.Fatalf("query checkpoint: %v", err)
	}
	rec.ActiveTasks = activeTasks
	rec.ConversationDigest = digest

	if rec.SessionID != sess.ID {
		t.Fatalf("SessionID = %q, want %q", rec.SessionID, sess.ID)
	}
	if rec.SystemPromptHash == "" {
		t.Fatal("SystemPromptHash should not be empty")
	}
	if rec.MemorySummary == "" {
		t.Fatal("MemorySummary should not be empty")
	}
	if rec.ConversationDigest != "latest assistant reply" {
		t.Fatalf("ConversationDigest = %q, want latest assistant reply", rec.ConversationDigest)
	}
	if rec.TurnCount != 10 {
		t.Fatalf("TurnCount = %d, want 10", rec.TurnCount)
	}
}
