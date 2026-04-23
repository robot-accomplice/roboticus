package modelstate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"roboticus/internal/core"
)

// Snapshot captures model/provider execution preconditions that make a
// benchmark result interpretable. Host-resource state tells us whether the
// machine was underwater; model state tells us whether the specific model was
// actually installed, loaded, and reachable.
type Snapshot struct {
	CollectedAt        string   `json:"collected_at"`
	Model              string   `json:"model,omitempty"`
	Provider           string   `json:"provider,omitempty"`
	IsLocal            bool     `json:"is_local,omitempty"`
	ProviderConfigured bool     `json:"provider_configured,omitempty"`
	ProviderReachable  bool     `json:"provider_reachable,omitempty"`
	ModelAvailable     bool     `json:"model_available,omitempty"`
	ModelLoaded        bool     `json:"model_loaded,omitempty"`
	StateClass         string   `json:"state_class,omitempty"`
	ProviderHTTPStatus int      `json:"provider_http_status,omitempty"`
	OllamaSizeBytes    uint64   `json:"ollama_size_bytes,omitempty"`
	OllamaVRAMBytes    uint64   `json:"ollama_vram_bytes,omitempty"`
	Errors             []string `json:"errors,omitempty"`
}

func (s Snapshot) Empty() bool {
	return s.Model == "" &&
		s.Provider == "" &&
		!s.IsLocal &&
		!s.ProviderConfigured &&
		!s.ProviderReachable &&
		!s.ModelAvailable &&
		!s.ModelLoaded &&
		s.StateClass == "" &&
		s.ProviderHTTPStatus == 0 &&
		s.OllamaSizeBytes == 0 &&
		s.OllamaVRAMBytes == 0 &&
		len(s.Errors) == 0
}

func Marshal(snapshot *Snapshot) string {
	if snapshot == nil {
		return ""
	}
	buf, err := json.Marshal(snapshot)
	if err != nil {
		return ""
	}
	return string(buf)
}

func FromJSON(raw string) *Snapshot {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var snapshot Snapshot
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return nil
	}
	return &snapshot
}

func MarshalList(snapshots []Snapshot) string {
	if len(snapshots) == 0 {
		return ""
	}
	buf, err := json.Marshal(snapshots)
	if err != nil {
		return ""
	}
	return string(buf)
}

func ListFromJSON(raw string) []Snapshot {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var snapshots []Snapshot
	if err := json.Unmarshal([]byte(raw), &snapshots); err != nil {
		return nil
	}
	return snapshots
}

var (
	samplerMu sync.RWMutex
	samplerFn = sampleLive
)

func SetSamplerForTests(fn func(context.Context, *core.Config, string) Snapshot) func() {
	samplerMu.Lock()
	prev := samplerFn
	samplerFn = fn
	samplerMu.Unlock()
	return func() {
		samplerMu.Lock()
		samplerFn = prev
		samplerMu.Unlock()
	}
}

func Sample(ctx context.Context, cfg *core.Config, model string) Snapshot {
	samplerMu.RLock()
	fn := samplerFn
	samplerMu.RUnlock()
	if fn == nil {
		return Snapshot{CollectedAt: time.Now().UTC().Format(time.RFC3339), Model: model}
	}
	return fn(ctx, cfg, model)
}

func SampleMany(ctx context.Context, cfg *core.Config, models []string) []Snapshot {
	if len(models) == 0 {
		return nil
	}
	out := make([]Snapshot, 0, len(models))
	for _, model := range models {
		if strings.TrimSpace(model) == "" {
			continue
		}
		out = append(out, Sample(ctx, cfg, model))
	}
	return out
}

func sampleLive(ctx context.Context, cfg *core.Config, model string) Snapshot {
	s := Snapshot{
		CollectedAt: time.Now().UTC().Format(time.RFC3339),
		Model:       model,
	}
	provider, bareModel := splitModelSpec(model)
	s.Provider = provider
	if cfg == nil || provider == "" {
		s.StateClass = "provider_unconfigured"
		s.Errors = append(s.Errors, "provider config unavailable")
		return s
	}
	prov, ok := cfg.Providers[provider]
	if !ok || strings.TrimSpace(prov.URL) == "" {
		s.StateClass = "provider_unconfigured"
		s.Errors = append(s.Errors, "provider not configured")
		return s
	}
	s.ProviderConfigured = true
	s.IsLocal = prov.IsLocal
	if strings.EqualFold(prov.Format, "ollama") || provider == "ollama" {
		return sampleOllama(ctx, strings.TrimRight(prov.URL, "/"), bareModel, s)
	}
	if !prov.IsLocal {
		s.StateClass = "provider_managed"
		return s
	}
	s.StateClass = "unsupported_local_provider"
	s.Errors = append(s.Errors, "local provider runtime-state probe not implemented for format "+prov.Format)
	return s
}

