package configcmd

import (
	"strings"
	"testing"
)

func TestBuildConfigTOML_Ollama(t *testing.T) {
	out := buildConfigTOML("TestBot", "ollama", "")
	if !strings.Contains(out, `name = "TestBot"`) {
		t.Errorf("expected agent name in config, got:\n%s", out)
	}
	if !strings.Contains(out, `primary = "llama3.2"`) {
		t.Error("expected llama3.2 as primary model for ollama")
	}
	if !strings.Contains(out, `is_local = true`) {
		t.Error("expected is_local = true for ollama")
	}
	if !strings.Contains(out, `[providers.ollama]`) {
		t.Error("expected [providers.ollama] section")
	}
}

func TestBuildConfigTOML_OpenAI(t *testing.T) {
	out := buildConfigTOML("Agent", "openai", "sk-test-key")
	if !strings.Contains(out, `primary = "gpt-4o"`) {
		t.Error("expected gpt-4o as primary model for openai")
	}
	if !strings.Contains(out, `[providers.openai]`) {
		t.Error("expected [providers.openai] section")
	}
	if !strings.Contains(out, `api_key_env = "OPENAI_API_KEY"`) {
		t.Error("expected OPENAI_API_KEY env ref")
	}
	if !strings.Contains(out, "OPENAI_API_KEY=sk-test-key") {
		t.Error("expected API key comment in output")
	}
}

func TestBuildConfigTOML_Anthropic(t *testing.T) {
	out := buildConfigTOML("Claude", "anthropic", "sk-ant-xxx")
	if !strings.Contains(out, `format = "anthropic"`) {
		t.Error("expected anthropic format")
	}
	if !strings.Contains(out, `api_key_env = "ANTHROPIC_API_KEY"`) {
		t.Error("expected ANTHROPIC_API_KEY env ref")
	}
	if !strings.Contains(out, "ANTHROPIC_API_KEY=sk-ant-xxx") {
		t.Error("expected API key comment in output")
	}
}

func TestBuildConfigTOML_NoAPIKey(t *testing.T) {
	out := buildConfigTOML("Bot", "openai", "")
	// Should NOT contain the API key comment section.
	if strings.Contains(out, "Set OPENAI_API_KEY=") {
		t.Error("should not include API key comment when key is empty")
	}
}
