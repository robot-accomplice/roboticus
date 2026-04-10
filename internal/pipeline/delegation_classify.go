// Delegation error classification.
//
// Classifies opaque delegation error strings into typed DelegationError
// variants for structured error handling and retry decisions.
//
// Ported from Rust: crates/roboticus-pipeline/src/delegation_classify.rs

package pipeline

import (
	"strconv"
	"strings"
)

// DelegationErrorKind classifies delegation failures.
type DelegationErrorKind int

const (
	// DelegErrPolicyDenied — policy engine blocked the delegation.
	DelegErrPolicyDenied DelegationErrorKind = iota + 1
	// DelegErrTimeout — delegation timed out.
	DelegErrTimeout
	// DelegErrSubagentUnavailable — target subagent not running/found.
	DelegErrSubagentUnavailable
	// DelegErrLLMCallFailed — LLM inference failed during delegation.
	DelegErrLLMCallFailed
)

func (k DelegationErrorKind) String() string {
	switch k {
	case DelegErrPolicyDenied:
		return "policy_denied"
	case DelegErrTimeout:
		return "timeout"
	case DelegErrSubagentUnavailable:
		return "subagent_unavailable"
	case DelegErrLLMCallFailed:
		return "llm_call_failed"
	default:
		return "unknown"
	}
}

// DelegationError is a typed delegation failure with extracted metadata.
type DelegationError struct {
	Kind       DelegationErrorKind
	Rule       string // for PolicyDenied
	Reason     string // human-readable reason
	DurationMs int64  // for Timeout
	Name       string // for SubagentUnavailable
	State      string // for SubagentUnavailable
	Provider   string // for LLMCallFailed
	Model      string // for LLMCallFailed
}

// ClassifyDelegationError classifies an opaque error string into a typed DelegationError.
func ClassifyDelegationError(errMsg string) DelegationError {
	lower := strings.ToLower(errMsg)

	// Policy denied.
	if strings.Contains(lower, "policy denied") || strings.Contains(lower, "not allowed") {
		parts := strings.SplitN(errMsg, ":", 3)
		rule := "unknown"
		reason := errMsg
		if len(parts) >= 2 {
			rule = strings.TrimSpace(parts[1])
		}
		if len(parts) >= 3 {
			reason = strings.TrimSpace(parts[2])
		}
		return DelegationError{Kind: DelegErrPolicyDenied, Rule: rule, Reason: reason}
	}

	// Timeout.
	if strings.Contains(lower, "timeout") || strings.Contains(lower, "timed out") {
		var durationMs int64
		for _, word := range strings.FieldsFunc(errMsg, func(c rune) bool { return c < '0' || c > '9' }) {
			if v, err := strconv.ParseInt(word, 10, 64); err == nil {
				durationMs = v
				break
			}
		}
		return DelegationError{Kind: DelegErrTimeout, DurationMs: durationMs, Reason: errMsg}
	}

	// Subagent unavailable.
	if strings.Contains(lower, "unavailable") || strings.Contains(lower, "not found") ||
		strings.Contains(lower, "not running") || strings.Contains(lower, "stopped") {
		name := "unknown"
		// Extract name from quotes: subagent 'analyst' not running
		if idx := strings.Index(errMsg, "'"); idx >= 0 {
			rest := errMsg[idx+1:]
			if end := strings.Index(rest, "'"); end >= 0 {
				name = rest[:end]
			}
		}
		return DelegationError{Kind: DelegErrSubagentUnavailable, Name: name, State: "unavailable", Reason: errMsg}
	}

	// Default: LLM call failed.
	return DelegationError{Kind: DelegErrLLMCallFailed, Provider: "unknown", Model: "unknown", Reason: errMsg}
}
