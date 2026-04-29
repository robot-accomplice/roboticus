package tools

import "testing"

func TestOperationClassForName_ReadFileIsArtifactRead(t *testing.T) {
	if got := OperationClassForName("read_file"); got != OperationArtifactRead {
		t.Fatalf("operation class = %q, want %q", got, OperationArtifactRead)
	}
	if !IsReadOnlyExploration("read_file") {
		t.Fatal("read_file should be treated as read-only exploration")
	}
}

func TestOperationClassForName_GlobFilesIsWorkspaceInspect(t *testing.T) {
	if got := OperationClassForName("glob_files"); got != OperationWorkspaceInspect {
		t.Fatalf("operation class = %q, want %q", got, OperationWorkspaceInspect)
	}
	if !IsReadOnlyExploration("glob_files") {
		t.Fatal("glob_files should be treated as read-only exploration")
	}
}

func TestOperationClassForName_SearchFilesIsWorkspaceInspect(t *testing.T) {
	if got := OperationClassForName("search_files"); got != OperationWorkspaceInspect {
		t.Fatalf("operation class = %q, want %q", got, OperationWorkspaceInspect)
	}
	if !IsReadOnlyExploration("search_files") {
		t.Fatal("search_files should be treated as read-only exploration")
	}
}

func TestOperationClassForName_InventoryProjectsIsWorkspaceInspect(t *testing.T) {
	if got := OperationClassForName("inventory_projects"); got != OperationWorkspaceInspect {
		t.Fatalf("operation class = %q, want %q", got, OperationWorkspaceInspect)
	}
	if !IsReadOnlyExploration("inventory_projects") {
		t.Fatal("inventory_projects should be treated as read-only exploration")
	}
}

// TestOperationClassForName_v108CoverageGaps covers tools that were
// previously falling through to OperationUnknown and silently being
// stripped by the policy stage. The v1.0.8 audit added explicit
// classifications for database, knowledge-graph, introspection, and
// web tools; this test pins those classifications so future drift
// triggers a regression instead of a stealth admission downgrade.
func TestOperationClassForName_v108CoverageGaps(t *testing.T) {
	cases := []struct {
		name string
		want OperationClass
	}{
		{"create_table", OperationDataWrite},
		{"insert_row", OperationDataWrite},
		{"alter_table", OperationDataWrite},
		{"drop_table", OperationDataWrite},
		{"query_table", OperationDataRead},
		{"query_knowledge_graph", OperationMemoryRead},
		{"find_workflow", OperationMemoryRead},
		{"introspect", OperationCapabilityInventory},
		{"get_channel_health", OperationInspection},
		{"compose-skill", OperationCapabilityInventory},
		{"web_search", OperationWebRead},
		{"http_fetch", OperationWebRead},
		{"ghola", OperationWebRead},
		{"browser_navigate", OperationWebRead},
		{"browser_snapshot", OperationWebRead},
		{"browser_click", OperationExecution},
	}
	for _, c := range cases {
		if got := OperationClassForName(c.name); got != c.want {
			t.Errorf("OperationClassForName(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestReadOnlyExploration_IncludesDataAndWebReads ensures the
// read-only filter recognises the new OperationDataRead and
// OperationWebRead classes so retrieval and exploration plans can
// safely include them without being downgraded by the loop.
func TestReadOnlyExploration_IncludesDataAndWebReads(t *testing.T) {
	for _, name := range []string{"query_table", "web_search", "http_fetch", "ghola", "browser_navigate", "browser_snapshot"} {
		if !IsReadOnlyExploration(name) {
			t.Errorf("IsReadOnlyExploration(%q) = false, want true", name)
		}
	}
}

func TestMakesExecutionProgress_IncludesDataWrite(t *testing.T) {
	if !MakesExecutionProgress("insert_row") {
		t.Fatal("insert_row should make execution progress")
	}
}

func TestReplayClassForName_DataWriteIsProtected(t *testing.T) {
	if got := ReplayClassForName("insert_row"); got != ReplayProtected {
		t.Fatalf("ReplayClassForName(insert_row) = %q, want %q", got, ReplayProtected)
	}
	if got := ReplayClassForName("query_table"); got != ReplaySafe {
		t.Fatalf("ReplayClassForName(query_table) = %q, want %q", got, ReplaySafe)
	}
	if got := ReplayClassForName("web_search"); got != ReplaySafe {
		t.Fatalf("ReplayClassForName(web_search) = %q, want %q", got, ReplaySafe)
	}
}

func TestReplayFingerprintForCall_ArtifactWriteUsesPathIdentity(t *testing.T) {
	first := ReplayFingerprintForCall("obsidian_write", `{"path":"Note.md","content":"# first"}`)
	second := ReplayFingerprintForCall("obsidian_write", `{"path":"note.md","content":"# second"}`)

	if first.Key == "" {
		t.Fatal("expected non-empty fingerprint key")
	}
	if first.Key != second.Key {
		t.Fatalf("fingerprint mismatch: %q vs %q", first.Key, second.Key)
	}
	if first.Resource != "note.md" {
		t.Fatalf("resource = %q, want note.md", first.Resource)
	}
}

func TestReplayFingerprintForResult_PrefersArtifactProof(t *testing.T) {
	proof := NewArtifactProof("workspace_file", "tmp/out.txt", "hello", false)
	fp := ReplayFingerprintForResult("write_file", `{"path":"tmp/ignored.txt","content":"hello"}`, proof.Metadata())

	if fp.Key != "artifact_write:tmp/out.txt" {
		t.Fatalf("key = %q, want artifact_write:tmp/out.txt", fp.Key)
	}
	if fp.Resource != "tmp/out.txt" {
		t.Fatalf("resource = %q, want tmp/out.txt", fp.Resource)
	}
}
