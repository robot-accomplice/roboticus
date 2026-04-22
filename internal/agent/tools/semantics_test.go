package tools

import "testing"

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
