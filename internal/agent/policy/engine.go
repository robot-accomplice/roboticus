package policy

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"roboticus/internal/agent/tools"
	"roboticus/internal/core"
)

// DecisionResult holds the outcome of a policy evaluation.
type DecisionResult struct {
	Allowed bool
	Rule    string // which rule denied (empty if allowed)
	Reason  string
}

// Denied returns true if the request was rejected.
func (d DecisionResult) Denied() bool {
	return !d.Allowed
}

// Allow is a convenience for an allowed result.
func Allow() DecisionResult {
	return DecisionResult{Allowed: true}
}

// Deny creates a denial result.
func Deny(rule, reason string) DecisionResult {
	return DecisionResult{Allowed: false, Rule: rule, Reason: reason}
}

// ToolCallRequest represents a tool invocation for policy evaluation.
type ToolCallRequest struct {
	ToolName      string
	Arguments     string // raw JSON
	Authority     core.AuthorityLevel
	SecurityClaim *core.SecurityClaim // optional; when set, rules can inspect the full claim
}

// Rule is the interface for individual policy checks.
type Rule interface {
	Name() string
	Priority() int // lower = evaluated first
	Evaluate(req *ToolCallRequest, reg *tools.Registry) DecisionResult
}

// Engine evaluates tool calls against a chain of rules.
// Rules are evaluated in priority order; first denial wins.
type Engine struct {
	mu    sync.RWMutex
	rules []Rule
}

// NewEngine creates an engine with the default rule chain.
func NewEngine(cfg Config) *Engine {
	rules := []Rule{
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
	return &Engine{rules: rules}
}

// Config controls policy engine thresholds.
type Config struct {
	MaxTransferCents   int64 // max financial transfer in cents (default 10000 = $100)
	RateLimitPerMinute int   // max tool calls per tool per minute (default 30)
	MaxParamBytes      int   // max serialized param size (default 102400)
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		MaxTransferCents:   10000,
		RateLimitPerMinute: 30,
		MaxParamBytes:      102400,
	}
}

// RegisterDynamic inserts a rule in priority-sorted order. Rules with lower
// priority values are evaluated first. Thread-safe.
func (pe *Engine) RegisterDynamic(rule Rule, priority int) {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	// Wrap if the rule's own Priority() disagrees with the explicit priority arg.
	wrapped := &priorityOverrideRule{inner: rule, priority: priority}

	// Find insertion point to keep sorted by priority ascending.
	idx := sort.Search(len(pe.rules), func(i int) bool {
		return pe.rules[i].Priority() > priority
	})

	pe.rules = append(pe.rules, nil)
	copy(pe.rules[idx+1:], pe.rules[idx:])
	pe.rules[idx] = wrapped
}

// Rules returns a snapshot of currently registered rules (for testing/inspection).
func (pe *Engine) Rules() []Rule {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	out := make([]Rule, len(pe.rules))
	copy(out, pe.rules)
	return out
}

// Evaluate checks a tool call against all rules in priority order.
func (pe *Engine) Evaluate(req *ToolCallRequest) DecisionResult {
	pe.mu.RLock()
	rules := make([]Rule, len(pe.rules))
	copy(rules, pe.rules)
	pe.mu.RUnlock()

	for _, rule := range rules {
		result := rule.Evaluate(req, nil)
		if result.Denied() {
			return result
		}
	}
	return Allow()
}

// EvaluateWithTools checks a tool call with access to the tool registry.
func (pe *Engine) EvaluateWithTools(req *ToolCallRequest, reg *tools.Registry) DecisionResult {
	pe.mu.RLock()
	rules := make([]Rule, len(pe.rules))
	copy(rules, pe.rules)
	pe.mu.RUnlock()

	for _, rule := range rules {
		result := rule.Evaluate(req, reg)
		if result.Denied() {
			return result
		}
	}
	return Allow()
}

// --- Rule: Authority Level Gating ---

type authorityRule struct{}

func (r *authorityRule) Name() string  { return "authority" }
func (r *authorityRule) Priority() int { return 1 }

func (r *authorityRule) Evaluate(req *ToolCallRequest, reg *tools.Registry) DecisionResult {
	if reg == nil {
		return Allow()
	}
	tool := reg.Get(req.ToolName)
	if tool == nil {
		return Allow() // unknown tool handled elsewhere
	}

	risk := tool.Risk()
	switch {
	case risk == tools.RiskForbidden && req.Authority != core.AuthorityCreator:
		return Deny("authority", "forbidden tools require creator authority")
	case risk == tools.RiskDangerous && req.Authority < core.AuthoritySelfGenerated:
		return Deny("authority", "dangerous tools require self-generated or higher authority")
	case risk == tools.RiskCaution && req.Authority < core.AuthorityPeer:
		return Deny("authority", "caution tools require peer or higher authority")
	}
	return Allow()
}

// --- Rule: Command Safety (block forbidden tools unconditionally from non-creators) ---

type commandSafetyRule struct{}

func (r *commandSafetyRule) Name() string  { return "command_safety" }
func (r *commandSafetyRule) Priority() int { return 2 }

func (r *commandSafetyRule) Evaluate(req *ToolCallRequest, reg *tools.Registry) DecisionResult {
	if reg == nil {
		return Allow()
	}
	tool := reg.Get(req.ToolName)
	if tool == nil {
		return Allow()
	}
	if tool.Risk() == tools.RiskForbidden {
		return Deny("command_safety", fmt.Sprintf("tool %q is classified as forbidden", req.ToolName))
	}
	return Allow()
}

