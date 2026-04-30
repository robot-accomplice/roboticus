#!/usr/bin/env python3
"""
Summarize ABC contract RCA evidence for behavior soak reports.

The behavior soak report contains scenario/session outcomes. The retained
isolated state.db contains the contract events needed to explain guard and
verifier behavior. This script joins those two artifacts and emits a compact
JSON summary suitable for before/after comparison.
"""

from __future__ import annotations

import argparse
import json
import sqlite3
from collections import Counter, defaultdict
from pathlib import Path
from typing import Any, Dict, Iterable, List, Optional, Tuple


CONTRACT_EVENT_TYPES = {
    "guard_contract_evaluated",
    "guard_retry_scheduled",
    "guard_retry_suppressed",
    "verifier_contract_evaluated",
    "verifier_retry_scheduled",
}


def load_json(path: Path) -> Dict[str, Any]:
    with path.open("r", encoding="utf-8") as fh:
        data = json.load(fh)
    if not isinstance(data, dict):
        raise ValueError(f"{path} did not contain a JSON object")
    return data


def report_db_path(report: Dict[str, Any], explicit: Optional[Path]) -> Path:
    if explicit is not None:
        return explicit
    managed = report.get("managed_server")
    if isinstance(managed, dict):
        snapshot_path = managed.get("state_db_snapshot")
        if isinstance(snapshot_path, str) and snapshot_path.strip():
            return Path(snapshot_path)
        db_path = managed.get("db_path")
        if isinstance(db_path, str) and db_path.strip():
            return Path(db_path)
    raise ValueError("state database path not supplied and not present in report.managed_server.db_path")


def report_sessions(report: Dict[str, Any]) -> List[str]:
    sessions: List[str] = []
    for row in report.get("results", []):
        if not isinstance(row, dict):
            continue
        session_id = row.get("session_id")
        if isinstance(session_id, str) and session_id and session_id not in sessions:
            sessions.append(session_id)
    return sessions


def connect(path: Path) -> sqlite3.Connection:
    if not path.exists():
        raise FileNotFoundError(path)
    conn = sqlite3.connect(str(path))
    conn.row_factory = sqlite3.Row
    return conn


def iter_contract_events(db_path: Path, sessions: Iterable[str]) -> Iterable[Tuple[str, str, Dict[str, Any]]]:
    session_list = [s for s in sessions if s]
    if not session_list:
        return
    placeholders = ",".join("?" for _ in session_list)
    query = f"""
        SELECT d.session_id, e.event_type, e.details_json
          FROM turn_diagnostics d
          JOIN turn_diagnostic_events e ON e.turn_id = d.turn_id
         WHERE d.session_id IN ({placeholders})
           AND e.event_type IN ({",".join("?" for _ in CONTRACT_EVENT_TYPES)})
         ORDER BY d.session_id, e.seq
    """
    params = session_list + sorted(CONTRACT_EVENT_TYPES)
    with connect(db_path) as conn:
        for row in conn.execute(query, params):
            details_raw = row["details_json"] or "{}"
            try:
                details = json.loads(details_raw)
            except json.JSONDecodeError:
                details = {"_malformed_details_json": details_raw}
            if isinstance(details, dict):
                yield str(row["session_id"]), str(row["event_type"]), details


def contract_list(details: Dict[str, Any]) -> List[Dict[str, Any]]:
    raw = details.get("contract_events")
    if not isinstance(raw, list):
        return []
    out: List[Dict[str, Any]] = []
    for item in raw:
        if isinstance(item, dict):
            out.append(item)
    return out


