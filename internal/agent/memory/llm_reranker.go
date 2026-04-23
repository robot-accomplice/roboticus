package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"roboticus/internal/llm"
)

// LLMRerankerConfig controls the optional semantic reranking stage that runs
// after fusion and before final deterministic filtering.
type LLMRerankerConfig struct {
	Enabled       bool
	MinCandidates int
	MaxCandidates int
	KeepTop       int
	Model         string
}

func DefaultLLMRerankerConfig() LLMRerankerConfig {
	return LLMRerankerConfig{
		Enabled:       false,
		MinCandidates: 5,
		MaxCandidates: 8,
		KeepTop:       5,
	}
}

type LLMReranker struct {
	config    LLMRerankerConfig
	completer llm.Completer
}

func NewLLMReranker(cfg LLMRerankerConfig, completer llm.Completer) *LLMReranker {
	return &LLMReranker{config: cfg, completer: completer}
}

type llmRerankResponse struct {
	RankedIDs []string `json:"ranked_ids"`
}

func (lr *LLMReranker) Rerank(ctx context.Context, query string, candidates []Evidence) ([]Evidence, bool) {
	status := "disabled"
	attempted := false
	inputCount := len(candidates)
	candidateCount := 0
	kept := 0
	model := ""
	changed := false
	defer func() {
		annotateLLMRerankStatus(ctx, status, attempted, inputCount, candidateCount, kept, model, changed)
	}()

	if !lr.config.Enabled {
		return nil, false
	}
	if lr.completer == nil {
		status = "no_completer"
		return nil, false
	}
	minCandidates := lr.config.MinCandidates
	if minCandidates <= 0 {
		minCandidates = 1
	}
	if len(candidates) < minCandidates {
		status = "below_threshold"
		return nil, false
	}

	selected := llmRerankCandidates(candidates, lr.config.MaxCandidates)
	candidateCount = len(selected)
	if len(selected) == 0 {
		status = "no_candidates"
		return nil, false
	}

	req := lr.buildRequest(query, selected)
	model = req.Model
	llmCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	attempted = true
	resp, err := lr.completer.Complete(llmCtx, req)
	if err != nil || resp == nil {
		status = "provider_error"
		return nil, false
	}
	if strings.TrimSpace(resp.Model) != "" {
		model = resp.Model
	}

	rankedIDs, ok := parseLLMRerankIDs(resp.Content)
	if !ok || len(rankedIDs) == 0 {
		status = "parse_failed"
		return nil, false
	}

	var (
		ordered  []Evidence
		reranked bool
	)
	ordered, reranked = applyLLMRanking(selected, rankedIDs, lr.config.KeepTop, model)
	changed = reranked
	if len(ordered) == 0 {
		status = "empty_ranking"
		return nil, false
	}

	status = "succeeded"
	kept = len(ordered)
	return ordered, true
}

func (lr *LLMReranker) buildRequest(query string, candidates []Evidence) *llm.Request {
	var b strings.Builder
	b.WriteString("You rank retrieved evidence for relevance to a user query.\n")
	b.WriteString("Return JSON only with this shape: {\"ranked_ids\":[\"c1\",\"c2\"]}.\n")
	b.WriteString("Rules:\n")
	b.WriteString("- Rank only the most relevant evidence IDs.\n")
	b.WriteString("- Prefer direct answer support over vague topical overlap.\n")
	b.WriteString("- Prefer canonical and corroborated evidence when relevance is otherwise similar.\n")
	b.WriteString("- Do not invent IDs.\n\n")
	b.WriteString("Query:\n")
	b.WriteString(query)
	b.WriteString("\n\nCandidates:\n")
	for i, ev := range candidates {
		fmt.Fprintf(&b, "%s | tier=%s | canonical=%t | corroboration=%d | source=%s | content=%s\n",
			llmCandidateID(i), ev.SourceTier, ev.IsCanonical, ev.CorroborationCount, llmCandidateSource(ev), compactEvidenceContent(ev.Content))
	}

	req := &llm.Request{
		Model:          strings.TrimSpace(lr.config.Model),
		Messages:       []llm.Message{{Role: "system", Content: "You are a retrieval reranker. Output strict JSON only."}, {Role: "user", Content: b.String()}},
		MaxTokens:      220,
		Temperature:    ptrFloat(0),
		NoEscalate:     true,
		AgentRole:      "subagent",
		TurnWeight:     "light",
		TaskIntent:     "question",
		TaskComplexity: "moderate",
		IntentClass:    "question",
	}
	return req
}

