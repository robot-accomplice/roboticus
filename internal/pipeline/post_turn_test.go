package pipeline

import (
	"context"
	"strings"
	"testing"

	"roboticus/internal/session"
	"roboticus/testutil"
)

func TestChunkText_SmallInput(t *testing.T) {
	chunks := ChunkText("Hello world.", 512)
	if len(chunks) != 1 {
		t.Errorf("small input should produce 1 chunk, got %d", len(chunks))
	}
	if chunks[0] != "Hello world." {
		t.Errorf("chunk = %q, want %q", chunks[0], "Hello world.")
	}
}

func TestChunkText_Empty(t *testing.T) {
	chunks := ChunkText("", 512)
	if chunks != nil {
		t.Errorf("empty input should return nil, got %v", chunks)
	}
}

func TestChunkText_SentenceBoundary(t *testing.T) {
	// Create a text with clear sentence boundaries.
	text := "First sentence here. Second sentence here. Third sentence here. Fourth sentence here. Fifth sentence here."
	chunks := ChunkText(text, 50)

	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d: %v", len(chunks), chunks)
	}

	// Each chunk should be <= 50 chars (plus a small tolerance for sentence boundary search).
	for i, c := range chunks {
		if len(c) > 60 { // allow some slack for sentence boundary not being perfect
			t.Errorf("chunk %d too long: %d chars: %q", i, len(c), c)
		}
	}

	// Reconstructing all chunks should cover the full original text.
	var totalLen int
	for _, c := range chunks {
		totalLen += len(c)
	}
	// Some whitespace may be trimmed, so just check we got most of the content.
	if totalLen < len(text)-len(chunks)*2 {
		t.Errorf("chunks lost too much content: total %d, original %d", totalLen, len(text))
	}
}

func TestChunkText_NoSentenceBoundary(t *testing.T) {
	// Long text without sentence-ending punctuation.
	text := "this is a long string without any sentence boundaries that keeps going on and on and on and on forever"
	chunks := ChunkText(text, 30)

	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}

	// Verify all content is captured.
	var totalLen int
	for _, c := range chunks {
		totalLen += len(c)
	}
	if totalLen < len(text)-len(chunks)*2 {
		t.Errorf("chunks lost content: total %d, original %d", totalLen, len(text))
	}
}

func TestChunkText_ZeroMaxChars(t *testing.T) {
	chunks := ChunkText("hello", 0)
	if len(chunks) != 1 {
		t.Errorf("zero maxChars should default, got %d chunks", len(chunks))
	}
}

func TestFindSentenceBoundary_AtPunctuation(t *testing.T) {
	text := "First sentence. Second sentence."
	cut := findSentenceBoundary(text, 20)
	// Should find the period after "First sentence."
	if cut < 1 || cut > 20 {
		t.Errorf("sentence boundary at %d, expected within budget", cut)
	}
}

func TestTruncatePreview(t *testing.T) {
	short := "hello"
	if got := truncatePreview(short, 200); got != "hello" {
		t.Errorf("short string: got %q, want %q", got, "hello")
	}

	long := "a" + string(make([]byte, 250))
	got := truncatePreview(long, 200)
	if len(got) != 203 { // 200 + "..."
		t.Errorf("long string truncated to %d chars, want 203", len(got))
	}
}

func TestPostTurnIngest_NilWorker(t *testing.T) {
	p := &Pipeline{} // no bgWorker
	// Should not panic.
	p.PostTurnIngest(context.Background(), NewSession("s", "a", "n"), "t1", "content")
}

func TestPostTurnIngest_EmptyContent(t *testing.T) {
	p := &Pipeline{}
	// Should not panic with empty content.
	p.PostTurnIngest(context.Background(), NewSession("s", "a", "n"), "t1", "")
}

func TestReflectOnTurn_UsesPersistedTurnArtifacts(t *testing.T) {
	store := testutil.TempStore(t)
	p := &Pipeline{store: store}
	ctx := context.Background()

	sess, err := store.FindOrCreateSession(ctx, "agent-reflect", "scope:reflect")
	if err != nil {
		t.Fatalf("FindOrCreateSession: %v", err)
	}
	if _, err := store.ExecContext(ctx,
		`INSERT INTO turns (id, session_id) VALUES (?, ?)`,
		"turn-reflect", sess.ID,
	); err != nil {
		t.Fatalf("insert turn: %v", err)
	}
	if _, err := store.ExecContext(ctx,
		`INSERT INTO tool_calls (id, turn_id, tool_name, input, output, status, duration_ms)
		 VALUES
		   ('tc-1', 'turn-reflect', 'search_memories', '{}', 'ok', 'success', 1200),
		   ('tc-2', 'turn-reflect', 'bash', '{}', 'error: denied', 'error', 350)`,
	); err != nil {
		t.Fatalf("insert tool calls: %v", err)
	}
	if _, err := store.ExecContext(ctx,
		`INSERT INTO pipeline_traces (id, turn_id, session_id, total_ms, stages_json)
		 VALUES ('pt-1', 'turn-reflect', ?, 2200, '[]')`,
		sess.ID,
	); err != nil {
		t.Fatalf("insert pipeline trace: %v", err)
	}

	live := session.New(sess.ID, sess.AgentID, "TestBot")
	live.AddUserMessage("find something relevant")
	live.AddAssistantMessage("final answer after tools", nil)

	p.reflectOnTurn(ctx, "turn-reflect", "find something relevant", live)

	var content string
	if err := store.QueryRowContext(ctx,
		`SELECT content
		   FROM episodic_memory
		  WHERE classification = 'episode_summary'
		  ORDER BY created_at DESC, rowid DESC
		  LIMIT 1`,
	).Scan(&content); err != nil {
		t.Fatalf("load episode summary: %v", err)
	}
	if content == "" {
		t.Fatal("episode summary content should not be empty")
	}
	if !containsAll(content,
		"Actions: search_memories",
		"bash",
		"Errors: error: denied",
		"Duration: 2s",
	) {
		t.Fatalf("episode summary did not reflect persisted turn artifacts: %q", content)
	}
}

func containsAll(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(haystack, needle) {
			return false
		}
	}
	return true
}
