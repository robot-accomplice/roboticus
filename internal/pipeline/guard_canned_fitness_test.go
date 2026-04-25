package pipeline

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGuardFallbackTemplatesAreNotCompiledIntoPipeline(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	dir := filepath.Dir(currentFile)
	forbidden := []string{
		"guardFallbackTemplates",
		"fallbackResponse(",
		"GetFallbackTemplate(",
		`Content: "I can't share`,
		`Content: "I can't assist`,
		`Content: "I cannot modify security-sensitive configuration`,
	}

	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(content)
		for _, marker := range forbidden {
			if strings.Contains(text, marker) {
				t.Fatalf("forbidden canned guard fallback marker %q found in %s", marker, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk pipeline source: %v", err)
	}
}
