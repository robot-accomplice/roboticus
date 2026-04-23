// retrieval_path_telemetry.go is the M3.3 dormancy-establishment surface.
//
// M3.2 emits "retrieval.path.<tier>" annotations on every covered tier;
// M3.3 retires the LIKE safety net only after evidence shows it is no
// longer doing meaningful work. This file is the evidence-gathering tool:
// it reads persisted pipeline_traces.stages_json, parses each memory_retrieval
// span's metadata for retrieval.path keys, and reports per-tier path
// distributions. Operators run the aggregator before deciding to remove a
// LIKE block — if the like_fallback share for a tier is at or below the
// retirement threshold over a meaningful observation window, that tier's
// safety net has been earned its way out of the codebase.
//
// Why a tally function rather than a streaming counter? Two reasons:
//   1. We want to inspect historical traces, not just live ones — operators
//      need to look back over days/weeks of production behavior, not the
//      current process uptime.
//   2. The per-tier retire decision is empirical and one-shot, not
//      something the runtime needs continuously. A query-on-demand
//      aggregator is the right shape: cheap to call (one SELECT, JSON
//      parse), produces a deterministic answer, and stays out of the hot
//      path entirely.
//
// The dormancy threshold (RetrievalPathRetirementThreshold) is opinionated:
// 0.01 means a tier is "dormant" when fewer than 1% of its measured
// retrievals fell back to LIKE. The threshold lives in code rather than
// config because the M3.3 deletion is a one-time action — once a tier's
// LIKE block is gone, the threshold no longer governs anything for that
// tier. Tightening it later only matters for any tier whose LIKE block
// hasn't been retired yet.

package memory

import (
	"context"
	"encoding/json"
	"sort"

	"roboticus/internal/db"
)

// RetrievalPathRetirementThreshold is the maximum fraction of measured
// retrievals that may fall through to the LIKE safety net for a tier to
// qualify as "dormant" and become eligible for LIKE-block removal under
// M3.3. Set conservatively at 1% — a tier observed at this rate or
// lower across a meaningful sample is genuinely no longer using the
// safety net.
const RetrievalPathRetirementThreshold = 0.01

// RetrievalPathTierStats summarises one tier's path distribution over
// the observation window.
type RetrievalPathTierStats struct {
	Tier            string         `json:"tier"`
	TotalMeasured   int            `json:"total_measured"`
	CountsByPath    map[string]int `json:"counts_by_path"`
	LikeFallbackPct float64        `json:"like_fallback_pct"` // 0.0 - 1.0
	IsDormant       bool           `json:"is_dormant"`        // LikeFallbackPct ≤ retirement threshold AND TotalMeasured ≥ minimum sample
}

// RetrievalPathDistribution is the aggregated view across all covered
// tiers within an observation window.
type RetrievalPathDistribution struct {
	TracesScanned int                                `json:"traces_scanned"`
	Tiers         map[string]*RetrievalPathTierStats `json:"tiers"`
}

// minSampleForDormancy is the minimum TotalMeasured before IsDormant can
// even be set to true. With too few observations a tier could trivially
// look "dormant" simply because it was barely queried — the gate must
// be evidence-backed, not an artifact of small samples.
const minSampleForDormancy = 200

// AggregateRetrievalPaths scans the most recent `limit` pipeline_traces
// rows and tallies retrieval.path.<tier> values across them. Returns the
// per-tier distribution plus a derived IsDormant flag operators consult
// before removing a tier's LIKE block.
//
// The function is deliberately read-only and side-effect free; it can be
// called from a dashboard route, a one-shot CLI command, or a periodic
// audit job without coordination.
func AggregateRetrievalPaths(ctx context.Context, store *db.Store, limit int) (*RetrievalPathDistribution, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := store.QueryContext(ctx,
		`SELECT stages_json FROM pipeline_traces
		  ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	dist := &RetrievalPathDistribution{
		Tiers: make(map[string]*RetrievalPathTierStats),
	}

	for rows.Next() {
		var stagesJSON string
		if err := rows.Scan(&stagesJSON); err != nil {
			continue
		}
		dist.TracesScanned++
		extractRetrievalPathsFromTrace(stagesJSON, dist)
	}

	for tier, stats := range dist.Tiers {
		if stats.TotalMeasured == 0 {
			continue
		}
		stats.LikeFallbackPct = float64(stats.CountsByPath[RetrievalPathLikeFallback]) / float64(stats.TotalMeasured)
		stats.IsDormant = stats.TotalMeasured >= minSampleForDormancy &&
			stats.LikeFallbackPct <= RetrievalPathRetirementThreshold
		stats.Tier = tier // ensure populated for json output
	}

	return dist, nil
}

// extractRetrievalPathsFromTrace walks a single trace's stages_json,
// inspects each span's metadata for retrieval.path.<tier> keys, and
// updates the distribution accumulator.
//
// We tolerate any shape of stages_json that survives JSON parsing —
// historical traces may have been written by an older recorder that lacked
// some fields. Spans without metadata are skipped silently; only the
// retrieval-path keys are interesting here.
func extractRetrievalPathsFromTrace(stagesJSON string, dist *RetrievalPathDistribution) {
	if stagesJSON == "" {
		return
	}
	var stages []struct {
		Metadata map[string]any `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(stagesJSON), &stages); err != nil {
		return
	}
	for _, span := range stages {
		if len(span.Metadata) == 0 {
			continue
		}
		for key, value := range span.Metadata {
			tier, ok := tierFromRetrievalPathKey(key)
			if !ok {
				continue
			}
			pathStr, ok := value.(string)
			if !ok || pathStr == "" {
				continue
			}
			stats, exists := dist.Tiers[tier]
			if !exists {
				stats = &RetrievalPathTierStats{
					Tier:         tier,
					CountsByPath: make(map[string]int),
				}
				dist.Tiers[tier] = stats
			}
			stats.TotalMeasured++
			stats.CountsByPath[pathStr]++
		}
	}
}

// tierFromRetrievalPathKey returns the tier suffix of a retrieval.path.<tier>
// annotation key, or "", false if the key isn't a retrieval-path annotation.
// Centralised here so the prefix string lives in exactly one place
// (alongside retrievalPathKey in retrieval_path.go).
func tierFromRetrievalPathKey(key string) (string, bool) {
	const prefix = "retrieval.path."
	if len(key) <= len(prefix) || key[:len(prefix)] != prefix {
		return "", false
	}
	return key[len(prefix):], true
}

// SortedTiers returns the per-tier stats ordered by tier name so callers
// (dashboards, reports) get deterministic output. Returns an empty slice
// when no tiers were observed.
func (d *RetrievalPathDistribution) SortedTiers() []*RetrievalPathTierStats {
	if d == nil {
		return nil
	}
	out := make([]*RetrievalPathTierStats, 0, len(d.Tiers))
	for _, stats := range d.Tiers {
		out = append(out, stats)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tier < out[j].Tier })
	return out
}
