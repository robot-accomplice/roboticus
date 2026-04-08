package llm

import "strings"

// EvalCase represents a single routing evaluation test case.
type EvalCase struct {
	Prompt       string    `json:"prompt"`
	ExpectedTier ModelTier `json:"expected_tier"`
	Tags         []string  `json:"tags,omitempty"` // e.g., "code", "math", "simple"

	// ExtraMessages adds preceding conversation turns to the request.
	// When non-zero, filler user/assistant message pairs are prepended.
	ExtraMessages int `json:"extra_messages,omitempty"`

	// ToolCount adds dummy tool definitions to the request, exercising the
	// tool-usage complexity signal.
	ToolCount int `json:"tool_count,omitempty"`
}

// EvalResult holds aggregate results from running a routing evaluation.
type EvalResult struct {
	Total    int                      `json:"total"`
	Correct  int                      `json:"correct"`
	Accuracy float64                  `json:"accuracy"`
	ByTier   map[ModelTier]TierResult `json:"by_tier"`
	Errors   []EvalError              `json:"errors,omitempty"`
}

// TierResult tracks per-tier accuracy within an evaluation run.
type TierResult struct {
	Total   int `json:"total"`
	Correct int `json:"correct"`
}

// EvalError records a single misrouted case.
type EvalError struct {
	Prompt   string    `json:"prompt"`
	Expected ModelTier `json:"expected"`
	Got      ModelTier `json:"got"`
}

// RunEval evaluates the router's complexity-based tier selection against a
// corpus of labeled test cases. It constructs a Request for each case,
// runs the complexity estimator, and compares the resulting tier against the
// expected tier.
func RunEval(_ *Router, corpus []EvalCase) *EvalResult {
	result := &EvalResult{
		ByTier: make(map[ModelTier]TierResult),
	}

	for _, tc := range corpus {
		req := buildEvalRequest(tc)

		complexity := estimateComplexity(req)
		got := tierForComplexity(complexity)

		tr := result.ByTier[tc.ExpectedTier]
		tr.Total++

		if got == tc.ExpectedTier {
			result.Correct++
			tr.Correct++
		} else {
			result.Errors = append(result.Errors, EvalError{
				Prompt:   tc.Prompt,
				Expected: tc.ExpectedTier,
				Got:      got,
			})
		}

		result.ByTier[tc.ExpectedTier] = tr
		result.Total++
	}

	if result.Total > 0 {
		result.Accuracy = float64(result.Correct) / float64(result.Total)
	}

	return result
}

// buildEvalRequest creates a *Request from an EvalCase, including optional
// extra messages and tool definitions.
func buildEvalRequest(tc EvalCase) *Request {
	var msgs []Message

	// Prepend filler conversation turns if requested.
	for i := 0; i < tc.ExtraMessages; i++ {
		msgs = append(msgs,
			Message{Role: "user", Content: "previous question"},
			Message{Role: "assistant", Content: "previous answer"},
		)
	}

	msgs = append(msgs, Message{Role: "user", Content: tc.Prompt})

	var tools []ToolDef
	for i := 0; i < tc.ToolCount; i++ {
		tools = append(tools, ToolDef{Type: "function"})
	}

	return &Request{
		Messages: msgs,
		Tools:    tools,
	}
}

