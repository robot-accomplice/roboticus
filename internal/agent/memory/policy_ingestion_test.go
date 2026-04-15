package memory

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"roboticus/testutil"
)

func TestIngestPolicyDocument_RequiresCoreFields(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	cases := []struct {
		name string
		in   PolicyIngestionInput
		want string // substring expected in the error
	}{
		{"missing category", PolicyIngestionInput{Key: "k", Content: "c", SourceLabel: "s"}, "category"},
		{"missing key", PolicyIngestionInput{Category: "policy", Content: "c", SourceLabel: "s"}, "key"},
		{"missing content", PolicyIngestionInput{Category: "policy", Key: "k", SourceLabel: "s"}, "content"},
		{"missing source_label", PolicyIngestionInput{Category: "policy", Key: "k", Content: "c"}, "source_label"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := mm.IngestPolicyDocument(ctx, tc.in)
			if err == nil {
				t.Fatalf("expected rejection for %s", tc.name)
			}
			if !errors.Is(err, ErrPolicyIngestionRejected) {
				t.Fatalf("expected ErrPolicyIngestionRejected, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error to mention %q, got %v", tc.want, err)
			}
		})
	}
}

func TestIngestPolicyDocument_EffectiveDateNullByDefault(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	res, err := mm.IngestPolicyDocument(ctx, PolicyIngestionInput{
		Category:    "policy",
		Key:         "refund_window",
		Content:     "30 days for unused items",
		SourceLabel: "policy/refund-v1",
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if res.EffectiveDate != "" {
		t.Fatalf("expected null effective_date by default, got %q", res.EffectiveDate)
	}
	var stored sql.NullString
	if err := store.QueryRowContext(ctx,
		`SELECT effective_date FROM semantic_memory WHERE id = ?`, res.ID,
	).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored.Valid {
		t.Fatalf("expected NULL effective_date in DB, got %q", stored.String)
	}
}

func TestIngestPolicyDocument_EffectiveDateParsedWhenSupplied(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	res, err := mm.IngestPolicyDocument(ctx, PolicyIngestionInput{
		Category:      "policy",
		Key:           "refund_window",
		Content:       "30 days for unused items",
		SourceLabel:   "policy/refund-v1",
		EffectiveDate: "2025-01-15",
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if !strings.HasPrefix(res.EffectiveDate, "2025-01-15") {
		t.Fatalf("expected parsed effective_date, got %q", res.EffectiveDate)
	}
}

func TestIngestPolicyDocument_CanonicalRequiresProvenance(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	// canonical=true but no AsserterID
	_, err := mm.IngestPolicyDocument(ctx, PolicyIngestionInput{
		Category: "policy", Key: "k", Content: "c", SourceLabel: "policy/x",
		Canonical: true, Version: 2,
	})
	if err == nil || !strings.Contains(err.Error(), "asserter_id") {
		t.Fatalf("expected canonical-without-asserter rejection, got %v", err)
	}

	// canonical=true with asserter but no version or date
	_, err = mm.IngestPolicyDocument(ctx, PolicyIngestionInput{
		Category: "policy", Key: "k", Content: "c", SourceLabel: "policy/x",
		Canonical: true, AsserterID: "ops-team",
	})
	if err == nil || !strings.Contains(err.Error(), "version or effective_date") {
		t.Fatalf("expected canonical-without-version-or-date rejection, got %v", err)
	}

	// canonical=true with full provenance succeeds
	res, err := mm.IngestPolicyDocument(ctx, PolicyIngestionInput{
		Category: "policy", Key: "k", Content: "c", SourceLabel: "policy/x",
		Canonical: true, AsserterID: "ops-team", Version: 2,
	})
	if err != nil {
		t.Fatalf("expected canonical with provenance to succeed, got %v", err)
	}
	if !res.Canonical {
		t.Fatal("expected result.Canonical=true")
	}

	var isCanonical int
	_ = store.QueryRowContext(ctx,
		`SELECT is_canonical FROM semantic_memory WHERE id = ?`, res.ID,
	).Scan(&isCanonical)
	if isCanonical != 1 {
		t.Fatalf("expected is_canonical=1 in DB, got %d", isCanonical)
	}
}

func TestIngestPolicyDocument_CanonicalRejectsDisallowedAsserter(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	_, err := mm.IngestPolicyDocument(ctx, PolicyIngestionInput{
		Category: "policy", Key: "k", Content: "c", SourceLabel: "policy/x",
		Canonical: true, AsserterID: "agent-42", Version: 1,
		DisallowedAsserterIDs: []string{"agent-42"},
	})
	if err == nil || !strings.Contains(err.Error(), "not permitted") {
		t.Fatalf("expected disallowed-asserter rejection, got %v", err)
	}
}

func TestIngestPolicyDocument_RejectsSilentOverwrite(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	_, err := mm.IngestPolicyDocument(ctx, PolicyIngestionInput{
		Category: "policy", Key: "k", Content: "v1", SourceLabel: "policy/x",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Second ingest at same (category, key) with no version bump, no
	// ReplacePriorVersion → must be rejected.
	_, err = mm.IngestPolicyDocument(ctx, PolicyIngestionInput{
		Category: "policy", Key: "k", Content: "v2", SourceLabel: "policy/x",
	})
	if err == nil || !strings.Contains(err.Error(), "row already exists") {
		t.Fatalf("expected silent-overwrite rejection, got %v", err)
	}
}

func TestIngestPolicyDocument_ReplaceWithExplicitFlagMarksPriorStale(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	first, err := mm.IngestPolicyDocument(ctx, PolicyIngestionInput{
		Category: "policy", Key: "k", Content: "v1", SourceLabel: "policy/x",
	})
	if err != nil {
		t.Fatal(err)
	}

	second, err := mm.IngestPolicyDocument(ctx, PolicyIngestionInput{
		Category: "policy", Key: "k", Content: "v2", SourceLabel: "policy/x",
		ReplacePriorVersion: true,
	})
	if err != nil {
		t.Fatalf("expected replace with explicit flag to succeed, got %v", err)
	}
	if !second.Superseded {
		t.Fatalf("expected Superseded=true on replacement, got %+v", second)
	}
	if second.PriorID != first.ID {
		t.Fatalf("expected PriorID to reference the first row, got %q vs %q", second.PriorID, first.ID)
	}

	// Prior row should be stale with superseded_by pointing at the new row.
	var state string
	var ptr sql.NullString
	_ = store.QueryRowContext(ctx,
		`SELECT memory_state, superseded_by FROM semantic_memory WHERE id = ?`, first.ID,
	).Scan(&state, &ptr)
	if state != "stale" {
		t.Fatalf("expected prior row stale, got %q", state)
	}
	if !ptr.Valid || ptr.String != second.ID {
		t.Fatalf("expected superseded_by to point at second row, got %+v", ptr)
	}

	// Chain-walker should reach the new row from the old one.
	rev, err := mm.CurrentSemanticValue(ctx, first.ID)
	if err != nil || rev == nil {
		t.Fatalf("walk chain: %v %+v", err, rev)
	}
	if rev.ID != second.ID {
		t.Fatalf("expected chain to reach new row, got %+v", rev)
	}
}

func TestIngestPolicyDocument_NewerVersionReplacesWithoutFlag(t *testing.T) {
	// Ingesting a strictly-higher Version at the same (category, key) is
	// allowed without ReplacePriorVersion — the version bump IS the
	// explicit replacement intent.
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	_, err := mm.IngestPolicyDocument(ctx, PolicyIngestionInput{
		Category: "policy", Key: "k", Content: "v1", SourceLabel: "policy/x", Version: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := mm.IngestPolicyDocument(ctx, PolicyIngestionInput{
		Category: "policy", Key: "k", Content: "v2", SourceLabel: "policy/x", Version: 2,
	})
	if err != nil {
		t.Fatalf("expected newer version to replace, got %v", err)
	}
	if !res.Superseded {
		t.Fatal("expected Superseded=true")
	}
	if res.PersistedVersion != 2 {
		t.Fatalf("expected PersistedVersion=2, got %d", res.PersistedVersion)
	}
}

func TestIngestPolicyDocument_PromotionToCanonicalAllowedWithoutFlag(t *testing.T) {
	// Promoting a non-canonical row to canonical is a meaningful
	// assertion; requiring ReplacePriorVersion on top would be
	// redundant. Allowed so long as canonical provenance is present.
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	_, err := mm.IngestPolicyDocument(ctx, PolicyIngestionInput{
		Category: "policy", Key: "k", Content: "v1", SourceLabel: "policy/x",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := mm.IngestPolicyDocument(ctx, PolicyIngestionInput{
		Category: "policy", Key: "k", Content: "v1", SourceLabel: "policy/x",
		Canonical: true, AsserterID: "ops-team", Version: 1,
	})
	if err != nil {
		t.Fatalf("expected promotion-to-canonical to succeed, got %v", err)
	}
	if !res.Canonical {
		t.Fatal("expected promotion result to be canonical")
	}
}

func TestIngestPolicyDocument_RejectsUnparseableDate(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	_, err := mm.IngestPolicyDocument(ctx, PolicyIngestionInput{
		Category: "policy", Key: "k", Content: "c", SourceLabel: "policy/x",
		EffectiveDate: "yesterday-ish",
	})
	if err == nil || !strings.Contains(err.Error(), "effective_date") {
		t.Fatalf("expected unparseable-date rejection, got %v", err)
	}
}
