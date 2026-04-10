package pipeline

import (
	"strings"
)

// Guard is a post-inference content filter.
type Guard interface {
	Name() string
	Check(content string) GuardResult
}

// GuardVerdict classifies the type of guard action taken.
type GuardVerdict int

const (
	GuardPass           GuardVerdict = iota // Content passed without modification
	GuardRewritten                          // Content was rewritten by the guard
	GuardRetryRequested                     // Guard requests a full re-inference
)

// GuardResult holds the outcome of a guard check.
type GuardResult struct {
	Passed  bool
	Content string       // modified content (same as input if unchanged)
	Retry   bool         // request retry with modified prompt
	Reason  string       // why the guard triggered
	Verdict GuardVerdict // classification of the guard action (Wave 8, #72)
}

// GuardChain applies an ordered sequence of guards.
type GuardChain struct {
	guards []Guard
}

// NewGuardChain creates a guard chain with the given guards.
func NewGuardChain(guards ...Guard) *GuardChain {
	return &GuardChain{guards: guards}
}

// ApplyResult holds the outcome of the guard chain, including retry requests.
type ApplyResult struct {
	Content        string
	RetryRequested bool
	RetryReason    string
	Violations     []string
}

// Len returns the number of guards in the chain.
func (gc *GuardChain) Len() int { return len(gc.guards) }

// Apply runs all guards on the content. Returns the final result.
func (gc *GuardChain) Apply(content string) string {
	result := gc.ApplyFull(content)
	return result.Content
}

// ApplyFull runs all guards and returns the full result including retry info.
func (gc *GuardChain) ApplyFull(content string) ApplyResult {
	result := ApplyResult{Content: content}

	for _, g := range gc.guards {
		gr := g.Check(content)
		if !gr.Passed {
			result.Violations = append(result.Violations, g.Name()+": "+gr.Reason)
			if gr.Content != "" {
				content = gr.Content
				result.Content = content
			}
			if gr.Retry {
				result.RetryRequested = true
				result.RetryReason = gr.Reason
			}
		}
	}
	return result
}

// ApplyFrom runs guards starting from the given index, skipping guards that
// were already applied before a retry. This implements Rust's apply_from()
// for post-retry guard chain resumption — guards that already passed don't
// need to re-evaluate the retried content.
func (gc *GuardChain) ApplyFrom(content string, fromIndex int) ApplyResult {
	result := ApplyResult{Content: content}

	for i := fromIndex; i < len(gc.guards); i++ {
		g := gc.guards[i]
		gr := g.Check(content)
		if !gr.Passed {
			result.Violations = append(result.Violations, g.Name()+": "+gr.Reason)
			if gr.Content != "" {
				content = gr.Content
				result.Content = content
			}
			if gr.Retry {
				result.RetryRequested = true
				result.RetryReason = gr.Reason
			}
		}
	}
	return result
}

// GuardIndex returns the index of a guard by name, or -1 if not found.
func (gc *GuardChain) GuardIndex(name string) int {
	for i, g := range gc.guards {
		if g.Name() == name {
			return i
		}
	}
	return -1
}

// --- Built-in Guards ---

// EmptyResponseGuard catches empty or whitespace-only responses.
type EmptyResponseGuard struct{}

func (g *EmptyResponseGuard) Name() string { return "empty_response" }
func (g *EmptyResponseGuard) Check(content string) GuardResult {
	if strings.TrimSpace(content) == "" {
		return GuardResult{
			Passed:  false,
			Retry:   true,
			Reason:  "empty response",
			Verdict: GuardRetryRequested,
		}
	}
	return GuardResult{Passed: true, Content: content}
}

// SystemPromptLeakGuard catches accidental system prompt disclosure.
type SystemPromptLeakGuard struct {
	markers []string
}

// NewSystemPromptLeakGuard creates a guard with custom markers to detect.
func NewSystemPromptLeakGuard(markers ...string) *SystemPromptLeakGuard {
	if len(markers) == 0 {
		markers = []string{
			"## Platform Instructions",
			"## Identity",
			"## Tool Use",
			"## Safety",
			"You are an autonomous AI agent",
		}
	}
	return &SystemPromptLeakGuard{markers: markers}
}

func (g *SystemPromptLeakGuard) Name() string { return "system_prompt_leak" }
func (g *SystemPromptLeakGuard) Check(content string) GuardResult {
	lower := strings.ToLower(content)
	for _, marker := range g.markers {
		if strings.Contains(lower, strings.ToLower(marker)) {
			return GuardResult{
				Passed:  false,
				Content: "I can't share my system instructions. How else can I help you?",
				Reason:  "system prompt leak detected",
			}
		}
	}
	return GuardResult{Passed: true, Content: content}
}

// InternalMarkerGuard strips internal markers that shouldn't appear in output.
type InternalMarkerGuard struct {
	markers []string
}

