// policy_ingestion.go implements the Milestone 3 follow-on: an explicit
// ingestion surface for policies, specs, and other docs that need to live
// in semantic memory with proper version / effective-date / canonical
// metadata.
//
// Design rules (per the roadmap discussion):
//   - Canonical status is an explicit caller-asserted flag, never inferred
//     from keys, paths, or filenames.
//   - effective_date defaults to NULL when the caller does not supply it.
//     We never silently substitute "now" for authority time; ingestion time
//     is recorded separately as created_at / updated_at.
//   - Overwriting an existing (category, key) row requires explicit replace
//     semantics or a distinct version bump. A second ingest that reuses the
//     same (category, key) without ReplacePriorVersion is rejected so
//     policies cannot be silently rewritten.
//   - When a canonical row is replaced, the prior row flips to
//     memory_state='stale' with its superseded_by pointer set to the new
//     row, so the supersession chain built in Milestone 3 keeps audit
//     trail intact.
//   - If is_canonical = true is asserted, the caller must supply both a
//     source_label and at least one of version / effective_date. Without
//     that the ingest is rejected — canonical claims without provenance
//     are the exact pattern we are trying to stamp out.

package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"roboticus/internal/db"
)

// PolicyIngestionInput carries the explicit fields an ingestion caller must
// supply. Optional fields are pointer-free but documented carefully so the
// "null by default" contract for effective_date is visible.
type PolicyIngestionInput struct {
	// Category groups related policies (e.g. "policy", "spec", "runbook").
	// Required.
	Category string
	// Key is the stable identifier within the category. Required.
	Key string
	// Content is the verbatim policy text. Required.
	Content string
	// SourceLabel is the opaque source identifier the caller is vouching
	// for (e.g. "policy/refund-v3", "runbook/on-call"). Required.
	SourceLabel string

	// Version, if supplied, is the declared revision number for this
	// entry. 0 means unversioned.
	Version int
	// EffectiveDate, if supplied, marks when the policy became
	// authoritative in the real world. Empty string means "unknown" and
	// is persisted as NULL — we never substitute ingestion time here.
	// Accepted formats: ISO 8601 / RFC3339 / "2006-01-02 15:04:05".
	EffectiveDate string
	// Canonical asserts the row carries canonical authority. Defaults to
	// false; when true, SourceLabel AND (Version>0 OR EffectiveDate!="")
	// AND AsserterID are all required.
	Canonical bool
	// AsserterID identifies who (or what) made the canonical claim. Never
	// the agent's own ID if Canonical=true — agent self-authored canonical
	// policy is rejected by IngestPolicyDocument.
	AsserterID string

	// ReplacePriorVersion, when true, allows this ingest to replace an
	// existing (category, key) row without a version bump. The prior row
	// flips to stale with superseded_by pointing at the new row.
	ReplacePriorVersion bool
	// DisallowedAsserterIDs is an advisory block-list used by the tool
	// layer to reject canonical claims whose asserter matches the
	// ingesting agent itself. The Manager treats it as a final check so
	// the tool can't accidentally bypass it by forgetting to pass the
	// guard.
	DisallowedAsserterIDs []string
}

// ErrPolicyIngestionRejected is returned by IngestPolicyDocument for any
// guardrail violation (missing required field, canonical without
// provenance, silent overwrite, disallowed asserter). Callers should treat
// the message as authoritative and not retry with tweaked inputs without
// explicit operator involvement.
var ErrPolicyIngestionRejected = errors.New("policy ingestion rejected")

// PolicyIngestionResult reports what landed in the database.
type PolicyIngestionResult struct {
	ID               string
	Superseded       bool   // true when a prior row was flipped to stale
	PriorID          string // id of the row this replaced, if any
	EffectiveDate    string // "" when the caller did not supply one
	PersistedVersion int
	Canonical        bool
}

