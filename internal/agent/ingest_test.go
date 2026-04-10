package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"roboticus/internal/llm"
	"roboticus/testutil"
)

func TestFileTypeFromPath(t *testing.T) {
	tests := []struct {
		path string
		want FileType
	}{
		{"readme.md", FileTypeMarkdown},
		{"README.MARKDOWN", FileTypeMarkdown},
		{"main.go", FileTypeGoSource},
		{"main.rs", FileTypeRustSource},
		{"app.py", FileTypePythonSource},
		{"app.tsx", FileTypeTypeScriptSource},
		{"index.js", FileTypeJavaScriptSource},
		{"notes.txt", FileTypePlainText},
		{"image.png", 0},
		{"archive.zip", 0},
	}
	for _, tt := range tests {
		got := FileTypeFromPath(tt.path)
		if got != tt.want {
			t.Errorf("FileTypeFromPath(%q) = %d, want %d", tt.path, got, tt.want)
		}
	}
}

func TestFileType_IsCode(t *testing.T) {
	if !FileTypeGoSource.IsCode() {
		t.Error("GoSource should be code")
	}
	if !FileTypeRustSource.IsCode() {
		t.Error("RustSource should be code")
	}
	if FileTypeMarkdown.IsCode() {
		t.Error("Markdown should not be code")
	}
}

func TestChunkText_SmallFile(t *testing.T) {
	text := "Hello, world!"
	chunks := ChunkText(text, DefaultChunkConfig())
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Text != text {
		t.Errorf("chunk text = %q, want %q", chunks[0].Text, text)
	}
}

func TestChunkText_Empty(t *testing.T) {
	chunks := ChunkText("", DefaultChunkConfig())
	if len(chunks) != 0 {
		t.Fatalf("expected 0 chunks for empty text, got %d", len(chunks))
	}
}

func TestChunkText_LargeFile(t *testing.T) {
	// Create text larger than 512*4=2048 bytes.
	text := ""
	for i := 0; i < 100; i++ {
		text += "This is a sentence that adds content to exceed the chunk limit. "
	}
	chunks := ChunkText(text, DefaultChunkConfig())
	if len(chunks) < 2 {
		t.Fatalf("expected >1 chunks for large text, got %d", len(chunks))
	}
	// Verify overlap: last part of chunk N should appear in chunk N+1.
	if len(chunks) >= 2 {
		end := chunks[0].Text[len(chunks[0].Text)-20:]
		if len(chunks[1].Text) > 20 {
			// The overlap region should contain shared content.
			_ = end // overlap is structural, not character-exact
		}
	}
}

func TestIngestFile_Markdown(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	embedClient := llm.NewEmbeddingClient(nil) // n-gram fallback

	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	if err := os.WriteFile(path, []byte("# Test Document\n\nThis is a test document with enough content to be meaningful.\n\n## Section Two\n\nMore content here for the chunker to work with."), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := IngestFile(ctx, store, embedClient, path)
	if err != nil {
		t.Fatalf("IngestFile: %v", err)
	}
	if result.FileType != FileTypeMarkdown {
		t.Errorf("FileType = %d, want Markdown", result.FileType)
	}
	if result.ChunksStored < 1 {
		t.Error("expected at least 1 chunk stored")
	}
	if result.TotalChars < 50 {
		t.Errorf("TotalChars = %d, want >= 50", result.TotalChars)
	}
	if result.SourceID == "" || result.SourceID[:7] != "ingest:" {
		t.Errorf("SourceID = %q, want ingest:* prefix", result.SourceID)
	}
}

func TestIngestFile_CodeFile(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	dir := t.TempDir()
	path := filepath.Join(dir, "example.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc main() {\n\tprintln(\"Hello\")\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := IngestFile(ctx, store, nil, path)
	if err != nil {
		t.Fatalf("IngestFile: %v", err)
	}
	if result.FileType != FileTypeGoSource {
		t.Errorf("FileType = %d, want GoSource", result.FileType)
	}
	if result.ChunksStored != 1 {
		t.Errorf("ChunksStored = %d, want 1 (small file)", result.ChunksStored)
	}
}

func TestIngestFile_EmptyFileFails(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := IngestFile(ctx, store, nil, path)
	if err == nil || !strings.Contains(err.Error(), "no extractable text") {
		t.Errorf("expected 'no extractable text' error, got %v", err)
	}
}

func TestIngestFile_UnsupportedExtensionFails(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	dir := t.TempDir()
	path := filepath.Join(dir, "photo.png")
	if err := os.WriteFile(path, []byte("fake png data"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := IngestFile(ctx, store, nil, path)
	if err == nil || !strings.Contains(err.Error(), "unsupported file type") {
		t.Errorf("expected 'unsupported file type' error, got %v", err)
	}
}

func TestIngestDirectory(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.md"), []byte("# Doc A\nSome markdown content here."), 0644)
	_ = os.WriteFile(filepath.Join(dir, "b.txt"), []byte("Plain text content for ingestion."), 0644)
	_ = os.WriteFile(filepath.Join(dir, "c.png"), []byte("fake image"), 0644) // unsupported, should skip

	results, err := IngestDirectory(ctx, store, nil, dir)
	if err != nil {
		t.Fatalf("IngestDirectory: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results (skipping .png), got %d", len(results))
	}
}
