package llm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"goboticus/internal/core"
	"goboticus/internal/db"
)

// Service is the top-level LLM orchestrator. It composes caching, routing,
// circuit breaking, dedup, and multi-provider failover into a single
// Complete/Stream interface.
//
// Request flow:
//
//	Request → Dedup → Cache check → Router (model selection) →
//	Circuit breaker → Client (format translation + HTTP) →
//	Cache store → Response
type Service struct {
	providers  map[string]*Client
	router     *Router
	breakers   *BreakerRegistry
	cache      *Cache
	dedup      *Dedup
	transforms *TransformPipeline
	primary    string   // primary model name
	fallbacks  []string // fallback model names
	store      *db.Store
	bgWorker   *core.BackgroundWorker
	Confidence *ConfidenceEvaluator
	Escalation *EscalationTracker
	quality    *QualityTracker
}

// ServiceConfig holds configuration for the LLM service.
type ServiceConfig struct {
	Providers       []Provider
	Primary         string
	Fallbacks       []string
	Cache           CacheConfig
	Breaker         CircuitBreakerConfig
	Router          RouterConfig
	ConfidenceFloor float64                // minimum confidence to accept local response (0 = use default)
	BGWorker        *core.BackgroundWorker // shared worker pool for async tasks
}

// NewService creates the LLM orchestrator.
func NewService(cfg ServiceConfig, store *db.Store) (*Service, error) {
	clients := make(map[string]*Client)
	var targets []RouteTarget

	// Build a map of provider → model name from primary + fallback specs.
	// "ollama/qwen3.5:35b-a3b" → providerModels["ollama"] = "qwen3.5:35b-a3b"
	providerModels := make(map[string]string)
	if cfg.Primary != "" {
		prov, model := splitModelSpec(cfg.Primary)
		if model != "" {
			providerModels[prov] = model
		}
	}
	for _, fb := range cfg.Fallbacks {
		prov, model := splitModelSpec(fb)
		if model != "" {
			if _, exists := providerModels[prov]; !exists {
				providerModels[prov] = model
			}
		}
	}

	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		client, err := NewClient(p)
		if err != nil {
			log.Warn().Err(err).Str("provider", p.Name).Msg("skipping provider")
			continue
		}
		clients[p.Name] = client

		// Use the model name from primary/fallback spec if available,
		// otherwise fall back to provider name (for direct model selection).
		modelName := p.Name
		if m, ok := providerModels[p.Name]; ok {
			modelName = m
		}

		targets = append(targets, RouteTarget{
			Model:    modelName,
			Provider: p.Name,
			IsLocal:  p.IsLocal,
			Cost:     p.CostPerOutputTok,
		})
	}

	if len(clients) == 0 {
		log.Warn().Msg("no LLM providers configured — inference will fail until a provider is added")
	}

	floor := 0.7
	if cfg.ConfidenceFloor > 0 {
		floor = cfg.ConfidenceFloor
	}

	bgw := cfg.BGWorker
	if bgw == nil {
		bgw = core.NewBackgroundWorker(16)
	}

	return &Service{
		providers:  clients,
		router:     NewRouter(targets, cfg.Router),
		breakers:   NewBreakerRegistry(cfg.Breaker),
		cache:      NewCache(cfg.Cache, store),
		dedup:      NewDedup(2000), // 2s dedup window
		transforms: DefaultTransformPipeline(),
		primary:    cfg.Primary,
		fallbacks:  cfg.Fallbacks,
		store:      store,
		bgWorker:   bgw,
		Confidence: NewConfidenceEvaluator(floor),
		Escalation: NewEscalationTracker(),
		quality:    NewQualityTracker(100),
	}, nil
}

// Complete sends a non-streaming request through the full pipeline.
func (s *Service) Complete(ctx context.Context, req *Request) (*Response, error) {
	// Dedup: collapse concurrent identical requests.
	dedupKey := hashRequest(req)
	return s.dedup.Do(ctx, dedupKey, func() (*Response, error) {
		return s.completeWithFallback(ctx, req)
	})
}

