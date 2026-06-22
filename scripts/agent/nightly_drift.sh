#!/usr/bin/env bash
# Nightly drift (Layer 1–3) + optional Cursor briefing. Invoked by launchd on Mac Mini or:
#   python scripts/run_agent.py nightly
set -euo pipefail

export PATH="/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:${PATH:-}"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Mac Mini: deployed copy at ~/bifrost-agent/nightly_drift.sh
if [[ -n "${BIFROST_AGENT_ROOT:-}" ]] && [[ -d "${BIFROST_AGENT_ROOT}/workspace/bifrost-platform" ]]; then
  AGENT_ROOT="${BIFROST_AGENT_ROOT}"
  PLATFORM_ROOT="${AGENT_ROOT}/workspace/bifrost-platform"
  REPORT_DIR="${AGENT_ROOT}/reports"
  LOG_DIR="${AGENT_ROOT}/logs"
  RUNNER_URL="${REMEDIATION_RUNNER_URL:-http://127.0.0.1:8781}"
  PLATFORM_API="${PLATFORM_API_URL:-http://192.168.10.73:30878}"
  [[ -f "${AGENT_ROOT}/config/env.sh" ]] && source "${AGENT_ROOT}/config/env.sh"
  [[ -f "${AGENT_ROOT}/config/.env" ]] && source "${AGENT_ROOT}/config/.env"
  [[ -n "${PLATFORM_API_URL:-}" ]] && PLATFORM_API="${PLATFORM_API_URL}"
  [[ -n "${REMEDIATION_RUNNER_URL:-}" ]] && RUNNER_URL="${REMEDIATION_RUNNER_URL}"
elif [[ -f "${SCRIPT_DIR}/../../agent/drift/scan_layer1.py" ]]; then
  PLATFORM_REPO="$(cd "${SCRIPT_DIR}/../../" && pwd)"
  AGENT_ROOT=""
  PLATFORM_ROOT="${PLATFORM_REPO}"
  REPORT_DIR="${PLATFORM_REPO}/agent/reports"
  LOG_DIR="${REPORT_DIR}"
  RUNNER_URL="${REMEDIATION_RUNNER_URL:-http://127.0.0.1:8781}"
  PLATFORM_API="${PLATFORM_API_URL:-http://127.0.0.1:8780}"
  if [[ -f "${PLATFORM_REPO}/.env" ]]; then
    set -a
    # shellcheck disable=SC1090
    source <(grep -E '^(PLATFORM_API_URL|PLATFORM_OPERATOR_TOKEN|REMEDIATION_RUNNER_URL|NIGHTLY_SKIP_CLUSTER)=' "${PLATFORM_REPO}/.env" | sed 's/^/export /')
    set +a
  fi
  [[ -n "${PLATFORM_API_URL:-}" ]] && PLATFORM_API="${PLATFORM_API_URL}"
  [[ -n "${REMEDIATION_RUNNER_URL:-}" ]] && RUNNER_URL="${REMEDIATION_RUNNER_URL}"
else
  echo "Error: cannot resolve bifrost-platform root from ${SCRIPT_DIR}" >&2
  exit 2
fi

mkdir -p "${REPORT_DIR}"
[[ -n "${AGENT_ROOT}" ]] && mkdir -p "${LOG_DIR}"

TIMESTAMP=$(date '+%Y-%m-%d_%H%M%S')
REPORT="${REPORT_DIR}/nightly-${TIMESTAMP}.md"
LATEST="${REPORT_DIR}/latest.md"

# Use process substitution so L1_EXIT/L2_EXIT/L3_EXIT survive (pipe would run in a subshell).
L1_EXIT=0
L2_EXIT=0
L3_EXIT=0

