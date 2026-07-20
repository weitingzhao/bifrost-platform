#!/usr/bin/env bash
# Daily Ops Checklist probe (L0) — 18 items via remediation scope daily-ops-checklist-run.
# Naming lock: AI Check (TCC Checklist) == this scope. Distinct from:
#   - Fleet cell Fix (per-cell remediation scopes)
#   - Operator Plane Fix (operator-plane-remediate)
# Invoked by launchd (interval or market-open), Console AI Check, or manually:
#   bash scripts/agent/daily_ops_checklist.sh
#
# Mini runners must reach platform-api routes used by report_checklist_signals
# (/checklist/signals). Prefer PLATFORM_API_URL / AGENT_PLATFORM_API_URL → Mac Pro :8780
# (or a cluster API that already serves checklist routes). See deploy_mac_mini.sh
# env.local.sh sourcing — do not break existing AGENT_ROLE / peer watchdog.
#
# After the agent calls report_checklist_signals(auto_dispatch=true), platform-api
# applies fixCapability gates (full_auto→remediation, semi_auto→Operate Queue,
# manual/observe→notify; D10 skips ib-feed).
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
  # Prefer platform .env REMEDIATION_RUNNER_URL (Mac Mini .50/.52) over local :8781.
  # Avoid process-substitution `source` — it can leave the var empty under set -u.
  if [[ -z "${REMEDIATION_RUNNER_URL:-}" && -f "${PLATFORM_ROOT}/.env" ]]; then
    _line="$(grep -E '^REMEDIATION_RUNNER_URL=' "${PLATFORM_ROOT}/.env" | head -1 || true)"
    if [[ -n "${_line}" ]]; then
      RUNNER_URL="$(printf '%s' "${_line#REMEDIATION_RUNNER_URL=}" | tr -d '\r' | sed -e 's/^["'\'']//' -e 's/["'\'']$//' -e 's/[[:space:]]*#.*$//' -e 's/[[:space:]]*$//')"
    fi
    unset _line
  elif [[ -n "${REMEDIATION_RUNNER_URL:-}" ]]; then
    RUNNER_URL="${REMEDIATION_RUNNER_URL}"
  fi
else
  echo "Error: cannot resolve bifrost-platform root from ${SCRIPT_DIR}" >&2
  exit 2
fi

if ! curl -sf --max-time 5 "${RUNNER_URL}/health" >/dev/null 2>&1; then
  echo "SKIP: remediation runner down at ${RUNNER_URL}"
  exit 0
fi

PROMPT=$(cat <<'EOF'
Scheduled Daily Ops Checklist probe (scope daily-ops-checklist-run).
1. Call verify_mission_snapshot, get_cluster_summary, get_agent_bridge, get_gitops_apps, get_stg_smoke, get_delivery_pipelines.
2. Map evidence to all 18 checklist item_ids (ok/degraded/fail/unknown).
3. Call report_checklist_signals with auto_dispatch=true and the full signals array.
4. Do not actuate directly in this job — platform gates dispatch (D10: never auto IB).
EOF
)

BODY=$(python3 -c "import json,sys; print(json.dumps({'scope':'daily-ops-checklist-run','actor':'daily-ops-checklist','issues':[],'prompt':sys.argv[1]}))" "$PROMPT")
RESPONSE=$(curl -sf --max-time 30 -X POST "${RUNNER_URL}/run" \
  -H "Content-Type: application/json" \
  -d "${BODY}" 2>&1) || RESPONSE="POST failed"

echo "${RESPONSE}" | python3 -m json.tool 2>/dev/null || echo "${RESPONSE}"

cat <<'NOTE'

# launchd (example — Mac Mini agent host)
# Interval (every 30m during desk hours) or StartCalendarInterval near market open.
# Label: com.bifrost.daily-ops-checklist
# ProgramArguments: /bin/bash, ~/bifrost-agent/workspace/bifrost-platform/scripts/agent/daily_ops_checklist.sh
# Keep StandardOutPath / StandardErrorPath under ~/bifrost-agent/logs/
NOTE