// DefaultEvalCorpus returns a curated set of test cases spanning all model
// tiers. The prompts and auxiliary parameters are chosen to exercise the
// complexity heuristics in estimateComplexity and tierForComplexity so that
// each case routes to its expected tier.
//
// Complexity scoring reference (from router.go):
//
//	chars > 10000: +0.3 | > 3000: +0.2 | > 500: +0.1
//	messages > 20: +0.2 | > 8: +0.1
//	tools > 0: +0.15  | > 5: +0.1 (cumulative +0.25)
//	complexity keyword: +0.05  (one-time)
//	simple keyword (<100 chars): -0.2
//
// Tier thresholds: Small < 0.2, Medium [0.2, 0.4), Large [0.4, 0.7), Frontier >= 0.7.
func DefaultEvalCorpus() []EvalCase {
	return []EvalCase{
		// ---------------------------------------------------------------
		// TierSmall (complexity < 0.2): short prompts, simple signals.
		// ---------------------------------------------------------------
		{Prompt: "hello", ExpectedTier: TierSmall, Tags: []string{"simple"}},
		{Prompt: "thanks", ExpectedTier: TierSmall, Tags: []string{"simple"}},
		{Prompt: "what time is it", ExpectedTier: TierSmall, Tags: []string{"simple"}},
		{Prompt: "yes", ExpectedTier: TierSmall, Tags: []string{"simple"}},
		{Prompt: "ok", ExpectedTier: TierSmall, Tags: []string{"simple"}},

		// ---------------------------------------------------------------
		// TierMedium (0.2 <= complexity < 0.4):
		// chars > 3000 gives +0.2, which lands at exactly 0.2.
		// chars > 3000 + keyword gives 0.25.
		// ---------------------------------------------------------------
		{
			Prompt:       "explain how HTTP works in detail. " + paddingOfSize(3000),
			ExpectedTier: TierMedium,
			Tags:         []string{"knowledge"},
		},
		{
			Prompt:       "write a haiku about coding and creativity. " + paddingOfSize(3000),
			ExpectedTier: TierMedium,
			Tags:         []string{"creative"},
		},
		{
			Prompt:       "list five tips for writing better unit tests. " + paddingOfSize(3000),
			ExpectedTier: TierMedium,
			Tags:         []string{"code"},
		},
		{
			Prompt:       "summarize the key ideas behind functional programming. " + paddingOfSize(3000),
			ExpectedTier: TierMedium,
			Tags:         []string{"knowledge"},
		},
		{
			Prompt:       "what are the differences between TCP and UDP protocols. " + paddingOfSize(3000),
			ExpectedTier: TierMedium,
			Tags:         []string{"knowledge"},
		},

		// ---------------------------------------------------------------
		// TierLarge (0.4 <= complexity < 0.7):
		// chars > 3000 (+0.2) + messages > 8 (+0.1) + tools (+0.15) = 0.45.
		// ---------------------------------------------------------------
		{
			Prompt:        "analyze the trade-offs between microservices and monoliths for a startup. " + paddingOfSize(3100),
			ExpectedTier:  TierLarge,
			Tags:          []string{"architecture"},
			ToolCount:     1,
			ExtraMessages: 5,
		},
		{
			Prompt:        "implement a binary search tree in Python with insert, delete, and search. " + paddingOfSize(3100),
			ExpectedTier:  TierLarge,
			Tags:          []string{"code"},
			ToolCount:     1,
			ExtraMessages: 5,
		},
		{
			Prompt:        "compare and evaluate three different caching strategies for a web application. " + paddingOfSize(3100),
			ExpectedTier:  TierLarge,
			Tags:          []string{"architecture"},
			ToolCount:     1,
			ExtraMessages: 5,
		},
		{
			Prompt:        "design a rate-limiting system that handles distributed deployments. " + paddingOfSize(3100),
			ExpectedTier:  TierLarge,
			Tags:          []string{"architecture"},
			ToolCount:     1,
			ExtraMessages: 5,
		},
		{
			Prompt:        "debug this SQL query that returns incorrect aggregation results when joining three tables. " + paddingOfSize(3100),
			ExpectedTier:  TierLarge,
			Tags:          []string{"code", "debug"},
			ToolCount:     1,
			ExtraMessages: 5,
		},

		// ---------------------------------------------------------------
		// TierFrontier (complexity >= 0.7):
		// chars > 10000 (+0.3) + keyword (+0.05) + tools > 5 (+0.25)
		//   + messages > 20 (+0.2) = 0.8, safely above 0.7.
		// ---------------------------------------------------------------
		{
			Prompt:        "analyze and refactor the following large codebase module to optimize performance and improve security. " + paddingOfSize(10500),
			ExpectedTier:  TierFrontier,
			Tags:          []string{"code", "refactor", "security"},
			ToolCount:     6,
			ExtraMessages: 11, // 22 filler + 1 user = 23 messages (> 20)
		},
		{
			Prompt:        "evaluate the trade-offs, then design and implement a complete distributed task scheduler with fault tolerance. " + paddingOfSize(10500),
			ExpectedTier:  TierFrontier,
			Tags:          []string{"architecture", "code"},
			ToolCount:     6,
			ExtraMessages: 11,
		},
		{
			Prompt:        "debug and optimize this performance-critical path, then architect a caching layer that handles invalidation across microservices. " + paddingOfSize(10500),
			ExpectedTier:  TierFrontier,
			Tags:          []string{"debug", "architecture"},
			ToolCount:     6,
			ExtraMessages: 11,
		},
		{
			Prompt:        "analyze security vulnerabilities in this authentication system and implement comprehensive fixes with proper testing. " + paddingOfSize(10500),
			ExpectedTier:  TierFrontier,
			Tags:          []string{"security", "code"},
			ToolCount:     6,
			ExtraMessages: 11,
		},
	}
}