func (s *Service) completeWithFallback(ctx context.Context, req *Request) (*Response, error) {
	// Cache check.
	if !req.Stream {
		if cached := s.cache.Get(ctx, req); cached != nil {
			return cached, nil
		}
	}

	// Route: select model if not explicitly set.
	if req.Model == "" {
		target := s.router.Select(req)
		req.Model = target.Model
	}

	// Parse "provider/model" format: "ollama/qwen3.5:35b-a3b" → provider="ollama", model="qwen3.5:35b-a3b".
	providerHint, modelName := splitModelSpec(req.Model)
	if modelName != "" {
		req.Model = modelName
	}

	// Try primary provider, then fallbacks.
	providers := s.resolveProviderChain(providerHint)
	var lastErr error

	for _, providerName := range providers {
		client, ok := s.providers[providerName]
		if !ok {
			continue
		}

		cb := s.breakers.Get(providerName)
		if !cb.Allow() {
			log.Debug().Str("provider", providerName).Msg("circuit breaker open, trying next")
			continue
		}

		start := time.Now()
		resp, err := client.Complete(ctx, req)
		latencyMs := time.Since(start).Milliseconds()
		if err != nil {
			cb.RecordFailure()
			lastErr = err
			log.Warn().Err(err).Str("provider", providerName).Msg("provider failed, trying next")
			continue
		}

		cb.RecordSuccess()

		// Tiered inference: if the provider is local, evaluate confidence.
		// If confidence is too low and non-local providers are available, escalate.
		if client.provider.IsLocal && s.Confidence != nil {
			latency := time.Duration(latencyMs) * time.Millisecond
			if !s.Confidence.IsConfident(resp.Content, latency) {
				s.Escalation.RecordLocalEscalated()
				log.Info().
					Float64("confidence", s.Confidence.ConfidenceScore(resp.Content, latency)).
					Str("provider", providerName).
					Msg("local response below confidence floor, escalating to cloud")
				// Continue to next (non-local) provider.
				continue
			}
			s.Escalation.RecordLocalAccepted()
		} else {
			s.Escalation.RecordCloudDirect()
		}

		// Apply response transforms (strip <think> blocks, injection markers, etc.).
		if s.transforms != nil {
			resp.Content = s.transforms.Apply(resp.Content)
		}

		// Cache the successful response.
		s.cache.Put(ctx, req, resp)

		// Record quality observation for model routing feedback.
		if s.quality != nil {
			s.quality.Record(resp.Model, qualityFromResponse(resp))
		}

		// Record cost asynchronously via tracked worker pool.
		pName := providerName
		s.bgWorker.Submit("recordCost", func(ctx context.Context) {
			s.recordCost(ctx, pName, resp)
		})

		return resp, nil
	}

	if lastErr != nil {
		return nil, core.WrapError(core.ErrLLM, "all providers failed", lastErr)
	}
	return nil, core.NewError(core.ErrLLM, "no providers available")
}

// Stream sends a streaming request through the pipeline. Returns chunk and
// error channels. The chunk channel closes when streaming completes.
func (s *Service) Stream(ctx context.Context, req *Request) (<-chan StreamChunk, <-chan error) {
	req.Stream = true

	// Cache check: if we have a cached response, emit it as a single chunk.
	if cached := s.cache.Get(ctx, req); cached != nil {
		chunks := make(chan StreamChunk, 1)
		errs := make(chan error)
		chunks <- StreamChunk{Delta: cached.Content, FinishReason: "stop"}
		close(chunks)
		close(errs)
		return chunks, errs
	}

	// Route if needed.
	if req.Model == "" {
		target := s.router.Select(req)
		req.Model = target.Model
	}

	// Parse "provider/model" format.
	providerHint, modelName := splitModelSpec(req.Model)
	if modelName != "" {
		req.Model = modelName
	}

	providers := s.resolveProviderChain(providerHint)

	for _, providerName := range providers {
		client, ok := s.providers[providerName]
		if !ok {
			continue
		}

		cb := s.breakers.Get(providerName)
		if !cb.Allow() {
			continue
		}

		chunks, errs := client.Stream(ctx, req)

		// Wrap to track circuit breaker state and cache the full response.
		outChunks, outErrs := s.wrapStreamBreaker(ctx, chunks, errs, cb, providerName)
		outChunks = s.wrapStreamCache(ctx, outChunks, req)
		return outChunks, outErrs
	}

	// No providers available.
	chunks := make(chan StreamChunk)
	errs := make(chan error, 1)
	close(chunks)
	errs <- core.NewError(core.ErrLLM, "no providers available for streaming")
	close(errs)
	return chunks, errs
}

// wrapStreamCache accumulates streamed chunks and caches the full response.
func (s *Service) wrapStreamCache(ctx context.Context, in <-chan StreamChunk, req *Request) <-chan StreamChunk {
	out := make(chan StreamChunk, 32)
	go func() {
		defer close(out)
		var full strings.Builder
		for chunk := range core.OrDone(ctx.Done(), in) {
			full.WriteString(chunk.Delta)
			select {
			case out <- chunk:
			case <-ctx.Done():
				return
			}
		}
		if content := full.String(); content != "" {
			s.cache.Put(ctx, req, &Response{Content: content})
		}
	}()
	return out
}

