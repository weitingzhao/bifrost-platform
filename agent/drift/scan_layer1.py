#!/usr/bin/env python3
"""Layer 1 deterministic catalog drift scan (no LLM).

Scans bifrost-platform console catalog TypeScript for:
  - Repo-relative path references that do not exist on disk
  - Known service port literals that differ from canonical map

Stdout: markdown report. Exit 0 if no findings, 1 if drift detected.
"""

from __future__ import annotations

import argparse
import re
import sys
from dataclasses import dataclass
from pathlib import Path

# Canonical ports — mirror console/src/lib/environments-catalog.ts PLATFORM_PORTS + trade API range
CANONICAL_PORTS: dict[int, str] = {
    5173: "trade frontend",
    5180: "platform console",
    5555: "flower",
    8050: "infra docs",
    8060: "platform docs",
    8780: "platform-api",
    8781: "remediation runner",
    8765: "api-monitor",
    8766: "api-massive",
    8767: "api-docs",
    8768: "api-ops",
    8769: "api-trading",
    8770: "api-strategy",
    8771: "api-portfolio",
    8772: "api-market",
    8773: "api-research",
    30878: "platform-console-stg NodePort",
    30879: "platform-api-stg NodePort",
}

# Ports allowed in catalog strings without flagging (SSH, PG, Redis, HTTP alt)
PORT_ALLOWLIST = {22, 80, 443, 3000, 4001, 4002, 5432, 6379, 6443, 7496, 7497, 8080, 30500, 30880, 30882}

PATH_PATTERNS = [
    re.compile(r"bifrost-[a-z0-9-]+/[a-zA-Z0-9_./-]+"),
    re.compile(r"docs/[a-zA-Z0-9_./-]+\.md"),
    re.compile(r"config/[a-zA-Z0-9_./-]+\.ya?ml"),
    re.compile(r"k8s/[a-zA-Z0-9_./-]+"),
    re.compile(r"\.cursor/[a-zA-Z0-9_./-]+"),
]

PORT_PATTERN = re.compile(r":(\d{4,5})\b")

# Tekton pipeline slugs in API routes — not repo paths (e.g. bifrost-deliver-stg/runs)
TEKTON_PIPELINE_REF = re.compile(r"^bifrost-deliver-[a-z0-9-]+/")


def should_skip_path_ref(line: str, ref: str, match_start: int) -> bool:
    """Filter provenance comments and API route fragments."""
    if TEKTON_PIPELINE_REF.match(ref):
        return True
    if "Migrated from" in line and ref in line:
        return True
    prefix = line[max(0, match_start - 80):match_start]
    if "/api/" in prefix or "/pipelines/" in prefix:
        return True
    # JSDoc provenance lines only document migration source, not live links
    stripped = line.strip()
    if stripped.startswith("*") and "docs/" in ref:
        return True
    return False


@dataclass
class Finding:
    kind: str
    file: str
    line: int
    detail: str


def platform_root() -> Path:
    return Path(__file__).resolve().parent.parent.parent


def stocks_root(platform: Path) -> Path:
    return platform.parent


def scan_roots(platform: Path) -> list[Path]:
    lib = platform / "console" / "src" / "lib"
    config = platform / "config"
    roots: list[Path] = []
    if lib.is_dir():
        roots.append(lib)
    if config.is_dir():
        roots.append(config)
    return roots


def resolve_candidate(stocks: Path, candidate: str) -> Path | None:
    """Try to resolve a path string against stocks workspace."""
    candidate = candidate.strip().strip("`\"'")
    if candidate.startswith("http://") or candidate.startswith("https://"):
        return None

    candidates_to_try = [candidate]
    if candidate.startswith("docs/"):
        candidates_to_try.extend(
            [
                f"bifrost-trade-infra/{candidate}",
                f"bifrost-platform/{candidate}",
                f"bifrost-trade-frontend/{candidate}",
            ]
        )
    if candidate.startswith("k8s/"):
        candidates_to_try.append(f"bifrost-trade-infra/{candidate}")
    if candidate.startswith(".cursor/"):
        candidates_to_try.extend(
            [
                f"bifrost-trade-frontend/{candidate}",
                f"bifrost-platform/{candidate}",
            ]
        )

    for cand in candidates_to_try:
        cand = re.sub(r"[.,;:)]+$", "", cand)
        direct = stocks / cand
        if direct.exists():
            return direct
        plat = stocks / "bifrost-platform" / cand
        if plat.exists():
            return plat

    return None