// --- Rule: Financial Limits ---

type financialRule struct {
	maxAmountCents int64
}

func (r *financialRule) Name() string  { return "financial" }
func (r *financialRule) Priority() int { return 3 }

func (r *financialRule) Evaluate(req *ToolCallRequest, _ *tools.Registry) DecisionResult {
	// Parse arguments looking for amount fields.
	var args map[string]json.RawMessage
	if err := json.Unmarshal([]byte(req.Arguments), &args); err != nil {
		return Allow() // not JSON, can't check
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
			return Deny("financial",
				fmt.Sprintf("amount %.0f cents exceeds limit of %d cents", cents, r.maxAmountCents))
		}
	}
	return Allow()
}

// --- Rule: Path Protection ---

type pathProtectionRule struct{}

func (r *pathProtectionRule) Name() string  { return "path_protection" }
func (r *pathProtectionRule) Priority() int { return 4 }

var protectedPatterns = []string{
	".env", ".ssh", "/etc/", "wallet.json", "roboticus.toml",
	"roboticus.toml", "credentials", "secret", "private_key",
}

func (r *pathProtectionRule) Evaluate(req *ToolCallRequest, _ *tools.Registry) DecisionResult {
	lower := strings.ToLower(req.Arguments)
	for _, pattern := range protectedPatterns {
		if strings.Contains(lower, pattern) {
			return Deny("path_protection",
				fmt.Sprintf("arguments reference protected path pattern %q", pattern))
		}
	}

	// Check for path traversal.
	if strings.Contains(req.Arguments, "..") {
		return Deny("path_protection", "path traversal detected")
	}
	return Allow()
}

// --- Rule: Rate Limiting ---

type rateLimitRule struct {
	mu           sync.Mutex
	maxPerMinute int
	calls        map[string][]time.Time
}

func (r *rateLimitRule) Name() string  { return "rate_limit" }
func (r *rateLimitRule) Priority() int { return 5 }

func (r *rateLimitRule) Evaluate(req *ToolCallRequest, _ *tools.Registry) DecisionResult {
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
		return Deny("rate_limit",
			fmt.Sprintf("tool %q exceeded %d calls/minute", req.ToolName, r.maxPerMinute))
	}

	r.calls[req.ToolName] = append(fresh, now)
	return Allow()
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

func (r *validationRule) Evaluate(req *ToolCallRequest, _ *tools.Registry) DecisionResult {
	// Size limit.
	if len(req.Arguments) > r.maxParamBytes {
		return Deny("validation",
			fmt.Sprintf("serialized params exceed %d bytes", r.maxParamBytes))
	}

	// Shell injection detection.
	for _, pattern := range shellPatterns {
		if strings.Contains(req.Arguments, pattern) {
			return Deny("validation",
				fmt.Sprintf("potential shell injection: %q found in arguments", pattern))
		}
	}

	return Allow()
}

// --- priorityOverrideRule wraps a Rule with an explicit priority ---

type priorityOverrideRule struct {
	inner    Rule
	priority int
}

func (r *priorityOverrideRule) Name() string  { return r.inner.Name() }
func (r *priorityOverrideRule) Priority() int { return r.priority }
func (r *priorityOverrideRule) Evaluate(req *ToolCallRequest, reg *tools.Registry) DecisionResult {
	return r.inner.Evaluate(req, reg)
}

// --- Rule: Financial Drain Detection ---

// drainPatterns are tool-call argument patterns that indicate an attempt to
// drain a wallet or transfer all funds.
var drainPatterns = []string{
	"drain", "withdraw_all", "sweep", "transfer_all",
	"empty_wallet", "max_amount", "send_all",
}

type financialDrainRule struct{}

func (r *financialDrainRule) Name() string  { return "financial_drain" }
func (r *financialDrainRule) Priority() int { return 3 } // same tier as financial rule

// CheckFinancialDrain scans a tool call request for drain/withdraw-all patterns
// in both the tool name and its arguments. Returns a denial if detected.
func CheckFinancialDrain(req *ToolCallRequest) DecisionResult {
	r := &financialDrainRule{}
	return r.Evaluate(req, nil)
}

func (r *financialDrainRule) Evaluate(req *ToolCallRequest, _ *tools.Registry) DecisionResult {
	// Check tool name.
	lowerTool := strings.ToLower(req.ToolName)
	for _, pattern := range drainPatterns {
		if strings.Contains(lowerTool, pattern) {
			return Deny("financial_drain",
				fmt.Sprintf("tool name %q matches drain pattern %q", req.ToolName, pattern))
		}
	}

	// Check arguments (raw JSON string scan).
	lowerArgs := strings.ToLower(req.Arguments)
	for _, pattern := range drainPatterns {
		if strings.Contains(lowerArgs, pattern) {
			return Deny("financial_drain",
				fmt.Sprintf("arguments contain drain pattern %q", pattern))
		}
	}

	// Check for percentage-based drains (100% or "all").
	if strings.Contains(lowerArgs, `"percentage":100`) ||
		strings.Contains(lowerArgs, `"percentage": 100`) ||
		strings.Contains(lowerArgs, `"amount":"all"`) ||
		strings.Contains(lowerArgs, `"amount": "all"`) {
		return Deny("financial_drain", "arguments indicate full-balance drain")
	}

	return Allow()
}
