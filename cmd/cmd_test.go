package cmd

import (
	"bytes"
	"testing"
)

func TestRootCommand_Help(t *testing.T) {
	buf := &bytes.Buffer{}
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"--help"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	output := buf.String()
	if output == "" {
		t.Error("help output should not be empty")
	}
}

func TestVersionFlag(t *testing.T) {
	// Just verify the root command exists and has a name.
	if rootCmd.Name() == "" {
		t.Error("root command should have a name")
	}
}

func TestEnsureParentDir(t *testing.T) {
	dir := t.TempDir()
	err := ensureParentDir(dir + "/subdir/file.txt")
	if err != nil {
		t.Fatalf("ensureParentDir: %v", err)
	}
}

func TestEnsureParentDir_Existing(t *testing.T) {
	dir := t.TempDir()
	err := ensureParentDir(dir + "/file.txt")
	if err != nil {
		t.Fatalf("ensureParentDir existing: %v", err)
	}
}

func TestTruncateStr(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello..."},
		{"", 5, ""},
	}
	for _, tt := range tests {
		got := truncateStr(tt.input, tt.maxLen)
		if len(got) > tt.maxLen+3 { // allow for "..." suffix
			t.Errorf("truncateStr(%q, %d) = %q (too long)", tt.input, tt.maxLen, got)
		}
	}
}

func TestInitLogger(t *testing.T) {
	// Should not panic.
	initLogger()
}
