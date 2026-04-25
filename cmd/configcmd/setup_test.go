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
	if !strings.Contains(out, `api_key_ref = "openai_api_key"`) {
		t.Error("expected openai_api_key keystore ref")
	}
	if strings.Contains(out, "sk-test-key") {
		t.Error("config output must not include raw API key")
	}
	if !strings.Contains(out, "keystore entry openai_api_key") {
		t.Error("expected keystore storage comment in output")
	}
}

func TestBuildConfigTOML_Anthropic(t *testing.T) {
	out := buildConfigTOML("Claude", "anthropic", "sk-ant-xxx")
	if !strings.Contains(out, `format = "anthropic"`) {
		t.Error("expected anthropic format")
	}
	if !strings.Contains(out, `api_key_ref = "anthropic_api_key"`) {
		t.Error("expected anthropic_api_key keystore ref")
	}
	if strings.Contains(out, "sk-ant-xxx") {
		t.Error("config output must not include raw API key")
	}
	if !strings.Contains(out, "keystore entry anthropic_api_key") {
		t.Error("expected keystore storage comment in output")
	}
}

func TestBuildConfigTOML_NoAPIKey(t *testing.T) {
	out := buildConfigTOML("Bot", "openai", "")
	// Should NOT contain the API key comment section.
	if strings.Contains(out, "keystore entry openai_api_key") {
		t.Error("should not include API key comment when key is empty")
	}
}

func TestBuildConfigTOML_DeepSeek(t *testing.T) {
	out := buildConfigTOML("Deep", "deepseek", "sk-deepseek")
	if !strings.Contains(out, `primary = "deepseek/deepseek-v4-pro"`) {
		t.Error("expected provider-qualified DeepSeek primary model")
	}
	if !strings.Contains(out, `[providers.deepseek]`) {
		t.Error("expected [providers.deepseek] section")
	}
	if !strings.Contains(out, `url = "https://api.deepseek.com"`) {
		t.Error("expected DeepSeek base URL")
	}
	if !strings.Contains(out, `chat_path = "/chat/completions"`) {
		t.Error("expected DeepSeek chat completions path")
	}
	if !strings.Contains(out, `api_key_ref = "deepseek_api_key"`) {
		t.Error("expected deepseek_api_key keystore ref")
	}
	if strings.Contains(out, "sk-deepseek") {
		t.Error("config output must not include raw DeepSeek API key")
	}
}

func TestBuildConfigTOML_AdditionalBundledCloudProviders(t *testing.T) {
	tests := []struct {
		provider string
		want     []string
	}{
		{
			provider: "google",
			want: []string{
				`primary = "google/gemini-3.1-pro-preview"`,
				`[providers.google]`,
				`format = "google"`,
				`api_key_ref = "google_api_key"`,
			},
		},
		{
			provider: "moonshot",
			want: []string{
				`primary = "moonshot/kimi-k2.5"`,
				`[providers.moonshot]`,
				`chat_path = "/v1/chat/completions"`,
				`api_key_ref = "moonshot_api_key"`,
			},
		},
		{
			provider: "openrouter",
			want: []string{
				`primary = "openrouter/google/gemini-3.1-pro-preview"`,
				`[providers.openrouter]`,
				`auth_header = "Authorization"`,
				`api_key_ref = "openrouter_api_key"`,
			},
		},
	}
	for _, tc := range tests {
		out := buildConfigTOML("Cloud", tc.provider, "")
		for _, want := range tc.want {
			if !strings.Contains(out, want) {
				t.Errorf("%s config missing %q:\n%s", tc.provider, want, out)
			}
		}
	}
}
