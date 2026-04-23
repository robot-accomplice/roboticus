package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"roboticus/testutil"
)

func TestPolicyIngestTool_IngestsWithExplicitProvenance(t *testing.T) {
	store := testutil.TempStore(t)
	tool := NewPolicyIngestTool(store, "agent-under-test")
	args := `{
		"category": "policy",
		"key": "refund_window",
		"content": "Customers may return unused items within 30 days.",
		"source_label": "policy/refund-v1",
		"version": 1,
		"effective_date": "2025-01-15",
		"canonical": true,
		"asserter_id": "ops-team"
	}`
	res, err := tool.Execute(context.Background(), args, &Context{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var out policyIngestResult
	if err := json.Unmarshal([]byte(res.Output), &out); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, res.Output)
	}
	if !out.OK {
		t.Fatalf("expected OK=true, got %+v", out)
	}
	if !out.Canonical {
		t.Fatalf("expected canonical=true, got %+v", out)
	}
	if !strings.Contains(out.EffectiveDate, "2025-01-15") {
		t.Fatalf("expected effective_date parsed, got %q", out.EffectiveDate)
	}
	if out.PersistedVersion != 1 {
		t.Fatalf("expected persisted_version=1, got %d", out.PersistedVersion)
	}
}

func TestPolicyIngestTool_BlocksAgentSelfAsserterForCanonical(t *testing.T) {
	store := testutil.TempStore(t)
	tool := NewPolicyIngestTool(store, "agent-under-test")
	args := `{
		"category": "policy",
		"key": "refund_window",
		"content": "Something the agent decided.",
		"source_label": "policy/refund-v1",
		"version": 1,
		"canonical": true,
		"asserter_id": "agent-under-test"
	}`
	res, _ := tool.Execute(context.Background(), args, &Context{})
	var out policyIngestResult
	if err := json.Unmarshal([]byte(res.Output), &out); err != nil {
		t.Fatal(err)
	}
	if out.OK {
		t.Fatalf("expected rejection when agent asserts itself canonical, got ok result %+v", out)
	}
	if !strings.Contains(strings.ToLower(out.Rejection), "not permitted") {
		t.Fatalf("expected 'not permitted' rejection, got %q", out.Rejection)
	}
}

func TestPolicyIngestTool_RejectsSilentOverwrite(t *testing.T) {
	store := testutil.TempStore(t)
	tool := NewPolicyIngestTool(store, "agent-under-test")
	first := `{"category":"policy","key":"k","content":"v1","source_label":"policy/x"}`
	if r, err := tool.Execute(context.Background(), first, &Context{}); err != nil {
		t.Fatalf("first: %v", err)
	} else {
		var out policyIngestResult
		_ = json.Unmarshal([]byte(r.Output), &out)
		if !out.OK {
			t.Fatalf("first ingest should succeed, got %+v", out)
		}
	}
	// Second without replace_prior_version or higher version → reject.
	second := `{"category":"policy","key":"k","content":"v2","source_label":"policy/x"}`
	r, _ := tool.Execute(context.Background(), second, &Context{})
	var out policyIngestResult
	_ = json.Unmarshal([]byte(r.Output), &out)
	if out.OK {
		t.Fatalf("expected silent-overwrite rejection, got %+v", out)
	}
	if !strings.Contains(strings.ToLower(out.Rejection), "row already exists") {
		t.Fatalf("expected 'row already exists' rejection, got %q", out.Rejection)
	}
}

func TestPolicyIngestTool_ReplaceWithExplicitFlagWorks(t *testing.T) {
	store := testutil.TempStore(t)
	tool := NewPolicyIngestTool(store, "agent-under-test")
	_, _ = tool.Execute(context.Background(),
		`{"category":"policy","key":"k","content":"v1","source_label":"policy/x"}`, &Context{})
	r, _ := tool.Execute(context.Background(),
		`{"category":"policy","key":"k","content":"v2","source_label":"policy/x","replace_prior_version":true}`, &Context{})
	var out policyIngestResult
	_ = json.Unmarshal([]byte(r.Output), &out)
	if !out.OK {
		t.Fatalf("expected replace to succeed, got %+v", out)
	}
	if !out.Superseded {
		t.Fatal("expected superseded=true on replace")
	}
	if out.PriorID == "" {
		t.Fatal("expected prior_id to be populated")
	}
}

func TestPolicyIngestTool_NilStoreReturnsFriendlyMessage(t *testing.T) {
	tool := NewPolicyIngestTool(nil, "agent-x")
	r, _ := tool.Execute(context.Background(),
		`{"category":"policy","key":"k","content":"v","source_label":"s"}`, &Context{})
	if !strings.Contains(r.Output, "not available") {
		t.Fatalf("expected friendly unavailability message, got %q", r.Output)
	}
}

func TestPolicyIngestTool_SchemaIsValidJSON(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal(NewPolicyIngestTool(nil, "").ParameterSchema(), &schema); err != nil {
		t.Fatalf("schema not valid JSON: %v", err)
	}
	if schema["type"] != "object" {
		t.Fatalf("expected schema type=object, got %v", schema["type"])
	}
	required, _ := schema["required"].([]any)
	if len(required) != 4 {
		t.Fatalf("expected 4 required fields, got %+v", required)
	}
}

func TestPolicyIngestTool_RejectsMalformedJSON(t *testing.T) {
	tool := NewPolicyIngestTool(testutil.TempStore(t), "agent-x")
	r, _ := tool.Execute(context.Background(), `not-json`, &Context{})
	if !strings.Contains(r.Output, "invalid arguments") {
		t.Fatalf("expected invalid-arguments message, got %q", r.Output)
	}
}

func TestPolicyIngestTool_RiskIsDangerous(t *testing.T) {
	tool := NewPolicyIngestTool(nil, "")
	if tool.Risk() != RiskDangerous {
		t.Fatalf("expected RiskDangerous for a write-to-authority tool, got %s", tool.Risk())
	}
}
