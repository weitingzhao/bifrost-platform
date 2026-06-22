#!/usr/bin/env bash
# Ensure ops-context.yaml catalog_version matches environments-catalog.ts CATALOG_VERSION.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../" && pwd)"
YAML="${ROOT}/config/ops-context.yaml"
CATALOG_TS="${ROOT}/console/src/lib/environments-catalog.ts"

if [[ ! -f "$YAML" ]]; then
  echo "check_spine_catalog: missing $YAML" >&2
  exit 1
fi
if [[ ! -f "$CATALOG_TS" ]]; then
  echo "check_spine_catalog: missing $CATALOG_TS" >&2
  exit 1
fi

yaml_ver="$(grep -E '^\s*catalog_version:' "$YAML" | head -1 | sed -E 's/.*"([^"]+)".*/\1/')"
ts_ver="$(grep -E "^export const CATALOG_VERSION" "$CATALOG_TS" | sed -E "s/.*'([^']+)'.*/\1/")"

if [[ -z "$yaml_ver" || -z "$ts_ver" ]]; then
  echo "check_spine_catalog: could not parse versions" >&2
  echo "  yaml: ${yaml_ver:-<empty>}" >&2
  echo "  ts:   ${ts_ver:-<empty>}" >&2
  exit 1
fi

if [[ "$yaml_ver" != "$ts_ver" ]]; then
  echo "check_spine_catalog: catalog_version drift" >&2
  echo "  ops-context.yaml meta.catalog_version: $yaml_ver" >&2
  echo "  environments-catalog.ts CATALOG_VERSION:   $ts_ver" >&2
  echo "Update both when changing milestone narrative authority." >&2
  exit 1
fi

echo "check_spine_catalog: OK ($yaml_ver)"
