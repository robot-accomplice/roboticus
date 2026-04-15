// system_warnings_test.go pins the v1.0.6 dashboard wire shape.
// The dashboard's TypeScript types depend on:
//   * `warnings: SystemWarning[]` — never null, even when empty
//   * `count: number` — matches `warnings.length`, lets the
//     dashboard short-circuit "is there a banner?" without
//     calculating from the array
//   * Each warning's stable Code field — keys the dashboard's
//     localized strings and dismissal state
//
// If this test breaks, the dashboard's banner rendering breaks
// silently — there's no compile-time linkage between the Go wire
// shape and the TypeScript consumer.

package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"roboticus/internal/core"
)

func TestGetSystemWarnings_EmptyReturnsEmptyArrayNotNull(t *testing.T) {
	core.ResetSystemWarningsForTest()
	defer core.ResetSystemWarningsForTest()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/system-warnings", nil)
	GetSystemWarnings()(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200; got %d", rr.Code)
	}

	var resp SystemWarningsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Warnings == nil {
		t.Fatalf("warnings field must be empty array (not null) so dashboard TypeScript types stay non-nullable")
	}
	if len(resp.Warnings) != 0 {
		t.Fatalf("expected 0 warnings; got %d", len(resp.Warnings))
	}
	if resp.Count != 0 {
		t.Fatalf("expected count=0; got %d", resp.Count)
	}

	// Verify the raw JSON shape too — dashboard parsers may fail on
	// `null` even if Go's json.Unmarshal coerces it to a nil slice.
	rawJSON := rr.Body.String()
	if !containsBytes(rawJSON, `"warnings":[]`) {
		t.Fatalf("expected raw JSON to contain `\"warnings\":[]` (not null); got %s", rawJSON)
	}
}

func TestGetSystemWarnings_ReturnsAllRecordedWarnings(t *testing.T) {
	core.ResetSystemWarningsForTest()
	defer core.ResetSystemWarningsForTest()

	core.AddSystemWarning(core.SystemWarning{
		Code:     core.WarningCodeConfigDefaultsUsed,
		Title:    "Test config",
		Detail:   "details",
		Severity: core.SystemWarningSeverityHigh,
	})
	core.AddSystemWarning(core.SystemWarning{
		Code:     core.WarningCodeDatabaseCreatedAtPath,
		Title:    "Test db",
		Detail:   "/tmp/x.db",
		Severity: core.SystemWarningSeverityNormal,
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/system-warnings", nil)
	GetSystemWarnings()(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200; got %d", rr.Code)
	}

	var resp SystemWarningsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Count != 2 || len(resp.Warnings) != 2 {
		t.Fatalf("expected count=2 / len=2; got count=%d len=%d", resp.Count, len(resp.Warnings))
	}

	// Verify the stable code field round-trips intact — dashboard
	// keys localized strings and dismissal state on this.
	codesPresent := map[string]bool{}
	for _, w := range resp.Warnings {
		codesPresent[w.Code] = true
	}
	if !codesPresent[core.WarningCodeConfigDefaultsUsed] {
		t.Fatalf("expected %q in response; got %+v", core.WarningCodeConfigDefaultsUsed, resp.Warnings)
	}
	if !codesPresent[core.WarningCodeDatabaseCreatedAtPath] {
		t.Fatalf("expected %q in response; got %+v", core.WarningCodeDatabaseCreatedAtPath, resp.Warnings)
	}
}

func containsBytes(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
