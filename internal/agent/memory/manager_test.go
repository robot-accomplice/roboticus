package memory

import (
	"context"
	"strings"
	"testing"

	"roboticus/internal/db"
	"roboticus/internal/llm"
	"roboticus/testutil"
)

func TestClassifyTurn_ToolUse(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: "Read the file"},
		{Role: "assistant", Content: "I'll read the file."},
		{Role: "tool", Name: "read_file", Content: "file contents here"},
	}
	if got := classifyTurn(msgs); got != TurnToolUse {
		t.Errorf("got %v, want TurnToolUse", got)
	}
}

func TestClassifyTurn_Financial(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: "Please transfer my balance from the wallet to send funds"},
	}
	if got := classifyTurn(msgs); got != TurnFinancial {
		t.Errorf("got %v, want TurnFinancial", got)
	}
}

func TestClassifyTurn_Creative(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: "Create a poem about autumn"},
	}
	if got := classifyTurn(msgs); got != TurnCreative {
		t.Errorf("got %v, want TurnCreative", got)
	}
}

func TestClassifyTurn_Reasoning(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: "What is the meaning of life?"},
	}
	if got := classifyTurn(msgs); got != TurnReasoning {
		t.Errorf("got %v, want TurnReasoning", got)
	}
}

func TestClassifyTurn_EmptyMessages(t *testing.T) {
	if got := classifyTurn(nil); got != TurnReasoning {
		t.Errorf("empty: got %v, want TurnReasoning", got)
	}
}

func TestExtractEntities(t *testing.T) {
	tests := []struct {
		input string
		count int
	}{
		{"Hello @alice and @bob", 2},
		{"@alice @alice @alice", 1}, // dedup
		{"No mentions here", 0},
		{"@user. @other!", 2}, // punctuation stripped
		{"@", 0},              // bare @ ignored
		{"", 0},
	}

	for _, tt := range tests {
		entities := extractEntities(tt.input)
		if len(entities) != tt.count {
			t.Errorf("extractEntities(%q) = %v (len %d), want %d", tt.input, entities, len(entities), tt.count)
		}
	}
}

func TestExtractEntities_ProperNouns(t *testing.T) {
	entities := extractEntities("Can you check with Sarah Chen about it?")
	found := false
	for _, e := range entities {
		if e == "Sarah Chen" || e == "Sarah" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected to find 'Sarah Chen' or 'Sarah', got: %v", entities)
	}
}

func TestExtractEntities_SentenceStart(t *testing.T) {
	// "The" at start and "It" after period should be excluded.
	entities := extractEntities("The server crashed. It was Sarah who fixed it.")
	for _, e := range entities {
		lower := strings.ToLower(e)
		if lower == "the" || lower == "it" {
			t.Errorf("sentence-start word should be excluded, got: %v", entities)
		}
	}
	// Sarah should be found.
	found := false
	for _, e := range entities {
		if e == "Sarah" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'Sarah' in entities, got: %v", entities)
	}
}

func TestExtractEntities_Mixed(t *testing.T) {
	entities := extractEntities("@bob talked to Sarah Chen about the API")
	if len(entities) < 2 {
		t.Errorf("expected at least 2 entities (@bob + Sarah Chen), got %d: %v", len(entities), entities)
	}
}

func TestExtractEntities_AllCapsIgnored(t *testing.T) {
	entities := extractEntities("THE DEPLOYMENT FAILED BECAUSE NGINX CRASHED")
	if len(entities) != 0 {
		t.Errorf("all-caps text should not extract entities, got: %v", entities)
	}
}

func TestExtractEntities_MonthExcluded(t *testing.T) {
	entities := extractEntities("Meeting with Sarah on Monday in January")
	for _, e := range entities {
		lower := strings.ToLower(e)
		if lower == "monday" || lower == "january" {
			t.Errorf("month/day should be excluded, got: %v", entities)
		}
	}
}

func TestExtractEntities_Cap5(t *testing.T) {
	text := "talked to Alice about Bob then met Charlie and Dave with Eve and Frank plus Grace and Heidi"
	entities := extractEntities(text)
	if len(entities) > maxEntitiesPerMessage {
		t.Errorf("expected at most %d entities, got %d: %v", maxEntitiesPerMessage, len(entities), entities)
	}
}