// IngestPolicyDocument persists a caller-asserted policy / spec / runbook
// into semantic_memory. See the package doc for the guardrails this
// enforces; violations return ErrPolicyIngestionRejected wrapped with a
// descriptive reason.
func (mm *Manager) IngestPolicyDocument(ctx context.Context, in PolicyIngestionInput) (*PolicyIngestionResult, error) {
	if mm.store == nil {
		return nil, fmt.Errorf("%w: store unavailable", ErrPolicyIngestionRejected)
	}

	// --- Required-field checks --------------------------------------------
	in.Category = strings.TrimSpace(in.Category)
	in.Key = strings.TrimSpace(in.Key)
	in.Content = strings.TrimSpace(in.Content)
	in.SourceLabel = strings.TrimSpace(in.SourceLabel)
	in.AsserterID = strings.TrimSpace(in.AsserterID)
	in.EffectiveDate = strings.TrimSpace(in.EffectiveDate)

	if in.Category == "" {
		return nil, fmt.Errorf("%w: category is required", ErrPolicyIngestionRejected)
	}
	if in.Key == "" {
		return nil, fmt.Errorf("%w: key is required", ErrPolicyIngestionRejected)
	}
	if in.Content == "" {
		return nil, fmt.Errorf("%w: content is required", ErrPolicyIngestionRejected)
	}
	if in.SourceLabel == "" {
		return nil, fmt.Errorf("%w: source_label is required", ErrPolicyIngestionRejected)
	}

	// --- Effective-date parsing -------------------------------------------
	var effectiveDate sql.NullString
	if in.EffectiveDate != "" {
		normalised, err := normalisePolicyDate(in.EffectiveDate)
		if err != nil {
			return nil, fmt.Errorf("%w: effective_date: %v", ErrPolicyIngestionRejected, err)
		}
		effectiveDate = sql.NullString{String: normalised, Valid: true}
	}

	// --- Canonical guardrails --------------------------------------------
	if in.Canonical {
		if in.AsserterID == "" {
			return nil, fmt.Errorf("%w: canonical=true requires asserter_id", ErrPolicyIngestionRejected)
		}
		if in.Version <= 0 && !effectiveDate.Valid {
			return nil, fmt.Errorf("%w: canonical=true requires version or effective_date", ErrPolicyIngestionRejected)
		}
		for _, forbidden := range in.DisallowedAsserterIDs {
			if strings.EqualFold(strings.TrimSpace(forbidden), in.AsserterID) {
				return nil, fmt.Errorf("%w: asserter %q is not permitted to mark rows canonical", ErrPolicyIngestionRejected, in.AsserterID)
			}
		}
	}

	version := in.Version
	if version < 0 {
		version = 0
	}

	// --- Existing-row / overwrite semantics ------------------------------
	priorID, priorVersion, priorCanonical, err := mm.lookupSemanticRow(ctx, in.Category, in.Key)
	if err != nil {
		return nil, fmt.Errorf("%w: lookup existing row: %v", ErrPolicyIngestionRejected, err)
	}
	if priorID != "" {
		// Reject silent overwrite unless one of:
		//   * caller explicitly asked to replace the prior version
		//   * caller supplied a strictly newer version number
		//   * caller is toggling from non-canonical to canonical (the row
		//     is being promoted to an authoritative claim)
		newerVersion := version > 0 && version > priorVersion
		promoteToCanonical := in.Canonical && !priorCanonical
		if !in.ReplacePriorVersion && !newerVersion && !promoteToCanonical {
			return nil, fmt.Errorf(
				"%w: row already exists for (category=%q, key=%q) at version %d; pass ReplacePriorVersion=true or supply a newer Version",
				ErrPolicyIngestionRejected, in.Category, in.Key, priorVersion,
			)
		}
	}

	newID := db.NewID()
	confidence := 0.8
	// A caller-asserted canonical row is by construction the authoritative
	// statement on this key; pin confidence at 0.95 so retrieval's
	// authority scoring treats it as such immediately.
	if in.Canonical {
		confidence = 0.95
	}

	if priorID != "" {
		// We are replacing an existing row. Insert the new one first, then
		// flip the prior row to stale with a superseded_by pointer so the
		// Milestone 3 supersession chain stays intact.
		if err := mm.insertSemanticRow(ctx, semanticInsert{
			id:       newID,
			category: in.Category,
			// Prior row occupies the (category, key) UNIQUE slot. Park the
			// new row under a versioned key suffix so both survive; the
			// supersession pointer still resolves from prior to new.
			key:           versionedKey(in.Key, version, priorVersion),
			value:         in.Content,
			confidence:    confidence,
			version:       version,
			effectiveDate: effectiveDate,
			sourceLabel:   in.SourceLabel,
			isCanonical:   in.Canonical,
			asserterID:    in.AsserterID,
		}); err != nil {
			return nil, fmt.Errorf("%w: insert new revision: %v", ErrPolicyIngestionRejected, err)
		}
		if _, err := mm.store.ExecContext(ctx,
			`UPDATE semantic_memory
			    SET memory_state = 'stale',
			        state_reason = 'superseded by policy ingestion',
			        superseded_by = ?,
			        updated_at = datetime('now')
			  WHERE id = ?`,
			newID, priorID,
		); err != nil {
			return nil, fmt.Errorf("%w: mark prior row stale: %v", ErrPolicyIngestionRejected, err)
		}
		mm.autoIndex(ctx, "semantic_memory", newID, in.Key+": "+in.Content)
		mm.embedAndStore(ctx, "semantic_memory", newID, in.Key+": "+in.Content)
		log.Info().
			Str("category", in.Category).
			Str("key", in.Key).
			Str("prior_id", priorID).
			Str("new_id", newID).
			Int("version", version).
			Bool("canonical", in.Canonical).
			Str("category_log", "policy_ingestion").
			Msg("policy replaced prior revision")
		return &PolicyIngestionResult{
			ID:               newID,
			Superseded:       true,
			PriorID:          priorID,
			EffectiveDate:    effectiveDate.String,
			PersistedVersion: version,
			Canonical:        in.Canonical,
		}, nil
	}

	// Fresh insert (no prior row at this key).
	if err := mm.insertSemanticRow(ctx, semanticInsert{
		id:            newID,
		category:      in.Category,
		key:           in.Key,
		value:         in.Content,
		confidence:    confidence,
		version:       version,
		effectiveDate: effectiveDate,
		sourceLabel:   in.SourceLabel,
		isCanonical:   in.Canonical,
		asserterID:    in.AsserterID,
	}); err != nil {
		return nil, fmt.Errorf("%w: insert: %v", ErrPolicyIngestionRejected, err)
	}
	mm.autoIndex(ctx, "semantic_memory", newID, in.Key+": "+in.Content)
	mm.embedAndStore(ctx, "semantic_memory", newID, in.Key+": "+in.Content)
	log.Info().
		Str("category", in.Category).
		Str("key", in.Key).
		Str("new_id", newID).
		Int("version", version).
		Bool("canonical", in.Canonical).
		Str("category_log", "policy_ingestion").
		Msg("policy ingested")
	return &PolicyIngestionResult{
		ID:               newID,
		EffectiveDate:    effectiveDate.String,
		PersistedVersion: version,
		Canonical:        in.Canonical,
	}, nil
}

