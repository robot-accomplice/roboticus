package pipeline

import (
	"fmt"
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
	GuardBlocked                            // Guard blocks the response without synthesizing prose
)

// GuardContractEvent is the diagnostic contract fact emitted by guard checks.
// It is intentionally data-only: user-facing prose belongs to normal response
// generation, not hidden guard substitutions.
type GuardContractEvent struct {
	ContractID        string `json:"contract_id"`
	ContractGroup     string `json:"contract_group"`
	Phase             string `json:"phase"`
	Severity          string `json:"severity"`
	PreconditionState string `json:"precondition_state"`
	ViolationState    string `json:"violation_state"`
	RecoveryAction    string `json:"recovery_action"`
	RecoveryAttempt   int    `json:"recovery_attempt,omitempty"`
	RecoveryWindow    int    `json:"recovery_window,omitempty"`
	RecoveryOutcome   string `json:"recovery_outcome,omitempty"`
	ConfidenceEffect  string `json:"confidence_effect"`
}

// GuardResult holds the outcome of a guard check.
type GuardResult struct {
	Passed        bool
	Content       string             // modified content (same as input if unchanged)
	Retry         bool               // request retry with modified prompt
	Blocked       bool               // block response without substituting canned prose
	Reason        string             // why the guard triggered
	Verdict       GuardVerdict       // classification of the guard action (Wave 8, #72)
	ContractEvent GuardContractEvent // structured RCA evidence for this finding
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
	Blocked        bool
	BlockReason    string
	Violations     []string
	ContractEvents []GuardContractEvent
}

// Len returns the number of guards in the chain.
func (gc *GuardChain) Len() int { return len(gc.guards) }

// GuardNamesForTest exposes the runtime guard names for fitness and parity
// tests without exposing the concrete guard slice to production callers.
func (gc *GuardChain) GuardNamesForTest() []string {
	names := make([]string, 0, len(gc.guards))
	for _, g := range gc.guards {
		names = append(names, g.Name())
	}
	return names
}

// GuardTypesForTest exposes the concrete guard type names for fitness tests.
func (gc *GuardChain) GuardTypesForTest() []string {
	types := make([]string, 0, len(gc.guards))
	for _, g := range gc.guards {
		types = append(types, fmt.Sprintf("%T", g))
	}
	return types
}

func newGuardContractEvent(id, group, phase, severity, precondition, violation, recovery, confidence string) GuardContractEvent {
	return GuardContractEvent{
		ContractID:        id,
		ContractGroup:     group,
		Phase:             phase,
		Severity:          severity,
		PreconditionState: precondition,
		ViolationState:    violation,
		RecoveryAction:    recovery,
		RecoveryWindow:    1,
		RecoveryOutcome:   "pending",
		ConfidenceEffect:  confidence,
	}
}

func buildGuardContractEvent(guardName string, gr GuardResult) GuardContractEvent {
	event := gr.ContractEvent
	if event.ContractID == "" {
		event = defaultGuardContractEvent(guardName, gr)
	}
	if event.ContractID == "" {
		event.ContractID = guardName
	}
	if event.ContractGroup == "" {
		event.ContractGroup = "guard"
	}
	if event.Phase == "" {
		event.Phase = "reflect"
	}
	if event.Severity == "" {
		event.Severity = defaultGuardSeverity(guardName)
	}
	if event.PreconditionState == "" {
		event.PreconditionState = "guard precondition was evaluated"
	}
	if event.ViolationState == "" {
		event.ViolationState = gr.Reason
	}
	if event.RecoveryAction == "" {
		event.RecoveryAction = defaultGuardRecoveryAction(gr)
	}
	if event.RecoveryWindow == 0 && event.RecoveryAction == "retry" {
		event.RecoveryWindow = 1
	}
	if event.RecoveryOutcome == "" {
		event.RecoveryOutcome = "pending"
	}
	if event.ConfidenceEffect == "" {
		if event.Severity == "neutral" {
			event.ConfidenceEffect = "0"
		} else {
			event.ConfidenceEffect = "-1"
		}
	}
	return event
}

func defaultGuardContractEvent(guardName string, gr GuardResult) GuardContractEvent {
	return GuardContractEvent{
		ContractID:        guardName,
		ContractGroup:     "guard",
		Phase:             "reflect",
		Severity:          defaultGuardSeverity(guardName),
		PreconditionState: "assistant output must satisfy guard policy",
		ViolationState:    gr.Reason,
		RecoveryAction:    defaultGuardRecoveryAction(gr),
		RecoveryWindow:    1,
		RecoveryOutcome:   "pending",
		ConfidenceEffect:  "-1",
	}
}

func defaultGuardSeverity(guardName string) string {
	switch guardName {
	case "internal_marker", "repetition", "non_repetition_v2", "perspective", "literary_quote_retry":
		return "soft"
	default:
		return "hard"
	}
}

func defaultGuardRecoveryAction(gr GuardResult) string {
	switch {
	case gr.Blocked || gr.Verdict == GuardBlocked:
		return "block"
	case gr.Retry || gr.Verdict == GuardRetryRequested:
		return "retry"
	case gr.Content != "":
		return "rewrite"
	default:
		return "record"
	}
}

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
			result.ContractEvents = append(result.ContractEvents, buildGuardContractEvent(g.Name(), gr))
			if gr.Blocked || gr.Verdict == GuardBlocked {
				result.Blocked = true
				result.BlockReason = gr.Reason
				result.Content = ""
				return result
			}
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
			result.ContractEvents = append(result.ContractEvents, buildGuardContractEvent(g.Name(), gr))
			if gr.Blocked || gr.Verdict == GuardBlocked {
				result.Blocked = true
				result.BlockReason = gr.Reason
				result.Content = ""
				return result
			}
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
			Passed:        false,
			Retry:         true,
			Reason:        "empty response",
			Verdict:       GuardRetryRequested,
			ContractEvent: newGuardContractEvent("empty_response", "availability", "reflect", "hard", "response must contain usable content", "response was empty", "retry", "-1"),
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
				Passed:        false,
				Retry:         true,
				Reason:        "system prompt leak detected",
				Verdict:       GuardRetryRequested,
				ContractEvent: newGuardContractEvent("system_prompt_leak", "security", "reflect", "hard", "system instructions must remain hidden", "system prompt marker appeared in model output", "retry", "-1"),
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
				Passed:        false,
				Blocked:       true,
				Reason:        "harmful content detected",
				Retry:         false,
				Verdict:       GuardBlocked,
				ContractEvent: newGuardContractEvent("content_classification", "safety", "reflect", "hard", "assistant output must not contain harmful instruction content", "harmful content pattern appeared in model output", "block", "-1"),
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

// FullGuardChain returns the registry-authoritative full preset.
func FullGuardChain() *GuardChain {
	return NewDefaultGuardRegistry().Chain(GuardSetFull)
}

// StreamGuardChain returns the registry-authoritative streaming preset.
func StreamGuardChain() *GuardChain {
	return NewDefaultGuardRegistry().Chain(GuardSetStream)
}

// CachedGuardChain returns the registry-authoritative cached preset.
func CachedGuardChain() *GuardChain {
	return NewDefaultGuardRegistry().Chain(GuardSetCached)
}
