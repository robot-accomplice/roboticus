package db

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillCompositionRepository_UpsertInstruction(t *testing.T) {
	store := testTempStore(t)
	repo := NewSkillCompositionRepository(store, t.TempDir())

	created, spec, err := repo.Upsert(context.Background(), SkillCompositionSpec{
		Name:        "greeting_skill",
		Kind:        "instruction",
		Description: "Greets politely",
		Content:     "Say hello and offer help.",
		Triggers:    []string{"hello", "hi"},
		Priority:    7,
		Enabled:     true,
		Version:     "1.2.3",
		Author:      "tester",
		RiskLevel:   "Caution",
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if !created {
		t.Fatal("expected created=true for new skill")
	}
	if spec.SourcePath == "" || filepath.Ext(spec.SourcePath) != ".md" {
		t.Fatalf("unexpected source path: %q", spec.SourcePath)
	}
	if spec.ContentHash == "" {
		t.Fatal("expected content hash")
	}

	data, err := os.ReadFile(spec.SourcePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "---\n") || !strings.Contains(content, "name: greeting_skill") || !strings.Contains(content, "Say hello and offer help.") {
		t.Fatalf("unexpected instruction artifact:\n%s", content)
	}

	stored, err := NewSkillsRepository(store).GetByName(context.Background(), "greeting_skill")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if stored == nil || stored.Kind != "instruction" || stored.SourcePath != spec.SourcePath {
		t.Fatalf("unexpected stored row: %+v", stored)
	}
	var triggers []string
	if err := json.Unmarshal([]byte(stored.TriggersJSON), &triggers); err != nil {
		t.Fatalf("unmarshal triggers: %v", err)
	}
	if len(triggers) != 2 || triggers[0] != "hello" {
		t.Fatalf("unexpected triggers: %+v", triggers)
	}
}

func TestSkillCompositionRepository_UpsertStructured(t *testing.T) {
	store := testTempStore(t)
	repo := NewSkillCompositionRepository(store, t.TempDir())

	created, spec, err := repo.Upsert(context.Background(), SkillCompositionSpec{
		Name:        "triage_skill",
		Kind:        "structured",
		Description: "Runs a small triage chain",
		Triggers:    []string{"triage"},
		ToolChain: []SkillCompositionStep{
			{ToolName: "search_files", Params: json.RawMessage(`{"query":"error"}`)},
			{ToolName: "read_file", Params: json.RawMessage(`{"path":"README.md"}`)},
		},
		Enabled:        true,
		Version:        "0.9.0",
		Author:         "tester",
		RiskLevel:      "Dangerous",
		RegistrySource: "runtime",
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if !created {
		t.Fatal("expected created=true for new skill")
	}
	if spec.SourcePath == "" || filepath.Ext(spec.SourcePath) != ".yaml" {
		t.Fatalf("unexpected source path: %q", spec.SourcePath)
	}

	data, err := os.ReadFile(spec.SourcePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "tool_chain:") || !strings.Contains(content, "tool: search_files") || !strings.Contains(content, "tool: read_file") {
		t.Fatalf("unexpected structured artifact:\n%s", content)
	}

	stored, err := NewSkillsRepository(store).GetByName(context.Background(), "triage_skill")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if stored == nil || stored.Kind != "structured" {
		t.Fatalf("unexpected stored row: %+v", stored)
	}
	var chain []string
	if err := json.Unmarshal([]byte(stored.ToolChainJSON), &chain); err != nil {
		t.Fatalf("unmarshal tool chain: %v", err)
	}
	if len(chain) != 2 || chain[0] != "search_files" || chain[1] != "read_file" {
		t.Fatalf("unexpected tool chain: %+v", chain)
	}
}