func NewInternalMarkerGuard(markers ...string) *InternalMarkerGuard {
	if len(markers) == 0 {
		markers = []string{
			"[INTERNAL]",
			"[SYSTEM_NOTE]",
			"[DECOMPOSITION]",
			"[DELEGATION]",
		}
	}
	return &InternalMarkerGuard{markers: markers}
}

func (g *InternalMarkerGuard) Name() string { return "internal_marker" }
func (g *InternalMarkerGuard) Check(content string) GuardResult {
	modified := content
	for _, marker := range g.markers {
		modified = strings.ReplaceAll(modified, marker, "")
	}
	if modified != content {
		return GuardResult{
			Passed:  false,
			Content: strings.TrimSpace(modified),
			Reason:  "internal markers stripped",
		}
	}
	return GuardResult{Passed: true, Content: content}
}

// ContentClassificationGuard flags potentially harmful or off-topic content.
type ContentClassificationGuard struct {
	harmfulPatterns []string
}

func NewContentClassificationGuard() *ContentClassificationGuard {
	return &ContentClassificationGuard{
		harmfulPatterns: []string{
			"how to make a bomb",
			"how to hack into",
			"how to steal",
			"social security number",
			"credit card number",
		},
	}
}

func (g *ContentClassificationGuard) Name() string { return "content_classification" }
func (g *ContentClassificationGuard) Check(content string) GuardResult {
	lower := strings.ToLower(content)
	for _, pattern := range g.harmfulPatterns {
		if strings.Contains(lower, pattern) {
			return GuardResult{
				Passed:  false,
				Content: "I can't assist with that request. Please ask something else.",
				Reason:  "harmful content detected",
				Retry:   false,
			}
		}
	}
	return GuardResult{Passed: true, Content: content}
}

// RepetitionGuard detects when the model is stuck in a repetitive loop.
type RepetitionGuard struct {
	minRepeatLen int
}

func NewRepetitionGuard() *RepetitionGuard {
	return &RepetitionGuard{minRepeatLen: 50}
}

func (g *RepetitionGuard) Name() string { return "repetition" }
func (g *RepetitionGuard) Check(content string) GuardResult {
	if len(content) < g.minRepeatLen*2 {
		return GuardResult{Passed: true, Content: content}
	}
	// Check if the second half is mostly a repeat of the first half.
	mid := len(content) / 2
	first := content[:mid]
	second := content[mid:]
	if strings.Contains(second, first[:g.minRepeatLen]) {
		// Truncate to the first occurrence.
		return GuardResult{
			Passed:  false,
			Content: first,
			Reason:  "repetitive output detected and truncated",
			Retry:   true,
		}
	}
	return GuardResult{Passed: true, Content: content}
}

// DefaultGuardChain returns the standard guard chain (backward compat).
func DefaultGuardChain() *GuardChain {
	return FullGuardChain()
}

// FullGuardChain returns all guards for standard inference.
func FullGuardChain() *GuardChain {
	return NewGuardChain(
		// Core guards.
		&EmptyResponseGuard{},
		NewContentClassificationGuard(),
		NewRepetitionGuard(),
		NewSystemPromptLeakGuard(),
		NewInternalMarkerGuard(),
		// Behavioral guards.
		&SubagentClaimGuard{},
		&TaskDeferralGuard{},
		&InternalJargonGuard{},
		&DeclaredActionGuard{},
		&PerspectiveGuard{},      // Wave 8, #78
		&InternalProtocolGuard{}, // Wave 8, #79
		// Quality guards.
		&LowValueParrotingGuard{},
		&NonRepetitionGuardV2{},
		&OutputContractGuard{},
		&UserEchoGuard{},
		// Truthfulness guards.
		&ModelIdentityTruthGuard{},
		&CurrentEventsTruthGuard{},
		&ExecutionTruthGuard{},
		&ExecutionBlockGuard{},
		&DelegationMetadataGuard{},
		&FilesystemDenialGuard{},
		&FinancialActionTruthGuard{},
		&PersonalityIntegrityGuard{},
		&ActionVerificationGuard{}, // Wave 8, #76
		&LiteraryQuoteRetryGuard{}, // Wave 8, #77
		// Protection guards.
		&ConfigProtectionGuard{},
	)
}

// StreamGuardChain returns a lightweight chain for SSE streaming.
// Matches Rust's 6-guard streaming set: no retry-capable guards.
func StreamGuardChain() *GuardChain {
	return NewGuardChain(
		&EmptyResponseGuard{},
		&ExecutionTruthGuard{},
		&TaskDeferralGuard{},
		&ModelIdentityTruthGuard{},
		&InternalJargonGuard{},
		&NonRepetitionGuardV2{},
	)
}

// CachedGuardChain returns the guard chain for cached responses.
// Matches Rust's cached guard set (same as Full).
func CachedGuardChain() *GuardChain {
	return FullGuardChain()
}
