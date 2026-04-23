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
