package pipeline

import (
	"fmt"
	"strings"
)

// VerificationIssue captures one reason a response should be revised.
type VerificationIssue struct {
	Code   string
	Detail string
}

// VerificationResult is the outcome of a lightweight post-reasoning check.
type VerificationResult struct {
	Passed bool
	Issues []VerificationIssue
}

// VerificationContext carries the minimum state needed to sanity-check
// whether an answer matches the available evidence and the user request.
type VerificationContext struct {
	UserPrompt           string
	Intent               string
	Complexity           string
	PlannedAction        string
	Intents              []string
	MemoryContext        string
	HasEvidence          bool
	HasGaps              bool
	HasFreshnessRisk     bool
	HasContradictions    bool
	HasCanonicalEvidence bool
	PolicySensitive      bool
	RequiresFreshness    bool
	RequiresActionPlan   bool
	Subgoals             []string
}

func BuildVerificationContext(session *Session) VerificationContext {
	ctx := VerificationContext{}
	if session == nil {
		return ctx
	}

	msgs := session.Messages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			ctx.UserPrompt = msgs[i].Content
			break
		}
	}
	ctx.Intent = session.TaskIntent()
	ctx.Complexity = session.TaskComplexity()
	ctx.PlannedAction = session.TaskPlannedAction()
	if ctx.Intent == "" && ctx.UserPrompt != "" {
		reg := NewIntentRegistry()
		intent, _ := reg.Classify(ctx.UserPrompt)
		ctx.Intent = string(intent)
	}
	if ctx.Intent != "" {
		ctx.Intents = []string{ctx.Intent}
	}
	ctx.MemoryContext = session.MemoryContext()
	ctx.HasEvidence = strings.Contains(ctx.MemoryContext, "[Retrieved Evidence]")
	ctx.HasGaps = strings.Contains(ctx.MemoryContext, "[Gaps]")
	ctx.HasFreshnessRisk = strings.Contains(ctx.MemoryContext, "[Freshness Risks]")
	ctx.HasContradictions = strings.Contains(ctx.MemoryContext, "[Contradictions]")
	ctx.HasCanonicalEvidence = strings.Contains(strings.ToLower(ctx.MemoryContext), "canonical")
	ctx.Subgoals = session.TaskSubgoals()
	if len(ctx.Subgoals) == 0 {
		ctx.Subgoals = verificationSubgoals(ctx.UserPrompt)
	}
	ctx.PolicySensitive = verificationPolicySensitive(ctx.UserPrompt, ctx.Intent)
	ctx.RequiresFreshness = verificationRequiresFreshness(ctx.UserPrompt, ctx.Intent)
	ctx.RequiresActionPlan = verificationRequiresActionPlan(ctx.UserPrompt, ctx.Subgoals)
	return ctx
}

func verificationSubgoals(prompt string) []string {
	var goals []string
	for _, task := range extractSubtasks(prompt) {
		if trimmed := strings.TrimSpace(task); trimmed != "" {
			goals = append(goals, trimmed)
		}
	}
	if len(goals) > 0 {
		return goals
	}
	for _, part := range strings.FieldsFunc(prompt, func(r rune) bool {
		return r == '?' || r == ';'
	}) {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			goals = append(goals, trimmed)
		}
	}
	if len(goals) > 1 {
		return goals
	}
	if strings.Contains(strings.ToLower(prompt), " and ") {
		for _, part := range strings.Split(prompt, " and ") {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				goals = append(goals, trimmed)
			}
		}
	}
	return goals
}