// wrapStreamBreaker wraps a stream to record circuit breaker state.
// It owns the original errs channel and returns a new one to prevent data races.
func (s *Service) wrapStreamBreaker(ctx context.Context, in <-chan StreamChunk, errs <-chan error, cb *CircuitBreaker, provider string) (<-chan StreamChunk, <-chan error) {
	out := make(chan StreamChunk, 32)
	outErrs := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(outErrs)
		gotChunk := false

		for chunk := range core.OrDone(ctx.Done(), in) {
			if !gotChunk {
				cb.RecordSuccess()
				gotChunk = true
			}
			select {
			case out <- chunk:
			case <-ctx.Done():
				return
			}
		}

		// Drain the original error channel (single reader).
		select {
		case err := <-errs:
			if err != nil {
				if !gotChunk {
					cb.RecordFailure()
				}
				log.Warn().Err(err).Str("provider", provider).Msg("stream failed")
				outErrs <- err
			}
		default:
		}
	}()
	return out, outErrs
}

// resolveProviderChain returns the ordered list of providers to try.
// splitModelSpec parses "provider/model" format into (provider, model).
// If there's no slash, returns (spec, "") — the spec is treated as a provider name.
func splitModelSpec(spec string) (provider, model string) {
	if i := strings.Index(spec, "/"); i >= 0 {
		return spec[:i], spec[i+1:]
	}
	return spec, ""
}

func (s *Service) resolveProviderChain(providerHint string) []string {
	var chain []string

	// If the hint matches a provider name directly, use it first.
	if _, ok := s.providers[providerHint]; ok {
		chain = append(chain, providerHint)
	}

	// Add primary provider (extracted from "provider/model" format).
	if s.primary != "" {
		primaryProvider, _ := splitModelSpec(s.primary)
		if !contains(chain, primaryProvider) {
			if _, ok := s.providers[primaryProvider]; ok {
				chain = append(chain, primaryProvider)
			}
		}
	}

	// Add fallback providers.
	for _, fb := range s.fallbacks {
		fbProvider, _ := splitModelSpec(fb)
		if !contains(chain, fbProvider) {
			if _, ok := s.providers[fbProvider]; ok {
				chain = append(chain, fbProvider)
			}
		}
	}

	// Add any remaining providers as last resort.
	for name := range s.providers {
		if !contains(chain, name) {
			chain = append(chain, name)
		}
	}

	return chain
}

// recordCost logs inference cost to the database for analytics.
func (s *Service) recordCost(ctx context.Context, providerName string, resp *Response) {
	if s.store == nil {
		return
	}

	client, ok := s.providers[providerName]
	if !ok {
		return
	}

	cost := resp.Usage.Cost(client.provider)
	_, _ = s.store.ExecContext(ctx,
		`INSERT INTO inference_costs (id, model, provider, tokens_in, tokens_out, cost, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, datetime('now'))`,
		fmt.Sprintf("%s-%d", resp.ID, resp.Usage.OutputTokens),
		resp.Model, providerName,
		resp.Usage.InputTokens, resp.Usage.OutputTokens, cost,
	)
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// ProviderStatus reports the health of each configured provider.
type ProviderStatus struct {
	Name    string       `json:"name"`
	State   CircuitState `json:"state"`
	Format  APIFormat    `json:"format"`
	IsLocal bool         `json:"is_local"`
}

// ResetBreaker resets the circuit breaker for a named provider.
// Returns an error if the provider does not exist.
func (s *Service) ResetBreaker(providerName string) error {
	if _, ok := s.providers[providerName]; !ok {
		return fmt.Errorf("unknown provider: %s", providerName)
	}
	s.breakers.Get(providerName).Reset()
	return nil
}

// ForceOpenBreaker force-opens the circuit breaker for a named provider.
// Unlike normal open, this is only cleared by an explicit Reset call.
func (s *Service) ForceOpenBreaker(providerName string) error {
	if _, ok := s.providers[providerName]; !ok {
		return fmt.Errorf("unknown provider: %s", providerName)
	}
	s.breakers.Get(providerName).ForceOpen()
	return nil
}

// Router returns the service's model router for external use (e.g., routing eval).
func (s *Service) Router() *Router {
	return s.router
}

// Status returns the health of all providers (for /api/health).
// providers is write-once (set only in NewService), so concurrent reads are safe.
func (s *Service) Status() []ProviderStatus {
	var statuses []ProviderStatus

	for name, client := range s.providers {
		cb := s.breakers.Get(name)
		statuses = append(statuses, ProviderStatus{
			Name:    name,
			State:   cb.State(),
			Format:  client.provider.Format,
			IsLocal: client.provider.IsLocal,
		})
	}
	return statuses
}
