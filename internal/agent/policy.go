package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"goboticus/internal/agent/tools"
	"goboticus/internal/core"
)

// PolicyDecisionResult holds the outcome of a policy evaluation.
type PolicyDecisionResult struct {
	Allowed bool
	Rule    string // which rule denied (empty if allowed)
	Reason  string
}

// Denied returns true if the request was rejected.
func (d PolicyDecisionResult) Denied() bool {
	return !d.Allowed
}

// PolicyAllow is a convenience for an allowed result.
func PolicyAllow() PolicyDecisionResult {
	return PolicyDecisionResult{Allowed: true}
}

// PolicyDeny creates a denial result.
func PolicyDeny(rule, reason string) PolicyDecisionResult {
	return PolicyDecisionResult{Allowed: false, Rule: rule, Reason: reason}
}

// ToolCallRequest represents a tool invocation for policy evaluation.
type ToolCallRequest struct {
	ToolName  string
	Arguments string // raw JSON
	Authority core.AuthorityLevel
}

// PolicyRule is the interface for individual policy checks.
type PolicyRule interface {
	Name() string
	Priority() int // lower = evaluated first
	Evaluate(req *ToolCallRequest, tools *ToolRegistry) PolicyDecisionResult
}

// PolicyEngine evaluates tool calls against a chain of rules.
// Rules are evaluated in priority order; first denial wins.
type PolicyEngine struct {
	rules []PolicyRule
}

// NewPolicyEngine creates an engine with the default rule chain.
func NewPolicyEngine(cfg PolicyConfig) *PolicyEngine {
	rules := []PolicyRule{
		&authorityRule{},
		&commandSafetyRule{},
		&financialRule{maxAmountCents: cfg.MaxTransferCents},
		&pathProtectionRule{},
		&rateLimitRule{
			maxPerMinute: cfg.RateLimitPerMinute,
			calls:        make(map[string][]time.Time),
		},
		&validationRule{maxParamBytes: cfg.MaxParamBytes},
	}
	return &PolicyEngine{rules: rules}
}

// PolicyConfig controls policy engine thresholds.
type PolicyConfig struct {
	MaxTransferCents   int64 // max financial transfer in cents (default 10000 = $100)
	RateLimitPerMinute int   // max tool calls per tool per minute (default 30)
	MaxParamBytes      int   // max serialized param size (default 102400)
}

// DefaultPolicyConfig returns sensible defaults.
func DefaultPolicyConfig() PolicyConfig {
	return PolicyConfig{
		MaxTransferCents:   10000,
		RateLimitPerMinute: 30,
		MaxParamBytes:      102400,
	}
}

// Evaluate checks a tool call against all rules in priority order.
func (pe *PolicyEngine) Evaluate(req *ToolCallRequest) PolicyDecisionResult {
	for _, rule := range pe.rules {
		result := rule.Evaluate(req, nil)
		if result.Denied() {
			return result
		}
	}
	return PolicyAllow()
}

// EvaluateWithTools checks a tool call with access to the tool registry.
func (pe *PolicyEngine) EvaluateWithTools(req *ToolCallRequest, reg *ToolRegistry) PolicyDecisionResult {
	for _, rule := range pe.rules {
		result := rule.Evaluate(req, reg)
		if result.Denied() {
			return result
		}
	}
	return PolicyAllow()
}

// --- Rule: Authority Level Gating ---

type authorityRule struct{}

func (r *authorityRule) Name() string  { return "authority" }
func (r *authorityRule) Priority() int { return 1 }

func (r *authorityRule) Evaluate(req *ToolCallRequest, reg *ToolRegistry) PolicyDecisionResult {
	if reg == nil {
		return PolicyAllow()
	}
	tool := reg.Get(req.ToolName)
	if tool == nil {
		return PolicyAllow() // unknown tool handled elsewhere
	}

	risk := tool.Risk()
	switch {
	case risk == tools.RiskForbidden && req.Authority != core.AuthorityCreator:
		return PolicyDeny("authority", "forbidden tools require creator authority")
	case risk == tools.RiskDangerous && req.Authority < core.AuthoritySelfGenerated:
		return PolicyDeny("authority", "dangerous tools require self-generated or higher authority")
	case risk == tools.RiskCaution && req.Authority < core.AuthorityPeer:
		return PolicyDeny("authority", "caution tools require peer or higher authority")
	}
	return PolicyAllow()
}

// --- Rule: Command Safety (block forbidden tools unconditionally from non-creators) ---

type commandSafetyRule struct{}

func (r *commandSafetyRule) Name() string  { return "command_safety" }
func (r *commandSafetyRule) Priority() int { return 2 }

