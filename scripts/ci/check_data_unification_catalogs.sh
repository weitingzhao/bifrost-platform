#!/usr/bin/env bash
# Prevent dual-source drift for business catalogs unified under YAML.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../" && pwd)"
cd "$ROOT"

fail=0

require_file() {
  if [[ ! -f "$1" ]]; then
    echo "check_data_unification: missing $1" >&2
    fail=1
  fi
}

require_file "config/lanes.yaml"
require_file "config/programs/_templates.yaml"
require_file "config/migrate-waves/trade-k8s-native.yaml"
require_file "config/migrate-waves/data-layer-k3s.yaml"
require_file "config/agent-tasks.yaml"
require_file "config/environments.yaml"
require_file "console/src/lib/architecture/migrateWaves.generated.ts"

# Generated migrate waves must match YAML (do not overwrite — --check only).
if ! python3 scripts/generate_migrate_waves_ts.py --check; then
  fail=1
fi

trade_yaml="$(grep -cE '^\s+- id:' config/migrate-waves/trade-k8s-native.yaml)"
data_yaml="$(grep -cE '^\s+- id:' config/migrate-waves/data-layer-k3s.yaml)"
trade_ts="$(grep -c 'wave:' console/src/lib/architecture/migrateWaves.generated.ts)"
data_ts="$(grep -c 'displayCode:' console/src/lib/architecture/migrateWaves.generated.ts)"

if [[ "$trade_yaml" -ne "$trade_ts" ]]; then
  echo "check_data_unification: trade wave count YAML=$trade_yaml TS=$trade_ts" >&2
  fail=1
fi
if [[ "$data_yaml" -ne "$data_ts" ]]; then
  echo "check_data_unification: data-layer count YAML=$data_yaml TS=$data_ts" >&2
  fail=1
fi

lane_count="$(grep -cE '^\s+- id:' config/lanes.yaml)"
if [[ "$lane_count" -lt 26 ]]; then
  echo "check_data_unification: expected >=26 lanes, got $lane_count" >&2
  fail=1
fi

tpl_count="$(grep -cE '^\s+- id:' config/programs/_templates.yaml)"
if [[ "$tpl_count" -lt 5 ]]; then
  echo "check_data_unification: expected >=5 templates, got $tpl_count" >&2
  fail=1
fi

task_count="$(grep -cE '^\s+- id:' config/agent-tasks.yaml)"
if [[ "$task_count" -lt 17 ]]; then
  echo "check_data_unification: expected >=17 agent tasks, got $task_count" >&2
  fail=1
fi

# TRADE_ENVIRONMENTS must remain documented as deprecated side-note (not runtime SSOT).
env_cat="console/src/lib/environments-catalog.ts"
if ! grep -q '@deprecated' "$env_cat" || ! grep -q 'TRADE_ENVIRONMENTS' "$env_cat"; then
  echo "check_data_unification: TRADE_ENVIRONMENTS must keep @deprecated annotation in $env_cat" >&2
  fail=1
fi

# Forbid reintroducing Go hardcode maps for migrate waves / templates
if grep -qn 'var tradeK8sNativeWaves' api/internal/migratewave/*.go 2>/dev/null; then
  echo "check_data_unification: tradeK8sNativeWaves hardcoded again" >&2
  fail=1
fi
if grep -qn 'var programTemplates = map' api/internal/devagent/*.go 2>/dev/null; then
  echo "check_data_unification: programTemplates map hardcoded again" >&2
  fail=1
fi

if [[ "$fail" -ne 0 ]]; then
  exit 1
fi
echo "check_data_unification: OK (lanes=$lane_count templates=$tpl_count tasks=$task_count trade=$trade_yaml data=$data_yaml)"
