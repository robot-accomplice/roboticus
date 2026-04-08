package pipeline

import (
	"regexp"
	"strings"
)

// --- LowValueParrotingGuard ---

// LowValueParrotingGuard rejects placeholder responses that add no value
// ("ready", "on it") and responses that parrot the user's input verbatim.
type LowValueParrotingGuard struct{}

var placeholderPhrases = []string{
	"ready", "on it", "working on that now", "i await your insights",
	"understood, processing", "let me think about that",
	"sure thing", "absolutely", "of course",
}

func (g *LowValueParrotingGuard) Name() string { return "low_value_parroting" }
func (g *LowValueParrotingGuard) Check(content string) GuardResult {
	return GuardResult{Passed: true}
}
func (g *LowValueParrotingGuard) CheckWithContext(content string, ctx *GuardContext) GuardResult {
	if ctx == nil {
		return GuardResult{Passed: true}
	}
	trimmed := strings.TrimSpace(strings.ToLower(content))

	// Check placeholders.
	for _, ph := range placeholderPhrases {
		if trimmed == ph || trimmed == ph+"." || trimmed == ph+"!" {
			return GuardResult{
				Passed: false, Retry: true,
				Reason: "low-value placeholder response: " + ph,
			}
		}
	}

	// Check parroting: high token overlap with user prompt.
	// Requires ALL THREE thresholds: overlap >= 0.88, prefix_ratio >= 0.55, length_ratio <= 1.35.
	if ctx.UserPrompt != "" && len(content) > 20 {
		overlap := tokenOverlapRatio(content, ctx.UserPrompt)
		prefixRatio := commonPrefixRatio(content, ctx.UserPrompt)
		lengthRatio := float64(len(content)) / float64(len(ctx.UserPrompt))
		if overlap >= 0.88 && prefixRatio >= 0.55 && lengthRatio <= 1.35 {
			return GuardResult{
				Passed: false, Retry: true,
				Reason: "response parrots user input",
			}
		}
	}
	return GuardResult{Passed: true}
}

// --- NonRepetitionGuard (enhanced) ---

// NonRepetitionGuardV2 extends the basic repetition guard with cross-turn
// detection. It checks both within-turn repetition and verbatim overlap
// against prior assistant messages.
type NonRepetitionGuardV2 struct{}

func (g *NonRepetitionGuardV2) Name() string { return "non_repetition_v2" }
func (g *NonRepetitionGuardV2) Check(content string) GuardResult {
	return GuardResult{Passed: true}
}
func (g *NonRepetitionGuardV2) CheckWithContext(content string, ctx *GuardContext) GuardResult {
	if ctx == nil {
		return GuardResult{Passed: true}
	}

	// Cross-turn: compare with previous assistant message.
	if ctx.PreviousAssistant != "" {
		overlap := tokenOverlapRatio(content, ctx.PreviousAssistant)
		prefix := commonPrefixRatio(content, ctx.PreviousAssistant)
		if overlap >= 0.86 || (overlap >= 0.72 && prefix >= 0.55) {
			return GuardResult{
				Passed: false, Retry: true,
				Reason: "response repeats previous assistant message",
			}
		}
	}

	// History scan: check for 10+ word exact matches in prior messages.
	contentWords := strings.Fields(strings.ToLower(content))
	for _, prior := range ctx.PriorAssistantMessages {
		priorWords := strings.Fields(strings.ToLower(prior))
		if longestCommonSubseq(contentWords, priorWords) >= 10 {
			return GuardResult{
				Passed: false, Retry: true,
				Reason: "response contains verbatim fragment from prior turn",
			}
		}
	}

	// Cross-turn self-echo detection (Wave 8, #80).
	if echoReason := findSelfEchoAcrossHistory(content, ctx.PriorAssistantMessages); echoReason != "" {
		return GuardResult{
			Passed:  false,
			Retry:   true,
			Reason:  echoReason,
			Verdict: GuardRetryRequested,
		}
	}

	// Rust-aligned: fresh-delta detection. When the user asks for a status
	// update but the response repeats prior content, rewrite with a delta message.
	if ctx.UserPrompt != "" && ctx.PreviousAssistant != "" {
		if userRequestsFreshDelta(ctx.UserPrompt) {
			overlap := tokenOverlapRatio(content, ctx.PreviousAssistant)
			if overlap >= 0.72 {
				return GuardResult{
					Passed:  false,
					Content: "No verified delta since last check. Will report back when something changes.",
					Reason:  "user requested fresh delta but response repeats prior content",
					Verdict: GuardRewritten,
				}
			}
		}
	}

	return GuardResult{Passed: true}
}

// freshDeltaMarkers are phrases that indicate the user wants new/changed info.
var freshDeltaMarkers = []string{
	"status", "update", "what changed", "anything changed",
	"fresh check", "check again", "still", "latest", "current", "sitrep",
}