def summarize_lane(label: str, report_path: Path, db_path: Optional[Path]) -> Dict[str, Any]:
    report = load_json(report_path)
    resolved_db = report_db_path(report, db_path)
    sessions = report_sessions(report)

    event_type_counts: Counter[str] = Counter()
    contract_ids: Counter[str] = Counter()
    groups: Counter[str] = Counter()
    severities: Counter[str] = Counter()
    phases: Counter[str] = Counter()
    recovery_actions: Counter[str] = Counter()
    recovery_outcomes: Counter[str] = Counter()
    confidence_effects: Counter[str] = Counter()
    session_contract_counts: Dict[str, int] = defaultdict(int)
    total_contract_events = 0

    for session_id, event_type, details in iter_contract_events(resolved_db, sessions):
        event_type_counts[event_type] += 1
        for event in contract_list(details):
            total_contract_events += 1
            session_contract_counts[session_id] += 1
            contract_ids[str(event.get("contract_id") or "unknown")] += 1
            groups[str(event.get("contract_group") or "unknown")] += 1
            severities[str(event.get("severity") or "unknown")] += 1
            phases[str(event.get("phase") or "unknown")] += 1
            recovery_actions[str(event.get("recovery_action") or "unknown")] += 1
            recovery_outcomes[str(event.get("recovery_outcome") or "unknown")] += 1
            confidence_effects[str(event.get("confidence_effect") or "unknown")] += 1

    total = int(report.get("total") or 0)
    passed = int(report.get("passed") or 0)
    failed = int(report.get("failed") or 0)
    scenario_names = [
        str(row.get("name"))
        for row in report.get("results", [])
        if isinstance(row, dict) and row.get("name")
    ]
    failed_scenarios = [
        str(row.get("name"))
        for row in report.get("results", [])
        if isinstance(row, dict) and row.get("name") and not row.get("passed")
    ]

    return {
        "label": label,
        "report_path": str(report_path),
        "db_path": str(resolved_db),
        "commit": report.get("git_commit") or report.get("commit") or "unknown",
        "cache": report.get("cache"),
        "server_mode": report.get("server_mode"),
        "session_isolation": report.get("session_isolation"),
        "timeout_s": report.get("timeout_s"),
        "max_latency_s": report.get("max_latency_s"),
        "models_seen": report.get("models_seen"),
        "config_evidence": report.get("config_evidence") or {},
        "total": total,
        "passed": passed,
        "failed": failed,
        "scenario_names": scenario_names,
        "failed_scenarios": failed_scenarios,
        "sessions": sessions,
        "contract_event_count": total_contract_events,
        "diagnostic_event_counts": dict(sorted(event_type_counts.items())),
        "contract_ids": dict(contract_ids.most_common()),
        "contract_groups": dict(groups.most_common()),
        "severities": dict(severities.most_common()),
        "phases": dict(phases.most_common()),
        "recovery_actions": dict(recovery_actions.most_common()),
        "recovery_outcomes": dict(recovery_outcomes.most_common()),
        "confidence_effects": dict(confidence_effects.most_common()),
        "sessions_with_contract_events": dict(sorted(session_contract_counts.items())),
    }


def compare(baseline: Dict[str, Any], after: Dict[str, Any]) -> Dict[str, Any]:
    base_failed = set(baseline.get("failed_scenarios", []))
    after_failed = set(after.get("failed_scenarios", []))
    base_scenarios = set(baseline.get("scenario_names", []))
    after_scenarios = set(after.get("scenario_names", []))
    invalid_reasons: List[str] = []
    if base_scenarios != after_scenarios:
        invalid_reasons.append("scenario set differs")
    if baseline.get("commit") == "unknown" or after.get("commit") == "unknown":
        invalid_reasons.append("one or both lane commits are unknown")
    if baseline.get("cache") != after.get("cache"):
        invalid_reasons.append("cache settings differ")
    if baseline.get("server_mode") != after.get("server_mode"):
        invalid_reasons.append("server mode differs")
    if baseline.get("session_isolation") != after.get("session_isolation"):
        invalid_reasons.append("session isolation differs")
    if baseline.get("timeout_s") != after.get("timeout_s"):
        invalid_reasons.append("request timeout differs")
    if baseline.get("max_latency_s") != after.get("max_latency_s"):
        invalid_reasons.append("latency ceiling differs")
    if baseline.get("models_seen") != after.get("models_seen"):
        invalid_reasons.append("models seen differ")
    if baseline.get("config_evidence", {}).get("config_sha256") != after.get("config_evidence", {}).get("config_sha256"):
        invalid_reasons.append("effective config hash differs")
    if baseline.get("config_evidence", {}).get("providers_file_sha256") != after.get("config_evidence", {}).get("providers_file_sha256"):
        invalid_reasons.append("provider pack hash differs")

    hard_delta = int(after.get("severities", {}).get("hard", 0)) - int(
        baseline.get("severities", {}).get("hard", 0)
    )
    soft_delta = int(after.get("severities", {}).get("soft", 0)) - int(
        baseline.get("severities", {}).get("soft", 0)
    )
    contract_delta = int(after.get("contract_event_count") or 0) - int(
        baseline.get("contract_event_count") or 0
    )
    return {
        "valid_for_abc_attribution": not invalid_reasons,
        "invalid_reasons": invalid_reasons,
        "scenario_set_matches": base_scenarios == after_scenarios,
        "new_failures": sorted(after_failed - base_failed),
        "resolved_failures": sorted(base_failed - after_failed),
        "pass_delta": int(after.get("passed") or 0) - int(baseline.get("passed") or 0),
        "failure_delta": int(after.get("failed") or 0) - int(baseline.get("failed") or 0),
        "contract_event_delta": contract_delta,
        "hard_violation_delta": hard_delta,
        "soft_violation_delta": soft_delta,
        "abc_attribution_claim": (
            "valid"
            if not invalid_reasons
            else "invalid: fix confounders before claiming ABC gains or losses"
        ),
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline-report", required=True, type=Path)
    parser.add_argument("--baseline-db", type=Path)
    parser.add_argument("--after-report", type=Path)
    parser.add_argument("--after-db", type=Path)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()

    baseline = summarize_lane("baseline", args.baseline_report, args.baseline_db)
    output: Dict[str, Any] = {"baseline": baseline}
    if args.after_report:
        after = summarize_lane("after", args.after_report, args.after_db)
        output["after"] = after
        output["comparison"] = compare(baseline, after)

    rendered = json.dumps(output, indent=2, sort_keys=True)
    if args.output:
        args.output.write_text(rendered + "\n", encoding="utf-8")
    else:
        print(rendered)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