func (r *commandSafetyRule) Evaluate(req *ToolCallRequest, reg *ToolRegistry) PolicyDecisionResult {
	if reg == nil {
		return PolicyAllow()
	}
	tool := reg.Get(req.ToolName)
	if tool == nil {
		return PolicyAllow()
	}
	if tool.Risk() == tools.RiskForbidden {
		return PolicyDeny("command_safety", fmt.Sprintf("tool %q is classified as forbidden", req.ToolName))
	}
	return PolicyAllow()
}

// --- Rule: Financial Limits ---

type financialRule struct {
	maxAmountCents int64
}

func (r *financialRule) Name() string  { return "financial" }
func (r *financialRule) Priority() int { return 3 }

func (r *financialRule) Evaluate(req *ToolCallRequest, _ *ToolRegistry) PolicyDecisionResult {
	// Parse arguments looking for amount fields.
	var args map[string]json.RawMessage
	if err := json.Unmarshal([]byte(req.Arguments), &args); err != nil {
		return PolicyAllow() // not JSON, can't check
	}

	amountKeys := []string{"amount_cents", "amount_dollars", "amount", "dollars", "value"}
	for _, key := range amountKeys {
		raw, ok := args[key]
		if !ok {
			continue
		}
		var amount float64
		if err := json.Unmarshal(raw, &amount); err != nil {
			continue
		}

		// Convert dollars to cents if needed.
		cents := amount
		if key == "amount_dollars" || key == "dollars" {
			cents = amount * 100
		}

		if int64(cents) > r.maxAmountCents {
			return PolicyDeny("financial",
				fmt.Sprintf("amount %.0f cents exceeds limit of %d cents", cents, r.maxAmountCents))
		}
	}
	return PolicyAllow()
}

// --- Rule: Path Protection ---

type pathProtectionRule struct{}

func (r *pathProtectionRule) Name() string  { return "path_protection" }
func (r *pathProtectionRule) Priority() int { return 4 }

var protectedPatterns = []string{
	".env", ".ssh", "/etc/", "wallet.json", "roboticus.toml",
	"goboticus.toml", "credentials", "secret", "private_key",
}

func (r *pathProtectionRule) Evaluate(req *ToolCallRequest, _ *ToolRegistry) PolicyDecisionResult {
	lower := strings.ToLower(req.Arguments)
	for _, pattern := range protectedPatterns {
		if strings.Contains(lower, pattern) {
			return PolicyDeny("path_protection",
				fmt.Sprintf("arguments reference protected path pattern %q", pattern))
		}
	}

	// Check for path traversal.
	if strings.Contains(req.Arguments, "..") {
		return PolicyDeny("path_protection", "path traversal detected")
	}
	return PolicyAllow()
}

// --- Rule: Rate Limiting ---

type rateLimitRule struct {
	mu           sync.Mutex
	maxPerMinute int
	calls        map[string][]time.Time
}

func (r *rateLimitRule) Name() string  { return "rate_limit" }
func (r *rateLimitRule) Priority() int { return 5 }

func (r *rateLimitRule) Evaluate(req *ToolCallRequest, _ *ToolRegistry) PolicyDecisionResult {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-time.Minute)

	// Prune old entries.
	times := r.calls[req.ToolName]
	fresh := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			fresh = append(fresh, t)
		}
	}

	if len(fresh) >= r.maxPerMinute {
		return PolicyDeny("rate_limit",
			fmt.Sprintf("tool %q exceeded %d calls/minute", req.ToolName, r.maxPerMinute))
	}

	r.calls[req.ToolName] = append(fresh, now)
	return PolicyAllow()
}

// --- Rule: Validation (injection detection in params) ---

type validationRule struct {
	maxParamBytes int
}

func (r *validationRule) Name() string  { return "validation" }
func (r *validationRule) Priority() int { return 6 }

// Shell injection patterns — covers command substitution, chaining, piping, and redirection.
var shellPatterns = []string{
	"$(", "`", "${", // command substitution
	";", "&&", "||", // command chaining
	"|",                  // pipe
	">", ">>", "<", "<<", // redirection
	"\n", // newline injection
}

func (r *validationRule) Evaluate(req *ToolCallRequest, _ *ToolRegistry) PolicyDecisionResult {
	// Size limit.
	if len(req.Arguments) > r.maxParamBytes {
		return PolicyDeny("validation",
			fmt.Sprintf("serialized params exceed %d bytes", r.maxParamBytes))
	}

	// Shell injection detection.
	for _, pattern := range shellPatterns {
		if strings.Contains(req.Arguments, pattern) {
			return PolicyDeny("validation",
				fmt.Sprintf("potential shell injection: %q found in arguments", pattern))
		}
	}

	return PolicyAllow()
}
