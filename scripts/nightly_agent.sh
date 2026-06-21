#!/usr/bin/env bash
# Nightly agent job — Layer 1 drift scan + optional cluster health verification via platform-api.
# Intended host: Mac Mini agent (192.168.10.50) with remediation runner on :8781.
#
# Env (from bifrost-platform/.env or export):
#   PLATFORM_API_URL          Mac Pro platform-api (e.g. http://192.168.10.x:8780)
#   PLATFORM_OPERATOR_TOKEN   L1 operator token for remediation/start
#   NIGHTLY_SKIP_CLUSTER=1    Skip cluster verification (drift only)
#
# Reports: agent/reports/nightly-YYYY-MM-DD_HHMM.md

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
REPORT_DIR="$ROOT/agent/reports"
mkdir -p "$REPORT_DIR"

STAMP="$(date +%Y-%m-%d_%H%M)"
REPORT="$REPORT_DIR/nightly-${STAMP}.md"
LATEST="$REPORT_DIR/latest.md"

load_dotenv() {
  local env_file="$ROOT/.env"
  if [[ -f "$env_file" ]]; then
    set -a
    # shellcheck disable=SC1090
    source <(grep -E '^(PLATFORM_API_URL|PLATFORM_OPERATOR_TOKEN|NIGHTLY_SKIP_CLUSTER|CURSOR_API_KEY)=' "$env_file" | sed 's/^/export /')
    set +a
  fi
}

load_dotenv

{
  echo "# Nightly Agent Report"
  echo ""
  echo "- Host: $(hostname)"
  echo "- Time: $(date -Iseconds)"
  echo "- Platform root: $ROOT"
  echo ""

  echo "## Runner health"
  if curl -sf "http://127.0.0.1:${REMEDIATION_RUNNER_PORT:-8781}/health" 2>/dev/null | python3 -m json.tool; then
  else
    echo "Remediation runner not reachable on :${REMEDIATION_RUNNER_PORT:-8781}"
    echo "Start with: make dev-agent"
  fi
  echo ""

  echo "## Layer 1 drift scan"
  set +e
  python3 "$ROOT/agent/drift/scan_layer1.py"
  DRIFT_EXIT=$?
  set -e
  echo ""

  if [[ "${NIGHTLY_SKIP_CLUSTER:-}" == "1" ]]; then
    echo "## Cluster verification"
    echo "Skipped (NIGHTLY_SKIP_CLUSTER=1)"
  elif [[ -z "${PLATFORM_API_URL:-}" ]]; then
    echo "## Cluster verification"
    echo "Skipped — set PLATFORM_API_URL in .env (Mac Pro platform-api address)"
  elif [[ -z "${PLATFORM_OPERATOR_TOKEN:-}" ]]; then
    echo "## Cluster verification"
    echo "Skipped — set PLATFORM_OPERATOR_TOKEN in .env"
  else
    echo "## Cluster verification (platform-api remediation)"
    BODY='{"scope":"nightly-health-check","issues":[],"prompt":"Nightly scheduled verification pass. Report only; no destructive actions unless open issues exist."}'
    RESP="$(curl -sf -X POST \
      -H "Authorization: Bearer ${PLATFORM_OPERATOR_TOKEN}" \
      -H "Content-Type: application/json" \
      -d "$BODY" \
      "${PLATFORM_API_URL%/}/api/v1/remediation/start" 2>&1)" || RESP="POST failed: $RESP"
    echo "$RESP" | python3 -m json.tool 2>/dev/null || echo "$RESP"
    echo ""
    echo "View job progress in Ops Console → Cluster or remediation API."
  fi

  echo ""
  echo "---"
  echo "Report file: $REPORT"
} | tee "$REPORT"

cp "$REPORT" "$LATEST"
echo "Latest report: $LATEST"

# Exit non-zero if drift found (for launchd / monitoring)
if [[ "$DRIFT_EXIT" -ne 0 ]]; then
  exit 1
fi
