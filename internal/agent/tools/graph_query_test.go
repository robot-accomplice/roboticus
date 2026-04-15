package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"roboticus/internal/db"
	"roboticus/testutil"
)

func seedGraph(t *testing.T, store *db.Store, rows [][4]string) {
	t.Helper()
	ctx := context.Background()
	for i, row := range rows {
		id := row[0]
		if id == "" {
			id = db.NewID()
		}
		if _, err := store.ExecContext(ctx,
			`INSERT INTO knowledge_facts (id, subject, relation, object, source_table, source_id, confidence)
			 VALUES (?, ?, ?, ?, 'test', ?, 0.9)`,
			id, row[1], row[2], row[3], id,
		); err != nil {
			t.Fatalf("seed row %d: %v", i, err)
		}
	}
}

func TestGraphQueryTool_Path_ReturnsMultiHopChain(t *testing.T) {
	store := testutil.TempStore(t)
	seedGraph(t, store, [][4]string{
		{"f1", "Billing", "depends_on", "Ledger"},
		{"f2", "Ledger", "depends_on", "Postgres"},
		{"f3", "Postgres", "version_of", "Postgres 15"},
	})

	tool := NewGraphQueryTool(store)
	res, err := tool.Execute(context.Background(),
		`{"operation": "path", "from": "Billing", "to": "Postgres", "max_depth": 4}`,
		&Context{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var out graphPathResult
	if err := json.Unmarshal([]byte(res.Output), &out); err != nil {
		t.Fatalf("unmarshal result: %v\n%s", err, res.Output)
	}
	if !out.Found {
		t.Fatalf("expected path found, got %+v", out)
	}
	if out.Depth != 2 {
		t.Fatalf("expected 2-hop path, got %d (%+v)", out.Depth, out.Hops)
	}
	if !strings.EqualFold(out.Hops[len(out.Hops)-1].To, "Postgres") {
		t.Fatalf("expected final hop at Postgres, got %+v", out.Hops)
	}
}

func TestGraphQueryTool_Path_NoPath(t *testing.T) {
	store := testutil.TempStore(t)
	seedGraph(t, store, [][4]string{
		{"f1", "A", "depends_on", "B"},
	})

	tool := NewGraphQueryTool(store)
	res, err := tool.Execute(context.Background(),
		`{"operation": "path", "from": "A", "to": "C", "max_depth": 3}`,
		&Context{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var out graphPathResult
	if err := json.Unmarshal([]byte(res.Output), &out); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, res.Output)
	}
	if out.Found {
		t.Fatalf("expected Found=false, got %+v", out)
	}
	if !strings.Contains(strings.ToLower(out.Summary), "no path") {
		t.Fatalf("expected 'no path' in summary, got %q", out.Summary)
	}
}

func TestGraphQueryTool_Impact_WalksReverseAdjacency(t *testing.T) {
	store := testutil.TempStore(t)
	seedGraph(t, store, [][4]string{
		{"f1", "A", "depends_on", "B"},
		{"f2", "B", "depends_on", "C"},
		{"f3", "X", "depends_on", "B"},
	})

	tool := NewGraphQueryTool(store)
	res, err := tool.Execute(context.Background(),
		`{"operation": "impact", "seed": "C", "max_depth": 3}`,
		&Context{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var out graphTraversalResult
	if err := json.Unmarshal([]byte(res.Output), &out); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, res.Output)
	}
	names := make(map[string]bool)
	for _, node := range out.Nodes {
		names[strings.ToLower(node.Name)] = true
	}
	for _, expected := range []string{"b", "a", "x"} {
		if !names[expected] {
			t.Fatalf("expected %s in impact of C, got %+v", expected, out.Nodes)
		}
	}
	if out.Mode != "impact" {
		t.Fatalf("expected mode=impact, got %q", out.Mode)
	}
}

func TestGraphQueryTool_Dependencies_WalksForwardAdjacency(t *testing.T) {
	store := testutil.TempStore(t)
	seedGraph(t, store, [][4]string{
		{"f1", "A", "depends_on", "B"},
		{"f2", "B", "depends_on", "C"},
		{"f3", "C", "depends_on", "D"},
	})

	tool := NewGraphQueryTool(store)
	res, err := tool.Execute(context.Background(),
		`{"operation": "dependencies", "seed": "A", "max_depth": 3}`,
		&Context{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var out graphTraversalResult
	if err := json.Unmarshal([]byte(res.Output), &out); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, res.Output)
	}
	names := make(map[string]bool)
	for _, node := range out.Nodes {
		names[strings.ToLower(node.Name)] = true
	}
	for _, expected := range []string{"b", "c", "d"} {
		if !names[expected] {
			t.Fatalf("expected %s in dependencies of A, got %+v", expected, out.Nodes)
		}
	}
	if out.Mode != "dependencies" {
		t.Fatalf("expected mode=dependencies, got %q", out.Mode)
	}
}

func TestGraphQueryTool_RejectsUnknownOperation(t *testing.T) {
	store := testutil.TempStore(t)
	tool := NewGraphQueryTool(store)
	res, err := tool.Execute(context.Background(),
		`{"operation": "summon"}`,
		&Context{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "unknown operation") {
		t.Fatalf("expected unknown-operation message, got %q", res.Output)
	}
}

func TestGraphQueryTool_RequiresBothFromAndToForPath(t *testing.T) {
	store := testutil.TempStore(t)
	tool := NewGraphQueryTool(store)
	res, _ := tool.Execute(context.Background(),
		`{"operation": "path", "from": "A"}`,
		&Context{})
	if !strings.Contains(res.Output, "requires both") {
		t.Fatalf("expected required-field message, got %q", res.Output)
	}
}

func TestGraphQueryTool_CapsMaxDepth(t *testing.T) {
	store := testutil.TempStore(t)
	seedGraph(t, store, [][4]string{
		{"f1", "A", "depends_on", "B"},
	})
	tool := NewGraphQueryTool(store)
	// Request an absurd depth; tool must clamp internally without error.
	res, err := tool.Execute(context.Background(),
		`{"operation": "dependencies", "seed": "A", "max_depth": 999}`,
		&Context{})
	if err != nil {
		t.Fatal(err)
	}
	var out graphTraversalResult
	if err := json.Unmarshal([]byte(res.Output), &out); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, res.Output)
	}
	if len(out.Nodes) == 0 {
		t.Fatalf("expected at least one dependency node, got %+v", out)
	}
}

func TestGraphQueryTool_NilStoreReturnsFriendlyError(t *testing.T) {
	tool := NewGraphQueryTool(nil)
	res, err := tool.Execute(context.Background(), `{"operation": "path"}`, &Context{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "not available") {
		t.Fatalf("expected friendly unavailability message, got %q", res.Output)
	}
}

func TestGraphQueryTool_ParameterSchemaIsValidJSON(t *testing.T) {
	tool := NewGraphQueryTool(nil)
	var schema map[string]any
	if err := json.Unmarshal(tool.ParameterSchema(), &schema); err != nil {
		t.Fatalf("schema not valid JSON: %v", err)
	}
	if schema["type"] != "object" {
		t.Fatalf("expected schema type=object, got %v", schema["type"])
	}
}
