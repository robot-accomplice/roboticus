package scripts_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestABCContractSoakReportScript_PinsEvidenceContract(t *testing.T) {
	testDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("cwd: %v", err)
	}
	path := filepath.Join(testDir, "abc_contract_soak_report.py")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(data)

	required := []string{
		`"guard_contract_evaluated"`,
		`"guard_retry_scheduled"`,
		`"guard_retry_suppressed"`,
		`"verifier_contract_evaluated"`,
		`"verifier_retry_scheduled"`,
		`turn_diagnostics`,
		`turn_diagnostic_events`,
		`state_db_snapshot`,
		`contract_event_count`,
		`diagnostic_event_counts`,
		`confidence_effects`,
		`valid_for_abc_attribution`,
		`invalid_reasons`,
		`abc_attribution_claim`,
		`scenario_set_matches`,
		`new_failures`,
		`resolved_failures`,
		`pass_delta`,
		`failure_delta`,
		`hard_violation_delta`,
		`soft_violation_delta`,
		`scenario set differs`,
		`cache settings differ`,
		`effective config hash differs`,
	}
	for _, marker := range required {
		if !strings.Contains(text, marker) {
			t.Fatalf("ABC contract soak report script missing invariant marker %q", marker)
		}
	}
}

func TestBehaviorSoakScript_PinsABCEvidenceCapture(t *testing.T) {
	testDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("cwd: %v", err)
	}
	path := filepath.Join(testDir, "run-agent-behavior-soak.py")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(data)

	required := []string{
		`SOAK_DB_SNAPSHOT_PATH`,
		`DB_SNAPSHOT_PATH = os.environ.get("SOAK_DB_SNAPSHOT_PATH"`,
		`def git_commit() -> str:`,
		`def git_dirty() -> bool:`,
		`def config_evidence(config_path: Optional[Path]) -> Dict[str, object]:`,
		`def copy_sqlite_snapshot(src: Path, dst: Path) -> None:`,
		`stop_managed_server(managed)`,
		`REPORT_PATH + ".state.db"`,
		`"git_commit": git_commit()`,
		`"git_dirty": git_dirty()`,
		`"cache": {`,
		`"scenario_filter": [scenario.name for scenario in scenarios]`,
		`"models_seen": models_seen`,
		`"config_evidence": config_evidence(managed.config_path if managed else None)`,
		`"state_db_snapshot": str(db_snapshot_path) if db_snapshot_path else None`,
	}
	for _, marker := range required {
		if !strings.Contains(text, marker) {
			t.Fatalf("behavior soak script missing ABC evidence marker %q", marker)
		}
	}
}
