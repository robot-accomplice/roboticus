package pipeline

import (
	"fmt"
	"strings"
)

// --- ModelIdentityTruthGuard ---

// ModelIdentityTruthGuard rewrites responses to model identity questions
// with the canonical agent identity.
type ModelIdentityTruthGuard struct{}

func (g *ModelIdentityTruthGuard) Name() string { return "model_identity_truth" }
func (g *ModelIdentityTruthGuard) Check(content string) GuardResult {
	return GuardResult{Passed: true}
}
func (g *ModelIdentityTruthGuard) CheckWithContext(content string, ctx *GuardContext) GuardResult {
	if ctx == nil || !ctx.HasIntent("model_identity") {
		return GuardResult{Passed: true}
	}
	canonical := fmt.Sprintf("%s reporting in. I am currently running on %s.",
		ctx.AgentName, ctx.ResolvedModel)
	return GuardResult{Passed: false, Content: canonical}
}

// --- CurrentEventsTruthGuard ---

// CurrentEventsTruthGuard detects stale-knowledge disclaimers when the model
// refuses to answer about current events despite having tool access.
type CurrentEventsTruthGuard struct{}

var staleKnowledgeMarkers = []string{
	"as of my last update",
	"as of my last training",
	"i cannot provide real-time updates",
	"my training data only goes up to",
	"i don't have access to current",
	"as of 2023",
	"as of 2024",
}

func (g *CurrentEventsTruthGuard) Name() string { return "current_events_truth" }
func (g *CurrentEventsTruthGuard) Check(content string) GuardResult {
	return GuardResult{Passed: true}
}
func (g *CurrentEventsTruthGuard) CheckWithContext(content string, ctx *GuardContext) GuardResult {
	if ctx == nil || !ctx.HasIntent("current_events") {
		return GuardResult{Passed: true}
	}
	lower := strings.ToLower(content)
	for _, marker := range staleKnowledgeMarkers {
		if strings.Contains(lower, marker) {
			return GuardResult{
				Passed: false, Retry: true,
				Reason: "stale-knowledge disclaimer in current events response",
			}
		}
	}
	return GuardResult{Passed: true}
}

// --- ExecutionTruthGuard ---

// ExecutionTruthGuard validates that claims about tool execution match actual
// tool results. If the model says "I ran the command" but no tool was called,
// or if tools ran but the model denies capability, the response is corrected.
type ExecutionTruthGuard struct{}

func (g *ExecutionTruthGuard) Name() string { return "execution_truth" }
func (g *ExecutionTruthGuard) Check(content string) GuardResult {
	return GuardResult{Passed: true}
}
func (g *ExecutionTruthGuard) CheckWithContext(content string, ctx *GuardContext) GuardResult {
	if ctx == nil {
		return GuardResult{Passed: true}
	}

	// Check 1: Claims execution but no tools ran.
	if len(ctx.ToolResults) == 0 {
		lower := strings.ToLower(content)
		executionClaims := []string{
			"i ran", "i executed", "i've completed", "the command returned",
			"output:", "the result is", "here's what i found after running",
		}
		for _, claim := range executionClaims {
			if strings.Contains(lower, claim) {
				return GuardResult{
					Passed: false, Retry: true,
					Reason: "claimed tool execution but no tools were called",
				}
			}
		}
	}

	// Check 2: Tools ran but model denies capability.
	if len(ctx.ToolResults) > 0 {
		lower := strings.ToLower(content)
		denialPatterns := []string{
			"i cannot", "i'm unable to", "i don't have the ability",
			"i can't execute", "i cannot run",
		}
		for _, denial := range denialPatterns {
			if strings.Contains(lower, denial) {
				// Rewrite with actual tool results.
				var summary strings.Builder
				summary.WriteString("Here are the results from the tools I executed:\n\n")
				for _, tr := range ctx.ToolResults {
					fmt.Fprintf(&summary, "**%s**: %s\n", tr.ToolName, truncate(tr.Output, 500))
				}
				return GuardResult{Passed: false, Content: summary.String()}
			}
		}
	}

	// Check 3: Delegation claim without delegation tool.
	if ctx.HasIntent("delegation") {
		hasDelegationTool := false
		for _, tr := range ctx.ToolResults {
			if strings.Contains(tr.ToolName, "delegat") || strings.Contains(tr.ToolName, "subagent") {
				hasDelegationTool = true
				break
			}
		}
		if !hasDelegationTool && len(ctx.ToolResults) == 0 {
			lower := strings.ToLower(content)
			if strings.Contains(lower, "delegated") || strings.Contains(lower, "specialist completed") {
				return GuardResult{
					Passed: false, Retry: true,
					Reason: "claimed delegation but no delegation tool was called",
				}
			}
		}
	}

	return GuardResult{Passed: true}
}

// --- PersonalityIntegrityGuard ---

// PersonalityIntegrityGuard strips foreign AI identity boilerplate from
// responses (e.g., "As an AI developed by OpenAI" or "I am Claude").
type PersonalityIntegrityGuard struct{}

var foreignIdentityMarkers = []string{
	"as an ai developed by",
	"as an ai language model",
	"i am claude",
	"i'm chatgpt",
	"as a large language model",
	"i was created by openai",
	"i was created by anthropic",
	"i was made by google",
}

func (g *PersonalityIntegrityGuard) Name() string { return "personality_integrity" }
func (g *PersonalityIntegrityGuard) Check(content string) GuardResult {
	lower := strings.ToLower(content)
	for _, marker := range foreignIdentityMarkers {
		if strings.Contains(lower, marker) {
			cleaned := stripSentencesContaining(content, foreignIdentityMarkers)
			if strings.TrimSpace(cleaned) == "" {
				return GuardResult{
					Passed: false, Retry: true,
					Reason: "response consisted entirely of foreign identity boilerplate",
				}
			}
			return GuardResult{Passed: false, Content: cleaned}
		}
	}
	return GuardResult{Passed: true}
}
func (g *PersonalityIntegrityGuard) CheckWithContext(content string, ctx *GuardContext) GuardResult {
	return g.Check(content)
}

// --- Shared utilities ---

// truncate shortens a string to maxLen, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// stripSentencesContaining removes sentences that contain any of the markers.
func stripSentencesContaining(text string, markers []string) string {
	sentences := strings.Split(text, ". ")
	var kept []string
	for _, s := range sentences {
		lower := strings.ToLower(s)
		hasMarker := false
		for _, m := range markers {
			if strings.Contains(lower, m) {
				hasMarker = true
				break
			}
		}
		if !hasMarker {
			kept = append(kept, s)
		}
	}
	return strings.Join(kept, ". ")
}
