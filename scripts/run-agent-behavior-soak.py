#!/usr/bin/env python3
"""
Goboticus Agent Behavior Soak Test
===================================
Live API behavior assertions — sends real prompts to a running Goboticus
server and validates responses against quality gates.

Adapted from roboticus/scripts/run-agent-behavior-soak.py.

Usage:
  python3 scripts/run-agent-behavior-soak.py
  BASE_URL=http://localhost:18790 python3 scripts/run-agent-behavior-soak.py

Environment:
  BASE_URL                     Server URL (default: http://127.0.0.1:18790)
  SOAK_TIMEOUT_SECONDS         HTTP request timeout (default: 240)
  SOAK_MAX_LATENCY_SECONDS     Max acceptable latency per scenario (default: 120)
  SOAK_SCENARIO_PAUSE_SECONDS  Pause between scenarios (default: 1.5)
  SOAK_SESSION_ISOLATION        1=new session per scenario (default: 1)
  SOAK_AGENT_ID                Agent ID for session creation (default: duncan)
  SOAK_REPORT_PATH             JSON report output path
"""
import json
import os
import re
import sys
import time
import urllib.error
import urllib.request
from typing import Callable, Dict, List, Tuple

BASE_URL = os.environ.get("BASE_URL", "http://127.0.0.1:18790").rstrip("/")
TIMEOUT = int(os.environ.get("SOAK_TIMEOUT_SECONDS", "240"))
MAX_LATENCY = float(os.environ.get("SOAK_MAX_LATENCY_SECONDS", "120"))
SCENARIO_PAUSE = float(os.environ.get("SOAK_SCENARIO_PAUSE_SECONDS", "1.5"))
SESSION_ISOLATION = os.environ.get("SOAK_SESSION_ISOLATION", "1") != "0"
AGENT_ID = os.environ.get("SOAK_AGENT_ID", "duncan")
REPORT_PATH = os.environ.get(
    "SOAK_REPORT_PATH", "/tmp/goboticus-agent-behavior-soak-report.json"
)


# ── Quality gate markers ────────────────────────────────────────

STALE_MARKERS = [
    "as of my last update",
    "as of my last training",
    "cannot provide real-time updates",
    "can't provide real-time updates",
    "as of early 2023",
    "as of 2023",
]

INTERNAL_METADATA_MARKERS = [
    "delegated_subagent=",
    "selected_subagent=",
    "subtask 1 ->",
    "subtask 2 ->",
    "expected_utility_margin",
    "decomposition gate decision",
]

FOREIGN_IDENTITY_MARKERS = [
    "as an ai developed by microsoft",
    "as an ai language model",
    "as an ai text-based interface",
    "i am claude",
    "i'm claude",
    "i am chatgpt",
    "i'm chatgpt",
]

FILESYSTEM_DENIAL_MARKERS = [
    "can't access your files",
    "cannot access your files",
    "can't access your folders",
    "cannot access your folders",
    "don't have access to your files",
    "as an ai, i don't have access to your files",
    "as an ai text-based interface, i'm not able to directly access",
]


# ── HTTP helpers ────────────────────────────────────────────────

