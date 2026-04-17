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

	// Length-based logic: short responses get full canonical rewrite,
	// longer responses get model name redacted instead.
	lines := strings.Count(content, "\n") + 1
	if len(content) <= 200 && lines <= 3 {
		canonical := fmt.Sprintf("%s reporting in. I am currently running on %s.",
			ctx.AgentName, ctx.ResolvedModel)
		return GuardResult{Passed: false, Content: canonical}
	}

	// For longer responses, redact the model name instead of full rewrite.
	redacted := content
	if ctx.ResolvedModel != "" {
		redacted = strings.ReplaceAll(redacted, ctx.ResolvedModel, ctx.AgentName)
	}
	// Also redact common model family names.
	modelFamilies := []string{"Claude", "GPT-4", "GPT-3.5", "Gemini", "Llama", "Mistral", "DeepSeek"}
	for _, family := range modelFamilies {
		redacted = strings.ReplaceAll(redacted, family, ctx.AgentName)
	}
	if redacted != content {
		return GuardResult{Passed: false, Content: redacted}
	}
	return GuardResult{Passed: true}
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
	"i can't provide real-time updates",
	"i cannot provide real-time geopolitical analysis",
	"i can't provide real-time geopolitical analysis",
	"do not include live news feeds",
	"no live news feeds",
}

