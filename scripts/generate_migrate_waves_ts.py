#!/usr/bin/env python3
"""Generate console migrate wave arrays from config/migrate-waves/*.yaml.

Usage:
  python3 scripts/generate_migrate_waves_ts.py          # write generated file
  python3 scripts/generate_migrate_waves_ts.py --check  # exit 1 if stale vs YAML
"""
from __future__ import annotations
import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "console/src/lib/architecture/migrateWaves.generated.ts"


def parse_yaml_simple(path: Path):
    text = path.read_text()
    waves, cur = [], None
    for line in text.splitlines():
        if line.startswith("  - id:"):
            if cur:
                waves.append(cur)
            cur = {"id": line.split(":", 1)[1].strip()}
        elif cur is not None and line.startswith("    "):
            k, v = line.strip().split(":", 1)
            v = v.strip()
            if v.startswith('"') and v.endswith('"'):
                v = json.loads(v)
            elif k == "spine_index":
                v = int(v)
            cur[k] = v
    if cur:
        waves.append(cur)
    return waves


def ts_str(s):
    return json.dumps(s, ensure_ascii=False)


def render() -> str:
    trade = parse_yaml_simple(ROOT / "config/migrate-waves/trade-k8s-native.yaml")
    data = parse_yaml_simple(ROOT / "config/migrate-waves/data-layer-k3s.yaml")
    lines = [
        "// AUTO-GENERATED from config/migrate-waves/*.yaml — do not edit by hand.",
        "// Regenerate: python3 scripts/generate_migrate_waves_ts.py",
        "",
        "import type { TradeK8sNativeWave } from './tradeK8sNativeCatalogTypes'",
        "import type { DataLayerMigrationPhase } from './dataLayerCatalogTypes'",
        "",
        "export const GENERATED_TRADE_K8S_NATIVE_WAVES: TradeK8sNativeWave[] = [",
    ]
    for w in trade:
        lines += [
            "  {",
            f"    id: {ts_str(w['id'])},",
            f"    wave: {ts_str(w['code'])},",
            f"    spineIndex: {w['spine_index']},",
            f"    label: {ts_str(w['label'])},",
            f"    repo: {ts_str(w.get('repo', ''))},",
            f"    verify: {ts_str(w.get('verify', ''))},",
        ]
        if w.get("blocked_by"):
            lines.append(f"    blockedBy: {ts_str(w['blocked_by'])},")
        if w.get("delivered"):
            lines.append(f"    delivered: {ts_str(w['delivered'])},")
        lines.append("  },")
    lines.append("]")
    lines.append("")
    lines.append("export const GENERATED_DATA_LAYER_PHASES: DataLayerMigrationPhase[] = [")
    for w in data:
        lines += [
            "  {",
            f"    id: {ts_str(w['id'])},",
            f"    step: {w['spine_index'] + 1},",
            f"    spineIndex: {w['spine_index']},",
            f"    displayCode: {ts_str(w['code'])},",
            f"    label: {ts_str(w['label'])},",
            f"    repo: {ts_str(w.get('repo', ''))},",
            f"    verify: {ts_str(w.get('verify', ''))},",
        ]
        if w.get("blocked_by"):
            lines.append(f"    blockedBy: {ts_str(w['blocked_by'])},")
        lines.append("  },")
    lines.append("]")
    return "\n".join(lines) + "\n"


def main() -> int:
    check = "--check" in sys.argv
    content = render()
    if check:
        if not OUT.exists():
            print(f"check: missing {OUT}", file=sys.stderr)
            return 1
        existing = OUT.read_text()
        if existing != content:
            print(
                "check: migrateWaves.generated.ts is stale vs config/migrate-waves/*.yaml\n"
                "  Regenerate: python3 scripts/generate_migrate_waves_ts.py",
                file=sys.stderr,
            )
            return 1
        print(f"check: OK {OUT.relative_to(ROOT)}")
        return 0

    OUT.write_text(content)
    trade_n = content.count("wave:")
    data_n = content.count("displayCode:")
    print(f"wrote {OUT} (trade waves≈{trade_n}, data≈{data_n})")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
