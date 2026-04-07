package routes

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/llm"
)

// GetCosts returns per-request cost rows for the dashboard cost table and charts.
func GetCosts(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := parseIntParam(r, "limit", 500)
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, model, provider, cost, tokens_in, tokens_out, created_at, cached, COALESCE(latency_ms, 0)
			 FROM inference_costs ORDER BY created_at DESC LIMIT ?`, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query cost rows")
			return
		}
		defer func() { _ = rows.Close() }()

		costs := make([]map[string]any, 0)
		for rows.Next() {
			var id, model, provider, createdAt string
			var cost float64
			var tokensIn, tokensOut, cached, latencyMs int64
			if err := rows.Scan(&id, &model, &provider, &cost, &tokensIn, &tokensOut, &createdAt, &cached, &latencyMs); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read cost row")
				return
			}
			costs = append(costs, map[string]any{
				"id":         id,
				"model":      model,
				"provider":   provider,
				"cost":       cost,
				"tokens_in":  tokensIn,
				"tokens_out": tokensOut,
				"created_at": createdAt,
				"cached":     cached == 1,
				"latency_ms": latencyMs,
				"error":      nil,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"costs": costs})
	}
}

// GetCacheStats returns semantic cache statistics including hit_rate.
func GetCacheStats(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		row := store.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM semantic_cache`)
		var count int64
		if err := row.Scan(&count); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Compute hit_rate from inference_costs: ratio of cached requests to total.
		hitRate := 0.0
		costRow := store.QueryRowContext(r.Context(),
			`SELECT COUNT(*), COALESCE(SUM(CASE WHEN cached = 1 THEN 1 ELSE 0 END), 0) FROM inference_costs`)
		var total, cached int64
		if err := costRow.Scan(&total, &cached); err == nil && total > 0 {
			hitRate = float64(cached) / float64(total)
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"cached_entries": count,
			"hit_rate":       hitRate,
		})
	}
}

