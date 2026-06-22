#!/usr/bin/env bash
# Nightly health check — triggers remediation-runner at 3:00 AM.
# Called by launchd com.bifrost.nightly-health-check
set -euo pipefail

export PATH="/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:${PATH:-}"

RUNNER_URL="http://127.0.0.1:8781"
LOG_DIR="/Users/vision/bifrost-agent/logs"
TIMESTAMP=$(date '+%Y-%m-%d_%H%M%S')

mkdir -p "${LOG_DIR}"

echo "=== Nightly health check ${TIMESTAMP} ===" | tee -a "${LOG_DIR}/nightly.log"

# Check runner is alive
if ! curl -sf "${RUNNER_URL}/health" >/dev/null 2>&1; then
  echo "ERROR: remediation-runner not responding at ${RUNNER_URL}/health" | tee -a "${LOG_DIR}/nightly.log"
  exit 1
fi

# Trigger a health-check run (no issues = verification pass)
RESPONSE=$(curl -sf -X POST "${RUNNER_URL}/run" \
  -H "Content-Type: application/json" \
  -d '{
    "scope": "Nightly scheduled health verification",
    "actor": "launchd/nightly-health-check"
  }')

JOB_ID=$(echo "${RESPONSE}" | node -e "process.stdin.on('data',d=>{const j=JSON.parse(d);console.log(j.id)})" 2>/dev/null || echo "")

if [[ -z "${JOB_ID}" ]]; then
  echo "ERROR: Failed to start remediation job" | tee -a "${LOG_DIR}/nightly.log"
  echo "Response: ${RESPONSE}" >> "${LOG_DIR}/nightly.log"
  exit 1
fi

echo "Started job ${JOB_ID}" | tee -a "${LOG_DIR}/nightly.log"

# Wait for completion (max 10 minutes)
MAX_WAIT=600
ELAPSED=0
while [[ ${ELAPSED} -lt ${MAX_WAIT} ]]; do
  sleep 15
  ELAPSED=$((ELAPSED + 15))

  STATUS=$(curl -sf "${RUNNER_URL}/run/${JOB_ID}" | node -e "process.stdin.on('data',d=>{const j=JSON.parse(d);console.log(j.status)})" 2>/dev/null || echo "unknown")

  if [[ "${STATUS}" == "done" || "${STATUS}" == "failed" || "${STATUS}" == "cancelled" ]]; then
    break
  fi
done

# Fetch final result
FINAL=$(curl -sf "${RUNNER_URL}/run/${JOB_ID}" 2>/dev/null || echo '{"status":"unknown"}')
FINAL_STATUS=$(echo "${FINAL}" | node -e "process.stdin.on('data',d=>{const j=JSON.parse(d);console.log(j.status)})" 2>/dev/null || echo "unknown")
SUMMARY=$(echo "${FINAL}" | node -e "process.stdin.on('data',d=>{const j=JSON.parse(d);console.log(j.summary||'')})" 2>/dev/null || echo "")

echo "Completed: status=${FINAL_STATUS}" | tee -a "${LOG_DIR}/nightly.log"
if [[ -n "${SUMMARY}" ]]; then
  echo "Summary: ${SUMMARY}" | tee -a "${LOG_DIR}/nightly.log"
fi

# Save full report
echo "${FINAL}" > "${LOG_DIR}/nightly-report-${TIMESTAMP}.json"
echo "Report saved: ${LOG_DIR}/nightly-report-${TIMESTAMP}.json" | tee -a "${LOG_DIR}/nightly.log"
