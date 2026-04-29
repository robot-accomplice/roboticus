package policy

import (
	"fmt"
	"strings"

	"roboticus/internal/core"
)

// FormatDeniedToolResult builds the tool-result string shown to the model after
// policy denies a tool. It keeps the "Policy denied: …" line for compatibility
// with guards and memory filters, prefixes the exact engine rule id, and adds
// short, actionable operator context so the assistant can cite what blocked the
// call—without blaming unreachable remote sites.
func FormatDeniedToolResult(toolName string, decision DecisionResult, auth core.AuthorityLevel, claim *core.SecurityClaim) string {
	if decision.Allowed {
		return ""
	}
	var b strings.Builder
	rule := strings.TrimSpace(decision.Rule)
	if rule == "" {
		rule = "unknown"
	}
	fmt.Fprintf(&b, "Invoked policy: Roboticus tool policy engine (`internal/agent/policy.Engine`), rule %q", rule)
	if p := toolPolicyRulePriority(rule); p > 0 {
		fmt.Fprintf(&b, ", evaluation priority %d (first denying rule wins)", p)
	}
	b.WriteString(".\n")
	fmt.Fprintf(&b, "Policy denied: %s\n\n", decision.Reason)
	b.WriteString(denialRemediation(toolName, decision, auth, claim))
	return strings.TrimSpace(b.String())
}

// FormatBlockedToolResult is the tool-result string when a tool is listed under
// agent.approvals.blocked_tools (classification runs before the policy engine).
func FormatBlockedToolResult(toolName string) string {
	return fmt.Sprintf("[Tool %s blocked]: Invoked policy: approvals layer — `agent.approvals.blocked_tools` in roboticus.toml (tool explicitly blocked from execution). Operators remove %q from that list (or update config via the admin API) to allow it.", toolName, toolName)
}

func toolPolicyRulePriority(rule string) int {
	switch rule {
	case "authority":
		return 1
	case "command_safety":
		return 2
	case "financial", "financial_drain":
		return 3
	case "path_protection":
		return 4
	case "rate_limit":
		return 5
	case "validation":
		return 6
	case "config_protection":
		return 7
	default:
		return 0
	}
}

func denialRemediation(toolName string, decision DecisionResult, auth core.AuthorityLevel, claim *core.SecurityClaim) string {
	switch decision.Rule {
	case "authority":
		return authorityDenialRemediation(toolName, decision.Reason, auth, claim)
	case "command_safety":
		return "This tool is classified as forbidden for callers below creator authority. If that is intentional, use an operator or API-key session with creator-level trust, or remove the tool from the exposed roster for this channel."
	case "path_protection":
		return "Arguments touched a protected path pattern, traversal, or an absolute path outside the configured allowlist (when workspace-only mode is on). Operators can adjust `security.allowed_paths` / `security.filesystem` in roboticus.toml, or change the tool arguments to stay within allowed paths."
	case "rate_limit":
		return "This tool hit the configured per-minute rate limit. Wait briefly and retry, or raise the limit in policy engine config if appropriate."
	case "financial":
		return "The requested amount exceeds the configured transfer cap. Operators can adjust financial limits in policy configuration if this cap should be higher."
	case "validation":
		return "The tool arguments failed validation (size or safety heuristics). Retry with smaller or simpler parameters; if this is a false positive, operators may adjust validation limits in policy config."
	case "config_protection":
		return "The call attempted to touch protected configuration. Use supported config APIs or adjust operator-only workflows."
	default:
		return fmt.Sprintf("Rule %q blocked this call. Check Roboticus policy configuration and logs for details.", decision.Rule)
	}
}

func authorityDenialRemediation(toolName, reason string, auth core.AuthorityLevel, claim *core.SecurityClaim) string {
	apiLike := claim != nil && claimHasSource(claim.Sources, core.ClaimSourceAPIKey)
	threatCapped := claim != nil && claim.ThreatDowngraded && auth == core.AuthorityExternal

	var para []string
	para = append(para, fmt.Sprintf("Trust level for this session is %q (tool: %q). This is a local Roboticus gate—not a claim that the remote website or URL is unreachable or blocked.", auth.String(), toolName))

	switch {
	case strings.Contains(reason, "caution tools require"):
		if apiLike {
			para = append(para, "HTTP API sessions normally authenticate at a higher tier; if you still see this, check server logs for a threat/injection flag that capped authority to external.")
		} else {
			para = append(para, "For chats coming through a channel adapter, raise trust by allowlisting the chat, guild, or sender for that platform under `channels.*` (for example allowed_chat_ids / allowed_guild_ids / allowed_numbers), and/or add the sender or chat id to `security.trusted_sender_ids`. The authority granted by allowlist vs trusted entries is controlled by `security.allowlist_authority` and `security.trusted_authority` in roboticus.toml.")
		}
	case strings.Contains(reason, "dangerous tools require"):
		para = append(para, "Dangerous tools need at least self-generated authority (e.g. scheduled or system-originated tasks) or higher. Channel guests cannot invoke them; use a trusted operator path or API session configured for that tier.")
	case strings.Contains(reason, "forbidden tools require"):
		para = append(para, "Forbidden tools are limited to creator-level sessions. Use an operator-facing or API-key session with creator authority, or stop exposing this tool to lower-trust callers.")
	default:
		para = append(para, "Compare the session trust tier to the tool's risk class in the Roboticus docs or tool registry.")
	}

	if threatCapped {
		para = append(para, "Authority may have been capped after injection-defense flagged this turn; reviewing the user content and threat settings can restore the usual tier when appropriate.")
	}

	para = append(para, "When replying, name the invoked policy rule (e.g. authority) verbatim from the lines above; then paraphrase the rest. This was blocked by local trust settings, not by the remote site—unless a fetch tool later returns a real HTTP/network error.")

	return strings.Join(para, " ")
}

func claimHasSource(sources []core.ClaimSource, want core.ClaimSource) bool {
	for _, s := range sources {
		if s == want {
			return true
		}
	}
	return false
}
