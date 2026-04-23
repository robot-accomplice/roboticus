package modelstate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"roboticus/internal/core"
)

func TestSample_OllamaReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[{"name":"qwen3.5:35b-a3b","size":12345}]}`))
		case "/api/ps":
			_, _ = w.Write([]byte(`{"models":[{"name":"qwen3.5:35b-a3b","size_vram":6789}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := &core.Config{
		Providers: map[string]core.ProviderConfig{
			"ollama": {URL: srv.URL, Format: "ollama", IsLocal: true},
		},
	}

	got := Sample(context.Background(), cfg, "ollama/qwen3.5:35b-a3b")
	if got.StateClass != "ready" {
		t.Fatalf("state_class = %q, want ready", got.StateClass)
	}
	if !got.ProviderReachable || !got.ModelAvailable || !got.ModelLoaded {
		t.Fatalf("unexpected readiness flags: %+v", got)
	}
	if got.OllamaSizeBytes != 12345 || got.OllamaVRAMBytes != 6789 {
		t.Fatalf("unexpected ollama size fields: %+v", got)
	}
}

func TestSample_OllamaInstalledNotLoaded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[{"name":"mixtral:8x7b","size":555}]}`))
		case "/api/ps":
			_, _ = w.Write([]byte(`{"models":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := &core.Config{
		Providers: map[string]core.ProviderConfig{
			"ollama": {URL: srv.URL, Format: "ollama", IsLocal: true},
		},
	}

	got := Sample(context.Background(), cfg, "ollama/mixtral:8x7b")
	if got.StateClass != "installed_not_loaded" {
		t.Fatalf("state_class = %q, want installed_not_loaded", got.StateClass)
	}
	if !got.ModelAvailable || got.ModelLoaded {
		t.Fatalf("unexpected model flags: %+v", got)
	}
}

func TestSample_OllamaMissingModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[{"name":"phi4-mini:latest","size":555}]}`))
		case "/api/ps":
			_, _ = w.Write([]byte(`{"models":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := &core.Config{
		Providers: map[string]core.ProviderConfig{
			"ollama": {URL: srv.URL, Format: "ollama", IsLocal: true},
		},
	}

	got := Sample(context.Background(), cfg, "ollama/qwen32b-tq")
	if got.StateClass != "model_missing" {
		t.Fatalf("state_class = %q, want model_missing", got.StateClass)
	}
	if got.ModelAvailable {
		t.Fatalf("model_available = true, want false")
	}
}

func TestSample_CloudProviderManaged(t *testing.T) {
	cfg := &core.Config{
		Providers: map[string]core.ProviderConfig{
			"openrouter": {URL: "https://openrouter.ai/api", Format: "openai", IsLocal: false},
		},
	}

	got := Sample(context.Background(), cfg, "openrouter/openai/gpt-4o-mini")
	if got.StateClass != "provider_managed" {
		t.Fatalf("state_class = %q, want provider_managed", got.StateClass)
	}
	if !got.ProviderConfigured {
		t.Fatalf("provider should be configured: %+v", got)
	}
}
