package pipeline

import "testing"

func TestClassifyDelegationError_PolicyDenied(t *testing.T) {
	result := ClassifyDelegationError("Policy denied: max_tasks: limit exceeded")
	if result.Kind != DelegErrPolicyDenied {
		t.Errorf("expected PolicyDenied, got %v", result.Kind)
	}
	if result.Rule != "max_tasks" {
		t.Errorf("rule = %q, want max_tasks", result.Rule)
	}
	if result.Reason != "limit exceeded" {
		t.Errorf("reason = %q, want 'limit exceeded'", result.Reason)
	}
}

func TestClassifyDelegationError_Timeout(t *testing.T) {
	result := ClassifyDelegationError("operation timed out after 30000ms")
	if result.Kind != DelegErrTimeout {
		t.Errorf("expected Timeout, got %v", result.Kind)
	}
	if result.DurationMs != 30000 {
		t.Errorf("duration = %d, want 30000", result.DurationMs)
	}
}

func TestClassifyDelegationError_Unavailable(t *testing.T) {
	result := ClassifyDelegationError("subagent 'analyst' not running")
	if result.Kind != DelegErrSubagentUnavailable {
		t.Errorf("expected SubagentUnavailable, got %v", result.Kind)
	}
	if result.Name != "analyst" {
		t.Errorf("name = %q, want analyst", result.Name)
	}
}

func TestClassifyDelegationError_Default(t *testing.T) {
	result := ClassifyDelegationError("some unknown error occurred")
	if result.Kind != DelegErrLLMCallFailed {
		t.Errorf("expected LLMCallFailed, got %v", result.Kind)
	}
}

func TestDelegationErrorKind_String(t *testing.T) {
	tests := []struct {
		kind DelegationErrorKind
		want string
	}{
		{DelegErrPolicyDenied, "policy_denied"},
		{DelegErrTimeout, "timeout"},
		{DelegErrSubagentUnavailable, "subagent_unavailable"},
		{DelegErrLLMCallFailed, "llm_call_failed"},
	}
	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
}
