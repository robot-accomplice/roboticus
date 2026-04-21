package db

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/rs/zerolog/log"
)

// ModelPolicyRow is the durable policy record for one model.
type ModelPolicyRow struct {
	Model             string   `json:"model"`
	State             string   `json:"state"`
	PrimaryReasonCode string   `json:"primary_reason_code,omitempty"`
	ReasonCodes       []string `json:"reason_codes,omitempty"`
	HumanReason       string   `json:"human_reason,omitempty"`
	EvidenceRefs      []string `json:"evidence_refs,omitempty"`
	Source            string   `json:"source,omitempty"`
	CreatedAt         string   `json:"created_at,omitempty"`
	UpdatedAt         string   `json:"updated_at,omitempty"`
}

// UpsertModelPolicy persists or updates a model lifecycle policy.
func UpsertModelPolicy(ctx context.Context, store *Store, row ModelPolicyRow) error {
	reasonsJSON, err := json.Marshal(row.ReasonCodes)
	if err != nil {
		return err
	}
	evidenceJSON, err := json.Marshal(row.EvidenceRefs)
	if err != nil {
		return err
	}
	_, err = store.ExecContext(ctx,
		`INSERT INTO model_policies (
			model, state, primary_reason_code, reason_codes_json, human_reason, evidence_refs_json, source
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(model) DO UPDATE SET
			state = excluded.state,
			primary_reason_code = excluded.primary_reason_code,
			reason_codes_json = excluded.reason_codes_json,
			human_reason = excluded.human_reason,
			evidence_refs_json = excluded.evidence_refs_json,
			source = excluded.source,
			updated_at = datetime('now')`,
		strings.TrimSpace(row.Model), strings.TrimSpace(row.State), strings.TrimSpace(row.PrimaryReasonCode),
		string(reasonsJSON), strings.TrimSpace(row.HumanReason), string(evidenceJSON), strings.TrimSpace(row.Source),
	)
	if err != nil {
		log.Warn().Err(err).Str("model", row.Model).Msg("model_policy: upsert failed")
	}
	return err
}

// DeleteModelPolicy removes a persisted model lifecycle policy.
func DeleteModelPolicy(ctx context.Context, store *Store, model string) error {
	_, err := store.ExecContext(ctx, `DELETE FROM model_policies WHERE model = ?`, strings.TrimSpace(model))
	if err != nil {
		log.Warn().Err(err).Str("model", model).Msg("model_policy: delete failed")
	}
	return err
}

// ListModelPolicies returns all persisted model lifecycle policies.
func ListModelPolicies(ctx context.Context, store *Store) []ModelPolicyRow {
	rows, err := store.QueryContext(ctx,
		`SELECT model, state, COALESCE(primary_reason_code, ''), COALESCE(reason_codes_json, '[]'),
		        COALESCE(human_reason, ''), COALESCE(evidence_refs_json, '[]'), COALESCE(source, ''),
		        created_at, updated_at
		   FROM model_policies
		  ORDER BY model`)
	if err != nil {
		log.Warn().Err(err).Msg("model_policy: list query failed")
		return nil
	}
	defer func() { _ = rows.Close() }()

	var out []ModelPolicyRow
	for rows.Next() {
		var (
			row          ModelPolicyRow
			reasonsJSON  string
			evidenceJSON string
		)
		if err := rows.Scan(
			&row.Model, &row.State, &row.PrimaryReasonCode, &reasonsJSON,
			&row.HumanReason, &evidenceJSON, &row.Source, &row.CreatedAt, &row.UpdatedAt,
		); err != nil {
			continue
		}
		_ = json.Unmarshal([]byte(reasonsJSON), &row.ReasonCodes)
		_ = json.Unmarshal([]byte(evidenceJSON), &row.EvidenceRefs)
		out = append(out, row)
	}
	return out
}