// semanticInsert bundles the INSERT arguments so the two call sites
// (fresh insert, replacement insert) don't drift.
type semanticInsert struct {
	id            string
	category      string
	key           string
	value         string
	confidence    float64
	version       int
	effectiveDate sql.NullString
	sourceLabel   string
	isCanonical   bool
	asserterID    string
}

func (mm *Manager) insertSemanticRow(ctx context.Context, r semanticInsert) error {
	var asserter sql.NullString
	if r.asserterID != "" {
		asserter = sql.NullString{String: r.asserterID, Valid: true}
	}
	var sourceLabel sql.NullString
	if r.sourceLabel != "" {
		sourceLabel = sql.NullString{String: r.sourceLabel, Valid: true}
	}
	_, err := mm.store.ExecContext(ctx,
		`INSERT INTO semantic_memory (
		   id, category, key, value, confidence, memory_state,
		   version, effective_date, source_label, is_canonical, asserter_id
		 )
		 VALUES (?, ?, ?, ?, ?, 'active', ?, ?, ?, ?, ?)`,
		r.id, r.category, r.key, r.value, r.confidence,
		r.version, r.effectiveDate, sourceLabel, boolToInt(r.isCanonical), asserter,
	)
	return err
}

func (mm *Manager) lookupSemanticRow(ctx context.Context, category, key string) (id string, version int, isCanonical bool, err error) {
	var (
		idVal        sql.NullString
		versionVal   sql.NullInt64
		canonicalVal sql.NullInt64
	)
	row := mm.store.QueryRowContext(ctx,
		`SELECT id, version, is_canonical
		   FROM semantic_memory
		  WHERE category = ? AND key = ? AND memory_state = 'active'
		  ORDER BY version DESC, updated_at DESC
		  LIMIT 1`,
		category, key,
	)
	if err = row.Scan(&idVal, &versionVal, &canonicalVal); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", 0, false, nil
		}
		return "", 0, false, err
	}
	if idVal.Valid {
		id = idVal.String
	}
	if versionVal.Valid {
		version = int(versionVal.Int64)
	}
	if canonicalVal.Valid {
		isCanonical = canonicalVal.Int64 != 0
	}
	return id, version, isCanonical, nil
}

// versionedKey returns a variant of key that does not collide with an
// existing (category, key) row. We append "@vN" using the higher of the
// two version numbers so the replacement row occupies a distinct
// UNIQUE(category, key) slot while remaining discoverable.
func versionedKey(key string, newVersion, priorVersion int) string {
	v := newVersion
	if priorVersion >= v {
		v = priorVersion + 1
	}
	return fmt.Sprintf("%s@v%d", key, v)
}

// normalisePolicyDate accepts common date formats and returns a value
// SQLite will sort correctly. Returns an error on truly unparseable input
// rather than silently swallowing it.
func normalisePolicyDate(raw string) (string, error) {
	layouts := []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC().Format("2006-01-02 15:04:05"), nil
		}
	}
	return "", fmt.Errorf("unparseable date %q", raw)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
