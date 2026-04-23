#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import json
import os
from datetime import datetime, timezone
from pathlib import Path

try:
    import tomllib
except ModuleNotFoundError:  # pragma: no cover
    tomllib = None


def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def rfc3339_mtime(path: Path) -> str:
    return datetime.fromtimestamp(path.stat().st_mtime, tz=timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def load_toml(path: Path) -> dict:
    if not path.exists() or tomllib is None:
        return {}
    with path.open("rb") as f:
        return tomllib.load(f)


def collect_skill_hashes(skills_dir: Path) -> tuple[dict[str, str], str]:
    hashes: dict[str, str] = {}
    latest: datetime | None = None
    for path in sorted(p for p in skills_dir.rglob("*") if p.is_file()):
        rel = path.relative_to(skills_dir).as_posix()
        hashes[rel] = sha256_file(path)
        mtime = datetime.fromtimestamp(path.stat().st_mtime, tz=timezone.utc)
        if latest is None or mtime > latest:
            latest = mtime
    installed_at = ""
    if latest is not None:
        installed_at = latest.replace(microsecond=0).isoformat().replace("+00:00", "Z")
    return hashes, installed_at


def main() -> int:
    parser = argparse.ArgumentParser(description="Fallback utility: repair Roboticus updater state from an existing local install when automatic reconciliation was not enough.")
    parser.add_argument("--root", default=str(Path.home() / ".roboticus"), help="Roboticus home directory (default: ~/.roboticus)")
    parser.add_argument("--binary-version", default="", help="Binary version to record when update_state.json is missing or incomplete")
    parser.add_argument("--registry-url", default="", help="Registry URL override to record in repaired state")
    parser.add_argument("--apply", action="store_true", help="Write the repaired update_state.json back to disk after previewing the fallback repair")
    args = parser.parse_args()

    root = Path(args.root).expanduser()
    config_path = root / "roboticus.toml"
    state_path = root / "update_state.json"
    raw = load_toml(config_path)

    providers_path = Path(raw.get("providers_file") or (root / "providers.toml"))
    skills_dir = Path(((raw.get("skills") or {}).get("directory")) or (root / "skills"))
    registry_url = args.registry_url or ((raw.get("update") or {}).get("registry_url")) or ""

    state: dict = {}
    if state_path.exists():
        state = json.loads(state_path.read_text())

    state.setdefault("binary_version", "")
    state.setdefault("last_check", "")
    state.setdefault("registry_url", "")
    state.setdefault("installed_content", {})
    installed_content = state["installed_content"]

    repaired = False
    if not state["binary_version"] and args.binary_version:
        state["binary_version"] = args.binary_version.lstrip("v").strip()
        repaired = True
    if not state["registry_url"] and registry_url:
        state["registry_url"] = registry_url
        repaired = True

    if installed_content.get("providers") is None and providers_path.exists():
        installed_content["providers"] = {
            "version": "unknown",
            "sha256": sha256_file(providers_path),
            "installed_at": rfc3339_mtime(providers_path),
        }
        repaired = True

    if installed_content.get("skills") is None and skills_dir.exists() and skills_dir.is_dir():
        files, installed_at = collect_skill_hashes(skills_dir)
        if files:
            installed_content["skills"] = {
                "version": "unknown",
                "files": files,
                "installed_at": installed_at,
            }
            repaired = True

    if repaired and not state["last_check"]:
        state["last_check"] = datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")

    report = {
        "root": str(root),
        "config_path": str(config_path),
        "update_state_path": str(state_path),
        "providers_path": str(providers_path),
        "skills_dir": str(skills_dir),
        "repaired": repaired,
        "state": state,
    }
    print(json.dumps(report, indent=2))

    if args.apply and repaired:
        root.mkdir(mode=0o700, parents=True, exist_ok=True)
        state_path.write_text(json.dumps(state, indent=2) + "\n")
        os.chmod(state_path, 0o600)

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