{
  echo "# Nightly Agent Report"
  echo ""
  echo "- Host: $(hostname)"
  echo "- Time: $(date -Iseconds 2>/dev/null || date)"
  echo "- Mode: ${AGENT_ROOT:+Mac Mini}${AGENT_ROOT:-Mac Pro dev}"
  echo "- Platform root: ${PLATFORM_ROOT}"
  echo "- Platform API: ${PLATFORM_API}"
  echo ""

  echo "## Layer 1 — catalog drift"
  L1_EXIT=0
  if [[ -d "${PLATFORM_ROOT}/console/src/lib" ]]; then
    set +e
    python3 "${PLATFORM_ROOT}/agent/drift/scan_layer1.py" --platform-root "${PLATFORM_ROOT}"
    L1_EXIT=$?
    set -e
  else
    echo "SKIP: platform tree missing at ${PLATFORM_ROOT}"
    L1_EXIT=2
  fi
  echo ""

  echo "## Layer 2 — API probes"
  set +e
  python3 "${PLATFORM_ROOT}/agent/drift/scan_layer2.py" \
    --platform-api "${PLATFORM_API}" \
    --runner-url "${RUNNER_URL}"
  L2_EXIT=$?
  set -e
  echo ""

  echo "## Layer 3 — semantic / spine drift"
  set +e
  python3 "${PLATFORM_ROOT}/agent/drift/scan_layer3.py" \
    --platform-root "${PLATFORM_ROOT}" \
    --platform-api "${PLATFORM_API}"
  L3_EXIT=$?
  set -e
  echo ""

  NEED_AGENT=0
  if [[ "${L1_EXIT}" -ne 0 ]]; then NEED_AGENT=1; fi
  if [[ "${L2_EXIT}" -ne 0 ]]; then NEED_AGENT=1; fi
  if [[ "${L3_EXIT}" -ne 0 ]]; then NEED_AGENT=1; fi

  echo "## Layer 4 — auto-fix proposal (Owner approval gate)"
  L4_EXIT=0
  if [[ "${NEED_AGENT}" -eq 1 ]]; then
    set +e
    L4_JSON=$(python3 "${PLATFORM_ROOT}/agent/drift/scan_layer4.py" \
      --report "${REPORT}" \
      --l1-exit "${L1_EXIT}" \
      --l2-exit "${L2_EXIT}" \
      --l3-exit "${L3_EXIT}" \
      --host "$(hostname)" \
      --platform-api "${PLATFORM_API}")
    L4_EXIT=$?
    set -e
    HAS_DRIFT=$(python3 -c "import json,sys; print(json.loads(sys.argv[1]).get('has_drift', False))" "${L4_JSON}")
    PROPOSAL_API="${PLATFORM_API_URL:-${PLATFORM_API}}"
    if [[ "${HAS_DRIFT}" == "True" ]] && [[ -n "${PLATFORM_OPERATOR_TOKEN:-}" ]] && [[ -n "${PROPOSAL_API}" ]]; then
      CREATE_BODY=$(python3 -c "import json,sys; d=json.loads(sys.argv[1]); d.pop('has_drift', None); print(json.dumps(d))" "${L4_JSON}")
      CREATE_RESP=$(curl -sf --max-time 30 -X POST \
        -H "Authorization: Bearer ${PLATFORM_OPERATOR_TOKEN}" \
        -H "Content-Type: application/json" \
        -d "${CREATE_BODY}" \
        "${PROPOSAL_API%/}/api/v1/agent/drift-proposals" 2>&1) || CREATE_RESP="POST failed: ${CREATE_RESP}"
      echo "${CREATE_RESP}" | python3 -m json.tool 2>/dev/null || echo "${CREATE_RESP}"
      echo "Owner approval: Ops Console → Agent Briefing → Drift auto-fix proposal"
    elif [[ "${HAS_DRIFT}" == "True" ]]; then
      echo "SKIP proposal POST — set PLATFORM_OPERATOR_TOKEN + PLATFORM_API_URL on agent host"
      echo "${L4_JSON}" | python3 -m json.tool 2>/dev/null || echo "${L4_JSON}"
    else
      echo "No drift — no proposal created"
    fi
  else
    echo "No drift — skipping Layer 4 proposal"
  fi
  echo ""

  echo "## Runner health"
  curl -sf --max-time 5 "${RUNNER_URL}/health" | python3 -m json.tool 2>/dev/null || echo "Runner not reachable at ${RUNNER_URL}"
  echo ""

  echo "## Agent remediation (Cursor briefing)"
  if [[ "${NEED_AGENT}" -eq 0 ]]; then
    echo "No drift or API failures — skipping Cursor agent."
  elif ! curl -sf --max-time 5 "${RUNNER_URL}/health" >/dev/null 2>&1; then
    echo "SKIP: runner down; drift logged only."
  else
    L1_SUMMARY=$(python3 "${PLATFORM_ROOT}/agent/drift/scan_layer1.py" --platform-root "${PLATFORM_ROOT}" 2>/dev/null | head -20 || echo "(layer1 failed)")
    L2_SUMMARY=$(python3 "${PLATFORM_ROOT}/agent/drift/scan_layer2.py" \
      --platform-api "${PLATFORM_API}" --runner-url "${RUNNER_URL}" 2>/dev/null | head -20 || echo "(layer2 failed)")
    PROMPT=$(cat <<EOF
Nightly drift scan detected issues. Summarize for Owner morning briefing.
Do NOT apply fixes or destructive cluster actions — report only.

### Layer 1 excerpt
${L1_SUMMARY}

### Layer 2 excerpt
${L2_SUMMARY}

Reply with: (1) executive summary, (2) prioritized drift items, (3) suggested Owner actions.
EOF
)
    BODY=$(python3 -c "import json,sys; print(json.dumps({'scope':'nightly-drift-briefing','actor':'run_agent nightly','issues':[],'prompt':sys.argv[1]}))" "$PROMPT")
    RESPONSE=$(curl -sf --max-time 30 -X POST "${RUNNER_URL}/run" \
      -H "Content-Type: application/json" \
      -d "${BODY}" 2>&1) || RESPONSE="POST failed"
    echo "${RESPONSE}" | python3 -m json.tool 2>/dev/null || echo "${RESPONSE}"
  fi
  echo ""

  if [[ "${NIGHTLY_SKIP_CLUSTER:-}" == "1" ]]; then
    echo "## Cluster verification"
    echo "Skipped (NIGHTLY_SKIP_CLUSTER=1)"
  elif [[ -z "${PLATFORM_OPERATOR_TOKEN:-}" ]]; then
    echo "## Cluster verification"
    echo "Skipped — set PLATFORM_OPERATOR_TOKEN (Mac Pro dev only)"
  elif [[ -z "${PLATFORM_API_URL:-}" ]] && [[ -z "${PLATFORM_API:-}" ]]; then
    echo "## Cluster verification"
    echo "Skipped — set PLATFORM_API_URL"
  else
    echo "## Cluster verification (platform-api remediation)"
    VERIFY_API="${PLATFORM_API_URL:-${PLATFORM_API}}"
    BODY='{"scope":"nightly-health-check","issues":[],"prompt":"Verification pass only; no destructive actions."}'
    RESP="$(curl -sf --max-time 30 -X POST \
      -H "Authorization: Bearer ${PLATFORM_OPERATOR_TOKEN}" \
      -H "Content-Type: application/json" \
      -d "$BODY" \
      "${VERIFY_API%/}/api/v1/remediation/start" 2>&1)" || RESP="POST failed: $RESP"
    echo "$RESP" | python3 -m json.tool 2>/dev/null || echo "$RESP"
  fi

  echo ""
  echo "---"
  echo "Report: ${REPORT}"
} > >(tee "${REPORT}")

cp "${REPORT}" "${LATEST}"
echo "Latest report: ${LATEST}"

if [[ "${L1_EXIT}" -ne 0 || "${L2_EXIT}" -ne 0 || "${L3_EXIT}" -ne 0 ]]; then
  exit 1
fi
