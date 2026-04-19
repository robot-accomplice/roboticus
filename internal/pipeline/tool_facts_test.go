package pipeline

import (
	"context"
	"testing"

	agentmemory "roboticus/internal/agent/memory"
	"roboticus/internal/llm"
	"roboticus/internal/session"
	"roboticus/testutil"
)

// sessionWithToolResult builds a minimal session carrying one assistant
// tool-call message followed by a tool-result message. The final assistant
// content is appended separately so the reference-gate tests can control
// what the response says.
func sessionWithToolResult(t *testing.T, toolName, toolBody, finalResponse string) *session.Session {
	t.Helper()
	sess := session.New("s-tool", "a1", "Bot")
	sess.AddUserMessage("help")
	sess.AddAssistantMessage("using "+toolName, []llm.ToolCall{{
		ID: "c1", Type: "function",
		Function: llm.ToolCallFunc{Name: toolName, Arguments: `{}`},
	}})
	sess.AddToolResult("c1", toolName, toolBody, false)
	if finalResponse != "" {
		sess.AddAssistantMessage(finalResponse, nil)
	}
	return sess
}

func TestExtractToolFacts_RecallMemorySemantic(t *testing.T) {
	body := `{"source_table":"semantic_memory","category":"policy","key":"refund_window","value":"30 days for unused items","confidence":0.85,"id":"abc"}`
	sess := sessionWithToolResult(t, "recall_memory", body, "")
	facts := ExtractToolFacts(sess)
	if len(facts) != 1 {
		t.Fatalf("expected 1 fact, got %+v", facts)
	}
	if facts[0].Subject != "refund_window" {
		t.Fatalf("expected subject=refund_window, got %q", facts[0].Subject)
	}
	if facts[0].Confidence != 0.85 {
		t.Fatalf("expected inherited confidence 0.85, got %f", facts[0].Confidence)
	}
	if facts[0].Source != FactSourceMemoryRecall {
		t.Fatalf("expected memory_recall source, got %s", facts[0].Source)
	}
}

func TestExtractToolFacts_RecallMemoryCapsInheritedConfidence(t *testing.T) {
	body := `{"source_table":"semantic_memory","key":"k","value":"v","confidence":0.99}`
	facts := ExtractToolFacts(sessionWithToolResult(t, "recall_memory", body, ""))
	if len(facts) != 1 || facts[0].Confidence != 0.9 {
		t.Fatalf("expected confidence capped at 0.9, got %+v", facts)
	}
}

func TestExtractToolFacts_RecallMemoryKnowledgeFact(t *testing.T) {
	body := `{"source_table":"knowledge_facts","subject":"Billing","relation":"depends_on","object":"Ledger","confidence":0.8}`
	facts := ExtractToolFacts(sessionWithToolResult(t, "recall_memory", body, ""))
	if len(facts) != 1 {
		t.Fatalf("expected 1 fact, got %+v", facts)
	}
	if facts[0].Subject != "Billing" {
		t.Fatalf("expected subject=Billing, got %q", facts[0].Subject)
	}
}

func TestExtractToolFacts_SearchMemoriesInventoryConfidence(t *testing.T) {
	body := `Found 2 memories matching 'refund':
[{"source_table":"semantic_memory","source_id":"s1","content":"refund policy 30 days","category":"policy"},
 {"source_table":"semantic_memory","source_id":"s2","content":"refund flow overview","category":"policy"}]`
	facts := ExtractToolFacts(sessionWithToolResult(t, "search_memories", body, ""))
	if len(facts) != 2 {
		t.Fatalf("expected 2 facts, got %d", len(facts))
	}
	for _, f := range facts {
		if f.Confidence != 0.65 {
			t.Fatalf("expected inventory confidence 0.65, got %f", f.Confidence)
		}
		if f.Source != FactSourceMemorySearch {
			t.Fatalf("expected memory_search source, got %s", f.Source)
		}
	}
}

func TestExtractToolFacts_ReadFileNarrowKVOnly(t *testing.T) {
	body := `# comment
db_host: prod-01
db_port: 5432
unrelated line without colon
key with space: should-skip
ok_key: ok_value`
	facts := ExtractToolFacts(sessionWithToolResult(t, "read_file", body, ""))
	if len(facts) != 3 {
		t.Fatalf("expected 3 narrow facts, got %+v", facts)
	}
	subjects := map[string]bool{}
	for _, f := range facts {
		subjects[f.Subject] = true
		if f.Confidence != 0.75 {
			t.Fatalf("expected file-read confidence 0.75, got %f", f.Confidence)
		}
	}
	for _, expected := range []string{"db_host", "db_port", "ok_key"} {
		if !subjects[expected] {
			t.Fatalf("expected key %q, got %+v", expected, subjects)
		}
	}
}

