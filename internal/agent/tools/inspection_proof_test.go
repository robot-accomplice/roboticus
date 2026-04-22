package tools

import "testing"

func TestInspectionProof_JSONRoundTrip(t *testing.T) {
	proof := NewInspectionProof("file_glob", "glob_files", ".", 3).WithPattern("**/*.md")
	raw := proof.Metadata()
	parsed, ok := ParseInspectionProof(raw)
	if !ok {
		t.Fatal("expected inspection proof metadata")
	}
	if parsed.ToolName != "glob_files" || parsed.InspectionKind != "file_glob" {
		t.Fatalf("parsed = %+v", parsed)
	}
	if parsed.Count != 3 || parsed.Empty {
		t.Fatalf("parsed count/empty = %+v", parsed)
	}
	if parsed.Pattern != "**/*.md" {
		t.Fatalf("pattern = %q", parsed.Pattern)
	}
}
