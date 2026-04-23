package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/hostresources"
	"roboticus/internal/llm"
)

type modelPolicyUpsertRequest struct {
	Model             string   `json:"model"`
	State             string   `json:"state"`
	PrimaryReasonCode string   `json:"primary_reason_code,omitempty"`
	ReasonCodes       []string `json:"reason_codes,omitempty"`
	HumanReason       string   `json:"human_reason,omitempty"`
	EvidenceRefs      []string `json:"evidence_refs,omitempty"`
	Source            string   `json:"source,omitempty"`
}

func snapshotPtr(s hostresources.Snapshot) *hostresources.Snapshot {
	if s.Empty() {
		return nil
	}
	out := s
	return &out
}

func effectiveModelPolicies(ctx context.Context, store *db.Store, cfg *core.Config) map[string]llm.ModelPolicy {
	if cfg == nil {
		return nil
	}
	return llm.EffectiveModelPolicies(ctx, store, cfg.Models.Policy)
}

func benchmarkBlockedModels(models []string, policies map[string]llm.ModelPolicy) []string {
	blocked := make([]string, 0)
	for _, model := range models {
		policy := llm.EffectiveModelPolicy([]string{model}, policies)
		if len(policy) == 0 {
			continue
		}
		if !policy[0].BenchmarkEligible {
			blocked = append(blocked, model)
		}
	}
	return blocked
}

// ListModelPolicies returns persisted and effective model lifecycle policy.
func ListModelPolicies(store *db.Store, cfg *core.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		persisted := db.ListModelPolicies(r.Context(), store)
		effective := effectiveModelPolicies(r.Context(), store, cfg)
		writeJSON(w, http.StatusOK, map[string]any{
			"persisted": persisted,
			"effective": effective,
		})
	}
}

// UpsertModelPolicy persists a model lifecycle policy and returns the merged effective map.
func UpsertModelPolicy(store *db.Store, cfg *core.Config, llmSvc *llm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req modelPolicyUpsertRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if strings.TrimSpace(req.Model) == "" {
			writeError(w, http.StatusBadRequest, "model is required")
			return
		}
		state := strings.TrimSpace(req.State)
		switch state {
		case llm.ModelStateEnabled, llm.ModelStateNiche, llm.ModelStateDisabled, llm.ModelStateBenchmarkOnly:
		default:
			writeError(w, http.StatusBadRequest, "state must be enabled, niche, disabled, or benchmark_only")
			return
		}
		if err := db.UpsertModelPolicy(r.Context(), store, db.ModelPolicyRow{
			Model:             req.Model,
			State:             state,
			PrimaryReasonCode: req.PrimaryReasonCode,
			ReasonCodes:       append([]string(nil), req.ReasonCodes...),
			HumanReason:       req.HumanReason,
			EvidenceRefs:      append([]string(nil), req.EvidenceRefs...),
			Source:            req.Source,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to persist model policy")
			return
		}
		effective := effectiveModelPolicies(r.Context(), store, cfg)
		if llmSvc != nil {
			llmSvc.ApplyModelPolicies(effective)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":        true,
			"effective": effective,
		})
	}
}

// DeleteModelPolicy removes a persisted model lifecycle policy override.
func DeleteModelPolicy(store *db.Store, cfg *core.Config, llmSvc *llm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		model := strings.TrimSpace(r.URL.Query().Get("model"))
		if model == "" {
			writeError(w, http.StatusBadRequest, "model is required")
			return
		}
		if err := db.DeleteModelPolicy(r.Context(), store, model); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to delete model policy")
			return
		}
		effective := effectiveModelPolicies(r.Context(), store, cfg)
		if llmSvc != nil {
			llmSvc.ApplyModelPolicies(effective)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":        true,
			"effective": effective,
		})
	}
}
