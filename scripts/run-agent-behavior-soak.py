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
  SOAK_SERVER_MODE=clone python3 scripts/run-agent-behavior-soak.py
  SOAK_SERVER_MODE=fresh python3 scripts/run-agent-behavior-soak.py

Environment:
  BASE_URL                      Server URL (default: http://127.0.0.1:18790)
  SOAK_TIMEOUT_SECONDS          HTTP request timeout (default: 1800)
  SOAK_MAX_LATENCY_SECONDS      Max acceptable latency per scenario (default: 900)
  SOAK_SCENARIO_PAUSE_SECONDS   Pause between scenarios (default: 1.5)
  SOAK_SESSION_ISOLATION        1=new session per scenario (default: 1)
  SOAK_AGENT_ID                 Agent ID for session creation (default: duncan)
  SOAK_REPORT_PATH              JSON report output path
  SOAK_SERVER_MODE              external|clone|fresh (default: external)
  SOAK_SOURCE_ROOT              Source roboticus home for clone/fresh (default: ~/.roboticus)
  SOAK_REPO_ROOT                Repo root used to launch `go run . serve`
  SOAK_SERVER_START_TIMEOUT     Seconds to wait for managed server health (default: 90)
  SOAK_KEEP_ISOLATED_ROOT       1=keep isolated root after run (default: 0)
  SOAK_AUTONOMY_MAX_LOOP_SECS   Override autonomy_max_turn_duration_seconds
                                in the isolated config (clone/fresh only).
                                Unset = honor whatever was cloned from the
                                source config. Useful for re-soaking with a
                                higher ceiling when local models' cold-cache
                                latency causes the ReAct loop to miss its
                                wall-clock deadline before tool chains
                                complete. Prod configs are never touched.
  SOAK_PROMPT_COMPRESSION       inherit|off|on (default: inherit). For
                                clone/fresh runs, force the isolated
                                [cache].prompt_compression setting.
  SOAK_PROMPT_COMPRESSION_RATIO Optional float ratio override for the
                                isolated [cache].compression_target_ratio.
"""
import atexit
import hashlib
import json
import os
import re
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable, Dict, List, Optional, Tuple

BASE_URL = os.environ.get("BASE_URL", "http://127.0.0.1:18790").rstrip("/")
# Soak tests are long-running by design — timeouts must be extraordinarily high.
# The HTTP timeout is the maximum wall-clock time for a single API call.
# The latency threshold is the default max acceptable time per scenario.
# Individual scenarios can override latency via Scenario.max_latency_s.
TIMEOUT = int(os.environ.get("SOAK_TIMEOUT_SECONDS", "1800"))       # 30 minutes
MAX_LATENCY = float(os.environ.get("SOAK_MAX_LATENCY_SECONDS", "900"))  # 15 minutes
SCENARIO_PAUSE = float(os.environ.get("SOAK_SCENARIO_PAUSE_SECONDS", "1.5"))
SESSION_ISOLATION = os.environ.get("SOAK_SESSION_ISOLATION", "1") != "0"
AGENT_ID = os.environ.get("SOAK_AGENT_ID", "duncan")
REPORT_PATH = os.environ.get(
    "SOAK_REPORT_PATH", "/tmp/goboticus-agent-behavior-soak-report.json"
)
SERVER_MODE = os.environ.get("SOAK_SERVER_MODE", "external").strip().lower()

# v1.0.6: orthogonal cache toggles for "the same soak in two states."
# These let operators evaluate cache efficacy AND uncached agent
# efficacy against the same build, so a regression in EITHER surface
# is catchable independently. Pre-v1.0.6 the soak's clone-mode
# replayed cached responses with latency_s=0.0 across the board,
# masking real agent behavioral regressions for releases at a time.
#
#   SOAK_CLEAR_CACHE: 1=wipe semantic_cache from cloned state.db
#                     before run (default 1 in clone mode, 0 in
#                     external mode). "Do we start with a clean
#                     slate?"
#   SOAK_BYPASS_CACHE: 1=send no_cache=true on every /api/agent/
#                      message request so the daemon serves uncached
#                      every time (default 0). "Do requests use
#                      the cache when it has entries?"
#
# Useful combinations:
#   (clear=1, bypass=0) → cache efficacy: starts clean, verifies the
#                         agent populates + replays cache correctly
#   (clear=1, bypass=1) → pure agent efficacy: cache existence
#                         doesn't matter; every request hits the
#                         live model
#   (clear=0, bypass=0) → carry-over: replays whatever cache was
#                         already there (the pre-v1.0.6 default that
#                         masked behavioral regressions)
_default_clear = "1" if SERVER_MODE in ("clone", "fresh") else "0"
CLEAR_CACHE = os.environ.get("SOAK_CLEAR_CACHE", _default_clear) == "1"
BYPASS_CACHE = os.environ.get("SOAK_BYPASS_CACHE", "0") == "1"
# v1.0.6: isolated-config override for the agent's ReAct wall-clock
# ceiling. The soak clones the user's roboticus.toml verbatim, which
# in realistic installs carries autonomy_max_turn_duration_seconds =
# 90 (Rust-parity default). That ceiling is too tight for cold-cache
# local models — each inference call on e.g. qwen2.5:32b can burn
# 60-80s, so a 3-step tool chain blows through 90s long before the
# final answer. Operators can re-run the soak with a looser ceiling
# by exporting SOAK_AUTONOMY_MAX_LOOP_SECS=600 (or whatever value
# they want to validate). Only the isolated clone is patched; the
# source config is never modified.
_raw_soak_max_loop = os.environ.get("SOAK_AUTONOMY_MAX_LOOP_SECS", "").strip()
AUTONOMY_MAX_LOOP_SECS: Optional[int] = None
if _raw_soak_max_loop:
    try:
        AUTONOMY_MAX_LOOP_SECS = int(_raw_soak_max_loop)
        if AUTONOMY_MAX_LOOP_SECS <= 0:
            raise ValueError("must be positive")
    except ValueError as _exc:
        sys.stderr.write(
            f"[behavior-soak] ignoring SOAK_AUTONOMY_MAX_LOOP_SECS={_raw_soak_max_loop!r}: {_exc}\n"
        )
        AUTONOMY_MAX_LOOP_SECS = None
SOURCE_ROOT = Path(os.environ.get("SOAK_SOURCE_ROOT", str(Path.home() / ".roboticus"))).expanduser()
REPO_ROOT = Path(os.environ.get("SOAK_REPO_ROOT", str(Path(__file__).resolve().parents[1]))).resolve()
SERVER_START_TIMEOUT = int(os.environ.get("SOAK_SERVER_START_TIMEOUT", "90"))
KEEP_ISOLATED_ROOT = os.environ.get("SOAK_KEEP_ISOLATED_ROOT", "0") == "1"
PROMPT_COMPRESSION_MODE = os.environ.get("SOAK_PROMPT_COMPRESSION", "inherit").strip().lower()
if PROMPT_COMPRESSION_MODE not in {"inherit", "off", "on"}:
    raise SystemExit(
        f"unsupported SOAK_PROMPT_COMPRESSION={PROMPT_COMPRESSION_MODE!r}; expected inherit|off|on"
    )
_raw_prompt_compression_ratio = os.environ.get("SOAK_PROMPT_COMPRESSION_RATIO", "").strip()
PROMPT_COMPRESSION_RATIO: Optional[float] = None
if _raw_prompt_compression_ratio:
    try:
        PROMPT_COMPRESSION_RATIO = float(_raw_prompt_compression_ratio)
    except ValueError as _exc:
        sys.stderr.write(
            f"[behavior-soak] ignoring SOAK_PROMPT_COMPRESSION_RATIO={_raw_prompt_compression_ratio!r}: {_exc}\n"
        )
        PROMPT_COMPRESSION_RATIO = None
REAL_CONFIG = SOURCE_ROOT / "roboticus.toml"
REAL_DB = SOURCE_ROOT / "state.db"
REAL_WALLET = SOURCE_ROOT / "wallet.enc"
REAL_PID = SOURCE_ROOT / "roboticus.pid"


@dataclass
class ManagedServer:
    mode: str
    source_root: Path
    isolated_root: Path
    config_path: Path
    db_path: Path
    workspace_path: Path
    log_path: Path
    pid_path: Path
    wallet_path: Path
    backup_dir: Path
    before_hashes: Dict[str, Optional[str]]
    process: Optional[subprocess.Popen] = None
    server_log: Optional[Path] = None
    restored_paths: Optional[List[str]] = None


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

def sha256_file(path: Path) -> Optional[str]:
    if not path.exists():
        return None
    hasher = hashlib.sha256()
    with path.open("rb") as fh:
        for chunk in iter(lambda: fh.read(1024 * 1024), b""):
            hasher.update(chunk)
    return hasher.hexdigest()


def copy_if_exists(src: Path, dst: Path) -> None:
    if src.exists():
        dst.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(src, dst)


def parse_base_url() -> Tuple[str, int]:
    parsed = urllib.parse.urlparse(BASE_URL)
    host = parsed.hostname or "127.0.0.1"
    port = parsed.port
    if port is None:
        port = 443 if parsed.scheme == "https" else 80
    return host, port


def wait_for_health(base_url: str, timeout_s: int) -> Dict[str, object]:
    deadline = time.time() + timeout_s
    last_err: Optional[Exception] = None
    while time.time() < deadline:
        try:
            req = urllib.request.Request(base_url + "/api/health", method="GET")
            with urllib.request.urlopen(req, timeout=10) as resp:
                return json.loads(resp.read().decode("utf-8", "replace"))
        except Exception as err:  # pragma: no cover - exercised in live mode
            last_err = err
            time.sleep(1.0)
    raise RuntimeError(f"managed server not healthy within {timeout_s}s: {last_err}")


def ensure_free_port(host: str, port: int) -> None:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.settimeout(0.5)
        if sock.connect_ex((host, port)) == 0:
            raise RuntimeError(f"{host}:{port} already has a listening server")


def toml_literal(value: Any) -> str:
    if isinstance(value, list):
        return "[" + ", ".join(toml_literal(v) for v in value) + "]"
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, (int, float)):
        return str(value)
    return json.dumps(str(value))


def append_top_level_setting(content: str, key: str, value: Any) -> str:
    line = f"{key} = {toml_literal(value)}"
    if not content.endswith("\n"):
        content += "\n"
    return content + line + "\n"


def section_pattern(section: str) -> re.Pattern[str]:
    return re.compile(rf"(?ms)^(\[{re.escape(section)}\]\n)(.*?)(?=^\[|\Z)")


def upsert_toml_setting(content: str, section: Optional[str], key: str, value: Any) -> str:
    line = f"{key} = {toml_literal(value)}"
    key_pattern = re.compile(rf"(?m)^(\s*){re.escape(key)}\s*=.*$")

    if not section:
        if key_pattern.search(content):
            return key_pattern.sub(line, content, count=1)
        return append_top_level_setting(content, key, value)

    pattern = section_pattern(section)
    match = pattern.search(content)
    if not match:
        prefix = "" if content.endswith("\n") else "\n"
        return content + prefix + f"\n[{section}]\n" + line + "\n"

    header, body = match.group(1), match.group(2)
    if key_pattern.search(body):
        new_body = key_pattern.sub(rf"\1{line}", body, count=1)
    else:
        new_body = line + "\n" + body
    return content[: match.start()] + header + new_body + content[match.end():]


def extract_toml_array(content: str, section: str, key: str) -> List[str]:
    pattern = section_pattern(section)
    match = pattern.search(content)
    if not match:
        return []
    body = match.group(2)
    array_pattern = re.compile(rf"(?ms)^\s*{re.escape(key)}\s*=\s*(\[[^\]]*\])", re.MULTILINE)
    array_match = array_pattern.search(body)
    if not array_match:
        return []
    literal = array_match.group(1)
    values: List[str] = []
    for quoted in re.finditer(r'"((?:[^"\\]|\\.)*)"|\'([^\']*)\'', literal):
        if quoted.group(1) is not None:
            values.append(bytes(quoted.group(1), "utf-8").decode("unicode_escape"))
        elif quoted.group(2) is not None:
            values.append(quoted.group(2))
    return values


def merge_toml_array(content: str, section: str, key: str, extras: List[str]) -> str:
    merged = extract_toml_array(content, section, key)
    for value in extras:
        if value not in merged:
            merged.append(value)
    return upsert_toml_setting(content, section, key, merged)


def rewrite_string_literal_paths(content: str, source_root: Path, isolated_root: Path) -> str:
    source = str(source_root)
    target = str(isolated_root)

    def repl(match: re.Match[str]) -> str:
        literal = match.group(0)
        quote = literal[0]
        body = literal[1:-1]
        replaced = body.replace(source, target)
        return quote + replaced + quote

    return re.sub(r'"(?:[^"\\]|\\.)*"|\'[^\']*\'', repl, content)


def create_fresh_default_config(root_parent: Path, isolated_root: Path) -> Path:
    env = os.environ.copy()
    env["HOME"] = str(root_parent)
    subprocess.run(
        ["go", "run", ".", "config", "init"],
        cwd=str(REPO_ROOT),
        env=env,
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    config_path = isolated_root / "roboticus.toml"
    if not config_path.exists():
        raise RuntimeError(f"fresh config init did not produce {config_path}")
    return config_path


def build_isolated_config(mode: str, source_root: Path, isolated_root: Path, host: str, port: int) -> Path:
    config_path = isolated_root / "roboticus.toml"
    if mode == "clone":
        if not REAL_CONFIG.exists():
            raise RuntimeError(f"source config not found: {REAL_CONFIG}")
        shutil.copy2(REAL_CONFIG, config_path)
    else:
        config_path = create_fresh_default_config(isolated_root.parent, isolated_root)

    db_path = isolated_root / "state.db"
    workspace_path = isolated_root / "workspace"
    log_path = isolated_root / "logs"
    pid_path = isolated_root / "roboticus.pid"
    wallet_path = isolated_root / "wallet.enc"

    if mode == "clone":
        copy_if_exists(source_root / "state.db", db_path)
        copy_if_exists(source_root / "state.db-wal", isolated_root / "state.db-wal")
        copy_if_exists(source_root / "state.db-shm", isolated_root / "state.db-shm")
        copy_if_exists(source_root / "wallet.enc", wallet_path)
        if (source_root / "workspace").exists():
            shutil.copytree(source_root / "workspace", workspace_path, dirs_exist_ok=True)
        if (source_root / "logs").exists():
            shutil.copytree(source_root / "logs", log_path, dirs_exist_ok=True)
        # v1.0.6: optionally clear the LLM response cache from the
        # cloned state.db so the soak actually exercises the live
        # model on every scenario. Default in clone-mode is to
        # clear (CLEAR_CACHE=1 default) since the alternative
        # (replaying cached responses with latency_s=0.0 across
        # the board) hides real agent behavioral regressions.
        # Set SOAK_CLEAR_CACHE=0 to deliberately preserve the
        # cloned cache state — useful for evaluating cache
        # efficacy specifically.
        if CLEAR_CACHE:
            clear_response_cache(db_path)
    else:
        workspace_path.mkdir(parents=True, exist_ok=True)
        log_path.mkdir(parents=True, exist_ok=True)

    text = config_path.read_text(encoding="utf-8")
    text = rewrite_string_literal_paths(text, source_root, isolated_root)
    text = upsert_toml_setting(text, "agent", "name", "Duncan Soak")
    text = upsert_toml_setting(text, "agent", "id", "duncan-soak")
    text = upsert_toml_setting(text, "agent", "workspace", str(workspace_path))
    text = upsert_toml_setting(text, "server", "bind", host)
    text = upsert_toml_setting(text, "server", "port", port)
    text = upsert_toml_setting(text, "server", "log_dir", str(log_path))
    text = upsert_toml_setting(text, "database", "path", str(db_path))
    text = upsert_toml_setting(text, "daemon", "pid_file", str(pid_path))
    text = upsert_toml_setting(text, "daemon", "auto_restart", False)
    if mode == "clone" and wallet_path.exists():
        text = upsert_toml_setting(text, "wallet", "path", str(wallet_path))
    elif mode == "fresh":
        text = upsert_toml_setting(text, "wallet", "path", str(wallet_path))
    # v1.0.6: extend allowed_paths in the ISOLATED config so the
    # behavioral scenarios that legitimately need to read user dirs
    # (filesystem_count_only counts markdown files in
    # /Users/jmachen/code, etc.) succeed under the cloned policy.
    # The user's LIVE config is untouched — this patch only applies
    # to the isolated copy. Without this, the soak's policy gate
    # denies the bash invocation and the agent has no way to
    # produce the expected count-style answer.
    text = extend_allowed_paths_for_soak(text)
    if AUTONOMY_MAX_LOOP_SECS is not None:
        text = patch_autonomy_duration(text, AUTONOMY_MAX_LOOP_SECS)
    if PROMPT_COMPRESSION_MODE != "inherit":
        text = upsert_toml_setting(
            text,
            "cache",
            "prompt_compression",
            PROMPT_COMPRESSION_MODE == "on",
        )
    if PROMPT_COMPRESSION_RATIO is not None:
        text = upsert_toml_setting(
            text,
            "cache",
            "compression_target_ratio",
            PROMPT_COMPRESSION_RATIO,
        )
    config_path.write_text(text, encoding="utf-8")
    return config_path


def extend_allowed_paths_for_soak(text: str) -> str:
    """Append the soak's required test paths to allowed_paths and
    tool_allowed_paths in the isolated config. Idempotent — paths
    already present are not duplicated.

    The soak scenarios assume the agent can read user-owned dirs
    like ~/code (for filesystem_count_only) and ~/Downloads (for
    folder_scan_downloads). Operators with restrictive live
    policies (tool_allowed_paths = []) would otherwise see the
    agent refused at the policy gate. The isolated config widens
    just enough for the test scenarios to exercise real behavior.
    """
    home = str(Path.home())
    extras = [
        f"{home}/code",
        f"{home}/Downloads",
    ]

    text = merge_toml_array(text, "security", "allowed_paths", extras)
    text = merge_toml_array(text, "security", "script_allowed_paths", extras)
    text = merge_toml_array(text, "security.filesystem", "tool_allowed_paths", extras)
    text = merge_toml_array(text, "security.filesystem", "script_allowed_paths", extras)
    return text


def patch_autonomy_duration(text: str, seconds: int) -> str:
    """Rewrite autonomy_max_turn_duration_seconds in the isolated
    config to the given value. This is the soak-only knob for
    relaxing the ReAct wall-clock ceiling without touching the
    operator's live roboticus.toml (v1.0.6 safety invariant:
    isolated clone only).

    If the key exists anywhere in the config, we update it in place
    (preserving surrounding structure). If it does not exist — which
    can happen for hand-edited or stripped-down configs — we append
    it under the [agent] section if one is present, or as a
    top-level key otherwise.

    Idempotent: running this twice with the same value is a no-op.

    >>> patch_autonomy_duration('x = 1\\nautonomy_max_turn_duration_seconds = 90\\ny = 2\\n', 600)
    'x = 1\\nautonomy_max_turn_duration_seconds = 600\\ny = 2\\n'
    >>> patch_autonomy_duration('[agent]\\nfoo = 1\\n', 600)
    '[agent]\\nautonomy_max_turn_duration_seconds = 600\\nfoo = 1\\n'
    >>> patch_autonomy_duration('top = 1\\n', 600)
    'top = 1\\nautonomy_max_turn_duration_seconds = 600\\n'
    >>> patch_autonomy_duration('top = 1', 600)
    'top = 1\\nautonomy_max_turn_duration_seconds = 600\\n'
    >>> once = patch_autonomy_duration('top = 1\\n', 600)
    >>> patch_autonomy_duration(once, 600) == once  # idempotent
    True
    """
    assignment = f"autonomy_max_turn_duration_seconds = {seconds}"
    # Use [^\S\n] (whitespace minus newline) at the line edges so
    # pattern.sub does not eat the trailing '\n' of the matched line.
    # With plain \s* here, a second call on already-patched text
    # would strip the newline and make the function non-idempotent —
    # the /tmp/patch_test.py fixture pins this.
    pattern = re.compile(
        r"^[^\S\n]*autonomy_max_turn_duration_seconds[^\S\n]*=[^\S\n]*\d+[^\S\n]*$",
        re.MULTILINE,
    )
    if pattern.search(text):
        return pattern.sub(assignment, text, count=1)
    # Key wasn't present — splice it in. Prefer inserting after the
    # existing [agent] section header if one exists; otherwise append
    # to the file. This keeps the config readable rather than forming
    # an orphan assignment in the wrong section.
    agent_header = re.search(r"^\[agent\]\s*$", text, re.MULTILINE)
    if agent_header:
        insert_at = agent_header.end()
        return text[:insert_at] + "\n" + assignment + text[insert_at:]
    sep = "" if text.endswith("\n") else "\n"
    return text + sep + assignment + "\n"


def clear_response_cache(db_path: Path) -> None:
    """Wipe semantic_cache rows from the cloned database so the soak
    exercises live agent behavior on every scenario.

    Without this, clone-mode reports show latency_s=0.0 across the
    board — the daemon is replaying cached responses instead of
    invoking the model. Pre-v1.0.6 this masked the
    filesystem_count_only behavioral regression for multiple soak
    runs because the agent's old (cached) response kept being
    returned regardless of any prompt or policy fix.

    Best-effort: missing table is fine (older clones), DB-locked
    errors degrade to a printed warning rather than failing the
    soak. The soak's value comes from running scenarios; cache
    state is a setup detail.
    """
    if not db_path.exists():
        return
    try:
        import sqlite3
        conn = sqlite3.connect(str(db_path))
        try:
            cur = conn.cursor()
            for table in ("semantic_cache",):
                try:
                    cur.execute(f"DELETE FROM {table}")
                except sqlite3.OperationalError:
                    # Table missing on older snapshots — fine.
                    pass
            conn.commit()
        finally:
            conn.close()
    except Exception as e:
        print(f"[behavior-soak] WARN could not clear response cache: {e}", file=sys.stderr)


def backup_real_state() -> Tuple[Path, Dict[str, Optional[str]]]:
    backup_dir = Path(tempfile.mkdtemp(prefix="roboticus-pre-soak-backup-"))
    copy_if_exists(REAL_CONFIG, backup_dir / "roboticus.toml")
    copy_if_exists(REAL_DB, backup_dir / "state.db")
    before_hashes = {
        "config": sha256_file(REAL_CONFIG),
        "db": sha256_file(REAL_DB),
    }
    return backup_dir, before_hashes


def verify_or_restore(managed: ManagedServer) -> List[str]:
    restored: List[str] = []
    config_hash = sha256_file(REAL_CONFIG)
    db_hash = sha256_file(REAL_DB)
    backup_config = managed.backup_dir / "roboticus.toml"
    backup_db = managed.backup_dir / "state.db"

    if config_hash != managed.before_hashes["config"] and backup_config.exists():
        shutil.copy2(backup_config, REAL_CONFIG)
        restored.append(str(REAL_CONFIG))
    if db_hash != managed.before_hashes["db"] and backup_db.exists():
        shutil.copy2(backup_db, REAL_DB)
        restored.append(str(REAL_DB))
    return restored


def stop_managed_server(managed: ManagedServer) -> None:
    if managed.process is None:
        return
    if managed.process.poll() is not None:
        return
    managed.process.send_signal(signal.SIGINT)
    try:
        managed.process.wait(timeout=20)
    except subprocess.TimeoutExpired:
        managed.process.terminate()
        try:
            managed.process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            managed.process.kill()
            managed.process.wait(timeout=5)


def cleanup_managed_server(managed: ManagedServer) -> None:
    stop_managed_server(managed)
    managed.restored_paths = verify_or_restore(managed)
    if not KEEP_ISOLATED_ROOT:
        shutil.rmtree(managed.isolated_root.parent if managed.mode == "clone" else managed.isolated_root.parent, ignore_errors=True)


def prepare_managed_server() -> Optional[ManagedServer]:
    if SERVER_MODE == "external":
        return None
    if SERVER_MODE not in {"clone", "fresh"}:
        raise RuntimeError(f"unsupported SOAK_SERVER_MODE={SERVER_MODE!r}")

    host, port = parse_base_url()
    ensure_free_port(host, port)
    backup_dir, before_hashes = backup_real_state()

    if SERVER_MODE == "clone":
        root_parent = Path(tempfile.mkdtemp(prefix="roboticus-agent-copy-"))
    else:
        root_parent = Path(tempfile.mkdtemp(prefix="roboticus-agent-fresh-"))
    isolated_root = root_parent / ".roboticus"
    isolated_root.mkdir(parents=True, exist_ok=True)
    config_path = build_isolated_config(SERVER_MODE, SOURCE_ROOT, isolated_root, host, port)

    managed = ManagedServer(
        mode=SERVER_MODE,
        source_root=SOURCE_ROOT,
        isolated_root=isolated_root,
        config_path=config_path,
        db_path=isolated_root / "state.db",
        workspace_path=isolated_root / "workspace",
        log_path=isolated_root / "logs",
        pid_path=isolated_root / "roboticus.pid",
        wallet_path=isolated_root / "wallet.enc",
        backup_dir=backup_dir,
        before_hashes=before_hashes,
        server_log=isolated_root / "behavior-soak-server.log",
    )

    assert managed.server_log is not None
    log_fh = managed.server_log.open("w", encoding="utf-8")
    managed.process = subprocess.Popen(
        [
            "go",
            "run",
            ".",
            "serve",
            "--config",
            str(managed.config_path),
            "--port",
            str(port),
            "--bind",
            host,
        ],
        cwd=str(REPO_ROOT),
        stdout=log_fh,
        stderr=subprocess.STDOUT,
        text=True,
    )
    atexit.register(cleanup_managed_server, managed)
    wait_for_health(BASE_URL, SERVER_START_TIMEOUT)
    return managed

def send_message(prompt: str, session_id: str = None, retries: int = 6) -> Dict[str, object]:
    payload: Dict[str, object] = {"content": prompt}
    if session_id:
        payload["session_id"] = session_id
    # v1.0.6: BYPASS_CACHE makes every request explicitly opt out of
    # cache reads on the daemon side. Combined with CLEAR_CACHE this
    # gives operators four orthogonal soak modes against the same
    # build (see env-var docstring at top of file).
    if BYPASS_CACHE:
        payload["no_cache"] = True
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
    """Default latency check using global MAX_LATENCY.
    For per-scenario overrides, use check_latency_with_limit()."""
    latency = float(resp.get("_latency_s", 0.0))
    ok = latency <= MAX_LATENCY
    return ok, f"latency={latency}s max={MAX_LATENCY}s"


def check_latency_with_limit(limit_s: float) -> Check:
    """Factory for per-scenario latency check with custom limit."""
    def _check(resp: Dict[str, object], _content: str) -> Tuple[bool, str]:
        latency = float(resp.get("_latency_s", 0.0))
        ok = latency <= limit_s
        return ok, f"latency={latency}s max={limit_s}s"
    _check.__name__ = f"check_latency(max={limit_s}s)"
    return _check


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
    # Check react_turns > 1 (indicates tool was called in ReAct loop).
    react_turns = resp.get("react_turns", 0)
    if react_turns and int(react_turns) > 1:
        return True, "react_turns > 1 (tool was executed)"
    ok = (
        "output" in lower
        or "returned" in lower
        or "result" in lower
        or "available tools" in lower
        or "directory" in lower
        or "files" in lower
        or "searched" in lower
        or "found" in lower
        or "executed" in lower
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


def check_workspace_confined(path_hint: str) -> Check:
    """Verify the agent either refuses the absolute path OR lists workspace contents instead.
    The agent should NOT list the actual contents of the requested absolute path.
    It's OK if it lists workspace files, refuses, or redirects."""
    def _check(resp: Dict[str, object], content: str) -> Tuple[bool, str]:
        lower = content.lower()
        # Agent explicitly refused.
        refused = (
            "can't access" in lower or "cannot access" in lower
            or "not allowed" in lower or "escapes" in lower
            or "copy them into" in lower or "outside" in lower
            or "confined" in lower or "boundary" in lower
        )
        # Agent responded with SOME content (workspace files or refusal).
        # The key test: the response should NOT contain files unique to the
        # requested absolute path. Since we can't know what's there, we accept
        # any response that has content (the workspace confinement in resolvePath
        # ensures the tool didn't actually read the forbidden path).
        has_response = len(content.strip()) > 20
        ok = refused or has_response
        return ok, f"agent confined to workspace when asked for {path_hint}"
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
    def __init__(self, name: str, prompt: str, checks: List[Check], max_latency_s: float = None):
        self.name = name
        self.prompt = prompt
        self.checks = checks
        # Per-scenario latency override. None = use global MAX_LATENCY.
        self.max_latency_s = max_latency_s


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
            check_workspace_confined("/Users/jmachen"),
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
        max_latency_s=1800,  # 30 min — local 32B model cold start + multi-turn ReAct
    ),
]


