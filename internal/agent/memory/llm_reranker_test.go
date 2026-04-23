package memory

import (
	"context"
	"testing"

	"roboticus/internal/llm"
)

type fixtureCompleter struct {
	resp    *llm.Response
	err     error
	lastReq *llm.Request
}

func (f *fixtureCompleter) Complete(_ context.Context, req *llm.Request) (*llm.Response, error) {
	f.lastReq = req
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

func (f *fixtureCompleter) Stream(context.Context, *llm.Request) (<-chan llm.StreamChunk, <-chan error) {
	return nil, nil
}

func TestLLMReranker_ReranksAndAnnotates(t *testing.T) {
	tracer := &fixtureTracer{}
	ctx := WithRetrievalTracer(context.Background(), tracer)
	completer := &fixtureCompleter{
		resp: &llm.Response{
			Model:   "ollama/qwen3:30b-a3b",
			Content: `{"ranked_ids":["c2","c1","c4"]}`,
		},
	}
	lr := NewLLMReranker(LLMRerankerConfig{
		Enabled:       true,
		MinCandidates: 3,
		MaxCandidates: 4,
		KeepTop:       3,
	}, completer)

	candidates := []Evidence{
		{Content: "generic service note", FusionScore: 0.91},
		{Content: "billing depends on ledger service", FusionScore: 0.90, IsCanonical: true},
		{Content: "old topic overlap", FusionScore: 0.70},
		{Content: "ledger owned by revenue platform", FusionScore: 0.60},
	}

	out, ok := lr.Rerank(ctx, "what depends on ledger service?", candidates)
	if !ok {
		t.Fatal("expected rerank success")
	}
	if len(out) != 3 {
		t.Fatalf("expected 3 reranked candidates, got %d", len(out))
	}
	if out[0].Content != "billing depends on ledger service" {
		t.Fatalf("expected semantic rerank to elevate direct support, got %q", out[0].Content)
	}
	if out[0].LLMRank != 1 || out[0].LLMScore <= 0 {
		t.Fatalf("expected LLM rank metadata on first result, got %+v", out[0])
	}
	if completer.lastReq == nil || !completer.lastReq.NoEscalate {
		t.Fatal("expected no-escalate LLM rerank request")
	}
	for _, key := range []string{
		"retrieval.rerank.llm.attempted",
		"retrieval.rerank.llm.status",
		"retrieval.rerank.llm.changed",
		"retrieval.rerank.llm.model",
	} {
		if _, ok := tracer.get(key); !ok {
			t.Fatalf("expected %s annotation, got %+v", key, tracer.entries)
		}
	}
	if got, _ := tracer.get("retrieval.rerank.llm.status"); got != "succeeded" {
		t.Fatalf("expected succeeded status, got %v", got)
	}
}

func TestLLMReranker_ParseFailureFallsBackCleanly(t *testing.T) {
	tracer := &fixtureTracer{}
	ctx := WithRetrievalTracer(context.Background(), tracer)
	completer := &fixtureCompleter{
		resp: &llm.Response{
			Model:   "ollama/qwen3:30b-a3b",
			Content: `not-json`,
		},
	}
	lr := NewLLMReranker(LLMRerankerConfig{
		Enabled:       true,
		MinCandidates: 2,
		MaxCandidates: 4,
		KeepTop:       2,
	}, completer)

	candidates := []Evidence{
		{Content: "first", FusionScore: 0.8},
		{Content: "second", FusionScore: 0.7},
	}

	if out, ok := lr.Rerank(ctx, "query", candidates); ok || out != nil {
		t.Fatalf("expected rerank failure fallback, got ok=%v out=%+v", ok, out)
	}
	if got, _ := tracer.get("retrieval.rerank.llm.status"); got != "parse_failed" {
		t.Fatalf("expected parse_failed status, got %v", got)
	}
}
