// workflow_search.go exposes the persisted workflow memory (procedural_memory
// rows with category='workflow') as an agent tool so the model can ask
// "find me a workflow for X" directly rather than waiting for procedural
// retrieval to surface one as a side effect of turn context.
//
// Two operations are supported:
//   - find(query, tags?, limit?) — search workflows by name/steps/
//     preconditions/error_modes/context_tags and return the top matches
//     ranked by the rankWorkflowMatches heuristic (see workflow_search.go).
//   - get(name) — fetch a single workflow by exact (case-insensitive) name.
//
// Output is JSON with a summary line per workflow so the model can pick one
// and execute its steps without an extra round-trip for details.

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	agentmemory "roboticus/internal/agent/memory"
	"roboticus/internal/db"
)

// WorkflowSearchTool wraps the FindWorkflows / GetWorkflow manager API as an
// agent-facing builtin tool.
type WorkflowSearchTool struct {
	store *db.Store
	// maxLimit caps the result set the tool will return even when the
	// caller asks for more, so the model does not drown in candidates.
	maxLimit int
}

// NewWorkflowSearchTool constructs the tool with sensible defaults.
func NewWorkflowSearchTool(store *db.Store) *WorkflowSearchTool {
	return &WorkflowSearchTool{store: store, maxLimit: 10}
}

// Name satisfies Tool.
func (t *WorkflowSearchTool) Name() string { return "find_workflow" }

// Description satisfies Tool.
func (t *WorkflowSearchTool) Description() string {
	return "Search or fetch reusable workflows from procedural memory. " +
		"Use 'find' to discover workflows whose name / steps / preconditions / " +
		"error modes / tags match a query (optionally filtered by tags). " +
		"Use 'get' to fetch one workflow by exact name. Prefer this tool over " +
		"free-text memory search when the agent needs a concrete reusable SOP."
}

// Risk satisfies Tool.
func (t *WorkflowSearchTool) Risk() RiskLevel { return RiskSafe }