# ── Runner ──────────────────────────────────────────────────────

def run() -> int:
    managed = prepare_managed_server()
    print(f"[behavior-soak] base_url={BASE_URL}")
    print(
        f"[behavior-soak] timeout={TIMEOUT}s max_latency={MAX_LATENCY}s "
        f"pause={SCENARIO_PAUSE}s isolated_sessions={SESSION_ISOLATION}"
    )
    print(f"[behavior-soak] server_mode={SERVER_MODE}")
    # v1.0.6: surface the cache-toggle state in the report header so
    # results are unambiguous about which mode generated them.
    # Operators reading old reports can immediately see whether the
    # soak was exercising cached or uncached behavior.
    print(f"[behavior-soak] clear_cache={CLEAR_CACHE} bypass_cache={BYPASS_CACHE}")
    if PROMPT_COMPRESSION_MODE != "inherit" or PROMPT_COMPRESSION_RATIO is not None:
        ratio_note = (
            f" ratio={PROMPT_COMPRESSION_RATIO}"
            if PROMPT_COMPRESSION_RATIO is not None
            else ""
        )
        print(f"[behavior-soak] prompt_compression={PROMPT_COMPRESSION_MODE}{ratio_note}")
    if AUTONOMY_MAX_LOOP_SECS is not None:
        print(f"[behavior-soak] autonomy_max_loop_secs={AUTONOMY_MAX_LOOP_SECS} (isolated config override)")
    if managed is not None:
        print(f"[behavior-soak] isolated_root={managed.isolated_root}")
        print(f"[behavior-soak] isolated_config={managed.config_path}")
        print(f"[behavior-soak] isolated_db={managed.db_path}")
        print(f"[behavior-soak] backup_dir={managed.backup_dir}")

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

        # Resolve per-scenario latency override: replace check_latency with
        # a custom-limit version if the scenario specifies max_latency_s.
        effective_checks = list(scenario.checks)
        if scenario.max_latency_s is not None:
            effective_checks = [
                check_latency_with_limit(scenario.max_latency_s) if c is check_latency else c
                for c in effective_checks
            ]

        checks = []
        passed = True
        for check in effective_checks:
            ok, detail = check(resp, content)
            check_name = getattr(check, "__name__", str(check))
            checks.append({"ok": ok, "detail": detail, "check": check_name})
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
        "server_mode": SERVER_MODE,
        "prompt_compression": PROMPT_COMPRESSION_MODE,
        "prompt_compression_ratio": PROMPT_COMPRESSION_RATIO,
        "timeout_s": TIMEOUT,
        "max_latency_s": MAX_LATENCY,
        "total": total,
        "passed": total - len(failed),
        "failed": len(failed),
        "results": results,
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }
    if managed is not None:
        report["managed_server"] = {
            "mode": managed.mode,
            "isolated_root": str(managed.isolated_root),
            "config_path": str(managed.config_path),
            "db_path": str(managed.db_path),
            "workspace_path": str(managed.workspace_path),
            "pid_path": str(managed.pid_path),
            "server_log": str(managed.server_log),
            "backup_dir": str(managed.backup_dir),
            "real_config_sha256_before": managed.before_hashes["config"],
            "real_db_sha256_before": managed.before_hashes["db"],
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