func VerifyResponse(content string, ctx VerificationContext) VerificationResult {
	result := VerificationResult{Passed: true}
	lowerContent := strings.ToLower(content)

	if ctx.HasGaps && !containsAny(lowerContent,
		"don't know", "do not know", "unclear", "not enough", "insufficient",
		"need more", "based on the available", "from the available", "i'm not certain",
	) {
		result.Passed = false
		result.Issues = append(result.Issues, VerificationIssue{
			Code:   "unsupported_certainty",
			Detail: "the evidence contains explicit gaps, but the response sounds fully certain",
		})
	}

	if ctx.HasContradictions && !containsAny(lowerContent,
		"conflict", "contradict", "inconsistent", "unclear", "mixed evidence", "however",
	) {
		result.Passed = false
		result.Issues = append(result.Issues, VerificationIssue{
			Code:   "ignored_contradictions",
			Detail: "the evidence contains contradictions, but the response does not acknowledge them",
		})
	}

	if ctx.RequiresFreshness && ctx.HasFreshnessRisk && !containsAny(lowerContent,
		"current", "latest", "as of", "may be outdated", "might be stale",
		"verify", "need a fresher", "need current", "available evidence",
		"not certain", "unclear",
	) {
		result.Passed = false
		result.Issues = append(result.Issues, VerificationIssue{
			Code:   "freshness_overclaim",
			Detail: "the request depends on current information, but the supporting evidence is stale and the response does not acknowledge that risk",
		})
	}

	if len(ctx.Subgoals) >= 2 {
		covered := 0
		for _, goal := range ctx.Subgoals {
			if verificationGoalCovered(goal, lowerContent) {
				covered++
			}
		}
		if covered < len(ctx.Subgoals) {
			result.Passed = false
			result.Issues = append(result.Issues, VerificationIssue{
				Code:   "subgoal_coverage",
				Detail: fmt.Sprintf("the response appears to cover %d/%d requested parts", covered, len(ctx.Subgoals)),
			})
		}
	}

	if ctx.RequiresActionPlan && !verificationHasActionPlan(lowerContent) {
		result.Passed = false
		result.Issues = append(result.Issues, VerificationIssue{
			Code:   "missing_action_plan",
			Detail: "the request asked for remediation, recommendations, or next steps, but the response does not offer a concrete action plan",
		})
	}

	if ctx.PolicySensitive && ctx.HasCanonicalEvidence &&
		containsAny(lowerContent, "definitely", "always", "never", "guaranteed", "certainly") &&
		!verificationMentionsCanonical(lowerContent) {
		result.Passed = false
		result.Issues = append(result.Issues, VerificationIssue{
			Code:   "canonical_source_omitted",
			Detail: "the response sounds absolute on a policy-sensitive question without anchoring itself to the canonical source",
		})
	}

	return result
}

func verificationGoalCovered(goal, response string) bool {
	keywords := verificationKeywords(goal)
	if len(keywords) == 0 {
		return true
	}
	matches := 0
	for _, kw := range keywords {
		if strings.Contains(response, kw) {
			matches++
		}
	}
	threshold := 1
	if len(keywords) >= 4 {
		threshold = 2
	}
	return matches >= threshold
}

func verificationKeywords(goal string) []string {
	stop := map[string]struct{}{
		"the": {}, "and": {}, "for": {}, "with": {}, "that": {}, "this": {},
		"from": {}, "into": {}, "what": {}, "when": {}, "where": {}, "which": {},
		"why": {}, "how": {}, "please": {}, "about": {}, "again": {}, "then": {},
		"need": {}, "want": {}, "does": {}, "have": {}, "your": {}, "our": {},
	}
	var out []string
	for _, token := range strings.Fields(strings.ToLower(goal)) {
		token = strings.Trim(token, ".,:;!?()[]{}\"'")
		if len(token) < 4 {
			continue
		}
		if _, ok := stop[token]; ok {
			continue
		}
		out = append(out, token)
	}
	return out
}

func containsAny(s string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

func verificationPolicySensitive(prompt, intent string) bool {
	lower := strings.ToLower(prompt + " " + intent)
	return containsAny(lower,
		"policy", "refund", "compliance", "rule", "rules", "permission",
		"permissions", "security", "procedure", "sop", "approved", "allowed",
	)
}

func verificationRequiresFreshness(prompt, intent string) bool {
	lower := strings.ToLower(prompt + " " + intent)
	return containsAny(lower,
		"latest", "current", "today", "now", "recent", "recently",
		"up-to-date", "updated", "newest", "version",
	)
}

func verificationRequiresActionPlan(prompt string, subgoals []string) bool {
	lowerPrompt := strings.ToLower(prompt)
	if containsAny(lowerPrompt,
		"remediation", "fix", "mitigation", "next step", "next steps",
		"recommend", "recommendation", "plan", "propose", "action", "actions",
	) {
		return true
	}
	for _, goal := range subgoals {
		lowerGoal := strings.ToLower(goal)
		if containsAny(lowerGoal,
			"remediation", "fix", "mitigation", "next step", "next steps",
			"recommend", "recommendation", "plan", "propose", "action", "actions",
		) {
			return true
		}
	}
	return false
}

func verificationHasActionPlan(response string) bool {
	return containsAny(response,
		"recommend", "recommended", "should", "next step", "next steps",
		"plan", "fix", "mitigation", "remediation", "action", "actions",
		"add ", "update ", "change ", "remove ", "invalidate ", "deploy ",
	)
}

func verificationMentionsCanonical(response string) bool {
	return containsAny(response,
		"according to", "current policy", "current rule", "policy says",
		"canonical", "documented", "source", "official", "current documentation",
	)
}

func (vr VerificationResult) RetryMessage() string {
	if vr.Passed || len(vr.Issues) == 0 {
		return ""
	}
	var parts []string
	for _, issue := range vr.Issues {
		parts = append(parts, issue.Detail)
	}
	return "Your previous response failed verification: " + strings.Join(parts, "; ") +
		". Revise the answer so it matches the available evidence, covers each requested part, and acknowledges uncertainty where needed."
}
