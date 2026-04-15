// system_warnings_test.go pins the v1.0.6 system-warnings collector
// contract: dedupe on (Code, Detail), severity defaulting, snapshot
// independence from the live slice.
//
// What this is and isn't testing:
//   * The collector itself (this file) — yes
//   * Wiring from initConfig / db.Open into the collector — covered
//     elsewhere (init_warning_test.go and store_warnings_test.go) so
//     each layer's contract stays independently verifiable
//   * The HTTP route shape — covered in internal/api/routes/

package core

import (
	"testing"
	"time"
)

func TestAddSystemWarning_DedupesOnCodeAndDetail(t *testing.T) {
	ResetSystemWarningsForTest()
	defer ResetSystemWarningsForTest()

	w := SystemWarning{
		Code:   WarningCodeConfigDefaultsUsed,
		Title:  "Defaults used",
		Detail: "search path: /tmp/none",
	}
	AddSystemWarning(w)
	AddSystemWarning(w) // same Code AND same Detail → must dedupe
	AddSystemWarning(w) // third time still dedupe

	got := SystemWarningsSnapshot()
	if len(got) != 1 {
		t.Fatalf("expected 1 warning after 3 identical Add calls; got %d (%+v)", len(got), got)
	}
}

func TestAddSystemWarning_SameCodeDifferentDetailIsTwoEntries(t *testing.T) {
	ResetSystemWarningsForTest()
	defer ResetSystemWarningsForTest()

	AddSystemWarning(SystemWarning{
		Code:   WarningCodeDatabaseCreatedAtPath,
		Title:  "DB created",
		Detail: "/tmp/a.db",
	})
	AddSystemWarning(SystemWarning{
		Code:   WarningCodeDatabaseCreatedAtPath,
		Title:  "DB created",
		Detail: "/tmp/b.db",
	})

	got := SystemWarningsSnapshot()
	if len(got) != 2 {
		t.Fatalf("expected 2 warnings (same Code, different Detail); got %d", len(got))
	}
}

func TestAddSystemWarning_DefaultsSeverityAndTimestamp(t *testing.T) {
	ResetSystemWarningsForTest()
	defer ResetSystemWarningsForTest()

	before := time.Now()
	AddSystemWarning(SystemWarning{
		Code:   WarningCodeConfigDefaultsUsed,
		Title:  "Test",
		Detail: "Test detail",
		// Severity and RaisedAt deliberately omitted.
	})
	after := time.Now()

	got := SystemWarningsSnapshot()
	if len(got) != 1 {
		t.Fatalf("expected 1 warning; got %d", len(got))
	}
	w := got[0]
	if w.Severity != SystemWarningSeverityNormal {
		t.Fatalf("expected default severity %q; got %q", SystemWarningSeverityNormal, w.Severity)
	}
	if w.RaisedAt.Before(before) || w.RaisedAt.After(after) {
		t.Fatalf("expected RaisedAt within [%v, %v]; got %v", before, after, w.RaisedAt)
	}
}

func TestAddSystemWarning_PreservesExplicitSeverity(t *testing.T) {
	ResetSystemWarningsForTest()
	defer ResetSystemWarningsForTest()

	AddSystemWarning(SystemWarning{
		Code:     WarningCodeConfigDefaultsUsed,
		Title:    "High severity",
		Severity: SystemWarningSeverityHigh,
	})
	got := SystemWarningsSnapshot()
	if len(got) != 1 || got[0].Severity != SystemWarningSeverityHigh {
		t.Fatalf("expected explicit high severity to be preserved; got %+v", got)
	}
}

func TestAddSystemWarning_RejectsEmptyCode(t *testing.T) {
	ResetSystemWarningsForTest()
	defer ResetSystemWarningsForTest()

	AddSystemWarning(SystemWarning{
		// No Code — invalid per the contract; must be silently dropped.
		Title: "Orphan",
	})
	if got := SystemWarningsSnapshot(); len(got) != 0 {
		t.Fatalf("expected empty-Code warning to be rejected; got %+v", got)
	}
}

func TestSystemWarningsSnapshot_ReturnsNilWhenEmpty(t *testing.T) {
	ResetSystemWarningsForTest()
	defer ResetSystemWarningsForTest()

	got := SystemWarningsSnapshot()
	if got != nil {
		t.Fatalf("snapshot of empty collector must be nil (not empty slice) so JSON marshalling produces null; got %+v", got)
	}
}

func TestSystemWarningsSnapshot_IsIndependentOfLiveCollector(t *testing.T) {
	ResetSystemWarningsForTest()
	defer ResetSystemWarningsForTest()

	AddSystemWarning(SystemWarning{Code: "test_code", Title: "before"})
	snap := SystemWarningsSnapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot must contain 1 entry after first Add; got %d", len(snap))
	}

	// Mutating the snapshot must not affect the live collector.
	snap[0].Title = "MUTATED"

	// Adding more warnings must not retroactively appear in the
	// previously-captured snapshot.
	AddSystemWarning(SystemWarning{Code: "test_code_2", Title: "after"})

	if snap[0].Title != "MUTATED" {
		t.Fatalf("snapshot mutation reverted; expected snapshot to be independent of live state")
	}
	if len(snap) != 1 {
		t.Fatalf("snapshot length grew after Add; expected snapshot to be a frozen copy at capture time")
	}

	live := SystemWarningsSnapshot()
	if len(live) != 2 {
		t.Fatalf("live collector should have 2 entries after second Add; got %d", len(live))
	}
	if live[0].Title == "MUTATED" {
		t.Fatalf("live collector reflects snapshot mutation; expected isolation between snapshots and live state")
	}
}
