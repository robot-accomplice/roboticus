package cmd

import (
	"bytes"
	"testing"
)

func TestTruncateStr_Comprehensive(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"within limit", "hello", 10, "hello"},
		{"at limit", "hello", 5, "hello"},
		{"over limit", "hello world this is a test", 5, "hello..."},
		{"empty string", "", 5, ""},
		{"single char over", "ab", 1, "a..."},
		{"zero max len", "hello", 0, "..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateStr(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateStr(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestExportMarkdown(t *testing.T) {
	data := map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
			map[string]any{"role": "assistant", "content": "Hi there!"},
		},
	}

	// Capture stdout by running the function (it prints to stdout).
	// We can't easily capture stdout in this test, but we can verify it doesn't error.
	err := exportMarkdown("test-session-123", data)
	if err != nil {
		t.Fatalf("exportMarkdown: %v", err)
	}
}

func TestExportMarkdown_NoMessages(t *testing.T) {
	data := map[string]any{}
	err := exportMarkdown("empty-session", data)
	if err != nil {
		t.Fatalf("exportMarkdown with no messages: %v", err)
	}
}

func TestSessionsExportCmd_Flags(t *testing.T) {
	// Verify the format flag exists with correct default.
	f := sessionsExportCmd.Flags().Lookup("format")
	if f == nil {
		t.Fatal("expected 'format' flag on sessions export command")
	}
	if f.DefValue != "json" {
		t.Errorf("expected default format 'json', got %q", f.DefValue)
	}
}

func TestSessionsCmd_SubcommandRegistration(t *testing.T) {
	subcommands := make(map[string]bool)
	for _, sub := range sessionsCmd.Commands() {
		subcommands[sub.Name()] = true
	}

	expected := []string{"list", "show", "delete", "export"}
	for _, name := range expected {
		if !subcommands[name] {
			t.Errorf("sessions command missing subcommand %q", name)
		}
	}
}

func TestSessionsShowCmd_RequiresArg(t *testing.T) {
	buf := &bytes.Buffer{}
	sessionsShowCmd.SetOut(buf)
	sessionsShowCmd.SetErr(buf)
	err := sessionsShowCmd.Args(sessionsShowCmd, []string{})
	if err == nil {
		t.Error("expected error when no args provided to sessions show")
	}
}

func TestSessionsDeleteCmd_RequiresArg(t *testing.T) {
	err := sessionsDeleteCmd.Args(sessionsDeleteCmd, []string{})
	if err == nil {
		t.Error("expected error when no args provided to sessions delete")
	}
}
