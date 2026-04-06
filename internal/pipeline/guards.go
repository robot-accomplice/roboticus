package pipeline

import (
	"strings"
)

// Guard is a post-inference content filter.
type Guard interface {
	Name() string
	Check(content string) GuardResult
}

// GuardResult holds the outcome of a guard check.
type GuardResult struct {
	Passed  bool
	Content string // modified content (same as input if unchanged)
	Retry   bool   // request retry with modified prompt
	Reason  string // why the guard triggered
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

// --- Built-in Guards ---

// EmptyResponseGuard catches empty or whitespace-only responses.
type EmptyResponseGuard struct{}

func (g *EmptyResponseGuard) Name() string { return "empty_response" }
func (g *EmptyResponseGuard) Check(content string) GuardResult {
	if strings.TrimSpace(content) == "" {
		return GuardResult{
			Passed:  false,
			Content: "I apologize, but I wasn't able to generate a response. Could you try rephrasing your request?",
			Reason:  "empty response",
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

// FullGuardChain returns all 19 guards for standard inference.
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
		// Quality guards.
		&LowValueParrotingGuard{},
		&NonRepetitionGuardV2{},
		&OutputContractGuard{},
		&UserEchoGuard{},
		// Truthfulness guards.
		&ModelIdentityTruthGuard{},
		&CurrentEventsTruthGuard{},
		&ExecutionTruthGuard{},
		&FinancialActionTruthGuard{},
		&PersonalityIntegrityGuard{},
		// Protection guards.
		&ConfigProtectionGuard{},
	)
}

// StreamGuardChain returns a lightweight chain for SSE streaming.
func StreamGuardChain() *GuardChain {
	return NewGuardChain(
		&EmptyResponseGuard{},
		&SubagentClaimGuard{},
		&InternalJargonGuard{},
		&PersonalityIntegrityGuard{},
		&NonRepetitionGuardV2{},
	)
}
