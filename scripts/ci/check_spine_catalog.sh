#!/usr/bin/env bash
# Ensure ops-context.yaml catalog_version matches environments-catalog.ts CATALOG_VERSION
# and Architecture catalog milestone refs exist on spine.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../" && pwd)"
YAML="${ROOT}/config/ops-context.yaml"
CATALOG_TS="${ROOT}/console/src/lib/environments-catalog.ts"
VISION_MAP="${ROOT}/console/src/lib/architecture/visionSpineMap.ts"

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

# Milestone ids referenced by Architecture catalogs must exist in ops-context milestones.
REFS=(
  k3s-stg-v2-deliver
  2c-b-prod-cutover
  legacy-retirement
)

if [[ -f "$VISION_MAP" ]]; then
  while IFS= read -r vid; do
    REFS+=("$vid")
  done < <(grep -E "spineMilestoneId: '" "$VISION_MAP" | sed -E "s/.*'([^']+)'.*/\1/")
fi

missing=()
for ref in "${REFS[@]}"; do
  if ! grep -qE "^\s*- id: ${ref}\s*$" "$YAML"; then
    missing+=("$ref")
  fi
done

if [[ ${#missing[@]} -gt 0 ]]; then
  echo "check_spine_catalog: milestone ref drift" >&2
  for m in "${missing[@]}"; do
    echo "  missing from ops-context.yaml milestones: $m" >&2
  done
  exit 1
fi

echo "check_spine_catalog: OK (catalog_version=$yaml_ver, milestone_refs=${#REFS[@]})"
