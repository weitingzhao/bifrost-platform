#!/usr/bin/env python3
"""Layer 3 — live API vs static catalog cross-check (no LLM).

Compares spine/catalog authority files with GET /api/v1/context and
visionSpineMap milestone IDs. Surfaces semantic drift where Console
catalogs and runtime spine disagree.

Authority for briefing content drift extensions:
  console/src/lib/architecture/briefingReconciliationCatalog.ts (DRIFT_LAYER_MAP)

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
from typing import Any, Callable

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


def read_trade_k8s_waves(platform: Path) -> list[tuple[str, int]]:
    """Ordered (wave id, spineIndex) pairs from tradeK8sNativeCatalog.ts (D-C).

    Mirrors TRADE_K8S_NATIVE_WAVES so the offline scan reconciles the SAME
    catalog spineIndex that the runtime projectWaveStatus consumes.
    """
    path = platform / "console/src/lib/architecture/tradeK8sNativeCatalog.ts"
    if not path.is_file():
        return []
    text = path.read_text(encoding="utf-8")
    pairs = re.findall(r"wave:\s*'(W\d+)',\s*\n\s*spineIndex:\s*(\d+)", text)
    return [(wave, int(idx)) for wave, idx in pairs]


def project_wave_status(spine_index: int, done: int, ready: int, status: str) -> str:
    """Offline mirror of waveProjection.ts projectWaveStatus (D-A/D-C)."""
    s = (status or "").lower()
    is_closed = s in ("closed", "signed")
    if spine_index < done:
        return "done"
    if spine_index < done + ready:
        return "ready_for_signoff"
    if is_closed:
        return "done"
    if spine_index == done + ready and s == "in_progress":
        return "next"
    return "pending"


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


def find_migrate_stream(context: dict[str, Any], stream_id: str) -> dict[str, Any] | None:
    tracks = context.get("tracks") or {}
    migrate = tracks.get("migrate") or {}
    streams = migrate.get("streams") or []
    for s in streams:
        if s.get("id") == stream_id:
            return s
    return None


def read_data_layer_phases(platform: Path) -> list[tuple[str, int, str]]:
    """Ordered (phase id, spineIndex, displayCode) from dataLayerCatalog.ts (D-C)."""
    path = platform / "console/src/lib/architecture/dataLayerCatalog.ts"
    if not path.is_file():
        return []
    text = path.read_text(encoding="utf-8")
    blocks = re.findall(
        r"id:\s*'(data-[^']+)',\s*\n\s*step:\s*\d+,\s*\n\s*spineIndex:\s*(\d+),"
        r"\s*\n\s*displayCode:\s*'([^']+)'",
        text,
    )
    return [(pid, int(idx), code) for pid, idx, code in blocks]


def check_migrate_stream_reconcile(
    context: dict[str, Any],
    stream_id: str,
    waves: list[tuple[str, int]],
    headline_code_at: Callable[[int], str] | None = None,
) -> list[Finding]:
    """Shared reconcile gate for a migrate stream catalog (trade-k8s-native, data-layer-k3s)."""
    findings: list[Finding] = []
    stream = find_migrate_stream(context, stream_id)
    if stream is None:
        return findings

    wave_ids = [w for w, _ in waves]
    idx_by_wave = {w: idx for w, idx in waves}
    total = stream.get("total")
    done = stream.get("done") or 0
    ready = stream.get("ready_for_signoff") or 0
    status = (stream.get("status") or "").lower()
    headline = ((context.get("focus") or {}).get("headline")) or ""

    if wave_ids and total is not None and len(wave_ids) != total:
        findings.append(
            Finding(
                "briefing_reconcile",
                f"{stream_id} gate-wave-count-vs-total: catalog has {len(wave_ids)} waves "
                f"but spine total={total}",
            )
        )

    if waves:
        indices = sorted(idx for _, idx in waves)
        if indices != list(range(len(indices))):
            findings.append(
                Finding(
                    "briefing_reconcile",
                    f"{stream_id} gate-spineindex-contiguous: spineIndex not 0..{len(indices) - 1} "
                    f"(got {indices})",
                )
            )

    if total is not None and done + ready > total:
        findings.append(
            Finding(
                "briefing_reconcile",
                f"{stream_id} gate-done-ready-bounds: done({done}) + ready_for_signoff({ready}) "
                f"> total({total})",
            )
        )

    if waves:
        projected = {w: project_wave_status(idx, done, ready, status) for w, idx in waves}
        for w, v in projected.items():
            if v == "done" and idx_by_wave[w] >= done and status not in ("closed", "signed"):
                findings.append(
                    Finding(
                        "briefing_reconcile",
                        f"{stream_id} gate-queue-vs-spine-done: {w} projected done but spineIndex "
                        f"{idx_by_wave[w]} >= done({done})",
                    )
                )

        if status not in ("closed", "signed"):
            done_count = sum(1 for v in projected.values() if v == "done")
            ready_count = sum(1 for v in projected.values() if v == "ready_for_signoff")
            next_count = sum(1 for v in projected.values() if v == "next")
            if done_count != done:
                findings.append(
                    Finding(
                        "briefing_reconcile",
                        f"{stream_id} gate-appendix-vs-queue: projected done={done_count} "
                        f"≠ spine.done({done})",
                    )
                )
            if ready_count != ready:
                findings.append(
                    Finding(
                        "briefing_reconcile",
                        f"{stream_id} gate-appendix-vs-queue: projected ready_for_signoff="
                        f"{ready_count} ≠ spine.ready_for_signoff({ready})",
                    )
                )
            if next_count > 1:
                findings.append(
                    Finding(
                        "briefing_reconcile",
                        f"{stream_id} gate-appendix-vs-queue: {next_count} waves projected next "
                        f"(expected <=1)",
                    )
                )

    if wave_ids and status == "in_progress" and headline_code_at is not None:
        active_idx = done + ready
        if 0 <= active_idx < len(wave_ids):
            active_code = headline_code_at(active_idx)
            if active_code not in headline:
                findings.append(
                    Finding(
                        "briefing_reconcile",
                        f"{stream_id} gate-headline-vs-next-task: focus.headline missing active "
                        f"code `{active_code}` (D-D)",
                    )
                )

    return findings


def check_trade_k8s_reconcile(
    context: dict[str, Any], platform: Path
) -> list[Finding]:
    """Briefing reconcile gate (offline) — full parity with reconcileBriefing.ts."""
    waves = sorted(read_trade_k8s_waves(platform), key=lambda t: t[1])
    wave_ids = [w for w, _ in waves]
    return check_migrate_stream_reconcile(
        context,
        "trade-k8s-native",
        waves,
        headline_code_at=lambda i: wave_ids[i] if i < len(wave_ids) else "",
    )


def check_data_layer_reconcile(
    context: dict[str, Any], platform: Path
) -> list[Finding]:
    """Briefing reconcile gate for data-layer-k3s — parity with reconcileBriefing.ts S13."""
    phases = sorted(read_data_layer_phases(platform), key=lambda t: t[1])
    waves = [(pid, idx) for pid, idx, _ in phases]
    codes = [code for _, _, code in phases]
    return check_migrate_stream_reconcile(
        context,
        "data-layer-k3s",
        waves,
        headline_code_at=lambda i: codes[i] if i < len(codes) else "",
    )


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

        # Briefing reconcile gate (DRIFT_LAYER_MAP L3 — full SYNC parity)
        findings.extend(check_trade_k8s_reconcile(context, platform))
        findings.extend(check_data_layer_reconcile(context, platform))

    report = format_report(findings, platform, args.platform_api)
    print(report)
    return 1 if findings else 0


if __name__ == "__main__":
    sys.exit(main())
