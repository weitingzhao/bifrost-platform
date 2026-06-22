#!/usr/bin/env bash
# Layer 1–3 drift scan (no report, no Cursor). Invoked by: python scripts/run_agent.py drift
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../" && pwd)"
export PATH="/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:${PATH:-}"

if [[ -f "${ROOT}/.env" ]]; then
  set -a
  # shellcheck disable=SC1090
  source <(grep -E '^(PLATFORM_API_URL|REMEDIATION_RUNNER_URL)=' "${ROOT}/.env" | sed 's/^/export /')
  set +a
fi

PLATFORM_API="${PLATFORM_API_URL:-http://127.0.0.1:8780}"
RUNNER_URL="${REMEDIATION_RUNNER_URL:-http://127.0.0.1:8781}"

EXIT=0
python3 "${ROOT}/agent/drift/scan_layer1.py" --platform-root "${ROOT}" || EXIT=1
python3 "${ROOT}/agent/drift/scan_layer2.py" \
  --platform-api "${PLATFORM_API}" \
  --runner-url "${RUNNER_URL}" || EXIT=1
python3 "${ROOT}/agent/drift/scan_layer3.py" \
  --platform-root "${ROOT}" \
  --platform-api "${PLATFORM_API}" || EXIT=1
exit "${EXIT}"
