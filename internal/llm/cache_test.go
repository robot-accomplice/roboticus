package llm

import (
	"context"
	"testing"
)

func TestCache_PutAndGet(t *testing.T) {
	cfg := DefaultCacheConfig()
	cache := NewCache(cfg, nil, nil)

	req := &Request{
		Model:    "gpt-4",
		Messages: []Message{{Role: "user", Content: "hello"}},
	}
	resp := &Response{
		ID:      "resp-1",
		Model:   "gpt-4",
		Content: "Hi there!",
		Usage:   Usage{InputTokens: 5, OutputTokens: 3},
	}

	cache.Put(context.Background(), req, resp)

	cached := cache.Get(context.Background(), req)
	if cached == nil {
		t.Fatal("should find cached response in L1")
	}
	if cached.Content != "Hi there!" {
		t.Errorf("cached content = %s", cached.Content)
	}
}

func TestCache_Miss(t *testing.T) {
	cfg := DefaultCacheConfig()
	cache := NewCache(cfg, nil, nil)

	req := &Request{
		Model:    "gpt-4",
		Messages: []Message{{Role: "user", Content: "unique query"}},
	}

	cached := cache.Get(context.Background(), req)
	if cached != nil {
		t.Error("should miss for new query")
	}
}

func TestCache_StillWorksWithDisabledFlag(t *testing.T) {
	// Note: Enabled flag is for the caller to check. Cache itself always works.
	cfg := DefaultCacheConfig()
	cfg.Enabled = false
	cache := NewCache(cfg, nil, nil)

	req := &Request{Model: "gpt-4", Messages: []Message{{Role: "user", Content: "hello"}}}
	cache.Put(context.Background(), req, &Response{Content: "resp"})
	cached := cache.Get(context.Background(), req)
	// Cache always stores/retrieves — Enabled is for service-level gating.
	if cached == nil {
		t.Error("cache should still work; Enabled is a config hint, not an internal gate")
	}
}

func TestCache_DifferentRequests(t *testing.T) {
	cfg := DefaultCacheConfig()
	cache := NewCache(cfg, nil, nil)

	req1 := &Request{Model: "gpt-4", Messages: []Message{{Role: "user", Content: "hello"}}}
	req2 := &Request{Model: "gpt-4", Messages: []Message{{Role: "user", Content: "goodbye"}}}

	cache.Put(context.Background(), req1, &Response{Content: "hi"})
	if cache.Get(context.Background(), req2) != nil {
		t.Error("different request should miss")
	}
}

func TestDefaultCacheConfig_Values(t *testing.T) {
	cfg := DefaultCacheConfig()
	if cfg.TTL <= 0 {
		t.Error("TTL should be positive")
	}
	if cfg.MaxEntries <= 0 {
		t.Error("MaxEntries should be positive")
	}
}

func TestHashRequest_Deterministic(t *testing.T) {
	req := &Request{Model: "gpt-4", Messages: []Message{{Role: "user", Content: "test"}}}
	h1 := hashRequest(req)
	h2 := hashRequest(req)
	if h1 != h2 {
		t.Error("hash should be deterministic")
	}
}

func TestHashRequest_Different(t *testing.T) {
	req1 := &Request{Model: "gpt-4", Messages: []Message{{Role: "user", Content: "hello"}}}
	req2 := &Request{Model: "gpt-4", Messages: []Message{{Role: "user", Content: "world"}}}
	if hashRequest(req1) == hashRequest(req2) {
		t.Error("different requests should produce different hashes")
	}
}