func (g *CurrentEventsTruthGuard) Name() string { return "current_events_truth" }
func (g *CurrentEventsTruthGuard) Check(content string) GuardResult {
	// Always strip stale-knowledge disclaimers regardless of intent context.
	// These phrases are never appropriate for a tool-equipped agent.
	lower := strings.ToLower(content)
	for _, marker := range staleKnowledgeMarkers {
		if strings.Contains(lower, marker) {
			cleaned := stripSentencesContaining(content, staleKnowledgeMarkers)
			if strings.TrimSpace(cleaned) == "" {
				return GuardResult{
					Passed: false, Retry: true,
					Reason: "response consisted entirely of stale-knowledge disclaimers",
				}
			}
			return GuardResult{Passed: false, Content: cleaned}
		}
	}
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

	// Rust parity: truthfulness.rs — 11 relevant intents (not 3).
	if len(ctx.Intents) > 0 {
		relevant := false
		for _, intent := range ctx.Intents {
			switch intent {
			case "execution", "task", "delegation", "cron",
				"file_distribution", "folder_scan",
				"wallet_address_scan", "image_count_scan",
				"markdown_count_scan", "obsidian_insights",
				"email_triage":
				relevant = true
			}
		}
		if !relevant {
			return GuardResult{Passed: true}
		}
	}

	// Rust parity: semantic FALSE_COMPLETION > 0.7 check.
	if ctx.SemanticScores != nil {
		if GuardScoreAboveThreshold(ctx.SemanticScores, "FALSE_COMPLETION") {
			return GuardResult{
				Passed: false, Retry: true,
				Reason: "semantic FALSE_COMPLETION detected — claimed action without tool evidence",
			}
		}
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
		for _, tr := range ctx.ToolResults {
			if toolResultSignalsPolicyOrSandboxDenial(tr) {
				return GuardResult{Passed: true}
			}
		}
		lower := strings.ToLower(content)
		denialPatterns := []string{
			"i cannot", "i'm unable to", "i don't have the ability",
			"i can't execute", "i cannot run",
		}
		for _, denial := range denialPatterns {
			if strings.Contains(lower, denial) {
				return GuardResult{
					Passed: false,
					Retry:  true,
					Reason: "falsely denied execution despite real tool results",
				}
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
	"as an ai developed by microsoft",
	"as an ai text-based interface",
	"as an ai, i can't",
	"as an ai, i cannot",
	"as a language model",
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

// --- ExecutionBlockGuard (LS-002) ---

// ExecutionBlockGuard detects false "I did not execute" messages where the
// agent claims it didn't run a tool when it should have. Matches Rust's
// execution truth guard for the specific "false negative" case.
type ExecutionBlockGuard struct{}

var execBlockPatterns = []string{
	"i did not execute a tool",
	"i did not execute a delegated subagent task",
	"i did not execute a cron scheduling tool",
}

func (g *ExecutionBlockGuard) Name() string { return "execution_block" }
func (g *ExecutionBlockGuard) Check(content string) GuardResult {
	lower := strings.ToLower(content)
	for _, m := range execBlockPatterns {
		if strings.Contains(lower, m) {
			cleaned := stripSentencesContaining(content, execBlockPatterns)
			if strings.TrimSpace(cleaned) == "" {
				return GuardResult{
					Passed: false, Retry: true,
					Reason: "response consisted entirely of false execution block claims",
				}
			}
			return GuardResult{Passed: false, Content: cleaned}
		}
	}
	return GuardResult{Passed: true}
}
func (g *ExecutionBlockGuard) CheckWithContext(content string, _ *GuardContext) GuardResult {
	return g.Check(content)
}

// --- DelegationMetadataGuard (LS-004) ---

// DelegationMetadataGuard strips internal delegation/orchestration metadata
// that should never be visible to the user. Matches Rust's
// strip_internal_delegation_metadata sanitizer.
type DelegationMetadataGuard struct{}

var delegationMetadataPatterns = []string{
	"delegated_subagent=",
	"selected_subagent=",
	"subtask 1 ->",
	"subtask 2 ->",
	"subtask 3 ->",
	"expected_utility_margin",
	"decomposition gate decision",
}

func (g *DelegationMetadataGuard) Name() string { return "delegation_metadata" }
func (g *DelegationMetadataGuard) Check(content string) GuardResult {
	lower := strings.ToLower(content)
	for _, m := range delegationMetadataPatterns {
		if strings.Contains(lower, m) {
			cleaned := stripSentencesContaining(content, delegationMetadataPatterns)
			if strings.TrimSpace(cleaned) == "" {
				return GuardResult{
					Passed: false, Retry: true,
					Reason: "response consisted entirely of internal delegation metadata",
				}
			}
			return GuardResult{Passed: false, Content: cleaned}
		}
	}
	return GuardResult{Passed: true}
}
func (g *DelegationMetadataGuard) CheckWithContext(content string, _ *GuardContext) GuardResult {
	return g.Check(content)
}

// --- FilesystemDenialGuard (LS-005) ---

// FilesystemDenialGuard detects false filesystem-access denials where the agent
// claims it cannot access files when it has tool access. Matches Rust's
// intent classifier + execution shortcut for filesystem prompts.
type FilesystemDenialGuard struct{}

var filesystemDenialPatterns = []string{
	"can't access your files",
	"cannot access your files",
	"can't access your folders",
	"cannot access your folders",
	"don't have access to your files",
	"as an ai, i don't have access to your files",
	"as an ai text-based interface, i'm not able to directly access",
}

func (g *FilesystemDenialGuard) Name() string { return "filesystem_denial" }
func (g *FilesystemDenialGuard) Check(content string) GuardResult {
	lower := strings.ToLower(content)
	for _, m := range filesystemDenialPatterns {
		if strings.Contains(lower, m) {
			cleaned := stripSentencesContaining(content, filesystemDenialPatterns)
			if strings.TrimSpace(cleaned) == "" {
				return GuardResult{
					Passed: false, Retry: true,
					Reason: "response consisted entirely of false filesystem-access denial",
				}
			}
			return GuardResult{Passed: false, Content: cleaned}
		}
	}
	return GuardResult{Passed: true}
}

func (g *FilesystemDenialGuard) CheckWithContext(content string, ctx *GuardContext) GuardResult {
	if ctx != nil {
		for _, tr := range ctx.ToolResults {
			if toolResultSignalsPolicyOrSandboxDenial(tr) {
				return GuardResult{Passed: true}
			}
		}
	}
	return g.Check(content)
}

// --- LiteraryQuoteRetryGuard (Wave 8, #77) ---

// LiteraryQuoteRetryGuard detects when the model narrates literary quotes,
// song lyrics, or extended passages instead of providing original content.
// These are often hallucinated or improperly attributed.
type LiteraryQuoteRetryGuard struct{}

var literaryQuoteMarkers = []string{
	"as the poet wrote",
	"in the words of",
	"to quote",
	"as shakespeare said",
	"the famous quote",
	"the poem goes",
	"the verse reads",
	"once upon a midnight dreary",
	"shall i compare thee",
	"it was the best of times",
	"all that glitters",
	"to be or not to be",
	"roses are red",
}

func (g *LiteraryQuoteRetryGuard) Name() string { return "literary_quote_retry" }
func (g *LiteraryQuoteRetryGuard) Check(content string) GuardResult {
	lower := strings.ToLower(content)

	// Count quotation mark pairs as indicator of extended quoting.
	quoteCount := strings.Count(content, "\"") / 2
	if quoteCount >= 3 {
		// Check if the content is mostly quotes.
		totalLen := len(content)
		quotedLen := 0
		inQuote := false
		for _, ch := range content {
			if ch == '"' {
				inQuote = !inQuote
				continue
			}
			if inQuote {
				quotedLen++
			}
		}
		if totalLen > 0 && float64(quotedLen)/float64(totalLen) > 0.5 {
			return GuardResult{
				Passed:  false,
				Retry:   true,
				Reason:  "response is predominantly quoted material",
				Verdict: GuardRetryRequested,
			}
		}
	}

	for _, marker := range literaryQuoteMarkers {
		if strings.Contains(lower, marker) {
			return GuardResult{
				Passed:  false,
				Retry:   true,
				Reason:  "narrated literary quote detected: " + marker,
				Verdict: GuardRetryRequested,
			}
		}
	}
	return GuardResult{Passed: true}
}
func (g *LiteraryQuoteRetryGuard) CheckWithContext(content string, _ *GuardContext) GuardResult {
	result := g.Check(content)
	if !result.Passed {
		return result
	}

	// Rust-aligned: detect overbroad refusals to provide literary content.
	lower := strings.ToLower(content)
	overbroadRefusalMarkers := []string{
		"i cannot provide quotes related to",
		"sensitive geopolitical situations",
		"helpful and harmless",
		"avoiding engagement with potentially harmful",
	}
	for _, m := range overbroadRefusalMarkers {
		if strings.Contains(lower, m) {
			return GuardResult{
				Passed:  false,
				Retry:   true,
				Reason:  "overbroad refusal to provide literary content: " + m,
				Verdict: GuardRetryRequested,
			}
		}
	}
	return GuardResult{Passed: true}
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