func llmRerankCandidates(candidates []Evidence, maxCandidates int) []Evidence {
	if len(candidates) == 0 {
		return nil
	}
	if maxCandidates <= 0 || maxCandidates > len(candidates) {
		maxCandidates = len(candidates)
	}
	out := make([]Evidence, maxCandidates)
	copy(out, candidates[:maxCandidates])
	return out
}

func parseLLMRerankIDs(raw string) ([]string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false
	}
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimPrefix(raw, "```json")
		raw = strings.TrimPrefix(raw, "```")
		raw = strings.TrimSuffix(strings.TrimSpace(raw), "```")
		raw = strings.TrimSpace(raw)
	}
	start := strings.IndexByte(raw, '{')
	end := strings.LastIndexByte(raw, '}')
	if start >= 0 && end > start {
		raw = raw[start : end+1]
	}
	var parsed llmRerankResponse
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, false
	}
	if len(parsed.RankedIDs) == 0 {
		return nil, false
	}
	return parsed.RankedIDs, true
}

func applyLLMRanking(candidates []Evidence, rankedIDs []string, keepTop int, model string) ([]Evidence, bool) {
	indexed := make(map[string]Evidence, len(candidates))
	original := make([]string, 0, len(candidates))
	for i, ev := range candidates {
		id := llmCandidateID(i)
		indexed[id] = ev
		original = append(original, id)
	}
	if keepTop <= 0 || keepTop > len(rankedIDs) {
		keepTop = len(rankedIDs)
	}
	out := make([]Evidence, 0, keepTop)
	finalIDs := make([]string, 0, keepTop)
	total := float64(keepTop + 1)
	for i, id := range rankedIDs {
		if len(out) >= keepTop {
			break
		}
		ev, ok := indexed[id]
		if !ok {
			continue
		}
		ev.LLMRank = i + 1
		ev.LLMScore = float64(keepTop-i) / total
		ev.LLMModel = model
		out = append(out, ev)
		finalIDs = append(finalIDs, id)
	}
	if len(out) == 0 {
		return nil, false
	}
	changed := !sameStringPrefix(original, finalIDs)
	return out, changed
}

func annotateLLMRerankStatus(ctx context.Context, status string, attempted bool, inputCount, candidateCount, kept int, model string, changed bool) {
	tracer := retrievalTracerFromContext(ctx)
	if tracer == nil {
		return
	}
	tracer.Annotate("retrieval.rerank.strategy", "llm_optional_then_score")
	tracer.Annotate("retrieval.rerank.llm.attempted", attempted)
	tracer.Annotate("retrieval.rerank.llm.status", status)
	tracer.Annotate("retrieval.rerank.llm.input", inputCount)
	tracer.Annotate("retrieval.rerank.llm.candidates", candidateCount)
	tracer.Annotate("retrieval.rerank.llm.kept", kept)
	tracer.Annotate("retrieval.rerank.llm.changed", changed)
	if model != "" {
		tracer.Annotate("retrieval.rerank.llm.model", model)
	}
}

func llmCandidateID(i int) string { return fmt.Sprintf("c%d", i+1) }

func llmCandidateSource(ev Evidence) string {
	if ev.SourceLabel != "" {
		return ev.SourceLabel
	}
	if ev.SourceTable != "" {
		return ev.SourceTable
	}
	return ev.SourceTier.String()
}

func compactEvidenceContent(content string) string {
	content = strings.Join(strings.Fields(strings.TrimSpace(content)), " ")
	if len(content) > 280 {
		return content[:277] + "..."
	}
	return content
}

func sameStringPrefix(a, b []string) bool {
	if len(b) > len(a) {
		return false
	}
	for i := range b {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func ptrFloat(v float64) *float64 { return &v }