def scan_file(stocks: Path, path: Path, findings: list[Finding]) -> None:
    try:
        text = path.read_text(encoding="utf-8")
    except OSError as exc:
        findings.append(Finding("read_error", str(path), 0, str(exc)))
        return

    lines = text.splitlines()
    for i, line in enumerate(lines, start=1):
        if line.strip().startswith("//"):
            continue

        for pattern in PATH_PATTERNS:
            for match in pattern.finditer(line):
                ref = match.group(0)
                if ref.endswith(".ts") or ref.endswith(".tsx") or "/" in ref:
                    # Skip shorter docs/k8s hits inside a longer bifrost-* path on same line
                    if ref.startswith("docs/") or ref.startswith("k8s/"):
                        start = match.start()
                        prefix = line[max(0, start - 40):start]
                        if "bifrost-" in prefix:
                            continue
                    if should_skip_path_ref(line, ref, match.start()):
                        continue
                    resolved = resolve_candidate(stocks, ref)
                    if resolved is None and not ref.endswith("/"):
                        findings.append(
                            Finding(
                                "missing_path",
                                str(path.relative_to(stocks / "bifrost-platform")),
                                i,
                                f"`{ref}` not found under stocks workspace",
                            )
                        )

        for match in PORT_PATTERN.finditer(line):
            port = int(match.group(1))
            if port in PORT_ALLOWLIST:
                continue
            if port in CANONICAL_PORTS:
                continue
            if 8765 <= port <= 8773:
                continue
            findings.append(
                Finding(
                    "unknown_port",
                    str(path.relative_to(stocks / "bifrost-platform")),
                    i,
                    f"port :{port} not in canonical map — verify or add to CANONICAL_PORTS",
                )
            )


def format_report(findings: list[Finding], platform: Path) -> str:
    stamp = __import__("datetime").datetime.now().isoformat(timespec="seconds")
    lines = [
        "## Layer 1 — Catalog drift scan",
        f"- Platform root: `{platform}`",
        f"- Scanned at: {stamp}",
        f"- Findings: {len(findings)}",
        "",
    ]
    if not findings:
        lines.append("No deterministic drift detected.")
        return "\n".join(lines)

    by_kind: dict[str, list[Finding]] = {}
    for f in findings:
        by_kind.setdefault(f.kind, []).append(f)

    for kind, items in sorted(by_kind.items()):
        lines.append(f"### {kind} ({len(items)})")
        for f in items[:50]:
            loc = f"{f.file}:{f.line}" if f.line else f.file
            lines.append(f"- `{loc}` — {f.detail}")
        if len(items) > 50:
            lines.append(f"- … and {len(items) - 50} more")
        lines.append("")

    return "\n".join(lines)


def main() -> int:
    parser = argparse.ArgumentParser(description="Layer 1 catalog drift scan")
    parser.add_argument(
        "--platform-root",
        type=Path,
        default=None,
        help="Path to bifrost-platform (default: auto-detect from script location)",
    )
    args = parser.parse_args()

    platform = args.platform_root or platform_root()
    stocks = stocks_root(platform)
    roots = scan_roots(platform)

    if not roots:
        print(f"Error: no scan roots under {platform}", file=sys.stderr)
        return 2

    findings: list[Finding] = []
    for root in roots:
        for ts_file in sorted(root.rglob("*.ts")):
            if "node_modules" in ts_file.parts:
                continue
            scan_file(stocks, ts_file, findings)

    report = format_report(findings, platform)
    print(report)
    return 1 if findings else 0


if __name__ == "__main__":
    sys.exit(main())
