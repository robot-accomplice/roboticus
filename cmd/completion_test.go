package cmd

import (
	"bytes"
	"testing"
)

func TestCompletionBash_Generation(t *testing.T) {
	buf := &bytes.Buffer{}
	// Call the underlying generation function directly with a buffer.
	err := rootCmd.GenBashCompletion(buf)
	if err != nil {
		t.Fatalf("bash completion generation: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("expected non-empty bash completion output")
	}
}

func TestCompletionZsh_Generation(t *testing.T) {
	buf := &bytes.Buffer{}
	err := rootCmd.GenZshCompletion(buf)
	if err != nil {
		t.Fatalf("zsh completion generation: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("expected non-empty zsh completion output")
	}
}

func TestCompletionFish_Generation(t *testing.T) {
	buf := &bytes.Buffer{}
	err := rootCmd.GenFishCompletion(buf, true)
	if err != nil {
		t.Fatalf("fish completion generation: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("expected non-empty fish completion output")
	}
}

func TestCompletionCmd_SubcommandRegistration(t *testing.T) {
	subcommands := make(map[string]bool)
	for _, sub := range completionCmd.Commands() {
		subcommands[sub.Name()] = true
	}
	for _, name := range []string{"bash", "zsh", "fish"} {
		if !subcommands[name] {
			t.Errorf("completion command missing subcommand %q", name)
		}
	}
}