func sampleOllama(ctx context.Context, baseURL, model string, snapshot Snapshot) Snapshot {
	sampleCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 && remaining < 500*time.Millisecond {
			cancel()
			sampleCtx, cancel = context.WithTimeout(context.Background(), remaining)
			defer cancel()
		}
	}

	client := &http.Client{Timeout: 450 * time.Millisecond}
	tagsResp, err := probeJSON(sampleCtx, client, baseURL+"/api/tags")
	if err != nil {
		snapshot.StateClass = "provider_unreachable"
		snapshot.Errors = append(snapshot.Errors, "ollama tags probe failed: "+err.Error())
		return snapshot
	}
	snapshot.ProviderReachable = true
	snapshot.ProviderHTTPStatus = tagsResp.status

	var tags ollamaTagsResponse
	if err := json.Unmarshal(tagsResp.body, &tags); err != nil {
		snapshot.StateClass = "provider_unreachable"
		snapshot.Errors = append(snapshot.Errors, "ollama tags decode failed: "+err.Error())
		return snapshot
	}
	for _, entry := range tags.Models {
		if sameModel(entry.Name, model) {
			snapshot.ModelAvailable = true
			snapshot.OllamaSizeBytes = entry.Size
			break
		}
	}
	if !snapshot.ModelAvailable {
		snapshot.StateClass = "model_missing"
		return snapshot
	}

	psResp, err := probeJSON(sampleCtx, client, baseURL+"/api/ps")
	if err != nil {
		snapshot.StateClass = "installed_unknown_runtime"
		snapshot.Errors = append(snapshot.Errors, "ollama ps probe failed: "+err.Error())
		return snapshot
	}
	if psResp.status != 0 {
		snapshot.ProviderHTTPStatus = psResp.status
	}
	var ps ollamaPSResponse
	if err := json.Unmarshal(psResp.body, &ps); err != nil {
		snapshot.StateClass = "installed_unknown_runtime"
		snapshot.Errors = append(snapshot.Errors, "ollama ps decode failed: "+err.Error())
		return snapshot
	}
	for _, entry := range ps.Models {
		if sameModel(entry.Name, model) {
			snapshot.ModelLoaded = true
			snapshot.OllamaVRAMBytes = entry.SizeVRAM
			break
		}
	}
	if snapshot.ModelLoaded {
		snapshot.StateClass = "ready"
	} else {
		snapshot.StateClass = "installed_not_loaded"
	}
	return snapshot
}

type probeResult struct {
	status int
	body   []byte
}

func probeJSON(ctx context.Context, client *http.Client, url string) (probeResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return probeResult{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return probeResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return probeResult{}, err
	}
	if resp.StatusCode >= 400 {
		return probeResult{status: resp.StatusCode, body: buf}, fmt.Errorf("http %d", resp.StatusCode)
	}
	return probeResult{status: resp.StatusCode, body: buf}, nil
}

func splitModelSpec(spec string) (provider, model string) {
	if i := strings.Index(spec, "/"); i >= 0 {
		return spec[:i], spec[i+1:]
	}
	return spec, ""
}

func sameModel(a, b string) bool {
	a = canonicalModelName(a)
	b = canonicalModelName(b)
	return a == b
}

func canonicalModelName(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	for _, prefix := range []string{"ollama/", "openrouter/", "openai/", "anthropic/", "google/", "moonshot/"} {
		if strings.HasPrefix(s, prefix) {
			return strings.TrimPrefix(s, prefix)
		}
	}
	return s
}

type ollamaTagsResponse struct {
	Models []struct {
		Name string `json:"name"`
		Size uint64 `json:"size"`
	} `json:"models"`
}

type ollamaPSResponse struct {
	Models []struct {
		Name     string `json:"name"`
		SizeVRAM uint64 `json:"size_vram"`
	} `json:"models"`
}
