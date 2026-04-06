package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

// newIngestCmd creates a fresh ingest command for testing to avoid mutating the global one.
func newIngestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:  "ingest [path]",
		Args: cobra.ExactArgs(1),
		RunE: runIngest,
	}
	cmd.Flags().Bool("recursive", true, "")
	cmd.Flags().StringSlice("extensions", []string{".md", ".txt", ".text"}, "")
	cmd.Flags().Int("chunk-size", 512, "")
	cmd.Flags().Bool("dry-run", false, "")
	return cmd
}

func TestRunIngest_DryRun(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.md"), []byte("# Hello\n\nThis is test content.\n\nSecond paragraph."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("Some notes here."), 0o644); err != nil {
		t.Fatal(err)
	}
	// Non-matching file should be ignored.
	if err := os.WriteFile(filepath.Join(dir, "image.png"), []byte("binary"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newIngestCmd()
	_ = cmd.Flags().Set("dry-run", "true")

	err := runIngest(cmd, []string{dir})
	if err != nil {
		t.Fatalf("runIngest dry-run: %v", err)
	}
}

func TestRunIngest_NonexistentPath(t *testing.T) {
	cmd := newIngestCmd()
	_ = cmd.Flags().Set("dry-run", "true")

	err := runIngest(cmd, []string{"/nonexistent/path/that/does/not/exist"})
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestRunIngest_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	cmd := newIngestCmd()
	_ = cmd.Flags().Set("dry-run", "true")

	err := runIngest(cmd, []string{dir})
	if err != nil {
		t.Fatalf("runIngest empty dir: %v", err)
	}
}

func TestRunIngest_SingleFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "single.md")
	if err := os.WriteFile(file, []byte("single file content"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newIngestCmd()
	_ = cmd.Flags().Set("dry-run", "true")

	err := runIngest(cmd, []string{file})
	if err != nil {
		t.Fatalf("runIngest single file: %v", err)
	}
}

func TestRunIngest_NonRecursive(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "sub")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "top.md"), []byte("top level"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "nested.md"), []byte("nested content"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newIngestCmd()
	_ = cmd.Flags().Set("dry-run", "true")
	_ = cmd.Flags().Set("recursive", "false")

	err := runIngest(cmd, []string{dir})
	if err != nil {
		t.Fatalf("runIngest non-recursive: %v", err)
	}
}

func TestChunkText_LargeInput(t *testing.T) {
	var text string
	for i := 0; i < 20; i++ {
		if i > 0 {
			text += "\n\n"
		}
		text += "This is paragraph number with some reasonable length content for chunk testing."
	}

	chunks := chunkText(text, 100)
	if len(chunks) < 2 {
		t.Errorf("expected multiple chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if c == "" {
			t.Errorf("chunk %d is empty", i)
		}
	}
}

func TestChunkText_SingleParagraph(t *testing.T) {
	chunks := chunkText("Short text", 100)
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0] != "Short text" {
		t.Errorf("expected 'Short text', got %q", chunks[0])
	}
}

func TestChunkText_EmptyString(t *testing.T) {
	chunks := chunkText("", 100)
	if len(chunks) != 1 || chunks[0] != "" {
		t.Errorf("expected single empty chunk for empty input, got %v", chunks)
	}
}
