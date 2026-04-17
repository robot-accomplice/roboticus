#!/usr/bin/env python3
"""
Prompt-compression soak orchestrator.

Runs the live behavior soak twice against isolated configs:
  1. prompt compression forced OFF
  2. prompt compression forced ON

Then compares scenario outcomes and flags any regression where the
compression-enabled lane fails a scenario that the baseline lane passed.

This is intentionally separate from run-agent-behavior-soak.py so the
base soak runner stays focused on one live config at a time while this
script owns the paired-comparison/reporting logic.
"""

import json
import os
import subprocess
import sys
import tempfile
import time
from pathlib import Path
from typing import Dict, List, Tuple


REPO_ROOT = Path(__file__).resolve().parents[1]
BASE_SOAK = REPO_ROOT / "scripts" / "run-agent-behavior-soak.py"
DEFAULT_REPORT = "/tmp/roboticus-prompt-compression-soak-report.json"
SERVER_MODE = os.environ.get("SOAK_SERVER_MODE", "clone").strip().lower()
RATIO = os.environ.get("SOAK_PROMPT_COMPRESSION_RATIO", "").strip()


def run_lane(label: str, compression_mode: str, report_path: Path) -> Tuple[int, Dict[str, object]]:
    env = os.environ.copy()
    env["SOAK_SERVER_MODE"] = SERVER_MODE
    env["SOAK_PROMPT_COMPRESSION"] = compression_mode
    env["SOAK_REPORT_PATH"] = str(report_path)
    # Quality evaluation should exercise the live model path, not cache replay.
    env.setdefault("SOAK_CLEAR_CACHE", "1")
    env.setdefault("SOAK_BYPASS_CACHE", "1")
    if RATIO:
        env["SOAK_PROMPT_COMPRESSION_RATIO"] = RATIO

    print(
        f"[prompt-compression-soak] lane={label} "
        f"compression={compression_mode} clear_cache={env['SOAK_CLEAR_CACHE']} "
        f"bypass_cache={env['SOAK_BYPASS_CACHE']}"
        + (f" ratio={RATIO}" if RATIO else "")
    )

    proc = subprocess.run(
        [sys.executable, str(BASE_SOAK)],
        cwd=str(REPO_ROOT),
        env=env,
        text=True,
    )
    with report_path.open("r", encoding="utf-8") as fh:
        report = json.load(fh)
    report["exit_code"] = proc.returncode
    return proc.returncode, report


def index_results(report: Dict[str, object]) -> Dict[str, Dict[str, object]]:
    results = report.get("results", [])
    indexed: Dict[str, Dict[str, object]] = {}
    for row in results:
        if isinstance(row, dict) and row.get("name"):
            indexed[str(row["name"])] = row
    return indexed


def summarize_diffs(
    baseline: Dict[str, object],
    compressed: Dict[str, object],
) -> Tuple[List[Dict[str, object]], List[Dict[str, object]], List[Dict[str, object]]]:
    base_rows = index_results(baseline)
    comp_rows = index_results(compressed)
    regressions: List[Dict[str, object]] = []
    improvements: List[Dict[str, object]] = []
    unchanged_failures: List[Dict[str, object]] = []

    for name, base_row in base_rows.items():
        comp_row = comp_rows.get(name)
        if comp_row is None:
            regressions.append(
                {
                    "name": name,
                    "reason": "scenario missing from compression-enabled lane",
                    "baseline_passed": base_row.get("passed"),
                    "compressed_passed": None,
                }
            )
            continue

        base_pass = bool(base_row.get("passed"))
        comp_pass = bool(comp_row.get("passed"))
        if base_pass and not comp_pass:
            regressions.append(
                {
                    "name": name,
                    "reason": "compression caused a pass->fail regression",
                    "baseline_passed": base_pass,
                    "compressed_passed": comp_pass,
                    "baseline_latency_s": base_row.get("latency_s"),
                    "compressed_latency_s": comp_row.get("latency_s"),
                    "baseline_checks": base_row.get("checks"),
                    "compressed_checks": comp_row.get("checks"),
                }
            )
        elif (not base_pass) and comp_pass:
            improvements.append(
                {
                    "name": name,
                    "reason": "compression improved a failing baseline scenario",
                    "baseline_passed": base_pass,
                    "compressed_passed": comp_pass,
                    "baseline_latency_s": base_row.get("latency_s"),
                    "compressed_latency_s": comp_row.get("latency_s"),
                }
            )
        elif (not base_pass) and (not comp_pass):
            unchanged_failures.append(
                {
                    "name": name,
                    "reason": "scenario failed in both lanes",
                    "baseline_checks": base_row.get("checks"),
                    "compressed_checks": comp_row.get("checks"),
                }
            )

    return regressions, improvements, unchanged_failures


def main() -> int:
    if SERVER_MODE not in {"clone", "fresh"}:
        print(
            "[prompt-compression-soak] SOAK_SERVER_MODE must be clone or fresh "
            "so the isolated config can force prompt compression on/off safely",
            file=sys.stderr,
        )
        return 2

    tmpdir = Path(tempfile.mkdtemp(prefix="roboticus-prompt-compression-soak-"))
    baseline_report_path = tmpdir / "baseline-off.json"
    compressed_report_path = tmpdir / "compression-on.json"
    final_report_path = Path(os.environ.get("SOAK_REPORT_PATH", DEFAULT_REPORT))

    started = time.time()
    baseline_exit, baseline_report = run_lane("baseline", "off", baseline_report_path)
    compressed_exit, compressed_report = run_lane("compressed", "on", compressed_report_path)
    regressions, improvements, unchanged_failures = summarize_diffs(baseline_report, compressed_report)

    combined = {
        "runtime": "goboticus",
        "kind": "prompt-compression-comparison",
        "server_mode": SERVER_MODE,
        "prompt_compression_ratio": RATIO or None,
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "duration_s": round(time.time() - started, 2),
        "baseline": baseline_report,
        "compressed": compressed_report,
        "baseline_exit_code": baseline_exit,
        "compressed_exit_code": compressed_exit,
        "regressions": regressions,
        "improvements": improvements,
        "unchanged_failures": unchanged_failures,
    }

    final_report_path.parent.mkdir(parents=True, exist_ok=True)
    with final_report_path.open("w", encoding="utf-8") as fh:
        json.dump(combined, fh, indent=2)

    print(f"[prompt-compression-soak] report={final_report_path}")
    print(
        f"[prompt-compression-soak] baseline={baseline_report.get('passed')}/{baseline_report.get('total')} "
        f"compressed={compressed_report.get('passed')}/{compressed_report.get('total')}"
    )
    print(
        f"[prompt-compression-soak] regressions={len(regressions)} "
        f"improvements={len(improvements)} unchanged_failures={len(unchanged_failures)}"
    )

    if regressions:
        print("[prompt-compression-soak] FAIL prompt compression introduced regressions", file=sys.stderr)
        for row in regressions:
            print(f"  - {row['name']}: {row['reason']}", file=sys.stderr)
        return 1

    if baseline_exit != 0 or compressed_exit != 0:
        print(
            "[prompt-compression-soak] FAIL one or both underlying soaks failed "
            "without a compression-specific regression; inspect the combined report",
            file=sys.stderr,
        )
        return 1

    print("[prompt-compression-soak] PASS no compression-specific regressions detected")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
