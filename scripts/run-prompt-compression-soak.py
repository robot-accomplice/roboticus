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
import threading
import time
from queue import Empty, Queue
from pathlib import Path
from typing import Dict, List, Tuple


REPO_ROOT = Path(__file__).resolve().parents[1]
BASE_SOAK = REPO_ROOT / "scripts" / "run-agent-behavior-soak.py"
DEFAULT_REPORT = "/tmp/roboticus-prompt-compression-soak-report.json"
SERVER_MODE = os.environ.get("SOAK_SERVER_MODE", "clone").strip().lower()
RATIO = os.environ.get("SOAK_PROMPT_COMPRESSION_RATIO", "").strip()
_raw_lane_timeout = os.environ.get("SOAK_LANE_TIMEOUT_SECONDS", "").strip()
try:
    LANE_TIMEOUT_SECONDS = int(_raw_lane_timeout) if _raw_lane_timeout else 0
except ValueError as exc:
    raise SystemExit(
        f"unsupported SOAK_LANE_TIMEOUT_SECONDS={_raw_lane_timeout!r}: {exc}"
    ) from exc
_raw_lane_heartbeat = os.environ.get("SOAK_LANE_HEARTBEAT_SECONDS", "").strip()
try:
    LANE_HEARTBEAT_SECONDS = int(_raw_lane_heartbeat) if _raw_lane_heartbeat else 30
except ValueError as exc:
    raise SystemExit(
        f"unsupported SOAK_LANE_HEARTBEAT_SECONDS={_raw_lane_heartbeat!r}: {exc}"
    ) from exc


def ts_now() -> str:
    return time.strftime("%Y-%m-%dT%H:%M:%S%z", time.localtime())


def log_line(message: str, *, file=sys.stdout) -> None:
    print(f"{ts_now()} {message}", file=file, flush=True)


def _stream_reader(stream, sink, queue: Queue[Tuple[str, str]]) -> None:
    try:
        for line in iter(stream.readline, ""):
            sink.write(line)
            sink.flush()
            queue.put(("line", line))
    finally:
        stream.close()
        queue.put(("eof", ""))


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

    log_line(
        f"[prompt-compression-soak] lane={label} "
        f"compression={compression_mode} clear_cache={env['SOAK_CLEAR_CACHE']} "
        f"bypass_cache={env['SOAK_BYPASS_CACHE']}"
        + (f" ratio={RATIO}" if RATIO else "")
    )

    proc = subprocess.Popen(
        [sys.executable, str(BASE_SOAK)],
        cwd=str(REPO_ROOT),
        env=env,
        text=True,
        bufsize=1,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    assert proc.stdout is not None
    assert proc.stderr is not None

    queue: Queue[Tuple[str, str]] = Queue()
    readers = [
        threading.Thread(target=_stream_reader, args=(proc.stdout, sys.stdout, queue), daemon=True),
        threading.Thread(target=_stream_reader, args=(proc.stderr, sys.stderr, queue), daemon=True),
    ]
    for reader in readers:
        reader.start()

    started = time.time()
    next_heartbeat = started + max(LANE_HEARTBEAT_SECONDS, 1)
    eof_count = 0
    timed_out = False

    while True:
        now = time.time()
        if LANE_TIMEOUT_SECONDS > 0 and now - started > LANE_TIMEOUT_SECONDS:
            timed_out = True
            proc.kill()
            break
        if now >= next_heartbeat:
            elapsed = int(now - started)
            report_state = "present" if report_path.exists() else "pending"
            log_line(
                f"[prompt-compression-soak] lane={label} heartbeat "
                f"elapsed={elapsed}s report={report_state}"
            )
            next_heartbeat = now + max(LANE_HEARTBEAT_SECONDS, 1)
        if proc.poll() is not None and eof_count >= 2:
            break
        try:
            kind, _ = queue.get(timeout=1.0)
            if kind == "eof":
                eof_count += 1
        except Empty:
            pass

    return_code = proc.wait()
    for reader in readers:
        reader.join(timeout=1.0)

    if timed_out:
        return 124, {
            "runtime": "goboticus",
            "kind": "behavior-soak-lane",
            "lane": label,
            "prompt_compression": compression_mode,
            "passed": 0,
            "failed": 0,
            "total": 0,
            "results": [],
            "harness_error": (
                f"underlying soak timed out after {LANE_TIMEOUT_SECONDS}s "
                f"before producing report {report_path}"
            ),
        }

    if not report_path.exists():
        return return_code, {
            "runtime": "goboticus",
            "kind": "behavior-soak-lane",
            "lane": label,
            "prompt_compression": compression_mode,
            "passed": 0,
            "failed": 0,
            "total": 0,
            "results": [],
            "harness_error": (
                f"underlying soak exited {return_code} without producing "
                f"report {report_path}"
            ),
        }
    with report_path.open("r", encoding="utf-8") as fh:
        report = json.load(fh)
    report["exit_code"] = return_code
    return return_code, report


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
        log_line(
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

    log_line(f"[prompt-compression-soak] report={final_report_path}")
    log_line(
        f"[prompt-compression-soak] baseline={baseline_report.get('passed')}/{baseline_report.get('total')} "
        f"compressed={compressed_report.get('passed')}/{compressed_report.get('total')}"
    )
    log_line(
        f"[prompt-compression-soak] regressions={len(regressions)} "
        f"improvements={len(improvements)} unchanged_failures={len(unchanged_failures)}"
    )

    if regressions:
        log_line("[prompt-compression-soak] FAIL prompt compression introduced regressions", file=sys.stderr)
        for row in regressions:
            log_line(f"[prompt-compression-soak] regression {row['name']}: {row['reason']}", file=sys.stderr)
        return 1

    if baseline_exit != 0 or compressed_exit != 0:
        log_line(
            "[prompt-compression-soak] FAIL one or both underlying soaks failed "
            "without a compression-specific regression; inspect the combined report",
            file=sys.stderr,
        )
        return 1

    log_line("[prompt-compression-soak] PASS no compression-specific regressions detected")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
