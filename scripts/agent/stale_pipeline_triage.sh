#!/usr/bin/env bash
# L0 stale pipeline triage — classify deliver-stg fail vs STG smoke green before drift scan.
# Invoked by Hermes skill stale-pipeline-triage (cron) or manually:
#   bash scripts/agent/stale_pipeline_triage.sh
set -euo pipefail

export PATH="/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:${PATH:-}"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

if [[ -n "${BIFROST_AGENT_ROOT:-}" ]] && [[ -d "${BIFROST_AGENT_ROOT}/workspace/bifrost-platform" ]]; then
  PLATFORM_ROOT="${BIFROST_AGENT_ROOT}/workspace/bifrost-platform"
  RUNNER_URL="${REMEDIATION_RUNNER_URL:-http://127.0.0.1:8781}"
  [[ -f "${BIFROST_AGENT_ROOT}/config/env.sh" ]] && source "${BIFROST_AGENT_ROOT}/config/env.sh"
  [[ -f "${BIFROST_AGENT_ROOT}/config/.env" ]] && source "${BIFROST_AGENT_ROOT}/config/.env"
  [[ -n "${REMEDIATION_RUNNER_URL:-}" ]] && RUNNER_URL="${REMEDIATION_RUNNER_URL}"
elif [[ -d "${SCRIPT_DIR}/../../agent/remediation" ]]; then
  PLATFORM_ROOT="$(cd "${SCRIPT_DIR}/../../" && pwd)"
  RUNNER_URL="${REMEDIATION_RUNNER_URL:-http://127.0.0.1:8781}"
  if [[ -f "${PLATFORM_ROOT}/.env" ]]; then
    set -a
    # shellcheck disable=SC1090
    source <(grep -E '^(REMEDIATION_RUNNER_URL)=' "${PLATFORM_ROOT}/.env" | sed 's/^/export /')
    set +a
  fi
  [[ -n "${REMEDIATION_RUNNER_URL:-}" ]] && RUNNER_URL="${REMEDIATION_RUNNER_URL}"
else
  echo "Error: cannot resolve bifrost-platform root from ${SCRIPT_DIR}" >&2
  exit 2
fi

if ! curl -sf --max-time 5 "${RUNNER_URL}/health" >/dev/null 2>&1; then
  echo "SKIP: remediation runner down at ${RUNNER_URL}"
  exit 0
fi

PROMPT=$(cat <<'EOF'
Scheduled stale-pipeline triage (L0 read-only).
1. get_pipeline_runs for bifrost-deliver-stg and get_stg_smoke.
2. If pipeline failed AND STG smoke green → classify track=playbook (stale-fail); recommend deliver-stg-recover.
3. If smoke red → report runtime/cluster track.
4. NO actuation — classification report only for Owner / Defects context.
EOF
)

BODY=$(python3 -c "import json,sys; print(json.dumps({'scope':'stale-pipeline-triage','actor':'hermes stale-pipeline-triage','issues':[],'prompt':sys.argv[1]}))" "$PROMPT")
RESPONSE=$(curl -sf --max-time 30 -X POST "${RUNNER_URL}/run" \
  -H "Content-Type: application/json" \
  -d "${BODY}" 2>&1) || RESPONSE="POST failed"

echo "${RESPONSE}" | python3 -m json.tool 2>/dev/null || echo "${RESPONSE}"
