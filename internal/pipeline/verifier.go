package pipeline

import (
	"context"
	"fmt"
	"strings"

	agenttools "roboticus/internal/agent/tools"
	"roboticus/internal/llm"
	sessionpkg "roboticus/internal/session"
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

func (vr VerificationResult) HasIssue(code string) bool {
	for _, issue := range vr.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func (vr VerificationResult) HasAnyIssue(codes ...string) bool {
	set := make(map[string]struct{}, len(codes))
	for _, code := range codes {
		set[code] = struct{}{}
	}
	for _, issue := range vr.Issues {
		if _, ok := set[issue.Code]; ok {
			return true
		}
	}
	return false
}

// ClaimAudit is a structured record of a verifier decision on a single claim.
type ClaimAudit struct {
	Sentence          string   `json:"sentence"`
	Certainty         string   `json:"certainty"`
	CertaintyUpgraded bool     `json:"certainty_upgraded,omitempty"`
	Supported         bool     `json:"supported"`
	Anchored          bool     `json:"anchored"`
	Reconciled        bool     `json:"reconciled"`
	Contested         bool     `json:"contested,omitempty"`
	ProofSatisfied    bool     `json:"proof_satisfied,omitempty"`
	IssueCode         string   `json:"issue_code,omitempty"`
	KeywordHits       int      `json:"keyword_hits"`
	ProofRequired     []string `json:"proof_required,omitempty"`
	MissingProof      []string `json:"missing_proof,omitempty"`
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
	Contradictions       []sessionpkg.ContradictionEvidence
	ToolResults          []ToolResultEntry
	ArtifactProofs       []agenttools.ArtifactProof
	SourceArtifactProofs []agenttools.ArtifactReadProof
	InspectionProofs     []agenttools.InspectionProof
	ExpectedArtifacts    []ExpectedArtifactSpec
	SourceArtifacts      []string
	ArtifactConformance  ArtifactConformance
	SourceConformance    SourceArtifactConformance

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

	// Typed evidence only. Compatibility callers that still set only
	// MemoryContext are normalized at the session boundary when
	// SetMemoryContext derives a VerificationEvidence artifact.
	if ve := session.VerificationEvidence(); ve != nil {
		ctx.HasEvidence = ve.HasEvidence
		ctx.HasGaps = ve.HasGaps
		ctx.HasFreshnessRisk = ve.HasFreshnessRisks
		ctx.HasContradictions = ve.HasContradictions
		ctx.HasCanonicalEvidence = ve.HasCanonicalEvidence
		ctx.Contradictions = append([]sessionpkg.ContradictionEvidence(nil), ve.Contradictions...)
		ctx.EvidenceItems = append([]string(nil), ve.EvidenceItems...)
		ctx.UnresolvedQuestions = append([]string(nil), ve.UnresolvedQuestions...)
		ctx.VerifiedConclusions = append([]string(nil), ve.VerifiedConclusions...)
		ctx.StoppingCriteria = append([]string(nil), ve.StoppingCriteria...)
	}
	ctx.ToolResults = currentTurnToolResults(session)
	ctx.ArtifactProofs = toolResultArtifactProofs(ctx.ToolResults)
	ctx.SourceArtifactProofs = toolResultReadProofs(ctx.ToolResults)
	ctx.InspectionProofs = toolResultInspectionProofs(ctx.ToolResults)
	artifactContract := ParseArtifactPromptContract(ctx.UserPrompt)
	ctx.ExpectedArtifacts = artifactContract.ExpectedOutputs
	ctx.SourceArtifacts = artifactContract.SourceInputs
	ctx.ArtifactConformance = CompareArtifactConformance(ctx.ExpectedArtifacts, ctx.ArtifactProofs)
	ctx.SourceConformance = CompareSourceArtifactConformance(ctx.SourceArtifacts, ctx.SourceArtifactProofs)
	ctx.EvidenceItems = append(ctx.EvidenceItems, toolResultEvidenceItems(ctx.ToolResults)...)
	if len(ctx.ArtifactProofs) > 0 || len(ctx.SourceArtifactProofs) > 0 {
		ctx.HasEvidence = true
		ctx.HasCanonicalEvidence = true
	}
	if verificationNeedsSessionContinuity(ctx.UserPrompt) {
		historyEvidence := verificationSessionHistoryEvidence(session, ctx.UserPrompt)
		if len(historyEvidence) > 0 {
			ctx.EvidenceItems = append(ctx.EvidenceItems, historyEvidence...)
			ctx.HasEvidence = true
			ctx.HasCanonicalEvidence = true
			ctx.HasGaps = false
		}
	}

	ctx.Subgoals = normalizeSemanticSubgoals(session.TaskSubgoals())
	if len(ctx.Subgoals) == 0 {
		ctx.Subgoals = normalizeSemanticSubgoals(verificationSubgoals(ctx.UserPrompt))
	}
	ctx.PolicySensitive = verificationPolicySensitive(ctx.UserPrompt, ctx.Intent)
	ctx.RequiresFreshness = verificationRequiresFreshness(ctx.UserPrompt, ctx.Intent)
	ctx.RequiresActionPlan = verificationRequiresActionPlan(ctx.UserPrompt, ctx.Subgoals)

	return ctx
}

func verificationSubgoals(prompt string) []string {
	prompt = stripOutputShapeDirectives(prompt)
	var goals []string
	for _, task := range extractSubtasks(prompt) {
		if trimmed := strings.TrimSpace(task); trimmed != "" {
			goals = append(goals, trimmed)
		}
	}
	if len(goals) > 0 {
		return goals
	}
	goals = nil
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
		goals = nil
		for _, part := range strings.Split(prompt, " and ") {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				goals = append(goals, trimmed)
			}
		}
		if len(goals) > 1 {
			return goals
		}
	}
	return goals
}

