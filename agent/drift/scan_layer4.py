#!/usr/bin/env python3
"""Layer 4 — build drift auto-fix proposal JSON for platform-api (Owner approval gate).

Reads nightly scan exits + report excerpt. Does not apply fixes.
Stdout: JSON object for POST /api/v1/agent/drift-proposals
Exit 0 if no drift (no proposal needed), 1 if proposal should be created.
"""

from __future__ import annotations

import argparse
import json
import sys
from datetime import datetime, timezone
from pathlib import Path


def layer_excerpt(report_text: str, heading: str, max_lines: int = 25) -> str:
    lines = report_text.splitlines()
    out: list[str] = []
    in_section = False
    for line in lines:
        if line.startswith("## ") and heading.lower() in line.lower():
            in_section = True
            out.append(line)
            continue
        if in_section and line.startswith("## "):
            break
        if in_section:
            out.append(line)
            if len(out) >= max_lines:
                out.append("…")
                break
    return "\n".join(out).strip()


def main() -> int:
    parser = argparse.ArgumentParser(description="Layer 4 drift proposal builder")
    parser.add_argument("--report", required=True, help="Path to nightly markdown report")
    parser.add_argument("--l1-exit", type=int, default=0)
    parser.add_argument("--l2-exit", type=int, default=0)
    parser.add_argument("--l3-exit", type=int, default=0)
    parser.add_argument("--host", default="")
    parser.add_argument("--platform-api", default="")
    args = parser.parse_args()

    layers_failed: list[str] = []
    if args.l1_exit != 0:
        layers_failed.append("layer1")
    if args.l2_exit != 0:
        layers_failed.append("layer2")
    if args.l3_exit != 0:
        layers_failed.append("layer3")

    if not layers_failed:
        print(json.dumps({"has_drift": False}))
        return 0

    report_path = Path(args.report)
    report_text = report_path.read_text(encoding="utf-8") if report_path.is_file() else ""

    excerpts: list[str] = []
    for layer in layers_failed:
        key = {
            "layer1": "Layer 1",
            "layer2": "Layer 2",
            "layer3": "Layer 3",
        }.get(layer, layer)
        chunk = layer_excerpt(report_text, key)
        if chunk:
            excerpts.append(chunk)

    summary = "\n\n".join(excerpts) if excerpts else report_text[:2000]
    findings_count = sum(1 for line in report_text.splitlines() if line.strip().startswith("- "))

    proposal = {
        "has_drift": True,
        "host": args.host or None,
        "platform_api": args.platform_api or None,
        "report_source": str(report_path),
        "layers_failed": layers_failed,
        "findings_count": findings_count,
        "summary": summary,
        "created_at": datetime.now(timezone.utc).isoformat(),
    }
    print(json.dumps(proposal, ensure_ascii=False))
    return 1


if __name__ == "__main__":
    sys.exit(main())
