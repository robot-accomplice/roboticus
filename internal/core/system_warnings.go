// system_warnings.go is the v1.0.6 surface for actionable startup
// conditions that aren't errors but operators absolutely need to see.
// The driving cases are:
//
//   * No config file was loaded — the daemon is running on built-in
//     defaults, which usually means the wrong DB and wrong agent
//     identity. Pre-v1.0.6 this happened silently and produced the
//     rogue ambient `~/.roboticus/roboticus.db` from the v1.0.5
//     incident triage.
//   * The database file was just CREATED by sql.Open's lazy
//     materialization at a default path — strong signal that
//     something opened the wrong DB.
//   * (Future) Other latent gotchas: missing skills directory, stale
//     update-state, etc.
//
// The warnings are surfaced in three places:
//
//   1. Boot-time CLI output (via internal/daemon/banner.go's
//      bootStepWarn) so anyone watching `roboticus serve` or `serve
//      --foreground` sees them immediately.
//   2. Persistent log (already covered by the existing zerolog
//      Warn() calls in store.Open and root.initConfig) for
//      after-the-fact triage.
//   3. Dashboard banner via /api/admin/system-warnings, which the
//      web UI polls and renders as a dismissible top-of-page banner.
//
// The collector is a process-singleton so any subsystem that
// detects a warning condition can append without coordinating with
// the others. Reads are snapshot copies — the dashboard endpoint
// can poll without racing the writers.

package core

import (
	"sync"
	"time"
)

// SystemWarning captures one operator-visible startup condition.
// Severity is a soft signal — every warning is emitted at the same
// log level (Warn), but the dashboard renders "high" with a stronger
// styling than "normal".
type SystemWarning struct {
	Code      string    `json:"code"`     // stable identifier — dashboards key on this
	Title     string    `json:"title"`    // short headline (one line)
	Detail    string    `json:"detail"`   // longer explanation, may include paths
	Remedy    string    `json:"remedy"`   // suggested operator action
	Severity  string    `json:"severity"` // "high" or "normal"
	RaisedAt  time.Time `json:"raised_at"`
}

// Severity constants used by the dashboard styling.
const (
	SystemWarningSeverityHigh   = "high"
	SystemWarningSeverityNormal = "normal"
)

// Stable warning codes. Add new codes here when extending the
// surface so dashboards have a predictable enumeration to render
// against.
const (
	WarningCodeConfigDefaultsUsed     = "config_defaults_used"
	WarningCodeDatabaseCreatedAtPath  = "database_created_at_path"
	WarningCodeDatabaseRootOwned      = "database_root_owned"
	WarningCodeSkillsDirectoryMissing = "skills_directory_missing"
)

// systemWarnings holds the process-singleton list. Guarded by mu
// so writes from any subsystem are safe; the snapshot helper makes
// reads cheap and lock-free for the dashboard endpoint.
var (
	systemWarningsMu sync.Mutex
	systemWarnings   []SystemWarning
)

// AddSystemWarning records a new warning. Idempotent on (Code,
// Detail) pairs — if an identical warning was already recorded
// (same Code AND same Detail), the existing entry is left in place
// rather than duplicated. This matters because some warning sources
// (config defaults, DB-created-at-path) can fire from multiple
// observers in the same process and we don't want the dashboard
// banner to show "no config file at: X" three times.
func AddSystemWarning(w SystemWarning) {
	if w.Code == "" {
		return
	}
	if w.RaisedAt.IsZero() {
		w.RaisedAt = time.Now()
	}
	if w.Severity == "" {
		w.Severity = SystemWarningSeverityNormal
	}

	systemWarningsMu.Lock()
	defer systemWarningsMu.Unlock()
	for _, existing := range systemWarnings {
		if existing.Code == w.Code && existing.Detail == w.Detail {
			return
		}
	}
	systemWarnings = append(systemWarnings, w)
}

// SystemWarningsSnapshot returns a copy of the current warning list.
// Callers can iterate without holding any lock; the underlying slice
// is not shared. Returns nil (not an empty slice) when no warnings
// have been recorded so JSON marshalling produces `null` rather than
// an empty array — easier for dashboards to test "is there
// anything to show?"
func SystemWarningsSnapshot() []SystemWarning {
	systemWarningsMu.Lock()
	defer systemWarningsMu.Unlock()
	if len(systemWarnings) == 0 {
		return nil
	}
	out := make([]SystemWarning, len(systemWarnings))
	copy(out, systemWarnings)
	return out
}

// ResetSystemWarningsForTest clears the singleton — used by tests
// to ensure a clean slate. Not exported under a less-test-y name
// because resetting in production would silently lose actionable
// state.
func ResetSystemWarningsForTest() {
	systemWarningsMu.Lock()
	defer systemWarningsMu.Unlock()
	systemWarnings = nil
}
