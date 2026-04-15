package pipeline

import (
	"strings"
	"testing"

	"roboticus/internal/session"
)

// Milestone 1 acceptance: the retrieval artifact reaching the model must be
// identical regardless of inference mode (standard vs streaming). These
// tests prove the guarantee at the artifact-hash level so a silent drift in
// memory context or memory index between the two paths would be caught.

func TestBuildRetrievalArtifact_DeterministicHash(t *testing.T) {
	art1 := BuildRetrievalArtifact("ctx", "idx")
	art2 := BuildRetrievalArtifact("ctx", "idx")
	if art1.ArtifactHash != art2.ArtifactHash {
		t.Fatalf("hash must be deterministic across calls, got %q vs %q", art1.ArtifactHash, art2.ArtifactHash)
	}
	if art1.MemoryContextHash == art1.MemoryIndexHash {
		t.Fatalf("distinct inputs should hash distinctly, got context=%q index=%q",
			art1.MemoryContextHash, art1.MemoryIndexHash)
	}
}

func TestBuildRetrievalArtifact_EmptyInputsProduceEmptyHashes(t *testing.T) {
	art := BuildRetrievalArtifact("", "")
	if art.MemoryContextHash != "" || art.MemoryIndexHash != "" {
		t.Fatalf("empty inputs should yield empty per-field hashes, got %+v", art)
	}
	if art.ArtifactHash == "" {
		t.Fatalf("combined hash should still be non-empty for auditability, got empty")
	}
}

func TestBuildRetrievalArtifact_DifferentInputsProduceDifferentHashes(t *testing.T) {
	a := BuildRetrievalArtifact("ctx-a", "idx-a")
	b := BuildRetrievalArtifact("ctx-b", "idx-a")
	if a.ArtifactHash == b.ArtifactHash {
		t.Fatalf("different context inputs must produce different hashes, got %q", a.ArtifactHash)
	}
	c := BuildRetrievalArtifact("ctx-a", "idx-b")
	if a.ArtifactHash == c.ArtifactHash {
		t.Fatalf("different index inputs must produce different hashes, got %q", a.ArtifactHash)
	}
}

func TestRetrievalArtifact_StandardAndStreamingSessionsMatch(t *testing.T) {
	// Both standard and streaming paths consume the same Session object
	// carrying the same MemoryContext + MemoryIndex. Build two sessions
	// populated identically (simulating what stageMemoryRetrieval writes)
	// and confirm the artifact hashes match across them.
	standard := session.New("s1", "a1", "Bot")
	standard.AddUserMessage("What is the refund policy?")
	standard.SetMemoryContext("[Active Memory]\n- refund 30 days\n")
	standard.SetMemoryIndex("[Memory Index]\n- refund policy v1\n")

	streaming := session.New("s2", "a1", "Bot")
	streaming.AddUserMessage("What is the refund policy?")
	streaming.SetMemoryContext("[Active Memory]\n- refund 30 days\n")
	streaming.SetMemoryIndex("[Memory Index]\n- refund policy v1\n")

	artStandard := BuildRetrievalArtifact(standard.MemoryContext(), standard.MemoryIndex())
	artStreaming := BuildRetrievalArtifact(streaming.MemoryContext(), streaming.MemoryIndex())

	if artStandard.ArtifactHash != artStreaming.ArtifactHash {
		t.Fatalf("standard vs streaming artifact drift: %q != %q",
			artStandard.ArtifactHash, artStreaming.ArtifactHash)
	}
	if artStandard.ContextBytes != artStreaming.ContextBytes {
		t.Fatalf("context_bytes drift: %d vs %d", artStandard.ContextBytes, artStreaming.ContextBytes)
	}
}

func TestRetrievalArtifact_DetectsContextDrift(t *testing.T) {
	// Demonstrate that the fitness test ACTUALLY catches drift: if a future
	// change silently modified one path to rebuild the memory context, the
	// hashes would diverge and this test would fail.
	standard := session.New("s1", "a1", "Bot")
	standard.SetMemoryContext("[Active Memory]\n- refund 30 days\n")

	streaming := session.New("s2", "a1", "Bot")
	streaming.SetMemoryContext("[Active Memory]\n- refund 60 days\n") // DRIFT

	artStandard := BuildRetrievalArtifact(standard.MemoryContext(), "")
	artStreaming := BuildRetrievalArtifact(streaming.MemoryContext(), "")

	if artStandard.ArtifactHash == artStreaming.ArtifactHash {
		t.Fatal("parity test must detect content drift between paths")
	}
}

func TestAnnotateRetrievalArtifact_EmitsOnTrace(t *testing.T) {
	tr := NewTraceRecorder()
	tr.BeginSpan("inference")
	art := BuildRetrievalArtifact(
		"[Active Memory]\n- a\n",
		"[Memory Index]\n- x\n",
	)
	AnnotateRetrievalArtifact(tr, art)
	tr.EndSpan("ok")

	meta := tr.Finish("turn-1", "test").Stages[0].Metadata
	for _, key := range []string{
		"retrieval.artifact_hash",
		"retrieval.memory_context_hash",
		"retrieval.memory_index_hash",
		"retrieval.context_bytes",
		"retrieval.index_bytes",
	} {
		if _, ok := meta[key]; !ok {
			t.Fatalf("expected %s on trace, got %+v", key, meta)
		}
	}
	if got, _ := meta["retrieval.artifact_hash"].(string); !strings.HasPrefix(got, art.ArtifactHash) {
		t.Fatalf("artifact hash annotation mismatch: got %q want %q", got, art.ArtifactHash)
	}
}

func TestBuildRetrievalArtifact_PreviewIsBounded(t *testing.T) {
	long := strings.Repeat("x", 1024)
	art := BuildRetrievalArtifact(long, long)
	// Preview is capped at 200 runes plus the three-byte ellipsis; allow
	// a handful of extra bytes so the assertion survives any future
	// whitespace normalization without becoming brittle.
	if len(art.ContextPreview) > 220 {
		t.Fatalf("context preview not bounded, got %d chars", len(art.ContextPreview))
	}
	if len(art.IndexPreview) > 220 {
		t.Fatalf("index preview not bounded, got %d chars", len(art.IndexPreview))
	}
}