// GetTransactions returns recent financial transactions.
func GetTransactions(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := parseIntParam(r, "limit", 50)
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, tx_type, amount, currency, counterparty, tx_hash, created_at
			 FROM transactions ORDER BY created_at DESC LIMIT ?`, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query transactions")
			return
		}
		defer func() { _ = rows.Close() }()

		txns := make([]map[string]any, 0)
		for rows.Next() {
			var id, txType, currency, createdAt string
			var amount float64
			var counterparty, txHash *string
			if err := rows.Scan(&id, &txType, &amount, &currency, &counterparty, &txHash, &createdAt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read transaction row")
				return
			}
			t := map[string]any{
				"id": id, "tx_type": txType, "amount": amount,
				"currency": currency, "created_at": createdAt,
			}
			if counterparty != nil {
				t["counterparty"] = *counterparty
			}
			if txHash != nil {
				t["tx_hash"] = *txHash
			}
			txns = append(txns, t)
		}
		writeJSON(w, http.StatusOK, map[string]any{"transactions": txns})
	}
}

// GetCapacity returns provider capacity metrics from the LLM service.
// The dashboard expects sustained_hot, near_capacity, and headroom fields
// derived from the circuit breaker state for each provider.
func GetCapacity(llmSvc *llm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providers := make(map[string]any)
		if llmSvc != nil {
			for _, p := range llmSvc.Status() {
				// Map circuit breaker state to dashboard-expected capacity fields.
				sustainedHot := p.State == llm.CircuitOpen
				nearCapacity := p.State == llm.CircuitHalfOpen
				headroom := 1.0
				if sustainedHot {
					headroom = 0.0
				} else if nearCapacity {
					headroom = 0.25
				}

				var stateLabel string
				switch p.State {
				case llm.CircuitOpen:
					stateLabel = "open"
				case llm.CircuitHalfOpen:
					stateLabel = "half-open"
				default:
					stateLabel = "closed"
				}

				providers[p.Name] = map[string]any{
					"state":         stateLabel,
					"format":        p.Format,
					"is_local":      p.IsLocal,
					"sustained_hot": sustainedHot,
					"near_capacity": nearCapacity,
					"headroom":      headroom,
				}
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"providers": providers})
	}
}

// GetEfficiency returns efficiency metrics aggregated from inference_costs.
func GetEfficiency(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hours := parsePeriodHours(r.URL.Query().Get("period"), 24)
		ctx := r.Context()

		row := store.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(tokens_in + tokens_out), 0),
			        COALESCE(SUM(cost), 0),
			        COALESCE(AVG(latency_ms), 0),
			        COUNT(*),
			        COALESCE(SUM(CASE WHEN cached = 1 THEN 1 ELSE 0 END), 0)
			 FROM inference_costs
			 WHERE created_at >= datetime('now', ? || ' hours')`, strconv.Itoa(-hours))
		var totalTokens, count, cachedCount int64
		var totalCost, avgLatency float64
		if err := row.Scan(&totalTokens, &totalCost, &avgLatency, &count, &cachedCount); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query efficiency metrics")
			return
		}

		cacheRate := 0.0
		if count > 0 {
			cacheRate = float64(cachedCount) / float64(count) * 100.0
		}

		modelRows, err := store.QueryContext(ctx,
			`SELECT model, COUNT(*), COALESCE(SUM(cost), 0), COALESCE(SUM(tokens_in), 0), COALESCE(SUM(tokens_out), 0)
			 FROM inference_costs
			 WHERE created_at >= datetime('now', ? || ' hours')
			 GROUP BY model ORDER BY COUNT(*) DESC`, strconv.Itoa(-hours))
		models := make([]map[string]any, 0)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query efficiency breakdown")
			return
		}
		defer func() { _ = modelRows.Close() }()
		for modelRows.Next() {
			var model string
			var cnt, tokIn, tokOut int64
			var cost float64
			if err := modelRows.Scan(&model, &cnt, &cost, &tokIn, &tokOut); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read efficiency breakdown row")
				return
			}
			avgOut := 0.0
			costPerTurn := 0.0
			if cnt > 0 {
				avgOut = float64(tokOut) / float64(cnt)
				costPerTurn = cost / float64(cnt)
			}
			models = append(models, map[string]any{
				"model": model, "requests": cnt,
				"tokens_in": tokIn, "tokens_out": tokOut,
				"total_turns":        cnt,
				"cache_hit_rate":     0.0,
				"avg_output_tokens":  avgOut,
				"avg_output_density": 0.0,
				"cost": map[string]any{
					"total":              cost,
					"effective_per_turn": costPerTurn,
					"cache_savings":      0.0,
					"per_output_token":   func() float64 { if tokOut > 0 { return cost / float64(tokOut) }; return 0 }(),
					"cumulative_trend":   "stable",
					"attribution":        map[string]any{},
				},
				"trend": map[string]any{},
			})
		}

		// Convert models array to object keyed by model name (JS expects Object.keys(models)).
		modelsMap := make(map[string]any, len(models))
		var mostExpensiveModel, mostEfficientModel string
		var maxCost float64
		var minCostPerTurn float64 = -1
		for _, m := range models {
			name := m["model"].(string)
			modelsMap[name] = map[string]any{
				"total_turns":        m["total_turns"],
				"cache_hit_rate":     m["cache_hit_rate"],
				"avg_output_tokens":  m["avg_output_tokens"],
				"avg_output_density": m["avg_output_density"],
				"cost":               m["cost"],
			}
			costObj := m["cost"].(map[string]any)
			total := costObj["total"].(float64)
			ept := costObj["effective_per_turn"].(float64)
			if total > maxCost {
				maxCost = total
				mostExpensiveModel = name
			}
			if minCostPerTurn < 0 || ept < minCostPerTurn {
				minCostPerTurn = ept
				mostEfficientModel = name
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"period":         r.URL.Query().Get("period"),
			"total_tokens":   totalTokens,
			"total_cost":     totalCost,
			"cache_hit_rate": cacheRate,
			"avg_latency_ms": avgLatency,
			"requests":       count,
			"models":         modelsMap,
			"totals": map[string]any{
				"total_turns":          count,
				"total_cost":           totalCost,
				"most_expensive_model": mostExpensiveModel,
				"most_efficient_model": mostEfficientModel,
			},
		})
	}
}

