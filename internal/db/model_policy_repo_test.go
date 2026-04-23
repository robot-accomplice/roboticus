package db

import (
	"context"
	"testing"
)

func TestModelPolicyRepo_UpsertListDelete(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	err := UpsertModelPolicy(ctx, store, ModelPolicyRow{
		Model:             "ollama/qwen2.5:32b",
		State:             "disabled",
		PrimaryReasonCode: "latency_nonviable",
		ReasonCodes:       []string{"latency_nonviable", "provider_instability"},
		HumanReason:       "Disable on this hardware due to extreme latency and instability.",
		EvidenceRefs:      []string{"baseline:638104f760e5cdc6ec59bb069dbafbe0"},
		Source:            "operator_policy",
	})
	if err != nil {
		t.Fatalf("UpsertModelPolicy: %v", err)
	}

	rows := ListModelPolicies(ctx, store)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.Model != "ollama/qwen2.5:32b" {
		t.Fatalf("model = %q", row.Model)
	}
	if row.State != "disabled" {
		t.Fatalf("state = %q", row.State)
	}
	if row.PrimaryReasonCode != "latency_nonviable" {
		t.Fatalf("primary reason = %q", row.PrimaryReasonCode)
	}
	if len(row.ReasonCodes) != 2 {
		t.Fatalf("reason codes = %v", row.ReasonCodes)
	}
	if len(row.EvidenceRefs) != 1 {
		t.Fatalf("evidence refs = %v", row.EvidenceRefs)
	}

	if err := DeleteModelPolicy(ctx, store, "ollama/qwen2.5:32b"); err != nil {
		t.Fatalf("DeleteModelPolicy: %v", err)
	}
	rows = ListModelPolicies(ctx, store)
	if len(rows) != 0 {
		t.Fatalf("len(rows) after delete = %d, want 0", len(rows))
	}
}