func TestExtractToolFacts_ReadFileSkipsGiantBlob(t *testing.T) {
	// Produce a 2kb+ blob with valid-looking key:value lines. Even though
	// some lines match, the overall size disqualifies harvesting.
	lines := make([]byte, 0, 3000)
	for i := 0; i < 120; i++ {
		lines = append(lines, []byte("filler_key: filler_value\n")...)
	}
	facts := ExtractToolFacts(sessionWithToolResult(t, "read_file", string(lines), ""))
	if len(facts) != 0 {
		t.Fatalf("expected giant blob to yield no facts, got %+v", facts)
	}
}

func TestExtractToolFacts_GraphLookupPath(t *testing.T) {
	body := `{"summary":"path","found":true,"hops":[
		{"from":"Billing","to":"Ledger","relation":"depends_on","confidence":0.9},
		{"from":"Ledger","to":"Postgres","relation":"depends_on","confidence":0.85}
	]}`
	facts := ExtractToolFacts(sessionWithToolResult(t, "query_knowledge_graph", body, ""))
	if len(facts) != 2 {
		t.Fatalf("expected 2 hop facts, got %+v", facts)
	}
	for _, f := range facts {
		if f.Confidence != 0.75 {
			t.Fatalf("expected graph-lookup confidence 0.75, got %f", f.Confidence)
		}
		if f.Source != FactSourceGraphLookup {
			t.Fatalf("expected graph_lookup source, got %s", f.Source)
		}
	}
}

func TestExtractToolFacts_WorkflowFindIsInventory(t *testing.T) {
	body := `{"matches":[{"name":"canary-release","version":2,"confidence":0.9,"steps":["drain","flip"],"context_tags":["release"]}]}`
	facts := ExtractToolFacts(sessionWithToolResult(t, "find_workflow", body, ""))
	if len(facts) != 1 || facts[0].Confidence != 0.65 {
		t.Fatalf("expected 0.65 inventory confidence on find, got %+v", facts)
	}
}

func TestExtractToolFacts_WorkflowGetInheritsConfidence(t *testing.T) {
	body := `{"found":true,"workflow":{"name":"canary-release","version":2,"confidence":0.88,"steps":["drain","flip"],"context_tags":["release"]}}`
	facts := ExtractToolFacts(sessionWithToolResult(t, "find_workflow", body, ""))
	if len(facts) != 1 {
		t.Fatalf("expected 1 fact, got %+v", facts)
	}
	if facts[0].Confidence != 0.88 {
		t.Fatalf("expected inherited confidence 0.88, got %f", facts[0].Confidence)
	}
}

func TestExtractToolFacts_SkipsFailureOutput(t *testing.T) {
	body := `Error: file not found`
	facts := ExtractToolFacts(sessionWithToolResult(t, "read_file", body, ""))
	if len(facts) != 0 {
		t.Fatalf("expected failure body to be skipped, got %+v", facts)
	}
}

func TestExtractToolFacts_IgnoresNonAllowlistedTools(t *testing.T) {
	body := `{"something":"useful"}`
	facts := ExtractToolFacts(sessionWithToolResult(t, "bash", body, ""))
	if len(facts) != 0 {
		t.Fatalf("expected non-allowlisted tool to yield nothing, got %+v", facts)
	}
}

func TestFilterFactsReferencedByResponse_RequiresKeywordMatch(t *testing.T) {
	facts := []ToolFact{
		{Subject: "refund_window", Value: "30 days", Keywords: []string{"refund", "days"}},
		{Subject: "deploy_tool", Value: "kubectl", Keywords: []string{"deploy", "kubectl"}},
	}
	// Response only references the refund fact — deploy fact should be gated.
	kept := FilterFactsReferencedByResponse(facts, "The refund window is currently 30 days for unused items.")
	if len(kept) != 1 || kept[0].Subject != "refund_window" {
		t.Fatalf("expected only refund fact to pass the reference gate, got %+v", kept)
	}
}

func TestFilterFactsReferencedByResponse_RequiresTwoMatchesOnRichFacts(t *testing.T) {
	// Rich fact with 3+ keywords requires 2 matches — a single coincidental
	// match is not enough to count as "referenced".
	facts := []ToolFact{
		{Value: "Billing --depends_on--> Ledger", Keywords: []string{"billing", "ledger", "depends"}},
	}
	// Single keyword match: "billing" only.
	kept := FilterFactsReferencedByResponse(facts, "Billing had a bump last quarter.")
	if len(kept) != 0 {
		t.Fatalf("expected single match to be below the 2-of-3 threshold, got %+v", kept)
	}
	// Two keyword matches should pass.
	kept = FilterFactsReferencedByResponse(facts, "Billing depends on the ledger service.")
	if len(kept) != 1 {
		t.Fatalf("expected 2-of-3 match to pass the gate, got %+v", kept)
	}
}

