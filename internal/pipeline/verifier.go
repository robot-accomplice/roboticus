package pipeline

import (
	"context"
	"fmt"
	"strings"

	"roboticus/internal/llm"
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
	// ClaimAudits carries per-claim provenance judgments so traces and
	// observability layers can show exactly which claims were supported,
	// anchored, or flagged. Populated even on passing runs.
	ClaimAudits []ClaimAudit
}

// ClaimAudit is a structured record of a verifier decision on a single claim.
type ClaimAudit struct {
	Sentence          string `json:"sentence"`
	Certainty         string `json:"certainty"`
	CertaintyUpgraded bool   `json:"certainty_upgraded,omitempty"`
	Supported         bool   `json:"supported"`
	Anchored          bool   `json:"anchored"`
	Reconciled        bool   `json:"reconciled"`
	IssueCode         string `json:"issue_code,omitempty"`
	KeywordHits       int    `json:"keyword_hits"`
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
	EvidenceItems        []string

	// Executive state surfaces the current plan, assumptions, unresolved
	// questions, verified conclusions, decision checkpoints, and stopping
	// criteria so the verifier can check a response against durable task state.
	UnresolvedQuestions []string
	VerifiedConclusions []string
	StoppingCriteria    []string

	// CertaintyClassifier is the optional embedding-backed semantic
	// classifier used by claim extraction to tag certainty when none of
	// the lexical markers in verifier_claims.go match. Nil means
	// lexical-only behaviour, which is what tests rely on by default.
	CertaintyClassifier *llm.SemanticClassifier
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

	// v1.0.6 P2-C: prefer the typed evidence artifact the pipeline's
	// Stage 8.5 attaches via SetVerificationEvidence. This replaces the
	// pre-v1.0.6 pattern where the verifier `strings.Contains`'d its
	// way through the rendered "[Retrieved Evidence]", "[Gaps]",
	// "[Freshness Risks]", "[Contradictions]" markers — a coupling
	// that would silently break the verifier if the assembler ever
	// renamed a section header. The string parse is kept as a
	// fallback for callers that don't flow through the full pipeline
	// (tests, smoke harnesses, ad-hoc CLI paths).
	if ve := session.VerificationEvidence(); ve != nil {
		ctx.HasEvidence = ve.HasEvidence
		ctx.HasGaps = ve.HasGaps
		ctx.HasFreshnessRisk = ve.HasFreshnessRisks
		ctx.HasContradictions = ve.HasContradictions
		ctx.HasCanonicalEvidence = ve.HasCanonicalEvidence
		ctx.EvidenceItems = append([]string(nil), ve.EvidenceItems...)
		ctx.UnresolvedQuestions = append([]string(nil), ve.UnresolvedQuestions...)
		ctx.VerifiedConclusions = append([]string(nil), ve.VerifiedConclusions...)
		ctx.StoppingCriteria = append([]string(nil), ve.StoppingCriteria...)
	} else {
		// String-parse fallback for non-pipeline callers.
		ctx.HasEvidence = strings.Contains(ctx.MemoryContext, "[Retrieved Evidence]")
		ctx.HasGaps = strings.Contains(ctx.MemoryContext, "[Gaps]")
		ctx.HasFreshnessRisk = strings.Contains(ctx.MemoryContext, "[Freshness Risks]")
		ctx.HasContradictions = strings.Contains(ctx.MemoryContext, "[Contradictions]")
		ctx.HasCanonicalEvidence = strings.Contains(strings.ToLower(ctx.MemoryContext), "canonical")
		ctx.EvidenceItems = verificationSectionItems(ctx.MemoryContext, "[Retrieved Evidence]")
		ctx.UnresolvedQuestions = verificationExecutiveSection(ctx.MemoryContext, "Unresolved questions")
		ctx.VerifiedConclusions = verificationExecutiveSection(ctx.MemoryContext, "Verified conclusions")
		ctx.StoppingCriteria = verificationExecutiveSection(ctx.MemoryContext, "Stopping criteria")
	}

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
		"sources disagree", "depending on", "disagree", "differs", "differ ",
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

	if len(ctx.Subgoals) > 0 && len(ctx.EvidenceItems) > 0 {
		var unsupported []string
		for _, goal := range ctx.Subgoals {
			if !verificationGoalCovered(goal, lowerContent) {
				continue
			}
			if verificationGoalAllowsPlanInference(goal) {
				continue
			}
			if !verificationGoalSupportedByEvidence(goal, lowerContent, ctx.EvidenceItems) {
				unsupported = append(unsupported, goal)
			}
		}
		if len(unsupported) > 0 {
			result.Passed = false
			result.Issues = append(result.Issues, VerificationIssue{
				Code:   "unsupported_subgoal",
				Detail: "the response answers requested parts that are not supported by the retrieved evidence: " + strings.Join(unsupported, "; "),
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

	// Claim-level verification: parse the response into individual claims with
	// certainty metadata and check each absolute claim for evidence support,
	// contradiction reconciliation, and canonical anchoring on high-risk queries.
	claims := ExtractClaims(content)
	upgradeClaimCertaintyWithClassifier(claims, ctx.CertaintyClassifier)
	verifyClaimLevel(content, ctx, claims, &result)
	verifyExecutiveState(lowerContent, ctx, &result)

	return result
}

// upgradeClaimCertaintyWithClassifier runs the embedding-backed semantic
// classifier over every claim still tagged CertaintyModerate (which means
// no lexical marker matched) and upgrades the tag if the classifier
// confidently identifies an absolute / high / hedged paraphrase.
//
// Lexical-tagged claims are intentionally untouched — they are 100%
// precision for their phrases and the classifier's job is to fill the
// paraphrase gap, not to second-guess known matches.
//
// Mutates claims in place to keep the call site one line.
func upgradeClaimCertaintyWithClassifier(claims []VerifiedClaim, classifier *llm.SemanticClassifier) {
	if classifier == nil || len(claims) == 0 {
		return
	}
	ctx := context.Background()
	for i := range claims {
		if claims[i].Certainty != CertaintyModerate {
			continue
		}
		if upgraded, ok := classifyCertaintySemantic(ctx, classifier, claims[i].Sentence); ok {
			claims[i].Certainty = upgraded
			claims[i].CertaintyUpgraded = true
		}
	}
}

// verifyExecutiveState checks that the response honors durable executive-state
// artifacts from prior turns: unresolved questions should not be silently
// dropped when the response is materially related to them, and stopping
// criteria should shape the agent's commitments.
func verifyExecutiveState(lowerContent string, ctx VerificationContext, result *VerificationResult) {
	if len(ctx.UnresolvedQuestions) == 0 && len(ctx.StoppingCriteria) == 0 {
		return
	}

	var abandoned []string
	for _, question := range ctx.UnresolvedQuestions {
		trimmed := strings.TrimSpace(question)
		if trimmed == "" {
			continue
		}
		keywords := verificationKeywords(trimmed)
		if len(keywords) == 0 {
			continue
		}
		// If the response does not even mention the keywords of a still-open
		// question that the prompt is now asking to close, flag it — the agent
		// is drifting away from its own prior task state.
		promptRelated := verificationKeywordsOverlap(keywords, ctx.UserPrompt) > 0
		if !promptRelated {
			continue
		}
		matches := 0
		for _, kw := range keywords {
			if strings.Contains(lowerContent, kw) {
				matches++
			}
		}
		if matches == 0 {
			abandoned = append(abandoned, trimmed)
		}
	}
	if len(abandoned) > 0 {
		result.Passed = false
		result.Issues = append(result.Issues, VerificationIssue{
			Code:   "abandoned_unresolved_question",
			Detail: "the response ignores unresolved executive-state questions that the current prompt is related to: " + strings.Join(abandoned, "; "),
		})
	}

	// Stopping criteria surfaced in executive state should be honored. When a
	// response declares a high-certainty claim to "complete" something while
	// the stopping criterion has not been met, flag it. The check is narrow:
	// only trips when the stopping criterion keywords are absent from the
	// response AND the response claims completion.
	if len(ctx.StoppingCriteria) > 0 && containsAny(lowerContent,
		"task complete", "task completed", "done.", "finished.", "we are done",
		"we're done", "work is complete",
	) {
		var missing []string
		for _, criterion := range ctx.StoppingCriteria {
			crit := strings.TrimSpace(criterion)
			if crit == "" {
				continue
			}
			keywords := verificationKeywords(crit)
			matches := 0
			for _, kw := range keywords {
				if strings.Contains(lowerContent, kw) {
					matches++
				}
			}
			threshold := 1
			if len(keywords) >= 4 {
				threshold = 2
			}
			if matches < threshold {
				missing = append(missing, crit)
			}
		}
		if len(missing) > 0 {
			result.Passed = false
			result.Issues = append(result.Issues, VerificationIssue{
				Code:   "stopping_criteria_unmet",
				Detail: "the response declares the task complete without addressing the stopping criteria: " + strings.Join(missing, "; "),
			})
		}
	}
}

func verificationKeywordsOverlap(keywords []string, text string) int {
	if len(keywords) == 0 || text == "" {
		return 0
	}
	lower := strings.ToLower(text)
	overlap := 0
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			overlap++
		}
	}
	return overlap
}

// verificationExecutiveSection parses a labeled subsection out of the working
// state block rendered by the memory assembler (see executive.go::FormatForContext).
// The block has the shape:
//
//	Executive State:
//	Plan:
//	- ...
//	Unresolved questions:
//	- ...
//
// Each bullet becomes a returned string.
func verificationExecutiveSection(memoryContext, label string) []string {
	if memoryContext == "" || label == "" {
		return nil
	}
	needle := label + ":"
	idx := strings.Index(memoryContext, needle)
	if idx < 0 {
		return nil
	}
	rest := memoryContext[idx+len(needle):]
	// Terminate at the next labeled subsection or blank line.
	lines := strings.Split(rest, "\n")
	var items []string
	for i, line := range lines {
		if i == 0 {
			continue // skip the label line tail
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			break
		}
		if !strings.HasPrefix(trimmed, "-") {
			// Next labeled section such as "Verified conclusions:".
			break
		}
		item := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
		// Strip trailing parenthetical metadata "(steps=..., ...)".
		if paren := strings.LastIndex(item, " ("); paren > 0 {
			item = item[:paren]
		}
		if item != "" {
			items = append(items, item)
		}
	}
	return items
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

func verificationGoalSupportedByEvidence(goal, response string, evidenceItems []string) bool {
	if len(evidenceItems) == 0 {
		return false
	}

	if verificationGoalNeedsEntitySupport(goal) {
		responseKeywords := verificationEntityKeywords(response)
		if len(responseKeywords) == 0 {
			responseKeywords = verificationKeywords(goal)
		}
		matches := verificationKeywordEvidenceMatches(responseKeywords, evidenceItems)
		threshold := 1
		if len(responseKeywords) >= 2 {
			threshold = 2
		}
		return matches >= threshold
	}

	goalMatches := verificationKeywordEvidenceMatches(verificationKeywords(goal), evidenceItems)
	responseMatches := verificationKeywordEvidenceMatches(verificationKeywords(response), evidenceItems)

	if goalMatches >= 1 {
		return true
	}
	return responseMatches >= 2
}

func verificationGoalAllowsPlanInference(goal string) bool {
	lower := strings.ToLower(goal)
	return containsAny(lower,
		"remediation", "fix", "mitigation", "next step", "next steps",
		"recommend", "recommendation", "plan", "propose", "action", "actions",
	)
}

func verificationGoalNeedsEntitySupport(goal string) bool {
	lower := strings.ToLower(goal)
	return containsAny(lower,
		"affected system", "affected systems", "impacted system", "impacted systems",
		"affected component", "affected components", "impacted component", "impacted components",
		"affected service", "affected services", "dependency", "dependencies", "blast radius",
		"what breaks", "impact",
	)
}

func verificationEntityKeywords(text string) []string {
	stop := map[string]struct{}{
		"root": {}, "cause": {}, "affected": {}, "systems": {}, "system": {},
		"services": {}, "service": {}, "components": {}, "component": {},
		"impact": {}, "impacted": {}, "because": {}, "stale": {}, "cache": {},
		"entry": {}, "deploy": {}, "after": {}, "before": {}, "during": {},
	}

	var out []string
	seen := make(map[string]struct{})
	for _, token := range verificationKeywords(text) {
		if _, skip := stop[token]; skip {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	return out
}

func verificationKeywordEvidenceMatches(keywords []string, evidenceItems []string) int {
	if len(keywords) == 0 {
		return 0
	}
	matches := 0
	for _, kw := range keywords {
		for _, item := range evidenceItems {
			if strings.Contains(strings.ToLower(item), kw) {
				matches++
				break
			}
		}
	}
	return matches
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

func verificationSectionItems(memoryContext, header string) []string {
	if memoryContext == "" {
		return nil
	}
	idx := strings.Index(memoryContext, header)
	if idx < 0 {
		return nil
	}
	rest := memoryContext[idx+len(header):]
	if next := strings.Index(rest, "\n["); next >= 0 {
		rest = rest[:next]
	}

	var items []string
	for _, line := range strings.Split(rest, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimLeft(line, "- ")
		if dot := strings.Index(line, "] "); strings.HasPrefix(line, "1.") || strings.HasPrefix(line, "2.") || strings.HasPrefix(line, "3.") || strings.HasPrefix(line, "4.") || strings.HasPrefix(line, "5.") {
			if dot >= 0 && dot+2 < len(line) {
				line = line[dot+2:]
			}
		}
		items = append(items, line)
	}
	return items
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