func VerifyResponse(content string, ctx VerificationContext) VerificationResult {
	result := VerificationResult{Passed: true}
	lowerContent := strings.ToLower(content)
	artifactProofPrimary := contradictionChecksSuppressedByArtifactProof(ctx, content)
	artifactClaims := CompareArtifactClaims(content, ctx.ExpectedArtifacts, ctx.SourceArtifacts, ctx.ArtifactProofs, ctx.InspectionProofs, ctx.UserPrompt)
	allowsArtifactBackedCompletion := verificationAllowsArtifactBackedCompletion(ctx)

	if verificationIsSocialTurn(ctx.UserPrompt, ctx.Intent) && verificationLooksOperationalStatus(lowerContent) {
		result.Passed = false
		result.Issues = append(result.Issues, VerificationIssue{
			Code:   "off_topic_social_turn",
			Detail: "the user made a lightweight social or colloquial greeting, but the response pivoted into operational status instead of acknowledging the greeting",
		})
	}

	if ctx.HasGaps && !artifactProofPrimary && !containsAny(lowerContent,
		"don't know", "do not know", "unclear", "not enough", "insufficient",
		"need more", "based on the available", "from the available", "available evidence",
		"i'm not certain", "needs verification", "requires verification",
	) {
		if !verificationAcknowledgesUncertainty(lowerContent) {
			result.Passed = false
			result.Issues = append(result.Issues, VerificationIssue{
				Code:   "unsupported_certainty",
				Detail: "the evidence contains explicit gaps, but the response sounds fully certain",
			})
		}
	}

	if ctx.HasContradictions && !artifactProofPrimary && !containsAny(lowerContent,
		"conflict", "contradict", "inconsistent", "unclear", "mixed evidence", "however",
		"sources disagree", "depending on", "disagree", "differs", "differ ",
	) {
		result.Passed = false
		result.Issues = append(result.Issues, VerificationIssue{
			Code:   "ignored_contradictions",
			Detail: "the evidence contains contradictions, but the response does not acknowledge them",
		})
	}

	if len(ctx.ExpectedArtifacts) > 0 && ctx.ArtifactConformance.HasUnsatisfied() {
		result.Passed = false
		if len(ctx.ArtifactConformance.Unexpected) > 0 {
			result.Issues = append(result.Issues, VerificationIssue{
				Code:   "artifact_unexpected_write",
				Detail: "unexpected artifacts were written outside the requested exact set: " + strings.Join(ctx.ArtifactConformance.Unexpected, ", "),
			})
		}
		if len(ctx.ArtifactConformance.Missing) > 0 {
			var paths []string
			for _, spec := range ctx.ArtifactConformance.Missing {
				paths = append(paths, spec.Path)
			}
			result.Issues = append(result.Issues, VerificationIssue{
				Code:   "artifact_content_mismatch",
				Detail: "exact artifact content could not be proven for: " + strings.Join(paths, ", "),
			})
		} else if len(ctx.ArtifactConformance.Mismatched) > 0 {
			var paths []string
			for _, mismatch := range ctx.ArtifactConformance.Mismatched {
				paths = append(paths, mismatch.Path)
			}
			result.Issues = append(result.Issues, VerificationIssue{
				Code:   "artifact_content_mismatch",
				Detail: "written artifact content did not match the requested exact content for: " + strings.Join(paths, ", "),
			})
		}
	}

	if ctx.SourceConformance.HasUnread() {
		result.Passed = false
		result.Issues = append(result.Issues, VerificationIssue{
			Code:   "source_artifact_unread",
			Detail: "the response depends on source artifacts that were referenced in the prompt but not read through authoritative tool-backed evidence: " + strings.Join(ctx.SourceConformance.Unread, ", "),
		})
	}

	if artifactClaims.HasUnsupported() {
		result.Passed = false
		result.Issues = append(result.Issues, VerificationIssue{
			Code:   "artifact_set_overclaim",
			Detail: "the response claimed artifacts that were neither requested nor proven by write evidence: " + strings.Join(artifactClaims.UnsupportedClaim, ", "),
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

	if len(ctx.Subgoals) >= 2 && !allowsArtifactBackedCompletion {
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

func verificationAllowsArtifactBackedCompletion(ctx VerificationContext) bool {
	if len(ctx.ArtifactProofs) == 0 {
		return false
	}
	if len(ctx.ExpectedArtifacts) > 0 && ctx.ArtifactConformance.HasUnsatisfied() {
		return false
	}
	if ctx.SourceConformance.HasUnread() {
		return false
	}
	if strings.TrimSpace(ctx.PlannedAction) != "" && ctx.PlannedAction != "execute_directly" {
		return false
	}
	if !verificationIsArtifactAuthoringPrompt(ctx.UserPrompt) {
		return false
	}
	if !verificationSubgoalsAreArtifactInternal(ctx.Subgoals) {
		return false
	}
	return true
}

func verificationIsArtifactAuthoringPrompt(prompt string) bool {
	lower := strings.ToLower(prompt)
	if !containsAny(lower, "write", "create", "save", "generate", "draft") {
		return false
	}
	return containsAny(lower,
		"report",
		"document",
		"note",
		"file",
		"markdown",
		"md",
		"vault",
	)
}

func verificationSubgoalsAreArtifactInternal(subgoals []string) bool {
	if len(subgoals) == 0 {
		return true
	}
	for _, goal := range subgoals {
		lower := strings.TrimSpace(strings.ToLower(goal))
		if lower == "" {
			return false
		}
		if containsAny(lower,
			"summarize", "summary", "explain", "describe", "recommend", "next step",
			"tell me", "in chat", "here", "why ", "how ",
			"top ", "compare", "analyze", "analysis",
		) {
			return false
		}
		if containsAny(lower,
			"project path", "project name", "project language", "first edit date",
			"last edit date", "remote", "origin repo", "direction", "status",
			"field", "column", "order", "sort", "descending", "ascending",
		) {
			continue
		}
		// Noun-phrase field labels like "path" or "name" are acceptable on their own.
		if !strings.Contains(lower, " ") && containsAny(lower,
			"path", "name", "language", "date", "status", "direction", "repo", "field",
		) {
			continue
		}
		return false
	}
	return true
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

func verificationNeedsSessionContinuity(prompt string) bool {
	lower := strings.ToLower(prompt)
	return containsAny(lower,
		"did i tell you",
		"for the rest of this session",
		"what codename",
		"what did i tell you",
		"quiet ticker",
		"target docs dir",
	)
}

func verificationSessionHistoryEvidence(session *Session, prompt string) []string {
	if session == nil {
		return nil
	}
	lowerPrompt := strings.ToLower(prompt)
	msgs := session.Messages()
	if len(msgs) == 0 {
		return nil
	}
	start := 0
	if len(msgs) > 8 {
		start = len(msgs) - 8
	}
	var items []string
	for i := start; i < len(msgs); i++ {
		msg := msgs[i]
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		if msg.Role == "user" && strings.Contains(lowerPrompt, "quiet ticker") &&
			strings.Contains(strings.ToLower(content), "quiet ticker") {
			items = append(items, "session continuity: "+content)
			continue
		}
		if msg.Role == "user" && strings.Contains(lowerPrompt, "target docs dir") &&
			strings.Contains(strings.ToLower(content), "target docs dir") {
			items = append(items, "session continuity: "+content)
			continue
		}
		if strings.Contains(lowerPrompt, "what codename") || strings.Contains(lowerPrompt, "did i tell you") {
			items = append(items, "session history: "+content)
		}
	}
	return items
}

func verificationAcknowledgesUncertainty(lowerContent string) bool {
	return containsAny(lowerContent,
		"don't know", "do not know", "unclear", "not enough", "insufficient",
		"need more", "based on the available", "from the available", "available evidence",
		"i'm not certain", "needs verification", "requires verification",
		"don't have up-to-date information", "do not have up-to-date information",
		"don't currently have", "do not currently have",
		"best to refer", "best to check", "recommend checking",
		"within the limitations of my resources", "within my limitations",
		"i can't verify", "cannot verify", "may be outdated",
		"don't have specific information", "do not have specific information",
		"don't have specific details", "do not have specific details",
		"don't have updates", "do not have updates",
		"topic is complex", "can shift rapidly", "changes rapidly",
	)
}

func verificationGoalCovered(goal, response string) bool {
	if verificationGoalAllowsPlanInference(goal) && verificationHasActionPlan(response) {
		return true
	}
	if verificationGoalNeedsEntitySupport(goal) {
		if containsAny(response,
			"affected", "impacted", "impact to", "impact on", "blast radius", "needs verification",
		) {
			return true
		}
		if len(verificationEntityKeywords(response)) > 0 {
			return true
		}
	}
	keywords := verificationKeywords(goal)
	if len(keywords) == 0 {
		return true
	}
	matches := 0
	for _, kw := range keywords {
		if verificationTextContainsKeyword(response, kw) {
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
		"create": {}, "report": {}, "write": {}, "summarize": {}, "summary": {},
		"tell": {}, "exactly": {}, "describe": {}, "discover": {}, "current": {},
		"use": {}, "using": {}, "provide": {}, "give": {}, "show": {}, "return": {},
		"order": {}, "explains": {}, "explain": {}, "propose": {}, "proposed": {},
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
		if verificationResponseAcknowledgesEntityUncertainty(response) && matches >= 1 {
			return true
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
		"system affected", "systems affected", "systems were affected",
		"service affected", "services affected", "services were affected",
		"component affected", "components affected", "components were affected",
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
			if verificationTextContainsKeyword(strings.ToLower(item), kw) {
				matches++
				break
			}
		}
	}
	return matches
}

func verificationTextContainsKeyword(text, keyword string) bool {
	lower := strings.ToLower(text)
	if strings.Contains(lower, keyword) {
		return true
	}
	stemmedKeyword := verificationStemKeyword(keyword)
	if stemmedKeyword == "" {
		return false
	}
	for _, token := range strings.FieldsFunc(lower, func(r rune) bool {
		return r < 'a' || r > 'z'
	}) {
		if verificationStemKeyword(token) == stemmedKeyword {
			return true
		}
	}
	return false
}

func verificationStemKeyword(token string) string {
	token = strings.TrimSpace(strings.ToLower(token))
	if len(token) < 4 {
		return token
	}
	switch {
	case strings.HasSuffix(token, "ing") && len(token) > 5:
		return strings.TrimSuffix(token, "ing")
	case strings.HasSuffix(token, "ed") && len(token) > 4:
		trimmed := strings.TrimSuffix(token, "ed")
		if !strings.HasSuffix(trimmed, "e") {
			trimmed += "e"
		}
		return trimmed
	case strings.HasSuffix(token, "s") && !strings.HasSuffix(token, "ss") && len(token) > 4:
		return strings.TrimSuffix(token, "s")
	default:
		return token
	}
}

func verificationResponseAcknowledgesEntityUncertainty(response string) bool {
	return containsAny(response,
		"needs verification", "need verification", "still needs verification",
		"requires verification", "require verification", "unverified",
		"available evidence confirms", "based on the available evidence",
		"not certain", "unclear", "still needs confirmation", "needs confirmation",
	)
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

func verificationIsSocialTurn(prompt, intent string) bool {
	if intent == "conversational" && isSocialConversationalTurn(strings.ToLower(prompt)) {
		return true
	}
	return false
}

func verificationLooksOperationalStatus(response string) bool {
	return containsAny(response,
		"sandbox", "sandboxed", "allow-list", "allow list",
		"workspace", "vault", "runtime status", "still locked",
		"tool access", "access yet", "memory that tells me",
	)
}

func (vr VerificationResult) RetryMessage() string {
	if vr.Passed || len(vr.Issues) == 0 {
		return ""
	}
	var parts []string
	summary := SummarizeVerification(vr)
	for _, issue := range vr.Issues {
		parts = append(parts, issue.Detail)
	}
	if vr.HasIssue("off_topic_social_turn") {
		return "Your previous response failed verification: " + strings.Join(parts, "; ") +
			". Revise the answer so it directly answers the user's greeting in a brief, natural way. Do not mention sandbox state, workspace paths, vault access, memory state, or runtime status unless the user explicitly asked about them."
	}
	if vr.HasAnyIssue("proof_obligation_unmet", "unresolved_contradicted_claim", "unsupported_absolute_claim") {
		var requirements []string
		if summary.ContestedCount > 0 {
			requirements = append(requirements, "explicitly reconcile contested evidence before making definite claims")
		}
		if summary.ProofGapCount > 0 {
			requirements = append(requirements, "anchor each high-risk claim to retrieved or canonical evidence, or clearly lower certainty")
		}
		if len(requirements) > 0 {
			return "Your previous response failed verification: " + strings.Join(parts, "; ") +
				". Revise the answer so it matches the available evidence, covers each requested part, and " + strings.Join(requirements, "; ") + "."
		}
	}
	return "Your previous response failed verification: " + strings.Join(parts, "; ") +
		". Revise the answer so it matches the available evidence, covers each requested part, and acknowledges uncertainty where needed."
}

func verifierFinalizationMessage(vr VerificationResult) string {
	if vr.Passed || len(vr.Issues) == 0 {
		return ""
	}
	var parts []string
	for _, issue := range vr.Issues {
		parts = append(parts, issue.Detail)
	}
	return "I can't honestly claim success because the final verification still failed. Remaining issues: " +
		strings.Join(parts, "; ") + "."
}