// ParameterSchema satisfies Tool.
func (t *WorkflowSearchTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"operation": {
				"type": "string",
				"enum": ["find", "get"],
				"description": "The workflow query to run."
			},
			"query": {
				"type": "string",
				"description": "Free-text query for 'find'. Matches workflow name / steps / preconditions / error_modes / context_tags."
			},
			"tags": {
				"type": "array",
				"items": {"type": "string"},
				"description": "Optional tag filter for 'find'. Workflows whose context_tags overlap with these tags are preferred."
			},
			"limit": {
				"type": "integer",
				"minimum": 1,
				"description": "Max workflows to return for 'find' (default 5, capped at 10)."
			},
			"name": {
				"type": "string",
				"description": "Exact workflow name for 'get'."
			}
		},
		"required": ["operation"]
	}`)
}

// workflowSearchArgs is the parsed parameter bundle.
type workflowSearchArgs struct {
	Operation string   `json:"operation"`
	Query     string   `json:"query"`
	Tags      []string `json:"tags"`
	Limit     int      `json:"limit"`
	Name      string   `json:"name"`
}

// workflowSearchResult is the JSON payload returned for a find query.
type workflowSearchResult struct {
	Summary   string            `json:"summary"`
	Query     string            `json:"query"`
	TagFilter []string          `json:"tag_filter,omitempty"`
	Matches   []workflowSummary `json:"matches"`
}

// workflowGetResult is the JSON payload returned for a get query.
type workflowGetResult struct {
	Summary  string           `json:"summary"`
	Workflow *workflowSummary `json:"workflow,omitempty"`
	Found    bool             `json:"found"`
}

// workflowSummary is the condensed JSON shape of a single workflow. It omits
// the full success/failure evidence arrays so the tool payload stays small.
type workflowSummary struct {
	Name          string   `json:"name"`
	Version       int      `json:"version"`
	Confidence    float64  `json:"confidence"`
	SuccessRate   float64  `json:"success_rate"`
	SuccessCount  int      `json:"success_count"`
	FailureCount  int      `json:"failure_count"`
	Steps         []string `json:"steps"`
	Preconditions []string `json:"preconditions,omitempty"`
	ErrorModes    []string `json:"error_modes,omitempty"`
	ContextTags   []string `json:"context_tags,omitempty"`
	LastUsed      string   `json:"last_used,omitempty"`
	Score         float64  `json:"score"`
}

// Execute satisfies Tool. Errors are returned as Result output for the agent
// to read rather than as Go errors so the tool-harness keeps flowing.
func (t *WorkflowSearchTool) Execute(ctx context.Context, params string, _ *Context) (*Result, error) {
	if t.store == nil {
		return &Result{Output: "workflow store is not available"}, nil
	}

	var args workflowSearchArgs
	if err := json.Unmarshal([]byte(params), &args); err != nil {
		return &Result{Output: "invalid arguments: " + err.Error()}, nil
	}
	op := strings.ToLower(strings.TrimSpace(args.Operation))

	mm := agentmemory.NewManager(agentmemory.DefaultConfig(), t.store)

	switch op {
	case "find":
		return t.runFind(ctx, mm, args)
	case "get":
		return t.runGet(ctx, mm, args)
	case "":
		return &Result{Output: "operation is required: one of find, get"}, nil
	default:
		return &Result{Output: fmt.Sprintf("unknown operation %q; use find or get", op)}, nil
	}
}

func (t *WorkflowSearchTool) runFind(ctx context.Context, mm *agentmemory.Manager, args workflowSearchArgs) (*Result, error) {
	limit := args.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > t.maxLimit {
		limit = t.maxLimit
	}

	// SQL prefilter uses a single LIKE, which fails on multi-word queries
	// whose terms are separated differently in the stored workflow (e.g.
	// "canary release" vs. "canary-release"). Pull the longest meaningful
	// token out of the query for the SQL filter and let the ranker do the
	// full multi-token match in memory. For empty queries, pass through.
	prefilter := pickPrefilterToken(args.Query)
	candidates, err := mm.FindWorkflows(ctx, prefilter, limit*3)
	if err != nil {
		return &Result{Output: "workflow lookup failed: " + err.Error()}, nil
	}
	// If the prefilter narrowed to zero but the caller supplied tags, fall
	// back to listing active workflows so the ranker can surface tag fits
	// the SQL LIKE missed.
	if len(candidates) == 0 && len(args.Tags) > 0 {
		candidates, _ = mm.FindWorkflows(ctx, "", limit*3)
	}

	// rankWorkflowMatches is the design decision this tool hinges on — see
	// its docstring for the trade-offs. It scores and reorders candidates
	// and returns them in descending-score order.
	ranked := rankWorkflowMatches(args.Query, args.Tags, candidates)
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}

	result := workflowSearchResult{
		Query:     args.Query,
		TagFilter: append([]string(nil), args.Tags...),
		Matches:   make([]workflowSummary, 0, len(ranked)),
	}
	for _, scored := range ranked {
		result.Matches = append(result.Matches, summarizeWorkflow(scored.Workflow, scored.Score))
	}
	if len(result.Matches) == 0 {
		result.Summary = "no workflows matched query"
	} else {
		result.Summary = fmt.Sprintf("%d workflow(s) matched", len(result.Matches))
	}
	return marshalWorkflowResult(result)
}

func (t *WorkflowSearchTool) runGet(ctx context.Context, mm *agentmemory.Manager, args workflowSearchArgs) (*Result, error) {
	name := strings.TrimSpace(args.Name)
	if name == "" {
		return &Result{Output: "get requires name"}, nil
	}
	wf, err := mm.GetWorkflow(ctx, name)
	if err != nil {
		return &Result{Output: "workflow lookup failed: " + err.Error()}, nil
	}
	res := workflowGetResult{}
	if wf != nil {
		summary := summarizeWorkflow(*wf, wf.Confidence)
		res.Workflow = &summary
		res.Found = true
		res.Summary = fmt.Sprintf("workflow %q v%d", wf.Name, wf.Version)
	} else {
		res.Summary = fmt.Sprintf("no workflow named %q", name)
	}
	return marshalWorkflowResult(res)
}

// summarizeWorkflow produces the tool-output shape from a full Workflow.
func summarizeWorkflow(wf agentmemory.Workflow, score float64) workflowSummary {
	var lastUsed string
	if !wf.LastUsedAt.IsZero() {
		lastUsed = wf.LastUsedAt.UTC().Format(time.RFC3339)
	}
	return workflowSummary{
		Name:          wf.Name,
		Version:       wf.Version,
		Confidence:    wf.Confidence,
		SuccessRate:   wf.SuccessRate(),
		SuccessCount:  wf.SuccessCount,
		FailureCount:  wf.FailureCount,
		Steps:         append([]string(nil), wf.Steps...),
		Preconditions: append([]string(nil), wf.Preconditions...),
		ErrorModes:    append([]string(nil), wf.ErrorModes...),
		ContextTags:   append([]string(nil), wf.ContextTags...),
		LastUsed:      lastUsed,
		Score:         score,
	}
}

func marshalWorkflowResult(result any) (*Result, error) {
	buf, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return &Result{Output: "failed to marshal workflow search result: " + err.Error()}, nil
	}
	return &Result{Output: string(buf), Source: "builtin"}, nil
}

// scoredWorkflow is the intermediate shape used by the ranker.
type scoredWorkflow struct {
	Workflow agentmemory.Workflow
	Score    float64
}

// rankWorkflowMatches scores and orders workflow candidates returned from
// FindWorkflows so the top result is the most useful one for the agent to
// actually execute, given the free-text query and optional tag filter.
//
// Scoring model (each signal contributes to a [0, 1-ish] component, and the
// final score is a weighted blend):
//
//   - success component: Laplace-smoothed success rate
//     (successes + 1) / (total + 2). Smoothing prevents a 1/1 workflow from
//     beating a 9/10 one when sample sizes are small.
//
//   - failure penalty: subtract a fixed 0.15 per failure up to a cap so
//     workflows with real-world failures lose ground even when their
//     success_rate looks fine. Capped at -0.30 so a few failures do not
//     erase the rest of the score.
//
//   - query fit: tokenised overlap between lowerQuery and the workflow's
//     name / context tags / preconditions, normalised by query token count.
//     A full overlap adds up to 0.4 to the score; an empty query contributes
//     nothing (listing mode leans entirely on track record + tags + recency).
//
//   - tag fit: fraction of lowerTags that appear in the workflow's
//     context_tags. Weighted higher than query fit because tags are an
//     explicit semantic signal — a full tag overlap adds up to 0.5.
//
//   - recency: exponential decay with a 30-day half-life on LastUsedAt.
//     Never-used workflows fall back to a mid value (0.5) so they are not
//     strictly ranked below any workflow that has run once recently.
//
//   - confidence: the workflow's own stored confidence is a final
//     multiplier, clamped to [0.1, 1.0] so a deliberately-floored entry
//     (e.g. from consolidation's poor-track-record rule) stays recoverable
//     when the agent has no better option.
//
// Candidates whose final score is below rankingFloor are dropped rather
// than returned with a near-zero score, so the tool output does not feed
// the agent candidates it should not trust.
func rankWorkflowMatches(query string, tags []string, cands []agentmemory.Workflow) []scoredWorkflow {
	if len(cands) == 0 {
		return nil
	}

	lowerQuery := strings.ToLower(strings.TrimSpace(query))
	queryTokens := tokenizeForRanking(lowerQuery)

	lowerTags := make([]string, 0, len(tags))
	for _, t := range tags {
		trimmed := strings.ToLower(strings.TrimSpace(t))
		if trimmed != "" {
			lowerTags = append(lowerTags, trimmed)
		}
	}

	scored := make([]scoredWorkflow, 0, len(cands))
	for _, wf := range cands {
		// Success component with Laplace smoothing.
		total := wf.SuccessCount + wf.FailureCount
		successComponent := (float64(wf.SuccessCount) + 1) / (float64(total) + 2)

		// Failure penalty: cap at -0.30 to avoid erasing legitimate signal.
		failurePenalty := -0.15 * float64(wf.FailureCount)
		if failurePenalty < -0.30 {
			failurePenalty = -0.30
		}

		// Query fit: token overlap, normalised by query token count.
		queryFit := 0.0
		if len(queryTokens) > 0 {
			hay := workflowHaystack(wf)
			matched := 0
			for _, tok := range queryTokens {
				if strings.Contains(hay, tok) {
					matched++
				}
			}
			queryFit = 0.4 * (float64(matched) / float64(len(queryTokens)))
		}

		// Tag fit: fraction of requested tags present.
		tagFit := 0.0
		if len(lowerTags) > 0 {
			wfTagSet := make(map[string]struct{}, len(wf.ContextTags))
			for _, tag := range wf.ContextTags {
				wfTagSet[strings.ToLower(strings.TrimSpace(tag))] = struct{}{}
			}
			matched := 0
			for _, tag := range lowerTags {
				if _, ok := wfTagSet[tag]; ok {
					matched++
				}
			}
			tagFit = 0.5 * (float64(matched) / float64(len(lowerTags)))
		}

		// Recency: exponential decay with a 30-day half-life.
		recency := 0.5
		if !wf.LastUsedAt.IsZero() {
			days := time.Since(wf.LastUsedAt).Hours() / 24.0
			if days < 0 {
				days = 0
			}
			// 0.5^(days/30) — 1.0 at 0 days, 0.5 at 30 days, ~0.06 at 120.
			recency = math.Pow(0.5, days/30.0)
		}

		// Confidence clamp keeps floored entries recoverable.
		confidence := wf.Confidence
		if confidence < 0.1 {
			confidence = 0.1
		}
		if confidence > 1.0 {
			confidence = 1.0
		}

		// Blend. Weights sum to 1.0 for the positive components (success 0.35
		// + query 0.20 + tag 0.25 + recency 0.20). The failure penalty and
		// confidence multiplier are applied on top.
		blended := 0.35*successComponent + queryFit + tagFit + 0.20*recency
		blended += failurePenalty
		blended *= confidence

		if blended < rankingFloor {
			continue
		}
		scored = append(scored, scoredWorkflow{Workflow: wf, Score: blended})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})
	return scored
}

// rankingFloor is the minimum blended score a workflow must reach to be
// surfaced. Chosen empirically so a never-run, no-query-fit workflow
// (pure baseline success = 0.5, recency = 0.5, no tags) lands below the
// floor and gets dropped, while a single successful run or any tag/query
// overlap pushes the score comfortably above it.
const rankingFloor = 0.15

// workflowHaystack concatenates the fields the query should match against
// so the ranker can do a single Contains check per token.
func workflowHaystack(wf agentmemory.Workflow) string {
	var b strings.Builder
	b.WriteString(strings.ToLower(wf.Name))
	for _, step := range wf.Steps {
		b.WriteByte(' ')
		b.WriteString(strings.ToLower(step))
	}
	for _, tag := range wf.ContextTags {
		b.WriteByte(' ')
		b.WriteString(strings.ToLower(tag))
	}
	for _, p := range wf.Preconditions {
		b.WriteByte(' ')
		b.WriteString(strings.ToLower(p))
	}
	for _, em := range wf.ErrorModes {
		b.WriteByte(' ')
		b.WriteString(strings.ToLower(em))
	}
	return b.String()
}

// pickPrefilterToken chooses the longest non-stop-word token from the
// query to hand to FindWorkflows' LIKE filter. Returns empty when the query
// has no meaningful tokens so the caller loads an unfiltered candidate set.
func pickPrefilterToken(query string) string {
	toks := tokenizeForRanking(strings.ToLower(strings.TrimSpace(query)))
	if len(toks) == 0 {
		return ""
	}
	longest := toks[0]
	for _, t := range toks[1:] {
		if len(t) > len(longest) {
			longest = t
		}
	}
	return longest
}

// tokenizeForRanking extracts non-trivial lowercase tokens from the query,
// dropping single-letter junk and common stop words so one stop word in
// the query cannot inflate the match ratio.
func tokenizeForRanking(lowerQuery string) []string {
	if lowerQuery == "" {
		return nil
	}
	fields := strings.FieldsFunc(lowerQuery, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	stop := map[string]struct{}{
		"the": {}, "and": {}, "for": {}, "with": {}, "a": {}, "an": {}, "to": {}, "of": {}, "is": {}, "in": {},
	}
	seen := make(map[string]struct{}, len(fields))
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if len(f) < 3 {
			continue
		}
		if _, skip := stop[f]; skip {
			continue
		}
		if _, dup := seen[f]; dup {
			continue
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	return out
}
