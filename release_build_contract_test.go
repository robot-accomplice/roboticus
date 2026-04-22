package main

import (
	"os"
	"strings"
	"testing"
)

func TestReleaseBuildPathsStampCanonicalVersionSymbols(t *testing.T) {
	t.Parallel()

	expected := []string{
		"roboticus/cmd/internal/cmdutil.Version=",
		"roboticus/internal/daemon.version=",
	}
	forbidden := []string{
		"roboticus/cmd.version=",
	}

	files := []string{
		".github/workflows/ci.yml",
		".github/workflows/release.yml",
		"justfile",
	}

	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		content := string(data)
		for _, needle := range expected {
			if !strings.Contains(content, needle) {
				t.Fatalf("%s missing required ldflags target %q", path, needle)
			}
		}
		for _, needle := range forbidden {
			if strings.Contains(content, needle) {
				t.Fatalf("%s still contains deprecated ldflags target %q", path, needle)
			}
		}
	}
}