// userRequestsFreshDelta returns true if the user prompt contains delta-request markers.
func userRequestsFreshDelta(prompt string) bool {
	lower := strings.ToLower(prompt)
	for _, m := range freshDeltaMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// findSelfEchoAcrossHistory detects structural self-echo: when the agent
// produces the same opening pattern across multiple prior turns, suggesting
// it's stuck in a response template loop.
func findSelfEchoAcrossHistory(content string, history []string) string {
	if len(history) < 2 {
		return ""
	}
	contentLines := strings.Split(strings.TrimSpace(content), "\n")
	if len(contentLines) == 0 {
		return ""
	}
	firstLine := strings.TrimSpace(contentLines[0])
	if len(firstLine) < 15 {
		return ""
	}

	// Check if the same opening line appears in 2+ prior assistant messages.
	matchCount := 0
	for _, prior := range history {
		priorLines := strings.Split(strings.TrimSpace(prior), "\n")
		if len(priorLines) > 0 && strings.TrimSpace(priorLines[0]) == firstLine {
			matchCount++
		}
	}
	if matchCount >= 2 {
		return "response opening repeats across 3+ turns (template loop detected)"
	}
	return ""
}

// --- OutputContractGuard ---

// OutputContractGuard checks that if the user requested a specific number
// of items (e.g., "give me 5 bullet points"), the response delivers exactly that.
type OutputContractGuard struct{}

var bulletCountRe = regexp.MustCompile(`(?i)(?:give me|list|provide|write)\s+(\d+)\s+(?:bullet|point|item|step|thing|reason|example)`)

func (g *OutputContractGuard) Name() string { return "output_contract" }
func (g *OutputContractGuard) Check(content string) GuardResult {
	return GuardResult{Passed: true}
}
func (g *OutputContractGuard) CheckWithContext(content string, ctx *GuardContext) GuardResult {
	if ctx == nil || ctx.UserPrompt == "" {
		return GuardResult{Passed: true}
	}
	matches := bulletCountRe.FindStringSubmatch(ctx.UserPrompt)
	if matches == nil {
		return GuardResult{Passed: true}
	}
	requested := 0
	for _, c := range matches[1] {
		requested = requested*10 + int(c-'0')
	}
	if requested < 1 || requested > 50 {
		return GuardResult{Passed: true}
	}

	// Count bullet lines in response.
	actual := 0
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") ||
			strings.HasPrefix(trimmed, "• ") || (len(trimmed) > 2 && trimmed[0] >= '1' && trimmed[0] <= '9' && trimmed[1] == '.') {
			actual++
		}
	}

	if actual != requested && actual > 0 {
		return GuardResult{
			Passed: false, Retry: true,
			Reason: "requested " + matches[1] + " items but response has " + strings.TrimSpace(strings.Repeat(" ", 0)) + string(rune('0'+actual)),
		}
	}
	return GuardResult{Passed: true}
}

// --- UserEchoGuard ---

// UserEchoGuard detects when the response echoes the user's exact words
// back (8+ word window match).
type UserEchoGuard struct{}

func (g *UserEchoGuard) Name() string { return "user_echo" }
func (g *UserEchoGuard) Check(content string) GuardResult {
	return GuardResult{Passed: true}
}
func (g *UserEchoGuard) CheckWithContext(content string, ctx *GuardContext) GuardResult {
	if ctx == nil || ctx.UserPrompt == "" || len(ctx.UserPrompt) < 40 {
		return GuardResult{Passed: true}
	}
	userWords := strings.Fields(strings.ToLower(ctx.UserPrompt))
	respWords := strings.Fields(strings.ToLower(content))
	if longestCommonSubseq(userWords, respWords) >= 8 {
		return GuardResult{
			Passed: false, Retry: true,
			Reason: "response echoes user input verbatim (8+ word match)",
		}
	}
	return GuardResult{Passed: true}
}

// --- Shared utilities ---

// tokenOverlapRatio computes the Jaccard-like overlap between two texts.
func tokenOverlapRatio(a, b string) float64 {
	aTokens := strings.Fields(strings.ToLower(a))
	bTokens := strings.Fields(strings.ToLower(b))
	if len(aTokens) == 0 || len(bTokens) == 0 {
		return 0
	}
	bSet := make(map[string]bool, len(bTokens))
	for _, t := range bTokens {
		bSet[t] = true
	}
	overlap := 0
	for _, t := range aTokens {
		if bSet[t] {
			overlap++
		}
	}
	return float64(overlap) / float64(max(len(aTokens), len(bTokens)))
}

// commonPrefixRatio returns the ratio of common prefix length to total length.
func commonPrefixRatio(a, b string) float64 {
	aWords := strings.Fields(strings.ToLower(a))
	bWords := strings.Fields(strings.ToLower(b))
	common := 0
	for i := 0; i < len(aWords) && i < len(bWords); i++ {
		if aWords[i] != bWords[i] {
			break
		}
		common++
	}
	total := max(len(aWords), len(bWords))
	if total == 0 {
		return 0
	}
	return float64(common) / float64(total)
}

// longestCommonSubseq finds the longest contiguous word sequence match.
func longestCommonSubseq(a, b []string) int {
	best := 0
	for i := 0; i < len(a); i++ {
		for j := 0; j < len(b); j++ {
			k := 0
			for i+k < len(a) && j+k < len(b) && a[i+k] == b[j+k] {
				k++
			}
			if k > best {
				best = k
			}
		}
	}
	return best
}