// GetModelSelections returns model selection events for the decision graph.
func GetModelSelections(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := parseIntParam(r, "limit", 50)
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, turn_id, session_id, selected_model, strategy, complexity, candidates_json, created_at
			 FROM model_selection_events ORDER BY created_at DESC LIMIT ?`, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query model selections")
			return
		}
		defer func() { _ = rows.Close() }()

		events := make([]map[string]any, 0)
		for rows.Next() {
			var id, turnID, sessionID, model, strategy, createdAt string
			var complexity, candidatesJSON *string
			if err := rows.Scan(&id, &turnID, &sessionID, &model, &strategy, &complexity, &candidatesJSON, &createdAt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read model selection row")
				return
			}
			e := map[string]any{
				"id": id, "turn_id": turnID, "session_id": sessionID,
				"selected_model": model, "strategy": strategy, "created_at": createdAt,
			}
			if complexity != nil {
				e["complexity"] = *complexity
			}
			if candidatesJSON != nil {
				var candidates any
				if json.Unmarshal([]byte(*candidatesJSON), &candidates) == nil {
					e["candidates"] = candidates
				}
			}
			events = append(events, e)
		}
		writeJSON(w, http.StatusOK, map[string]any{"events": events})
	}
}

// GetRoutingDiagnostics returns routing config for the efficiency page.
func GetRoutingDiagnostics(cfg *core.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"config": map[string]any{
				"routing_mode": cfg.Models.Routing.Mode,
				"primary":      cfg.Models.Primary,
				"fallbacks":    cfg.Models.Fallback,
				"cost_aware":   cfg.Models.Routing.CostAware,
				"cost_weight":  cfg.Models.Routing.CostWeight,
			},
		})
	}
}

// GetRecommendations returns optimization recommendations.
func GetRecommendations(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hours := parsePeriodHours(r.URL.Query().Get("period"), 24)
		ctx := r.Context()

		row := store.QueryRowContext(ctx,
			`SELECT COUNT(*),
			        COALESCE(SUM(cost), 0),
			        COALESCE(AVG(latency_ms), 0),
			        COALESCE(SUM(CASE WHEN cached = 1 THEN 1 ELSE 0 END), 0),
			        COALESCE(SUM(tokens_in + tokens_out), 0)
			 FROM inference_costs
			 WHERE created_at >= datetime('now', ? || ' hours')`, strconv.Itoa(-hours))

		var requests, cachedCount, totalTokens int64
		var totalCost, avgLatency float64
		if err := row.Scan(&requests, &totalCost, &avgLatency, &cachedCount, &totalTokens); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to analyze recommendations")
			return
		}

		recommendations := make([]map[string]any, 0)
		cacheRate := 0.0
		if requests > 0 {
			cacheRate = float64(cachedCount) / float64(requests) * 100.0
		}
		if requests == 0 {
			recommendations = append(recommendations, map[string]any{
				"type":     "observability",
				"priority": "low",
				"message":  "No inference traffic recorded in the selected period; gather more traffic before tuning routing or cache settings.",
			})
		} else {
			if cacheRate < 20 {
				recommendations = append(recommendations, map[string]any{
					"type":     "cache",
					"priority": "medium",
					"message":  "Cache hit rate is low; review prompt normalization and cache TTLs to improve reuse.",
					"value":    cacheRate,
				})
			}
			if avgLatency > 1500 {
				recommendations = append(recommendations, map[string]any{
					"type":     "latency",
					"priority": "high",
					"message":  "Average latency is elevated; investigate provider health, fallback churn, and routing thresholds.",
					"value":    avgLatency,
				})
			}
			if requests > 0 && totalCost/float64(requests) > 0.02 {
				recommendations = append(recommendations, map[string]any{
					"type":     "cost",
					"priority": "medium",
					"message":  "Average cost per request is high; audit routed model choices and fallback usage.",
					"value":    totalCost / float64(requests),
				})
			}
			if requests > 0 && float64(totalTokens)/float64(requests) > 4000 {
				recommendations = append(recommendations, map[string]any{
					"type":     "context",
					"priority": "medium",
					"message":  "Average token volume is high; trim context windows or increase pruning for long-running sessions.",
					"value":    float64(totalTokens) / float64(requests),
				})
			}
		}

		// Transform recommendations to match JS expected shape.
		transformed := make([]map[string]any, 0, len(recommendations))
		for _, rec := range recommendations {
			priority := rec["priority"].(string)
			// Capitalize priority: "high" -> "High", "medium" -> "Medium", "low" -> "Low"
			if len(priority) > 0 {
				priority = strings.ToUpper(priority[:1]) + priority[1:]
			}
			title := rec["message"].(string)
			t := map[string]any{
				"title":            title,
				"category":         rec["type"],
				"priority":         priority,
				"explanation":      title,
				"action":           "",
				"estimated_impact": map[string]any{},
			}
			if v, ok := rec["value"]; ok {
				t["value"] = v
			}
			transformed = append(transformed, t)
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"recommendations": transformed,
			"count":           len(transformed),
			"period":          r.URL.Query().Get("period"),
			"profile": map[string]any{
				"total_turns":    requests,
				"total_cost":     totalCost,
				"cache_hit_rate": cacheRate,
			},
		})
	}
}

// GenerateRecommendations triggers deep analysis.
func GenerateRecommendations(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		GetRecommendations(store).ServeHTTP(w, r)
	}
}

// GetTimeseries returns time series data for overview sparklines.
func GetTimeseries(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		days := parseIntParam(r, "days", 7)
		rows, err := store.QueryContext(r.Context(),
			`SELECT strftime('%Y-%m-%dT%H:00:00', created_at) as bucket,
			        COUNT(*) as requests, COALESCE(SUM(cost), 0) as cost,
			        COALESCE(SUM(tokens_in + tokens_out), 0) as tokens
			 FROM inference_costs
			 WHERE created_at >= datetime('now', ? || ' days')
			 GROUP BY bucket ORDER BY bucket`, strconv.Itoa(-days))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query timeseries")
			return
		}
		defer func() { _ = rows.Close() }()

		var buckets, requests, costs, tokens []any
		for rows.Next() {
			var bucket string
			var reqCount, tokCount int64
			var cost float64
			if err := rows.Scan(&bucket, &reqCount, &cost, &tokCount); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read timeseries row")
				return
			}
			buckets = append(buckets, bucket)
			requests = append(requests, reqCount)
			costs = append(costs, cost)
			tokens = append(tokens, tokCount)
		}
		// Build empty placeholder arrays of the same length for fields we don't track yet.
		n := len(buckets)
		empty := make([]any, n)
		for i := range empty {
			empty[i] = 0
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"labels": buckets,
			"series": map[string]any{
				"cost_per_hour":     costs,
				"tokens_per_hour":   tokens,
				"requests_per_hour": requests,
				"latency_p50_ms":    empty,
				"sessions_per_hour": empty,
				"cron_success_rate": empty,
			},
		})
	}
}

// GetEscalationStats returns tiered inference escalation stats.
func GetEscalationStats(llmSvc *llm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if llmSvc != nil && llmSvc.Escalation != nil {
			stats := llmSvc.Escalation.Stats()
			stats["local_acceptance_rate_pct"] = int64(llmSvc.Escalation.LocalAcceptanceRate() * 100)
			stats["cache_hit_rate_pct"] = int64(llmSvc.Escalation.CacheHitRate() * 100)
			writeJSON(w, http.StatusOK, stats)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{})
	}
}
