package api

import (
	"context"
	"testing"

	"roboticus/internal/core"
	"roboticus/testutil"
)

func TestBuildTopicSnapshots_WorkspaceUsesSharedRosterProducer(t *testing.T) {
	store := testutil.TempStore(t)
	if _, err := store.ExecContext(context.Background(),
		`INSERT INTO sub_agents (id, name, display_name, model, role, description, enabled, created_at)
		 VALUES ('sa1', 'researcher', 'Researcher', 'gpt-4', 'specialist', 'does research', 1, datetime('now'))`); err != nil {
		t.Fatalf("seed subagent: %v", err)
	}
	cfg := core.DefaultConfig()
	cfg.Agent.ID = "duncan"
	cfg.Agent.Name = "Duncan"
	cfg.Models.Primary = "deepseek/deepseek-v4-flash"

	snapshots := BuildTopicSnapshots(&AppState{Store: store, Config: &cfg})
	snapshot, ok := snapshots[TopicWorkspace]
	if !ok {
		t.Fatal("workspace topic snapshot missing")
	}
	payload, ok := snapshot().(map[string]any)
	if !ok {
		t.Fatalf("workspace snapshot type = %T, want map", snapshot())
	}
	agents, ok := payload["agents"].([]map[string]any)
	if !ok {
		t.Fatalf("agents type = %T", payload["agents"])
	}
	if len(agents) != 2 {
		t.Fatalf("agents = %d, want orchestrator + subagent", len(agents))
	}
	if agents[0]["role"] != "orchestrator" || agents[0]["name"] != "duncan" {
		t.Fatalf("first agent = %#v, want duncan orchestrator", agents[0])
	}
	if agents[1]["name"] != "researcher" {
		t.Fatalf("second agent = %#v, want researcher", agents[1])
	}
	if agents[1]["role"] != "subagent" {
		t.Fatalf("workspace subagent role = %v, want normalized subagent", agents[1]["role"])
	}
	if agents[1]["source_role"] != "specialist" {
		t.Fatalf("workspace subagent source_role = %v, want specialist", agents[1]["source_role"])
	}
}