def send_message(prompt: str, session_id: str = None, retries: int = 6) -> Dict[str, object]:
    payload: Dict[str, object] = {"content": prompt}
    if session_id:
        payload["session_id"] = session_id
    req = urllib.request.Request(
        BASE_URL + "/api/agent/message",
        data=json.dumps(payload).encode("utf-8"),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    started = time.time()
    attempt = 0
    while True:
        attempt += 1
        try:
            with urllib.request.urlopen(req, timeout=TIMEOUT) as resp:
                body = json.loads(resp.read().decode("utf-8", "replace"))
            body["_latency_s"] = round(time.time() - started, 2)
            return body
        except urllib.error.HTTPError as e:
            retryable = e.code in (429, 500, 502, 503, 504)
            if not retryable or attempt >= retries:
                raise
            time.sleep(min(2 ** (attempt - 1), 20))


def create_session(agent_id: str = AGENT_ID) -> str:
    req = urllib.request.Request(
        BASE_URL + "/api/sessions",
        data=json.dumps({"agent_id": agent_id}).encode("utf-8"),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=TIMEOUT) as resp:
        body = json.loads(resp.read().decode("utf-8", "replace"))
    # Goboticus returns 201 for session creation.
    sid = str(body.get("id") or "").strip()
    if not sid:
        raise RuntimeError("create_session returned no id")
    return sid


# ── Check functions ─────────────────────────────────────────────

def contains_any(text: str, markers: List[str]) -> bool:
    lower = text.lower()
    return any(m in lower for m in markers)


def has_execution_block(text: str) -> bool:
    lower = text.lower()
    return (
        "i did not execute a tool" in lower
        or "i did not execute a delegated subagent task" in lower
        or "i did not execute a cron scheduling tool" in lower
    )


def one_sentence_ack(text: str) -> bool:
    stripped = text.strip()
    if not stripped:
        return False
    sentence_count = len(re.findall(r"[.!?](?:\s|$)", stripped))
    if sentence_count == 0:
        sentence_count = 1
    return sentence_count == 1 and len(stripped.splitlines()) == 1


Check = Callable[[Dict[str, object], str], Tuple[bool, str]]


def check_latency(resp: Dict[str, object], _content: str) -> Tuple[bool, str]:
    latency = float(resp.get("_latency_s", 0.0))
    ok = latency <= MAX_LATENCY
    return ok, f"latency={latency}s max={MAX_LATENCY}s"


def check_no_stale(resp: Dict[str, object], content: str) -> Tuple[bool, str]:
    ok = not contains_any(content, STALE_MARKERS)
    return ok, "no stale-knowledge markers"


def check_no_internal_metadata(resp: Dict[str, object], content: str) -> Tuple[bool, str]:
    ok = not contains_any(content, INTERNAL_METADATA_MARKERS)
    return ok, "no internal delegation/orchestration metadata"


def check_no_foreign_identity(resp: Dict[str, object], content: str) -> Tuple[bool, str]:
    ok = not contains_any(content, FOREIGN_IDENTITY_MARKERS)
    return ok, "no foreign identity boilerplate"


def check_no_exec_block(resp: Dict[str, object], content: str) -> Tuple[bool, str]:
    ok = not has_execution_block(content)
    return ok, "no false execution/delegation block message"


def check_ack(resp: Dict[str, object], content: str) -> Tuple[bool, str]:
    ok = one_sentence_ack(content) and (
        "acknowledge" in content.lower() or "acknowledged" in content.lower()
        or "await" in content.lower()
    )
    return ok, "single-sentence acknowledgement"


def check_non_empty(resp: Dict[str, object], content: str) -> Tuple[bool, str]:
    ok = len(content.strip()) > 20
    return ok, f"response is substantive ({len(content.strip())} chars)"


def check_introspection_summary(resp: Dict[str, object], content: str) -> Tuple[bool, str]:
    lower = content.lower()
    evidence = [
        "subagent", "specialist", "memory", "runtime", "tool", "capability",
        "model", "session", "workspace", "active", "configured", "available",
    ]
    matches = sum(1 for e in evidence if e in lower)
    ok = len(content.strip()) > 80 and matches >= 3
    return ok, "introspection summary is substantive" if ok else f"only {matches}/3 evidence markers found"


def check_tool_use(resp: Dict[str, object], content: str) -> Tuple[bool, str]:
    lower = content.lower()
    ok = (
        "output" in lower
        or "returned" in lower
        or "result" in lower
        or "available tools" in lower
        or "tool" in lower and ("revealed" in lower or "shows" in lower or "status" in lower)
    )
    return ok, "returns concrete tool-use evidence"


def check_count_only_output(_resp: Dict[str, object], content: str) -> Tuple[bool, str]:
    stripped = content.strip()
    bare_number = bool(re.fullmatch(r"\d+", stripped))
    has_count = bool(re.search(r"\b\d+\b", stripped)) and (
        "count" in stripped.lower() or "found" in stripped.lower()
        or "file" in stripped.lower() or "markdown" in stripped.lower()
    )
    ok = bare_number or has_count
    return ok, "returns count-only numeric output" if bare_number else "returns count in natural language"


def check_cron(resp: Dict[str, object], content: str) -> Tuple[bool, str]:
    lower = content.lower()
    has_schedule = "*/5" in content or "every 5" in lower or "5 minute" in lower
    has_creation = (
        "scheduled" in lower or "created" in lower or "cron job" in lower
        or "name:" in lower or "id:" in lower
    )
    ok = has_schedule or has_creation
    return ok, "cron scheduled with explicit expression" if ok else "no cron creation evidence"


def check_distribution(path_hint: str) -> Check:
    def _check(resp: Dict[str, object], content: str) -> Tuple[bool, str]:
        lower = content.lower()
        path_lower = path_hint.lower().replace("~", "").strip("/")
        has_path = path_lower in lower or "/users/" in lower or "home" in lower
        has_distribution = (
            "distribution" in lower or "directory" in lower or "files" in lower
            or "breakdown" in lower or "overview" in lower
        )
        ok = has_path and has_distribution
        return ok, f"file distribution executed for {path_hint}"
    return _check


def check_folder_scan(path_hint: str) -> Check:
    def _check(resp: Dict[str, object], content: str) -> Tuple[bool, str]:
        lower = content.lower()
        path_lower = path_hint.lower()
        has_path = path_lower in lower or path_lower.replace("~/", "") in lower
        has_scan = (
            "scan" in lower or "files" in lower or "directory" in lower
            or "folder" in lower or "list" in lower or "results" in lower
        )
        ok = has_path and has_scan
        return ok, f"folder scan executed for {path_hint}"
    return _check


def check_no_filesystem_denial(_resp: Dict[str, object], content: str) -> Tuple[bool, str]:
    lower = content.lower()
    ok = not any(m in lower for m in FILESYSTEM_DENIAL_MARKERS)
    return ok, "no false filesystem-access denial"


def check_geopolitical_quality(_resp: Dict[str, object], content: str) -> Tuple[bool, str]:
    lower = content.lower()
    if not content.strip():
        return False, "empty geopolitical response"
    if contains_any(content, INTERNAL_METADATA_MARKERS):
        return False, "contains internal delegation metadata"
    ok = (
        "geopolitical" in lower
        or "situation" in lower
        or "conflict" in lower
        or "flashpoint" in lower
        or "sitrep" in lower
    )
    return ok, "geopolitical response is substantive and clean"


# ── Scenarios ───────────────────────────────────────────────────

class Scenario:
    def __init__(self, name: str, prompt: str, checks: List[Check]):
        self.name = name
        self.prompt = prompt
        self.checks = checks


SCENARIOS = [
    Scenario(
        "acknowledgement_sla",
        "Good evening Duncan. Acknowledge this request in one sentence, then wait.",
        [check_latency, check_ack, check_no_stale, check_no_foreign_identity],
    ),
    Scenario(
        "introspection_discovery",
        "use your introspection tool to discover your current subagent functionality and summarize it for me",
        [
            check_latency,
            check_no_exec_block,
            check_introspection_summary,
            check_no_stale,
            check_no_foreign_identity,
        ],
    ),
    Scenario(
        "introspection_followup",
        "and summarize the results for me",
        [check_latency, check_no_exec_block, check_no_stale, check_no_foreign_identity],
    ),
    Scenario(
        "tool_random_use",
        "tell me about the tools you can use, pick one at random, and use it",
        [
            check_latency,
            check_no_exec_block,
            check_tool_use,
            check_no_stale,
            check_no_foreign_identity,
        ],
    ),
    Scenario(
        "geopolitical_sitrep",
        "What's the geopolitical situation?",
        [
            check_latency,
            check_no_exec_block,
            check_geopolitical_quality,
            check_no_stale,
            check_no_internal_metadata,
            check_no_foreign_identity,
        ],
    ),
    Scenario(
        "cron",
        "schedule a cron job that runs every 5 minutes and tell me exactly what was scheduled",
        [check_latency, check_no_exec_block, check_cron, check_no_foreign_identity],
    ),
    Scenario(
        "tilde_distribution",
        "give me the file distribution in the folder ~",
        [
            check_latency,
            check_no_exec_block,
            check_distribution("~"),
            check_no_foreign_identity,
        ],
    ),
    Scenario(
        "abs_distribution",
        "give me the file distribution in the folder /Users/jmachen",
        [
            check_latency,
            check_no_exec_block,
            check_distribution("/Users/jmachen"),
            check_no_foreign_identity,
        ],
    ),
    Scenario(
        "folder_scan_downloads",
        "Now look in my Downloads folder",
        [
            check_latency,
            check_no_exec_block,
            check_folder_scan("Downloads"),
            check_no_filesystem_denial,
            check_no_foreign_identity,
        ],
    ),
    Scenario(
        "filesystem_count_only",
        "Count markdown files recursively in /Users/jmachen/code and return only the number.",
        [
            check_latency,
            check_no_exec_block,
            check_no_filesystem_denial,
            check_count_only_output,
            check_no_foreign_identity,
        ],
    ),
]


# ── Runner ──────────────────────────────────────────────────────

def run() -> int:
    print(f"[behavior-soak] base_url={BASE_URL}")
    print(
        f"[behavior-soak] timeout={TIMEOUT}s max_latency={MAX_LATENCY}s "
        f"pause={SCENARIO_PAUSE}s isolated_sessions={SESSION_ISOLATION}"
    )

    # Pre-flight: check server is reachable.
    try:
        req = urllib.request.Request(BASE_URL + "/api/health", method="GET")
        with urllib.request.urlopen(req, timeout=10) as resp:
            health = json.loads(resp.read().decode("utf-8", "replace"))
        print(f"[behavior-soak] server health: {health.get('status', 'unknown')}")
    except Exception as err:
        print(f"[behavior-soak] FATAL: server not reachable at {BASE_URL}: {err}", file=sys.stderr)
        return 1

    session_id = None
    results: List[Dict[str, object]] = []

    for scenario in SCENARIOS:
        if SESSION_ISOLATION:
            try:
                session_id = create_session()
            except Exception as err:
                row = {
                    "name": scenario.name,
                    "prompt": scenario.prompt,
                    "latency_s": None,
                    "model": None,
                    "session_id": None,
                    "passed": False,
                    "checks": [
                        {
                            "ok": False,
                            "detail": f"session creation failure: {err}",
                            "check": "create_session",
                        }
                    ],
                    "content": "",
                }
                results.append(row)
                print(f"[behavior-soak] FAIL {scenario.name} session error: {err}")
                continue

        try:
            resp = send_message(scenario.prompt, session_id)
        except Exception as err:
            row = {
                "name": scenario.name,
                "prompt": scenario.prompt,
                "latency_s": None,
                "model": None,
                "session_id": session_id,
                "passed": False,
                "checks": [{"ok": False, "detail": f"request failure: {err}", "check": "request"}],
                "content": "",
            }
            results.append(row)
            print(f"[behavior-soak] FAIL {scenario.name} request error: {err}")
            continue

        session_id = str(resp.get("session_id") or session_id or "")
        content = str(resp.get("content") or "")

        checks = []
        passed = True
        for check in scenario.checks:
            ok, detail = check(resp, content)
            checks.append({"ok": ok, "detail": detail, "check": check.__name__})
            if not ok:
                passed = False

        row = {
            "name": scenario.name,
            "prompt": scenario.prompt,
            "latency_s": resp.get("_latency_s"),
            "model": resp.get("model"),
            "session_id": resp.get("session_id"),
            "passed": passed,
            "checks": checks,
            "content": content[:500],  # truncate for report readability
        }
        results.append(row)

        status = "PASS" if passed else "FAIL"
        print(f"[behavior-soak] {status} {scenario.name} latency={resp.get('_latency_s')}s")
        if not passed:
            for c in checks:
                if not c["ok"]:
                    print(f"  - {c['check']}: {c['detail']}")

        time.sleep(SCENARIO_PAUSE)

    total = len(results)
    failed = [r for r in results if not r["passed"]]
    report = {
        "runtime": "goboticus",
        "base_url": BASE_URL,
        "timeout_s": TIMEOUT,
        "max_latency_s": MAX_LATENCY,
        "total": total,
        "passed": total - len(failed),
        "failed": len(failed),
        "results": results,
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }

    with open(REPORT_PATH, "w", encoding="utf-8") as f:
        json.dump(report, f, indent=2)

    print(f"\n[behavior-soak] report={REPORT_PATH}")
    print(f"[behavior-soak] {total - len(failed)}/{total} scenarios passed")
    if failed:
        print(f"[behavior-soak] FAIL {len(failed)}/{total} scenarios failed", file=sys.stderr)
        for r in failed:
            print(f"  - {r['name']}", file=sys.stderr)
        return 1
    print("[behavior-soak] PASS all scenarios")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(run())
    except urllib.error.HTTPError as e:
        print(f"[behavior-soak] HTTP error: {e.code} {e.reason}", file=sys.stderr)
        raise
    except Exception as e:
        print(f"[behavior-soak] FAIL: {e}", file=sys.stderr)
        raise