func TestFilterFactsReferencedByResponse_EmptyResponseKeepsNothing(t *testing.T) {
	facts := []ToolFact{{Keywords: []string{"anything"}}}
	if len(FilterFactsReferencedByResponse(facts, "")) != 0 {
		t.Fatal("expected empty response to gate out everything")
	}
}

// --- End-to-end wiring ------------------------------------------------------

func TestGrowExecutiveState_RecordsReferencedToolFacts(t *testing.T) {
	store := testutil.TempStore(t)
	p := &Pipeline{store: store}
	mm := agentmemory.NewManager(agentmemory.DefaultConfig(), store)
	ctx := context.Background()

	if err := mm.RecordPlan(ctx, "s-tool", "t1", "resolve refund question", agentmemory.PlanPayload{
		Subgoals: []string{"find the refund window"},
	}); err != nil {
		t.Fatal(err)
	}

	sess := session.New("s-tool", "a1", "Bot")
	sess.AddUserMessage("What is the refund window?")
	sess.SetTaskVerificationHints("question", "simple", "execute_directly", []string{"find the refund window"})
	sess.SetMemoryContext("[Active Memory]\n\n[Retrieved Evidence]\n1. [semantic, 0.9] refund policy\n")

	// Tool call returns a semantic_memory recall with inherited confidence.
	sess.AddAssistantMessage("let me check", []llm.ToolCall{{
		ID: "c1", Type: "function",
		Function: llm.ToolCallFunc{Name: "recall_memory", Arguments: `{}`},
	}})
	sess.AddToolResult("c1", "recall_memory",
		`{"source_table":"semantic_memory","key":"refund_window","value":"30 days","confidence":0.85}`, false)

	finalAnswer := "The refund window is 30 days based on the refund_window policy entry."
	result := p.growExecutiveState(ctx, sess, finalAnswer)
	if result.AssumptionsRecorded == 0 {
		t.Fatalf("expected at least one assumption recorded, got %+v", result)
	}

	// Verify the assumption actually carries the inherited confidence.
	state, err := mm.LoadExecutiveState(ctx, "s-tool", "t1")
	if err != nil {
		t.Fatal(err)
	}
	var found *agentmemory.ExecutiveEntry
	for i := range state.Assumptions {
		if src, _ := state.Assumptions[i].Payload["source"].(string); src == string(FactSourceMemoryRecall) {
			found = &state.Assumptions[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected a memory_recall-sourced assumption, got %+v", state.Assumptions)
	}
	conf, _ := found.Payload["confidence"].(float64)
	if conf != 0.85 {
		t.Fatalf("expected inherited confidence 0.85 persisted, got %f", conf)
	}
}

func TestGrowExecutiveState_SkipsUnreferencedToolFacts(t *testing.T) {
	store := testutil.TempStore(t)
	p := &Pipeline{store: store}
	mm := agentmemory.NewManager(agentmemory.DefaultConfig(), store)
	ctx := context.Background()

	if err := mm.RecordPlan(ctx, "s-skip", "t1", "unrelated plan", agentmemory.PlanPayload{
		Subgoals: []string{"do something unrelated"},
	}); err != nil {
		t.Fatal(err)
	}

	sess := session.New("s-skip", "a1", "Bot")
	sess.AddUserMessage("do something unrelated")
	sess.SetTaskVerificationHints("task", "simple", "execute_directly", []string{"do something unrelated"})
	sess.AddAssistantMessage("checking", []llm.ToolCall{{
		ID: "c1", Type: "function",
		Function: llm.ToolCallFunc{Name: "recall_memory", Arguments: `{}`},
	}})
	sess.AddToolResult("c1", "recall_memory",
		`{"source_table":"semantic_memory","key":"deployment_window","value":"Tuesday mornings","confidence":0.9}`, false)

	// Response does NOT reference the recalled fact — the reference gate
	// should drop it so it never lands as an assumption.
	finalAnswer := "I completed the unrelated action you requested."
	result := p.growExecutiveState(ctx, sess, finalAnswer)

	state, _ := mm.LoadExecutiveState(ctx, "s-skip", "t1")
	for _, a := range state.Assumptions {
		if src, _ := a.Payload["source"].(string); src == string(FactSourceMemoryRecall) {
			t.Fatalf("expected unreferenced tool fact to be gated out, got %+v (result=%+v)", a, result)
		}
	}
}
