package mcp

import (
	"encoding/json"
	"testing"
)

func TestFilterByAllowlist_EmptyAllowlist(t *testing.T) {
	tools := []ToolDescriptor{
		{Name: "alpha", Description: "a"},
		{Name: "beta", Description: "b"},
	}
	result := FilterByAllowlist(tools, nil)
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}

	result2 := FilterByAllowlist(tools, []string{})
	if len(result2) != 2 {
		t.Fatalf("expected 2, got %d", len(result2))
	}
}

func TestFilterByAllowlist_Filters(t *testing.T) {
	tools := []ToolDescriptor{
		{Name: "alpha", Description: "a", InputSchema: json.RawMessage(`{}`)},
		{Name: "beta", Description: "b", InputSchema: json.RawMessage(`{}`)},
		{Name: "gamma", Description: "c", InputSchema: json.RawMessage(`{}`)},
	}
	result := FilterByAllowlist(tools, []string{"beta", "gamma"})
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}
	if result[0].Name != "beta" || result[1].Name != "gamma" {
		t.Fatalf("unexpected tools: %+v", result)
	}
}

func TestFilterByAllowlist_NoMatch(t *testing.T) {
	tools := []ToolDescriptor{
		{Name: "alpha", Description: "a"},
	}
	result := FilterByAllowlist(tools, []string{"nonexistent"})
	if len(result) != 0 {
		t.Fatalf("expected 0, got %d", len(result))
	}
}
