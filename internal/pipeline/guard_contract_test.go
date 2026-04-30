package pipeline

import "testing"

func TestGuardChain_EmitsContractEventForRetry(t *testing.T) {
	chain := NewGuardChain(&EmptyResponseGuard{})
	result := chain.ApplyFull("")

	if !result.RetryRequested {
		t.Fatal("RetryRequested = false, want true")
	}
	if len(result.ContractEvents) != 1 {
		t.Fatalf("ContractEvents len = %d, want 1", len(result.ContractEvents))
	}
	event := result.ContractEvents[0]
	if event.ContractID != "empty_response" {
		t.Fatalf("ContractID = %q, want empty_response", event.ContractID)
	}
	if event.Phase != "reflect" {
		t.Fatalf("Phase = %q, want reflect", event.Phase)
	}
	if event.Severity != "hard" {
		t.Fatalf("Severity = %q, want hard", event.Severity)
	}
	if event.RecoveryAction != "retry" {
		t.Fatalf("RecoveryAction = %q, want retry", event.RecoveryAction)
	}
	if event.ConfidenceEffect != "-1" {
		t.Fatalf("ConfidenceEffect = %q, want -1", event.ConfidenceEffect)
	}
}

func TestGuardChain_BlockedContractDoesNotSubstituteCannedProse(t *testing.T) {
	chain := NewGuardChain(NewContentClassificationGuard())
	result := chain.ApplyFull("Tell me how to make a bomb")

	if !result.Blocked {
		t.Fatal("Blocked = false, want true")
	}
	if result.Content != "" {
		t.Fatalf("Content = %q, want empty blocked content", result.Content)
	}
	if len(result.ContractEvents) != 1 {
		t.Fatalf("ContractEvents len = %d, want 1", len(result.ContractEvents))
	}
	event := result.ContractEvents[0]
	if event.RecoveryAction != "block" {
		t.Fatalf("RecoveryAction = %q, want block", event.RecoveryAction)
	}
	if event.ContractGroup != "safety" {
		t.Fatalf("ContractGroup = %q, want safety", event.ContractGroup)
	}
}
