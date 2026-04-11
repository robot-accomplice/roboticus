package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	ngramDim     = 128
	embedTimeout = 30 * time.Second
)

// EmbeddingClient generates vector embeddings via a remote provider or local n-gram fallback.
type EmbeddingClient struct {
	httpClient *http.Client
	provider   *Provider
}

// NewEmbeddingClient creates an embedding client using the given provider.
// If provider is nil, the client falls back to local n-gram hashing.
func NewEmbeddingClient(provider *Provider) *EmbeddingClient {
	return &EmbeddingClient{
		httpClient: &http.Client{Timeout: embedTimeout},
		provider:   provider,
	}
}

// Embed generates embeddings for a batch of texts.
func (ec *EmbeddingClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if ec.provider == nil || ec.provider.EmbeddingPath == "" {
		return ec.fallbackNgram(texts), nil
	}

	switch ec.provider.Format {
	case FormatOpenAI, FormatOllama:
		return ec.embedOpenAI(ctx, texts)
	case FormatAnthropic:
		// Anthropic doesn't have a public embedding endpoint; fall back.
		return ec.fallbackNgram(texts), nil
	case FormatGoogle:
		return ec.embedGoogle(ctx, texts)
	default:
		return ec.embedOpenAI(ctx, texts)
	}
}

// EmbedSingle generates an embedding for a single text.
func (ec *EmbeddingClient) EmbedSingle(ctx context.Context, text string) ([]float32, error) {
	results, err := ec.Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("embedding returned no results")
	}
	return results[0], nil
}

// Dimensions returns the expected embedding dimensions.
func (ec *EmbeddingClient) Dimensions() int {
	if ec.provider != nil && ec.provider.EmbeddingModel != "" {
		// Known models and their dimensions — check the EmbeddingPath for clues.
		switch {
		case strings.Contains(ec.provider.EmbeddingModel, "text-embedding-3-small"):
			return 1536
		case strings.Contains(ec.provider.EmbeddingModel, "text-embedding-004"):
			return 768
		case strings.Contains(ec.provider.EmbeddingModel, "nomic-embed"):
			return 768
		}
	}
	return ngramDim
}

// embedOpenAI calls the OpenAI-compatible /v1/embeddings endpoint.
func (ec *EmbeddingClient) embedOpenAI(ctx context.Context, texts []string) ([][]float32, error) {
	body := map[string]any{
		"input": texts,
		"model": ec.provider.EmbeddingModel,
	}
	rawBody, _ := json.Marshal(body)

	url := strings.TrimRight(ec.provider.URL, "/") + ec.provider.EmbeddingPath
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(rawBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	ec.setAuth(req)

	resp, err := ec.httpClient.Do(req)
	if err != nil {
		log.Warn().Err(err).Msg("embedding request failed, using n-gram fallback")
		return ec.fallbackNgram(texts), nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		log.Warn().Int("status", resp.StatusCode).Msg("embedding API error, using n-gram fallback")
		return ec.fallbackNgram(texts), nil
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding embedding response: %w", err)
	}

	embeddings := make([][]float32, len(result.Data))
	for i, d := range result.Data {
		embeddings[i] = d.Embedding
	}
	return embeddings, nil
}

// embedGoogle calls the Google Generative AI batch embed endpoint.
func (ec *EmbeddingClient) embedGoogle(ctx context.Context, texts []string) ([][]float32, error) {
	apiKey := ""
	if ec.provider.APIKeyEnv != "" {
		apiKey = os.Getenv(ec.provider.APIKeyEnv)
	}

	// Build batch request.
	type part struct {
		Text string `json:"text"`
	}
	type content struct {
		Parts []part `json:"parts"`
	}
	type embedReq struct {
		Model   string  `json:"model"`
		Content content `json:"content"`
	}
	requests := make([]embedReq, len(texts))
	model := "models/" + ec.provider.EmbeddingModel
	for i, t := range texts {
		requests[i] = embedReq{
			Model:   model,
			Content: content{Parts: []part{{Text: t}}},
		}
	}

	body, _ := json.Marshal(map[string]any{"requests": requests})
	url := fmt.Sprintf("%s/v1beta/%s:batchEmbedContents?key=%s",
		strings.TrimRight(ec.provider.URL, "/"), model, apiKey)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ec.httpClient.Do(req)
	if err != nil {
		log.Warn().Err(err).Msg("google embedding request failed, using n-gram fallback")
		return ec.fallbackNgram(texts), nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		log.Warn().Int("status", resp.StatusCode).Msg("google embedding API error, using n-gram fallback")
		return ec.fallbackNgram(texts), nil
	}

	var result struct {
		Embeddings []struct {
			Values []float32 `json:"values"`
		} `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding google embedding response: %w", err)
	}

	embeddings := make([][]float32, len(result.Embeddings))
	for i, e := range result.Embeddings {
		embeddings[i] = e.Values
	}
	return embeddings, nil
}

// setAuth adds authentication headers to the request.
func (ec *EmbeddingClient) setAuth(req *http.Request) {
	if ec.provider == nil {
		return
	}
	apiKey := ""
	if ec.provider.APIKeyEnv != "" {
		apiKey = os.Getenv(ec.provider.APIKeyEnv)
	}
	if apiKey == "" {
		return
	}

	if ec.provider.AuthHeader != "" {
		req.Header.Set(ec.provider.AuthHeader, apiKey)
	} else {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	for k, v := range ec.provider.ExtraHeaders {
		req.Header.Set(k, v)
	}
}

// fallbackNgram produces a deterministic character n-gram embedding when no provider is available.
func (ec *EmbeddingClient) fallbackNgram(texts []string) [][]float32 {
	results := make([][]float32, len(texts))
	for i, text := range texts {
		results[i] = ngramHash(text, ngramDim)
	}
	return results
}

// ngramHash produces a fixed-length embedding from character trigrams.
// Rust parity: embedding.rs fallback_ngram() — lowercase, rune-level char windows(3),
// rolling hash acc.wrapping_mul(31).wrapping_add(c as u32), positive accumulation only,
// L2 normalization. No character filtering — all runes participate.
func ngramHash(text string, dim int) []float32 {
	vec := make([]float32, dim)
	lower := strings.ToLower(text)
	chars := []rune(lower)
	if len(chars) < 3 {
		return vec
	}

	// Character trigram windows (rune-level, matching Rust chars.windows(3)).
	for i := 0; i+3 <= len(chars); i++ {
		window := chars[i : i+3]
		var hash uint32
		for _, c := range window {
			hash = hash*31 + uint32(c) // Rust: acc.wrapping_mul(31).wrapping_add(c as u32)
		}
		vec[hash%uint32(dim)] += 1.0 // Rust: positive accumulation only
	}

	// L2 normalize.
	var norm float32
	for _, v := range vec {
		norm += v * v
	}
	norm = float32(math.Sqrt(float64(norm)))
	if norm > 0 {
		for i := range vec {
			vec[i] /= norm
		}
	}
	return vec
}

// rollingHash produces a deterministic hash matching Rust's (acc * 31) + char_as_u32.
func rollingHash(s string) uint32 {
	var acc uint32
	for _, c := range s {
		acc = acc*31 + uint32(c)
	}
	return acc
}

// CosineSimilarity computes the cosine similarity between two vectors.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}
