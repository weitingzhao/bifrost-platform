#!/usr/bin/env python3
"""Layer 3 — live API vs static catalog cross-check (no LLM).

Compares spine/catalog authority files with GET /api/v1/context and
visionSpineMap milestone IDs. Surfaces semantic drift where Console
catalogs and runtime spine disagree.

Stdout: markdown report. Exit 0 if no findings, 1 if drift detected.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import urllib.error
import urllib.request
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Any

DEFAULT_PLATFORM_API = "http://127.0.0.1:8780"


@dataclass
class Finding:
    kind: str
    detail: str


def platform_root_from_script() -> Path:
    return Path(__file__).resolve().parent.parent.parent


def grep_version(pattern: str, text: str) -> str | None:
    m = re.search(pattern, text, re.MULTILINE)
    return m.group(1) if m else None


def read_catalog_versions(platform: Path) -> tuple[str | None, str | None]:
    yaml_path = platform / "config/ops-context.yaml"
    ts_path = platform / "console/src/lib/environments-catalog.ts"
    yaml_ver = None
    ts_ver = None
    if yaml_path.is_file():
        yaml_ver = grep_version(r"^\s*catalog_version:\s*\"([^\"]+)\"", yaml_path.read_text())
    if ts_path.is_file():
        ts_ver = grep_version(r"^export const CATALOG_VERSION\s*=\s*'([^']+)'", ts_path.read_text())
    return yaml_ver, ts_ver


def read_vision_spine_ids(platform: Path) -> list[str]:
    path = platform / "console/src/lib/architecture/visionSpineMap.ts"
    if not path.is_file():
        return []
    text = path.read_text(encoding="utf-8")
    return re.findall(r"spineMilestoneId:\s*'([^']+)'", text)


def read_yaml_stream(platform: Path, stream_id: str) -> dict[str, Any] | None:
    """Parse a single automate stream block from ops-context.yaml (lightweight)."""
    yaml_path = platform / "config/ops-context.yaml"
    if not yaml_path.is_file():
        return None
    text = yaml_path.read_text(encoding="utf-8")
    pattern = (
        rf"- id: {re.escape(stream_id)}\n"
        r"[^\n]*\n"
        r"[^\n]*\n"
        r"[^\n]*\n"
        r"\s+total: (\d+)\n"
        r"\s+done: (\d+)\n"
        r"\s+status: (\w+)"
    )
    m = re.search(pattern, text)
    if not m:
        return None
    return {"total": int(m.group(1)), "done": int(m.group(2)), "status": m.group(3)}


def fetch_context(base_url: str) -> tuple[dict[str, Any] | None, str]:
    url = base_url.rstrip("/") + "/api/v1/context"
    req = urllib.request.Request(url, headers={"Accept": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=20) as resp:
            return json.loads(resp.read().decode()), ""
    except urllib.error.HTTPError as exc:
        body = exc.read().decode(errors="replace")[:200]
        return None, f"HTTP {exc.code}: {body}"
    except urllib.error.URLError as exc:
        return None, str(exc.reason)


def find_automate_stream(context: dict[str, Any], stream_id: str) -> dict[str, Any] | None:
    tracks = context.get("tracks") or {}
    automate = tracks.get("automate") or {}
    streams = automate.get("streams") or []
    for s in streams:
        if s.get("id") == stream_id:
            return s
    return None


def format_report(findings: list[Finding], platform: Path, api_base: str) -> str:
    stamp = datetime.now().isoformat(timespec="seconds")
    lines = [
        "## Layer 3 — Semantic / spine drift scan",
        f"- Platform root: `{platform}`",
        f"- Context API: `{api_base}`",
        f"- Scanned at: {stamp}",
        f"- Findings: {len(findings)}",
        "",
    ]
    if not findings:
        lines.append("Live spine matches static catalog authorities.")
        return "\n".join(lines)

    by_kind: dict[str, list[Finding]] = {}
    for f in findings:
        by_kind.setdefault(f.kind, []).append(f)

    for kind, items in sorted(by_kind.items()):
        lines.append(f"### {kind} ({len(items)})")
        for f in items:
            lines.append(f"- {f.detail}")
        lines.append("")
    return "\n".join(lines)


def main() -> int:
    parser = argparse.ArgumentParser(description="Layer 3 semantic drift scan")
    parser.add_argument(
        "--platform-root",
        type=Path,
        default=None,
        help="Path to bifrost-platform",
    )
    parser.add_argument(
        "--platform-api",
        default=os.environ.get("PLATFORM_API_URL", DEFAULT_PLATFORM_API),
        help="platform-api base URL",
    )
    args = parser.parse_args()

    platform = args.platform_root or platform_root_from_script()
    findings: list[Finding] = []

    yaml_ver, ts_ver = read_catalog_versions(platform)
    if yaml_ver and ts_ver and yaml_ver != ts_ver:
        findings.append(
            Finding(
                "catalog_version",
                f"ops-context.yaml `{yaml_ver}` ≠ environments-catalog.ts `{ts_ver}`",
            )
        )

    context, err = fetch_context(args.platform_api)
    if context is None:
        findings.append(Finding("api_unreachable", f"GET /api/v1/context failed: {err}"))
    else:
        api_meta_ver = (context.get("meta") or {}).get("catalog_version")
        if yaml_ver and api_meta_ver and yaml_ver != api_meta_ver:
            findings.append(
                Finding(
                    "catalog_version",
                    f"ops-context.yaml `{yaml_ver}` ≠ live API meta `{api_meta_ver}` "
                    "(restart platform-api or sync config)",
                )
            )

        milestone_ids = {m.get("id") for m in context.get("milestones") or []}
        for spine_id in read_vision_spine_ids(platform):
            if spine_id not in milestone_ids:
                findings.append(
                    Finding(
                        "vision_spine",
                        f"visionSpineMap spineMilestoneId `{spine_id}` missing from API milestones",
                    )
                )

        yaml_stream = read_yaml_stream(platform, "nightly-drift-scan")
        api_stream = find_automate_stream(context, "nightly-drift-scan")
        if yaml_stream and api_stream:
            for field in ("total", "done", "status"):
                yaml_val = yaml_stream.get(field)
                api_val = api_stream.get(field)
                if yaml_val is not None and api_val is not None and yaml_val != api_val:
                    findings.append(
                        Finding(
                            "track_stream",
                            f"nightly-drift-scan {field}: yaml `{yaml_val}` ≠ API `{api_val}` "
                            "(platform-api may need restart to reload ops-context.yaml)",
                        )
                    )

    report = format_report(findings, platform, args.platform_api)
    print(report)
    return 1 if findings else 0


if __name__ == "__main__":
    sys.exit(main())