func TestExtractEntities_SentenceStartRepeated(t *testing.T) {
	// "Sarah" appears at sentence start twice — frequency pass should catch it.
	entities := extractEntities("Sarah crashed the server. Later Sarah fixed it.")
	found := false
	for _, e := range entities {
		if strings.ToLower(e) == "sarah" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'Sarah' detected via frequency pass (2 occurrences), got: %v", entities)
	}
}

func TestExtractEntities_SentenceStartSingleton(t *testing.T) {
	// "Alice" appears once at sentence start — should now be detected because
	// "alice" is not in commonNonNameWords or entityExclusions.
	entities := extractEntities("Alice checked the logs.")
	found := false
	for _, e := range entities {
		if strings.ToLower(e) == "alice" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'Alice' detected at sentence start (not a common word), got: %v", entities)
	}
}

func TestExtractEntities_SentenceStartCommonWord(t *testing.T) {
	// "Check" at sentence start IS a common non-name word → should NOT be detected.
	entities := extractEntities("Check the deployment status.")
	for _, e := range entities {
		if strings.ToLower(e) == "check" {
			t.Errorf("common word 'Check' should not be detected as entity, got: %v", entities)
		}
	}
}

func TestExtractEntities_SentenceStartVerb(t *testing.T) {
	// "Running" at sentence start is a common technical word → NOT a name.
	entities := extractEntities("Running the tests now.")
	for _, e := range entities {
		if strings.ToLower(e) == "running" {
			t.Errorf("common word 'Running' should not be detected, got: %v", entities)
		}
	}
}

func TestSemanticKey_StableForSameContent(t *testing.T) {
	content := "the deployment uses Docker containers for isolation"
	k1 := semanticKey(content)
	k2 := semanticKey(content)
	if k1 != k2 {
		t.Errorf("same content should produce same key: %q != %q", k1, k2)
	}
}

func TestSemanticKey_DifferentForRephrase(t *testing.T) {
	k1 := semanticKey("the deployment uses Docker containers")
	k2 := semanticKey("the deployment uses Podman containers")
	if k1 == k2 {
		t.Error("different content should produce different keys")
	}
}

func TestSemanticKey_HumanReadablePrefix(t *testing.T) {
	content := "the deployment pipeline uses a three-stage process for reliability and safety"
	key := semanticKey(content)
	if !strings.HasPrefix(key, "the deployment pipeline") {
		t.Errorf("key should start with content prefix, got: %q", key)
	}
	// Key should contain the hash suffix.
	if !strings.Contains(key, "_") {
		t.Errorf("key should contain hash suffix separated by underscore, got: %q", key)
	}
}

func TestSubjectSimilarity_SameSubject(t *testing.T) {
	sim := subjectSimilarity("the deployment uses Docker containers", "the deployment uses Podman containers")
	if sim < 0.5 {
		t.Errorf("same subject should have high similarity, got %.4f", sim)
	}
}

func TestSubjectSimilarity_DifferentSubject(t *testing.T) {
	sim := subjectSimilarity("the deployment uses Docker containers", "the breakfast includes fresh fruit")
	if sim > 0.3 {
		t.Errorf("different subjects should have low similarity, got %.4f", sim)
	}
}

func TestIsToolFailure(t *testing.T) {
	failures := []string{
		"error: file not found",
		"Error: permission denied",
		"failed: connection refused",
		"fatal: segfault",
		`{"error": "bad request"}`,
		`{"err": "timeout"}`,
	}
	for _, f := range failures {
		if !isToolFailure(f) {
			t.Errorf("isToolFailure(%q) = false, want true", f)
		}
	}

	successes := []string{
		"file contents here",
		"operation completed successfully",
		"[]",
		"{}",
	}
	for _, s := range successes {
		if isToolFailure(s) {
			t.Errorf("isToolFailure(%q) = true, want false", s)
		}
	}
}

func TestExtractKnowledgeFacts(t *testing.T) {
	facts := extractKnowledgeFacts("billing-service", "Billing Service depends on Ledger Service for invoice settlement.")
	if len(facts) == 0 {
		t.Fatal("expected extracted facts")
	}
	found := false
	for _, fact := range facts {
		if fact.Relation == "depends_on" && fact.Subject == "Billing Service" && strings.Contains(fact.Object, "Ledger Service") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected depends_on fact, got %+v", facts)
	}
}

