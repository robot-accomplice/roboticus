package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
	"roboticus/internal/db"
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
	providers     map[string]*Client
	router        *Router
	breakers      *BreakerRegistry
	cache         *Cache
	dedup         *Dedup
	transforms    *TransformPipeline
	primary       string   // primary model name
	fallbacks     []string // fallback model names
	store         *db.Store
	bgWorker      *core.BackgroundWorker
	Confidence    *ConfidenceEvaluator
	Escalation    *EscalationTracker
	quality       *QualityTracker
	intentQuality *IntentQualityTracker
	latency       *LatencyTracker
	errBus        *core.ErrorBus
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
	ErrBus          *core.ErrorBus         // centralized error reporting
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

		// Only add routing targets for providers that appear in the primary
		// or fallback specs. Matching Rust: ordered_models is built from
		// primary + fallbacks only — providers not in that list are never
		// routing candidates (they can still be reached via explicit
		// "provider/model" requests).
		if modelName, ok := providerModels[p.Name]; ok {
			targets = append(targets, RouteTarget{
				Model:    modelName,
				Provider: p.Name,
				IsLocal:  p.IsLocal,
				Cost:     p.CostPerOutputTok,
			})
		}
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

	svc := &Service{
		providers:     clients,
		router:        NewRouter(targets, cfg.Router),
		breakers:      NewBreakerRegistry(cfg.Breaker),
		cache:         NewCache(cfg.Cache, store, cfg.ErrBus),
		dedup:         NewDedup(2000), // 2s dedup window
		transforms:    DefaultTransformPipeline(),
		primary:       cfg.Primary,
		fallbacks:     cfg.Fallbacks,
		store:         store,
		bgWorker:      bgw,
		Confidence:    NewConfidenceEvaluator(floor),
		Escalation:    NewEscalationTracker(),
		quality:       NewQualityTracker(100),
		intentQuality: NewIntentQualityTracker(100),
		latency:       NewLatencyTracker(100),
		errBus:        cfg.ErrBus,
	}

	// Metascore routing is always enabled when the service has quality/latency
	// tracking (which it always does). This ensures every code path that creates
	// a Service — daemon, API server, tests — gets metascore routing without
	// requiring explicit wiring at each call site.
	svc.router.EnableMetascoreRouting(svc.quality, svc.latency, nil, svc.breakers)

	return svc, nil
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
	// Cache check. Skip during exercise/baseline (NoEscalate) — we need
	// fresh inference to measure actual model performance.
	if !req.Stream && !req.NoEscalate {
		if cached := s.cache.Get(ctx, req); cached != nil {
			return cached, nil
		}
	}

	// Context-level model override (set by pipeline when API caller specifies a model).
	if override := core.ModelOverrideFromCtx(ctx); override != "" && req.Model == "" {
		req.Model = override
	}

	// Route: select model if not explicitly set.
	if req.Model == "" {
		target := s.router.Select(req)
		req.Model = ModelSpecForTarget(target)
	}
	// Fall back to configured primary if routing didn't produce a model.
	if req.Model == "" && s.primary != "" {
		req.Model = s.primary
	}

	// Build the provider/model chain: primary first, then each fallback
	// with its OWN provider and model name. Each entry is a (provider, model)
	// pair so we send the right model name to each provider.
	//
	// Matching Rust behavior: primary and fallbacks are expected in
	// "provider/model" format (e.g., "openai/gpt-4o"). Bare names without
	// a slash are handled gracefully: if the name matches a known provider,
	// it's used as the provider with the current model; otherwise it's
	// treated as a bare model name and tried on all registered providers.
	type providerModel struct {
		provider string
		model    string
	}
	var chain []providerModel
	var primaryModel string

	// Primary: parse "provider/model" from req.Model.
	primaryProvider, primaryModelParsed := splitModelSpec(req.Model)
	if primaryModelParsed != "" {
		// Explicit "provider/model" format — use as-is.
		primaryModel = primaryModelParsed
		chain = append(chain, providerModel{primaryProvider, primaryModel})
	} else {
		// No slash — bare name. Could be a provider name or a model name.
		if _, ok := s.providers[primaryProvider]; ok {
			// Known provider — use it with its own name as model (unusual case).
			primaryModel = primaryProvider
			chain = append(chain, providerModel{primaryProvider, primaryModel})
		} else {
			// Not a known provider — treat as bare model name, try all providers.
			primaryModel = req.Model
			for name := range s.providers {
				chain = append(chain, providerModel{name, primaryModel})
			}
		}
	}

	// Fallbacks: each has its own provider/model pair.
	for _, fb := range s.fallbacks {
		fbProvider, fbModel := splitModelSpec(fb)
		if fbModel != "" {
			// Explicit "provider/model" — use as-is.
		} else {
			// Bare name — if it's a known provider, use it with the primary model.
			if _, ok := s.providers[fbProvider]; ok {
				fbModel = primaryModel
			} else {
				// Unknown provider, no model — skip.
				continue
			}
		}
		// Deduplicate: skip if already in chain.
		dup := false
		for _, existing := range chain {
			if existing.provider == fbProvider && existing.model == fbModel {
				dup = true
				break
			}
		}
		if !dup {
			chain = append(chain, providerModel{fbProvider, fbModel})
		}
	}

	// If chain is still empty, try all providers with the original model name.
	if len(chain) == 0 {
		for name := range s.providers {
			chain = append(chain, providerModel{name, req.Model})
		}
	}

	var lastErr error

	log.Debug().Int("chain_len", len(chain)).Str("model", req.Model).Msg("inference chain built")

	for _, pm := range chain {
		client, ok := s.providers[pm.provider]
		if !ok {
			continue
		}

		cb := s.breakers.Get(pm.provider)
		if !cb.Allow() {
			log.Warn().Str("provider", pm.provider).Msg("circuit breaker open, trying next")
			continue
		}

		// Set the model for this provider.
		inferReq := *req
		inferReq.Model = pm.model

		log.Info().
			Str("provider", pm.provider).
			Str("model", pm.model).
			Int("tools", len(inferReq.Tools)).
			Str("format", string(client.provider.Format)).
			Msg("sending inference request")

		start := time.Now()
		resp, err := client.Complete(ctx, &inferReq)
		latencyMs := time.Since(start).Milliseconds()
		if err != nil {
			// Distinguish permanent errors from transient failures.
			// Credit and auth errors permanently trip the breaker — these
			// won't self-heal between requests.
			if errors.Is(err, core.ErrCreditExhausted) {
				cb.RecordCreditError()
				log.Error().Str("provider", pm.provider).Msg("provider credit exhausted — circuit breaker tripped permanently")
			} else if errors.Is(err, core.ErrUnauthorized) {
				cb.RecordCreditError() // Same permanent trip — no key means no recovery.
				log.Error().Str("provider", pm.provider).Msg("provider unauthorized — circuit breaker tripped permanently (missing or invalid API key)")
			} else {
				cb.RecordFailure()
			}
			lastErr = err
			log.Warn().Err(err).Str("provider", pm.provider).Str("model", pm.model).Msg("provider failed, trying next")
			continue
		}

		cb.RecordSuccess()

		// Tag response with provider metadata so the pipeline can make
		// routing decisions (confidence evaluation, escalation) at its layer.
		resp.Provider = pm.provider
		resp.IsLocal = client.provider.IsLocal
		resp.LatencyMs = latencyMs

		// Apply response transforms (strip <think> blocks, injection markers, etc.).
		if s.transforms != nil {
			resp.Content = s.transforms.Apply(resp.Content)
		}

		// Cache the successful response (skip during exercise/baseline).
		if !req.NoEscalate {
			s.cache.Put(ctx, req, resp)
		}

		// Record quality and latency observations for model routing feedback.
		qScore := qualityFromResponse(resp)
		if s.quality != nil {
			s.quality.Record(resp.Model, qScore)
		}
		// Also record with intent context for per-(model, intent) quality cells.
		if s.intentQuality != nil && req.IntentClass != "" {
			s.intentQuality.RecordWithIntent(resp.Model, req.IntentClass, qScore)
		}
		if s.latency != nil {
			s.latency.Record(resp.Model, latencyMs)
		}

		// Record cost asynchronously via tracked worker pool.
		pName := pm.provider
		s.bgWorker.Submit("recordCost", func(ctx context.Context) {
			s.recordCost(ctx, pName, resp)
		})

		log.Info().Str("provider", pm.provider).Str("model", resp.Model).Int("tokens_in", resp.Usage.InputTokens).Int("tokens_out", resp.Usage.OutputTokens).Int64("latency_ms", latencyMs).Msg("inference completed")
		return resp, nil
	}

	if lastErr != nil {
		log.Error().Err(lastErr).Msg("all LLM providers failed")
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
		req.Model = ModelSpecForTarget(target)
	}
	// Fall back to configured primary if routing didn't produce a model.
	if req.Model == "" && s.primary != "" {
		req.Model = s.primary
	}

	// Build provider/model chain using the same logic as completeWithFallback.
	// Streaming uses the same cascade but without confidence escalation (Rust parity).
	type streamPM struct {
		provider string
		model    string
	}
	var streamChain []streamPM
	var streamPrimaryModel string

	sProv, sModel := splitModelSpec(req.Model)
	if sModel != "" {
		streamPrimaryModel = sModel
		streamChain = append(streamChain, streamPM{sProv, sModel})
	} else {
		if _, ok := s.providers[sProv]; ok {
			streamPrimaryModel = sProv
			streamChain = append(streamChain, streamPM{sProv, sProv})
		} else {
			streamPrimaryModel = req.Model
			for name := range s.providers {
				streamChain = append(streamChain, streamPM{name, streamPrimaryModel})
			}
		}
	}
	for _, fb := range s.fallbacks {
		fbProv, fbModel := splitModelSpec(fb)
		if fbModel == "" {
			if _, ok := s.providers[fbProv]; ok {
				fbModel = streamPrimaryModel
			} else {
				continue
			}
		}
		dup := false
		for _, ex := range streamChain {
			if ex.provider == fbProv && ex.model == fbModel {
				dup = true
				break
			}
		}
		if !dup {
			streamChain = append(streamChain, streamPM{fbProv, fbModel})
		}
	}
	if len(streamChain) == 0 {
		for name := range s.providers {
			streamChain = append(streamChain, streamPM{name, req.Model})
		}
	}

	for _, pm := range streamChain {
		client, ok := s.providers[pm.provider]
		if !ok {
			continue
		}

		cb := s.breakers.Get(pm.provider)
		if !cb.Allow() {
			continue
		}

		// Set the correct model for this provider.
		streamReq := *req
		streamReq.Model = pm.model
		chunks, errs := client.Stream(ctx, &streamReq)

		// Wrap to track circuit breaker state and cache the full response.
		outChunks, outErrs := s.wrapStreamBreaker(ctx, chunks, errs, cb, pm.provider)
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

// IntentQuality returns the intent-class quality tracker for external access.
func (s *Service) IntentQuality() *IntentQualityTracker {
	if s == nil {
		return nil
	}
	return s.intentQuality
}

// Quality returns the quality tracker for external access (e.g., startup seeding).
func (s *Service) Quality() *QualityTracker {
	if s == nil {
		return nil
	}
	return s.quality
}

// Latency returns the latency tracker for external access.
func (s *Service) Latency() *LatencyTracker {
	if s == nil {
		return nil
	}
	return s.latency
}

// SeedStartup warms both quality and latency trackers from DB history.
// Called once at daemon startup.
func (s *Service) SeedStartup(ctx context.Context, store *db.Store) {
	if s == nil {
		return
	}
	if s.quality != nil {
		s.quality.SeedFromHistory(ctx, store)
	}
	if s.latency != nil {
		s.latency.SeedFromHistory(ctx, store)
	}

	// Log cold-start models that may need baselining.
	models := []string{s.primary}
	models = append(models, s.fallbacks...)
	for _, m := range models {
		if m == "" {
			continue
		}
		hasQ := s.quality != nil && s.quality.HasObservations(m)
		hasL := s.latency != nil && s.latency.HasObservations(m)
		if !hasQ && !hasL {
			log.Info().Str("model", m).Msg("model has no performance data — run 'roboticus models exercise' to baseline it")
		}
	}
}

// ResetQualityScores clears metascore quality observations. When model is empty,
// all observations are removed.
func (s *Service) ResetQualityScores(model string) int {
	if s == nil || s.quality == nil {
		return 0
	}
	if strings.TrimSpace(model) == "" {
		return s.quality.ClearAll()
	}
	return s.quality.ClearModel(model)
}

// Drain waits for all background workers (cost recording, etc.) to complete.
// Call in tests to prevent TempDir cleanup races.
func (s *Service) Drain(timeout time.Duration) {
	if s != nil && s.bgWorker != nil {
		s.bgWorker.Drain(timeout)
	}
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

// splitModelSpec parses "provider/model" format into (provider, model).
// If there's no slash, returns (spec, "") — the full spec as provider, empty model.
// Callers must check whether the returned "provider" is actually a registered
// provider name or a bare model name (see completeWithFallback for the pattern).
func splitModelSpec(spec string) (provider, model string) {
	if i := strings.Index(spec, "/"); i >= 0 {
		return spec[:i], spec[i+1:]
	}
	return spec, ""
}

// resolveProviderChain returns the ordered list of provider names to try.
// Note: this returns a flat list of providers, NOT (provider, model) pairs.
// For inference, use the chain-building logic in completeWithFallback/Stream
// which properly pairs each provider with its correct model name.
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

// CostMetadata holds additional metadata for cost recording.
// Populated by the pipeline and passed through to recordCost.
type CostMetadata struct {
	TurnID    string
	Latency   int64   // milliseconds
	Quality   float64 // 0–1
	Escalated bool
	Cached    bool
	Tier      string  // routing tier (e.g., "local", "cloud", "premium")
}

// recordCost logs inference cost to the database for analytics.
// Populates all Rust-parity fields: model, provider, tokens, cost,
// latency_ms, quality_score, escalation, turn_id, cached.
func (s *Service) recordCost(ctx context.Context, providerName string, resp *Response) {
	s.recordCostWithMeta(ctx, providerName, resp, CostMetadata{})
}

// recordCostWithMeta logs inference cost with full metadata (Rust parity).
func (s *Service) recordCostWithMeta(ctx context.Context, providerName string, resp *Response, meta CostMetadata) {
	if s.store == nil {
		return
	}

	client, ok := s.providers[providerName]
	if !ok {
		return
	}

	cost := resp.Usage.Cost(client.provider)
	escalated := 0
	if meta.Escalated {
		escalated = 1
	}
	cached := 0
	if meta.Cached {
		cached = 1
	}
	// Determine tier from provider metadata if not explicitly set.
	tier := meta.Tier
	if tier == "" && client.provider.IsLocal {
		tier = "local"
	} else if tier == "" {
		tier = "cloud"
	}

	_, err := s.store.ExecContext(ctx,
		`INSERT INTO inference_costs (id, model, provider, tokens_in, tokens_out, cost,
		 tier, latency_ms, quality_score, escalation, turn_id, cached, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		fmt.Sprintf("%s-%d", resp.ID, resp.Usage.OutputTokens),
		resp.Model, providerName,
		resp.Usage.InputTokens, resp.Usage.OutputTokens, cost,
		tier, meta.Latency, meta.Quality, escalated, meta.TurnID, cached,
	)
	if err != nil {
		s.errBus.ReportEvent(core.ErrorEvent{
			Subsystem: "llm",
			Op:        "record_cost",
			Err:       err,
			Severity:  core.SevWarning,
			Model:     resp.Model,
			Metadata:  map[string]string{"provider": providerName, "turn_id": meta.TurnID},
		})
	}
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// ModelSpecForTarget formats a RouteTarget as a "provider/model" spec string.
func ModelSpecForTarget(target RouteTarget) string {
	if target.Provider != "" && target.Model != "" && !strings.Contains(target.Model, "/") {
		return target.Provider + "/" + target.Model
	}
	if target.Model != "" {
		return target.Model
	}
	return target.Provider
}

// RecordModelSelection persists a model selection event to the database.
// Matches Rust's record_model_selection_event.
func (s *Service) RecordModelSelection(ctx context.Context, turnID, sessionID, agentID, channel, selectedModel, strategy, userExcerpt string) {
	if s.store == nil {
		return
	}
	primary := s.primary
	excerpt := userExcerpt
	if len(excerpt) > 200 {
		excerpt = excerpt[:200]
	}
	if _, err := s.store.ExecContext(ctx,
		`INSERT INTO model_selection_events (id, turn_id, session_id, agent_id, channel, selected_model, strategy, primary_model, user_excerpt, candidates_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '[]', datetime('now'))`,
		fmt.Sprintf("mse-%s", turnID), turnID, sessionID, agentID, channel,
		selectedModel, strategy, primary, excerpt,
	); err != nil {
		s.errBus.ReportIfErr(err, "llm", "record_selection_event", core.SevDebug)
	}
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

// Primary returns the configured primary model name.
func (s *Service) Primary() string {
	return s.primary
}

// Breakers returns the circuit breaker registry for metascore routing.
func (s *Service) Breakers() *BreakerRegistry {
	return s.breakers
}

// CapacityTracker returns nil — capacity is tracked per-provider in the router, not the service.
// Metascore routing works without capacity data (headroom defaults to 1.0).
func (s *Service) CapacityTracker() *CapacityTracker {
	return nil
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