// paddingOfSize returns a string of the given length to push prompts past
// character-count thresholds in the complexity estimator.
func paddingOfSize(n int) string {
	return strings.Repeat("x", n)
}

// MetascoreEvalCase defines a test case for evaluating the impact of different
// routing weight configurations on model selection.
type MetascoreEvalCase struct {
	Label    string         `json:"label"`
	Profiles []ModelProfile `json:"profiles"`
	Weights  RoutingWeights `json:"weights"`
	Expected string         `json:"expected"` // expected winning model name
}

// MetascoreEvalResult holds aggregate results from a metascore evaluation run.
type MetascoreEvalResult struct {
	Total   int                   `json:"total"`
	Correct int                   `json:"correct"`
	Errors  []MetascoreEvalError  `json:"errors,omitempty"`
	Details []MetascoreEvalDetail `json:"details,omitempty"`
}

// MetascoreEvalError records a single metascore misroute.
type MetascoreEvalError struct {
	Label    string `json:"label"`
	Expected string `json:"expected"`
	Got      string `json:"got"`
}

// MetascoreEvalDetail records the scoring details for a single case.
type MetascoreEvalDetail struct {
	Label  string             `json:"label"`
	Winner string             `json:"winner"`
	Scores map[string]float64 `json:"scores"`
}

// RunMetascoreEval evaluates metascore-based model selection against a corpus
// of labeled test cases. For each case, it computes metascores using the
// provided weights and checks whether the highest-scoring model matches the
// expected winner.
func RunMetascoreEval(corpus []MetascoreEvalCase) *MetascoreEvalResult {
	result := &MetascoreEvalResult{}

	for _, tc := range corpus {
		scores := make(map[string]float64, len(tc.Profiles))
		for _, p := range tc.Profiles {
			scores[p.Model] = p.MetascoreWith(tc.Weights)
		}

		winner := SelectByMetascoreWeighted(tc.Profiles, tc.Weights)
		winnerModel := ""
		if winner != nil {
			winnerModel = winner.Model
		}

		detail := MetascoreEvalDetail{
			Label:  tc.Label,
			Winner: winnerModel,
			Scores: scores,
		}
		result.Details = append(result.Details, detail)
		result.Total++

		if winnerModel == tc.Expected {
			result.Correct++
		} else {
			result.Errors = append(result.Errors, MetascoreEvalError{
				Label:    tc.Label,
				Expected: tc.Expected,
				Got:      winnerModel,
			})
		}
	}

	return result
}