func TestStoreSemanticMemory_ExtractsKnowledgeFacts(t *testing.T) {
	store := testutil.TempStore(t)
	mgr := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	mgr.storeSemanticMemory(ctx, "architecture", "billing-service", "Billing Service depends on Ledger Service for invoice settlement.")

	var count int
	err := store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM knowledge_facts WHERE relation = 'depends_on' AND subject = 'Billing Service'`).Scan(&count)
	if err != nil {
		t.Fatalf("query knowledge_facts: %v", err)
	}
	if count == 0 {
		t.Fatal("expected knowledge fact to be persisted")
	}

	var indexed int
	err = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_index WHERE source_table = 'knowledge_facts'`).Scan(&indexed)
	if err != nil {
		t.Fatalf("query memory_index: %v", err)
	}
	if indexed == 0 {
		t.Fatal("expected knowledge facts to be indexed")
	}
}

func TestKnowledgeFactID_Stable(t *testing.T) {
	id1 := knowledgeFactID("semantic_memory", db.NewID(), "Billing Service", "depends_on", "Ledger Service")
	id2 := knowledgeFactID("semantic_memory", db.NewID(), "Billing Service", "depends_on", "Ledger Service")
	if id1 == id2 {
		t.Fatal("different source IDs should produce different fact IDs")
	}
	id3 := knowledgeFactID("semantic_memory", "sem-1", "Billing Service", "depends_on", "Ledger Service")
	id4 := knowledgeFactID("semantic_memory", "sem-1", "Billing Service", "depends_on", "Ledger Service")
	if id3 != id4 {
		t.Fatal("same inputs should produce same fact ID")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("short: got %q", got)
	}
	if got := truncate("hello world", 5); got != "hello..." {
		t.Errorf("truncated: got %q", got)
	}
	if got := truncate("exact", 5); got != "exact" {
		t.Errorf("exact: got %q", got)
	}
}

func TestSummarizeToolOutput_JSONArray(t *testing.T) {
	got := summarizeToolOutput("web_search", `[{"title":"a"},{"title":"b"},{"title":"c"}]`)
	if got != "web_search: 3 items returned" {
		t.Errorf("array: got %q", got)
	}
}

func TestSummarizeToolOutput_EmptyArray(t *testing.T) {
	got := summarizeToolOutput("query_table", `[]`)
	if got != "query_table: 0 items returned" {
		t.Errorf("empty array: got %q", got)
	}
}

func TestSummarizeToolOutput_JSONError(t *testing.T) {
	got := summarizeToolOutput("query_table", `{"error":"table not found"}`)
	want := "query_table: error — table not found"
	if got != want {
		t.Errorf("error: got %q, want %q", got, want)
	}
}

func TestSummarizeToolOutput_JSONStatus(t *testing.T) {
	got := summarizeToolOutput("deploy", `{"status":"complete","id":"abc123"}`)
	if got != "deploy: status=complete" {
		t.Errorf("status: got %q", got)
	}
}

func TestSummarizeToolOutput_JSONKeys(t *testing.T) {
	got := summarizeToolOutput("api_call", `{"count":5,"name":"test","value":42}`)
	if got != "api_call: {count, name, value}" {
		t.Errorf("keys: got %q", got)
	}
}

func TestSummarizeToolOutput_PlainText(t *testing.T) {
	got := summarizeToolOutput("read_file", "hello world this is plain text")
	if got != "read_file: hello world this is plain text" {
		t.Errorf("plain: got %q", got)
	}
}

func TestSummarizeToolOutput_InvalidJSON(t *testing.T) {
	// Truncated JSON should NOT be stored as-is; fallback to plain truncation.
	content := `{"data": [1,2,3,`
	got := summarizeToolOutput("some_tool", content)
	want := "some_tool: " + content
	if got != want {
		t.Errorf("invalid json: got %q, want %q", got, want)
	}
}

func TestSummarizeToolOutput_LongContent(t *testing.T) {
	// Ensure output is capped at 150 chars.
	longErr := strings.Repeat("x", 200)
	got := summarizeToolOutput("tool", `{"error":"`+longErr+`"}`)
	if len(got) > 150 {
		t.Errorf("too long: len=%d, got %q", len(got), got)
	}
	if !strings.HasPrefix(got, "tool: error — ") {
		t.Errorf("wrong prefix: got %q", got)
	}
}

func TestExtractFirstSentence(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Hello world. More text.", "Hello world"},
		{"Question? Answer.", "Question"},
		{"Short", "Short"},
		{"", ""},
	}
	for _, tt := range tests {
		got := extractFirstSentence(tt.input)
		if got != tt.want {
			t.Errorf("extractFirstSentence(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
