// policy_ingest.go exposes Manager.IngestPolicyDocument as a thin,
// constrained agent tool. The design rules (from the Milestone 3 follow-on
// discussion) are intentionally strict:
//
//   - Explicit content only — the agent cannot invent policy text. Callers
//     must pass the content verbatim.
//   - Explicit metadata — category, key, and source_label are all required.
//     effective_date stays NULL unless the caller supplies it; ingestion
//     time is never silently substituted for authority time.
//   - Canonical status is caller-asserted, never inferred. Canonical=true
//     additionally requires version or effective_date and an asserter_id
//     that is NOT the ingesting agent's own identity. The tool therefore
//     cannot auto-mark agent-generated text as canonical.
//   - Silent overwrite is rejected. To replace an existing policy, the
//     caller must either set replace_prior_version=true or supply a
//     strictly-higher version.
//
// The tool is registered as RiskDangerous because it writes to semantic
// memory's authority layer; approval / gate policy should treat it
// accordingly.

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	agentmemory "roboticus/internal/agent/memory"
	"roboticus/internal/db"
)

// PolicyIngestTool wraps Manager.IngestPolicyDocument. AgentIdentity is the
// ID the tool blocks from being used as an asserter for canonical claims,
// preventing the agent from marking its own output as authoritative.
type PolicyIngestTool struct {
	store         *db.Store
	agentIdentity string
}

// NewPolicyIngestTool constructs the tool. agentIdentity is typically the
// running agent's ID; canonical assertions that name it will be rejected
// by the underlying Manager guardrail.
func NewPolicyIngestTool(store *db.Store, agentIdentity string) *PolicyIngestTool {
	return &PolicyIngestTool{store: store, agentIdentity: strings.TrimSpace(agentIdentity)}
}

// Name satisfies Tool.
func (t *PolicyIngestTool) Name() string { return "ingest_policy" }

// Description satisfies Tool.
func (t *PolicyIngestTool) Description() string {
	return "Ingest a policy, spec, or runbook into semantic memory with explicit " +
		"provenance. Category, key, content, and source_label are required. " +
		"canonical=true requires version OR effective_date AND asserter_id. " +
		"Replacing an existing row requires replace_prior_version=true OR a " +
		"strictly-higher version. The tool never invents content, never " +
		"infers authority, and never lets the calling agent mark its own " +
		"output as canonical."
}

// Risk satisfies Tool. The tool writes to the authority layer — treat as
// dangerous so approval / gate policy engages.
func (t *PolicyIngestTool) Risk() RiskLevel { return RiskDangerous }

// ParameterSchema satisfies Tool.
func (t *PolicyIngestTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"category": {
				"type": "string",
				"description": "Policy grouping (e.g., 'policy', 'spec', 'runbook'). Required."
			},
			"key": {
				"type": "string",
				"description": "Stable identifier within the category. Required."
			},
			"content": {
				"type": "string",
				"description": "Verbatim policy text. Required. The agent MUST NOT paraphrase or extract."
			},
			"source_label": {
				"type": "string",
				"description": "Opaque source identifier the caller is vouching for. Required."
			},
			"version": {
				"type": "integer",
				"description": "Declared revision number; 0 or omitted means unversioned."
			},
			"effective_date": {
				"type": "string",
				"description": "When the policy became authoritative in the real world (ISO 8601 / YYYY-MM-DD). Empty means unknown — it will NOT be silently defaulted to now."
			},
			"canonical": {
				"type": "boolean",
				"description": "Assert canonical authority. Requires version or effective_date AND asserter_id. Defaults to false."
			},
			"asserter_id": {
				"type": "string",
				"description": "Who (or what) is making the canonical claim. Required when canonical=true. Cannot be the ingesting agent."
			},
			"replace_prior_version": {
				"type": "boolean",
				"description": "Explicit consent to replace an existing (category, key) row. Not needed when passing a strictly-higher version or promoting to canonical."
			}
		},
		"required": ["category", "key", "content", "source_label"]
	}`)
}

type policyIngestArgs struct {
	Category            string `json:"category"`
	Key                 string `json:"key"`
	Content             string `json:"content"`
	SourceLabel         string `json:"source_label"`
	Version             int    `json:"version"`
	EffectiveDate       string `json:"effective_date"`
	Canonical           bool   `json:"canonical"`
	AsserterID          string `json:"asserter_id"`
	ReplacePriorVersion bool   `json:"replace_prior_version"`
}

type policyIngestResult struct {
	OK               bool   `json:"ok"`
	ID               string `json:"id,omitempty"`
	PriorID          string `json:"prior_id,omitempty"`
	Superseded       bool   `json:"superseded"`
	EffectiveDate    string `json:"effective_date,omitempty"`
	PersistedVersion int    `json:"persisted_version"`
	Canonical        bool   `json:"canonical"`
	Summary          string `json:"summary"`
	Rejection        string `json:"rejection,omitempty"`
}

// Execute satisfies Tool.
func (t *PolicyIngestTool) Execute(ctx context.Context, params string, _ *Context) (*Result, error) {
	if t.store == nil {
		return &Result{Output: "policy store is not available"}, nil
	}
	var args policyIngestArgs
	if err := json.Unmarshal([]byte(params), &args); err != nil {
		return &Result{Output: "invalid arguments: " + err.Error()}, nil
	}

	// DisallowedAsserterIDs carries the agent's own identity so the
	// Manager rejects canonical=true with asserter_id matching the agent.
	// This is the "never let the agent mark its own output canonical" rule.
	var disallowed []string
	if t.agentIdentity != "" {
		disallowed = []string{t.agentIdentity}
	}

	mm := agentmemory.NewManager(agentmemory.DefaultConfig(), t.store)
	res, err := mm.IngestPolicyDocument(ctx, agentmemory.PolicyIngestionInput{
		Category:              args.Category,
		Key:                   args.Key,
		Content:               args.Content,
		SourceLabel:           args.SourceLabel,
		Version:               args.Version,
		EffectiveDate:         args.EffectiveDate,
		Canonical:             args.Canonical,
		AsserterID:            args.AsserterID,
		ReplacePriorVersion:   args.ReplacePriorVersion,
		DisallowedAsserterIDs: disallowed,
	})
	if err != nil {
		return marshalPolicyResult(policyIngestResult{
			OK:        false,
			Summary:   "rejected: " + err.Error(),
			Rejection: err.Error(),
		})
	}

	summary := fmt.Sprintf("ingested %s/%s v%d", args.Category, args.Key, res.PersistedVersion)
	if res.Superseded {
		summary += fmt.Sprintf(" (superseded prior id %s)", res.PriorID)
	}
	if res.Canonical {
		summary += " [canonical]"
	}
	return marshalPolicyResult(policyIngestResult{
		OK:               true,
		ID:               res.ID,
		PriorID:          res.PriorID,
		Superseded:       res.Superseded,
		EffectiveDate:    res.EffectiveDate,
		PersistedVersion: res.PersistedVersion,
		Canonical:        res.Canonical,
		Summary:          summary,
	})
}

func marshalPolicyResult(r policyIngestResult) (*Result, error) {
	buf, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return &Result{Output: "failed to marshal policy ingest result: " + err.Error()}, nil
	}
	return &Result{Output: string(buf), Source: "builtin"}, nil
}
