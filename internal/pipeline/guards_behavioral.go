package pipeline

import (
	"strings"
)

// --- SubagentClaimGuard ---

// SubagentClaimGuard detects responses that narrate delegation instead of
// responding directly. If the model says "let me delegate" but no subagent
// task actually completed, the response is rejected for retry.
type SubagentClaimGuard struct{}

func (g *SubagentClaimGuard) Name() string { return "subagent_claim" }
func (g *SubagentClaimGuard) Check(content string) GuardResult {
	return GuardResult{Passed: true}
}
func (g *SubagentClaimGuard) CheckWithContext(content string, ctx *GuardContext) GuardResult {
	if ctx == nil {
		return GuardResult{Passed: true}
	}
	prov := ctx.DelegationProvenance
	if prov.SubagentTaskStarted && prov.SubagentTaskCompleted && prov.SubagentResultAttached {
		return GuardResult{Passed: true}
	}
	lower := strings.ToLower(content)
	markers := []string{
		"let me delegate", "delegating to", "i have a specialist",
		"passing this to", "routing to my", "subagent-generated",
		"my specialist will", "handing off to",
	}
	for _, m := range markers {
		if strings.Contains(lower, m) {
			return GuardResult{
				Passed: false, Retry: true,
				Reason: "narrated delegation without completing subagent lifecycle",
			}
		}
	}
	return GuardResult{Passed: true}
}

// --- TaskDeferralGuard ---

// TaskDeferralGuard blocks turns that only narrate future actions without
// actually performing them. Introspection-only turns (memory stats, runtime
// context) should not end with "let me do X next".
type TaskDeferralGuard struct{}

func (g *TaskDeferralGuard) Name() string { return "task_deferral" }
func (g *TaskDeferralGuard) Check(content string) GuardResult {
	return GuardResult{Passed: true}
}
func (g *TaskDeferralGuard) CheckWithContext(content string, ctx *GuardContext) GuardResult {
	if ctx == nil || len(ctx.ToolResults) == 0 {
		return GuardResult{Passed: true}
	}
	introspectionOnly := true
	introspectionTools := map[string]bool{
		"get_memory_stats": true, "get_runtime_context": true,
		"get_channel_health": true, "get_subagent_status": true,
	}
	for _, tr := range ctx.ToolResults {
		if !introspectionTools[tr.ToolName] {
			introspectionOnly = false
			break
		}
	}
	if !introspectionOnly {
		return GuardResult{Passed: true}
	}
	lower := strings.ToLower(content)
	deferralPatterns := []string{
		"let me ", "i'll ", "i will ", "i need to ",
		"next i can ", "next step", "i should ",
	}
	for _, p := range deferralPatterns {
		if strings.Contains(lower, p) {
			return GuardResult{
				Passed: false, Retry: true,
				Reason: "introspection-only turn narrated deferred action",
			}
		}
	}
	return GuardResult{Passed: true}
}

// --- InternalJargonGuard ---

// InternalJargonGuard strips internal infrastructure details from responses:
// subagent names, tool inventories, runtime state dumps.
type InternalJargonGuard struct{}

func (g *InternalJargonGuard) Name() string { return "internal_jargon" }
func (g *InternalJargonGuard) Check(content string) GuardResult {
	return GuardResult{Passed: true}
}
func (g *InternalJargonGuard) CheckWithContext(content string, ctx *GuardContext) GuardResult {
	if ctx == nil {
		return GuardResult{Passed: true}
	}
	lower := strings.ToLower(content)

	// Tier 1: Infrastructure leak keywords.
	infraMarkers := []string{
		"decomposition gate decision", "expected_utility_margin",
		"active model:", "enabled subagents:", "pipeline stage",
		"guard chain", "react loop", "inference_costs",
	}
	for _, m := range infraMarkers {
		if strings.Contains(lower, m) {
			return GuardResult{
				Passed: false, Retry: true,
				Reason: "infrastructure terminology leaked: " + m,
			}
		}
	}

	// Tier 2: Subagent name leak.
	for _, name := range ctx.SubagentNames {
		if strings.Contains(lower, strings.ToLower(name)) {
			return GuardResult{
				Passed: false, Retry: true,
				Reason: "subagent name leaked: " + name,
			}
		}
	}

	return GuardResult{Passed: true}
}

// --- DeclaredActionGuard ---

// DeclaredActionGuard detects when a user declares a physical action
// (attack, grab, cast, dodge) but the response doesn't resolve it.
// Primarily relevant for interactive fiction and tabletop RPG contexts.
type DeclaredActionGuard struct{}

var actionVerbs = []string{
	"attack", "stab", "grab", "cast", "hide", "dodge", "climb",
	"shoot", "throw", "kick", "punch", "slash", "block", "parry",
	"charge", "flee", "sneak", "search", "pick up", "open",
}

func (g *DeclaredActionGuard) Name() string { return "declared_action" }
func (g *DeclaredActionGuard) Check(content string) GuardResult {
	return GuardResult{Passed: true}
}
func (g *DeclaredActionGuard) CheckWithContext(content string, ctx *GuardContext) GuardResult {
	if ctx == nil || ctx.UserPrompt == "" {
		return GuardResult{Passed: true}
	}
	userLower := strings.ToLower(ctx.UserPrompt)
	var declaredVerb string
	for _, v := range actionVerbs {
		if strings.Contains(userLower, v) {
			declaredVerb = v
			break
		}
	}
	if declaredVerb == "" {
		return GuardResult{Passed: true}
	}
	// Check if response resolves the action.
	respLower := strings.ToLower(content)
	resolutionIndicators := []string{
		"roll", "d20", "dc ", "check", "succeed", "fail",
		"attempt", "consequences", "are you sure", "damage",
		"hit", "miss", "save", "result",
	}
	for _, ind := range resolutionIndicators {
		if strings.Contains(respLower, ind) {
			return GuardResult{Passed: true}
		}
	}
	return GuardResult{
		Passed: false, Retry: true,
		Reason: "declared action '" + declaredVerb + "' was not resolved in response",
	}
}
