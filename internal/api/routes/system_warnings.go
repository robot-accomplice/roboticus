// system_warnings.go exposes the v1.0.6 system-warnings collector to
// the dashboard. The collector itself lives in internal/core
// (process-singleton, write-many-from-anywhere); this route is the
// HTTP read-side that the dashboard banner polls.
//
// The dashboard is expected to:
//   1. Poll GET /api/admin/system-warnings on initial render and on
//      a refresh interval (e.g., every 30 seconds).
//   2. Show a top-of-page banner styled by the highest severity
//      present (high → red, normal → amber).
//   3. Render each warning's title + remedy with a "dismiss until
//      next restart" affordance — dismissal is client-side only;
//      the server keeps the warning live until the underlying
//      condition is fixed and the daemon restarts (warnings are
//      collected at boot, not on every request).
//
// The endpoint returns the snapshot as a JSON array. Empty array
// (not null, not 204) when no warnings — easier for the dashboard
// to detect "is there a banner to render?" without special-casing
// the no-warnings shape.

package routes

import (
	"encoding/json"
	"net/http"

	"roboticus/internal/core"
)

// SystemWarningsResponse is the wire shape returned by the endpoint.
// We wrap the warning slice in an object so future fields (e.g.,
// "last_collected_at", "active_dismissal_token") can be added
// without breaking the dashboard's JSON parsing.
type SystemWarningsResponse struct {
	Warnings []core.SystemWarning `json:"warnings"`
	Count    int                  `json:"count"`
}

// GetSystemWarnings returns the current snapshot of system warnings.
// Always 200 — the absence of warnings is a normal state, not an
// error condition. Dashboards rendering this should treat
// `count == 0` as "no banner needed."
func GetSystemWarnings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		warnings := core.SystemWarningsSnapshot()
		// Coerce nil → empty slice so the JSON marshalling produces
		// `[]` rather than `null`. The dashboard's TypeScript types
		// can rely on `warnings: SystemWarning[]` being non-null.
		if warnings == nil {
			warnings = []core.SystemWarning{}
		}
		resp := SystemWarningsResponse{
			Warnings: warnings,
			Count:    len(warnings),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}
