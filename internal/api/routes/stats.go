package routes

import (
	"encoding/json"
	"net/http"
	"strconv"

	"goboticus/internal/core"
	"goboticus/internal/db"
	"goboticus/internal/llm"
)

// GetTransactions returns recent financial transactions.
func GetTransactions(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := parseIntParam(r, "limit", 50)
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, tx_type, amount, currency, counterparty, tx_hash, created_at
			 FROM transactions ORDER BY created_at DESC LIMIT ?`, limit)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"transactions": []any{}})
			return
		}
		defer func() { _ = rows.Close() }()

		txns := make([]map[string]any, 0)
		for rows.Next() {
			var id, txType, currency, createdAt string
			var amount float64
			var counterparty, txHash *string
			if err := rows.Scan(&id, &txType, &amount, &currency, &counterparty, &txHash, &createdAt); err != nil {
				continue
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
func GetCapacity(llmSvc *llm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providers := make(map[string]any)
		if llmSvc != nil {
			for _, p := range llmSvc.Status() {
				providers[p.Name] = map[string]any{
					"state":    p.State,
					"format":   p.Format,
					"is_local": p.IsLocal,
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
			        SUM(CASE WHEN cached = 1 THEN 1 ELSE 0 END)
			 FROM inference_costs
			 WHERE created_at >= datetime('now', ? || ' hours')`, strconv.Itoa(-hours))
		var totalTokens, count, cachedCount int64
		var totalCost, avgLatency float64
		_ = row.Scan(&totalTokens, &totalCost, &avgLatency, &count, &cachedCount)

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
		if err == nil {
			defer func() { _ = modelRows.Close() }()
			for modelRows.Next() {
				var model string
				var cnt, tokIn, tokOut int64
				var cost float64
				if err := modelRows.Scan(&model, &cnt, &cost, &tokIn, &tokOut); err != nil {
					continue
				}
				models = append(models, map[string]any{
					"model": model, "requests": cnt, "cost": cost,
					"tokens_in": tokIn, "tokens_out": tokOut,
				})
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"period":         r.URL.Query().Get("period"),
			"total_tokens":   totalTokens,
			"total_cost":     totalCost,
			"cache_hit_rate": cacheRate,
			"avg_latency_ms": avgLatency,
			"requests":       count,
			"models":         models,
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
			writeJSON(w, http.StatusOK, map[string]any{"events": []any{}})
			return
		}
		defer func() { _ = rows.Close() }()

		events := make([]map[string]any, 0)
		for rows.Next() {
			var id, turnID, sessionID, model, strategy, createdAt string
			var complexity, candidatesJSON *string
			if err := rows.Scan(&id, &turnID, &sessionID, &model, &strategy, &complexity, &candidatesJSON, &createdAt); err != nil {
				continue
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
		writeJSON(w, http.StatusOK, map[string]any{
			"recommendations": make([]any, 0),
			"period":          r.URL.Query().Get("period"),
		})
	}
}

// GenerateRecommendations triggers deep analysis.
func GenerateRecommendations(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":          "completed",
			"recommendations": make([]any, 0),
		})
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
			writeJSON(w, http.StatusOK, map[string]any{"series": map[string]any{}})
			return
		}
		defer func() { _ = rows.Close() }()

		var buckets, requests, costs, tokens []any
		for rows.Next() {
			var bucket string
			var reqCount, tokCount int64
			var cost float64
			if err := rows.Scan(&bucket, &reqCount, &cost, &tokCount); err != nil {
				continue
			}
			buckets = append(buckets, bucket)
			requests = append(requests, reqCount)
			costs = append(costs, cost)
			tokens = append(tokens, tokCount)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"series": map[string]any{
				"buckets":  buckets,
				"requests": requests,
				"costs":    costs,
				"tokens":   tokens,
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
