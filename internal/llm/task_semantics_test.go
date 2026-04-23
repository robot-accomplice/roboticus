package llm

import "testing"

func TestRequestIntentClass_ToolBearingRequestUsesToolUse(t *testing.T) {
	req := &Request{
		TaskIntent: "question",
		Tools: []ToolDef{
			{Function: ToolFuncDef{Name: "list_tools"}},
		},
	}
	if got := requestIntentClass(req); got != IntentToolUse.String() {
		t.Fatalf("requestIntentClass(tool-bearing question) = %q, want %q", got, IntentToolUse.String())
	}
}

func TestRequestIntentClass_ToolBearingOverridesExplicitExecutionIntent(t *testing.T) {
	req := &Request{
		IntentClass: "EXECUTION",
		TaskIntent:  "question",
		Tools: []ToolDef{
			{Function: ToolFuncDef{Name: "list_tools"}},
		},
	}
	if got := requestIntentClass(req); got != IntentToolUse.String() {
		t.Fatalf("requestIntentClass(tool-bearing explicit EXECUTION) = %q, want %q", got, IntentToolUse.String())
	}
}

func TestRequestRoutingIntent_ToolBearingQuestionUsesTaskWeights(t *testing.T) {
	req := &Request{
		TaskIntent: "question",
		Tools: []ToolDef{
			{Function: ToolFuncDef{Name: "list_tools"}},
		},
	}
	if got := requestRoutingIntent(req); got != "task" {
		t.Fatalf("requestRoutingIntent(tool-bearing question) = %q, want %q", got, "task")
	}
}
