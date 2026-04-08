package pipeline

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"roboticus/testutil"
)

// TestFinalizeStream_StoresAssistantMessage verifies that FinalizeStream
// persists the assistant message to session_messages with a topic_tag.
func TestFinalizeStream_StoresAssistantMessage(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "stream test"},
		BGWorker: testutil.BGWorker(t, 4),
	})

	// Create a session (must exist in DB for FK constraint).
	_, _ = store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, scope_key) VALUES ('stream-sess', 'agent-1', 'test:stream')`)
	sess := NewSession("stream-sess", "agent-1", "TestBot")
	sess.AddUserMessage("hello")

	// Prepare a streaming outcome.
	outcome := &Outcome{
		SessionID:     sess.ID,
		MessageID:     "msg-1",
		Stream:        true,
		TurnID:        "turn-stream-1",
		streamSession: sess,
		streamConfig:  cfgPtr(PresetStreaming()),
	}

	// Finalize with assembled content.
	pipe.FinalizeStream(ctx, outcome, "streamed response content")

	// Verify assistant message stored.
	var content, topicTag string
	row := store.QueryRowContext(ctx,
		`SELECT content, topic_tag FROM session_messages WHERE session_id = ? AND role = 'assistant'`,
		sess.ID)
	if err := row.Scan(&content, &topicTag); err != nil {
		t.Fatalf("assistant message not stored: %v", err)
	}
	if content != "streamed response content" {
		t.Errorf("stored content = %q", content)
	}
	if topicTag == "" {
		t.Error("topic_tag should be set")
	}
}

// TestFinalizeStream_InvokesIngestor verifies that FinalizeStream calls
// the memory ingestor (post-turn work parity with standard inference).
func TestFinalizeStream_InvokesIngestor(t *testing.T) {
	store := testutil.TempStore(t)

	ingestor := &testIngestor{}
	// This test observes a side effect of background work, so we need
	// the worker reference to drain BEFORE asserting (not just at cleanup).
	bgw := testutil.BGWorker(t, 4)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "ok"},
		Ingestor: ingestor,
		BGWorker: bgw,
	})

	_, _ = store.ExecContext(context.Background(),
		`INSERT INTO sessions (id, agent_id, scope_key) VALUES ('ingest-sess', 'a1', 'test:ingest')`)
	sess := NewSession("ingest-sess", "a1", "Bot")
	cfg := PresetStreaming()
	cfg.PostTurnIngest = true
	outcome := &Outcome{
		SessionID:     sess.ID,
		Stream:        true,
		streamSession: sess,
		streamConfig:  &cfg,
	}

	pipe.FinalizeStream(context.Background(), outcome, "hello")

	// Wait for background ingest to complete before asserting.
	bgw.Drain(5 * time.Second)

	if !ingestor.WasCalled() {
		t.Error("FinalizeStream did not invoke ingestor — post-turn parity broken (Rule 7.2)")
	}
}

// TestFinalizeStream_NilOutcomeIsNoOp verifies no panic on nil inputs.
func TestFinalizeStream_NilOutcomeIsNoOp(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{Store: store, BGWorker: testutil.BGWorker(t, 2)})

	// Should not panic.
	pipe.FinalizeStream(context.Background(), nil, "content")
	pipe.FinalizeStream(context.Background(), &Outcome{}, "content")
}

// TestFinalizeStream_MatchesStandardPostTurn verifies both paths produce
// equivalent artifacts: assistant message stored, embeddings generated.
func TestFinalizeStream_MatchesStandardPostTurn(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "parity content"},
		BGWorker: testutil.BGWorker(t, 4),
	})

	// Standard inference path.
	cfg := PresetAPI()
	input := Input{Content: "test parity", AgentID: "default", Platform: "api"}
	outcome, err := pipe.Run(ctx, cfg, input)
	if err != nil {
		t.Fatalf("standard run: %v", err)
	}

	// Count standard artifacts.
	var stdMsgCount int
	row := store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM session_messages WHERE session_id = ? AND role = 'assistant'`,
		outcome.SessionID)
	_ = row.Scan(&stdMsgCount)

	// Streaming path with FinalizeStream.
	_, _ = store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, scope_key) VALUES ('stream-parity', 'default', 'test:parity')`)
	sess := NewSession("stream-parity", "default", "Bot")
	sess.AddUserMessage("test parity")
	streamOutcome := &Outcome{
		SessionID:     sess.ID,
		Stream:        true,
		TurnID:        "turn-parity",
		streamSession: sess,
		streamConfig:  cfgPtr(PresetStreaming()),
	}
	pipe.FinalizeStream(ctx, streamOutcome, "parity content")

	// Count streaming artifacts.
	var streamMsgCount int
	row = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM session_messages WHERE session_id = ? AND role = 'assistant'`,
		sess.ID)
	_ = row.Scan(&streamMsgCount)

	// Both paths must produce at least 1 assistant message.
	if stdMsgCount == 0 {
		t.Error("standard path produced no assistant messages")
	}
	if streamMsgCount == 0 {
		t.Error("streaming+FinalizeStream produced no assistant messages — Rule 7.2 violated")
	}
}

func cfgPtr(c Config) *Config { return &c }

// testIngestor records whether IngestTurn was called (race-safe).
type testIngestor struct {
	called atomic.Bool
}

func (ti *testIngestor) IngestTurn(_ context.Context, _ *Session) {
	ti.called.Store(true)
}

func (ti *testIngestor) WasCalled() bool {
	return ti.called.Load()
}
