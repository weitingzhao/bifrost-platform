#!/usr/bin/env bash
set -euo pipefail

# Deploy Hermes Gateway to a Mac Mini target.
# Usage:
#   ./scripts/agent/deploy_hermes_gateway.sh trader@192.168.1.50
#   AGENT_ROOT=~/bifrost-agent ./scripts/agent/deploy_hermes_gateway.sh trader@192.168.1.52

REMOTE="${1:?Usage: deploy_hermes_gateway.sh user@host}"
AGENT_ROOT="${AGENT_ROOT:-~/bifrost-agent}"
GATEWAY_PORT="${GATEWAY_PORT:-8782}"
PLIST_LABEL="com.bifrost.hermes-gateway"
PLATFORM_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

echo "==> Deploying Hermes Gateway to ${REMOTE}"
echo "    agent root: ${AGENT_ROOT}"
echo "    gateway port: ${GATEWAY_PORT}"

echo "==> Syncing hermes-gateway source"
ssh "${REMOTE}" "mkdir -p ${AGENT_ROOT}/hermes-gateway"
rsync -az --delete \
  --exclude node_modules \
  --exclude .git \
  "${PLATFORM_ROOT}/agent/hermes-gateway/" \
  "${REMOTE}:${AGENT_ROOT}/hermes-gateway/"

# Skill scripts (skills.yaml `script:` names) resolve against HERMES_SCRIPTS_DIR,
# which the plist points here. Before TD-228 only the gateway dir was synced and
# every scheduled run failed with "No such file or directory".
echo "==> Syncing skill scripts (scripts/agent → ${AGENT_ROOT}/hermes-scripts)"
ssh "${REMOTE}" "mkdir -p ${AGENT_ROOT}/hermes-scripts"
rsync -az --delete \
  --include '*.sh' \
  --exclude '*' \
  "${PLATFORM_ROOT}/scripts/agent/" \
  "${REMOTE}:${AGENT_ROOT}/hermes-scripts/"

echo "==> Installing npm dependencies on target"
ssh "${REMOTE}" "cd ${AGENT_ROOT}/hermes-gateway && npm install --production 2>&1 | tail -3"

echo "==> Installing launchd plist"
REMOTE_HOME="$(ssh "${REMOTE}" 'echo $HOME')"
EXPANDED_ROOT="$(echo "${AGENT_ROOT}" | sed "s|~|${REMOTE_HOME}|g")"

PLIST_SRC="${PLATFORM_ROOT}/agent/deploy/${PLIST_LABEL}.plist"
PLIST_TMP="$(mktemp)"
sed \
  -e "s|__AGENT_ROOT__|${EXPANDED_ROOT}|g" \
  -e "s|__HOME__|${REMOTE_HOME}|g" \
  "${PLIST_SRC}" > "${PLIST_TMP}"

scp "${PLIST_TMP}" "${REMOTE}:${REMOTE_HOME}/Library/LaunchAgents/${PLIST_LABEL}.plist"
rm -f "${PLIST_TMP}"

echo "==> Creating log directory"
ssh "${REMOTE}" "mkdir -p ${REMOTE_HOME}/bifrost-agent/logs ${REMOTE_HOME}/bifrost-agent/hermes"

echo "==> Checking REMEDIATION_RUNNER_TOKEN on target (key presence only)"
if ! ssh "${REMOTE}" "grep -qE '^(export )?REMEDIATION_RUNNER_TOKEN=.+' \"${REMOTE_HOME}/bifrost-agent/config/.env\""; then
  echo "ERROR: REMEDIATION_RUNNER_TOKEN is missing or empty in ${REMOTE}:~/bifrost-agent/config/.env" >&2
  echo "The gateway binds 0.0.0.0. Copy the key with deploy_mac_mini.sh before starting the gateway." >&2
  exit 1
fi

echo "==> Reloading launchd service"
ssh "${REMOTE}" "launchctl bootout gui/\$(id -u) ${REMOTE_HOME}/Library/LaunchAgents/${PLIST_LABEL}.plist 2>/dev/null || true"
ssh "${REMOTE}" "launchctl bootstrap gui/\$(id -u) ${REMOTE_HOME}/Library/LaunchAgents/${PLIST_LABEL}.plist"

echo "==> Post-deploy health check"
HEALTH_URL="http://$(echo "${REMOTE}" | cut -d@ -f2):${GATEWAY_PORT}/health"
SMOKE_OK=false
for i in 1 2 3 4 5; do
  sleep 2
  if curl -sf --max-time 5 "${HEALTH_URL}" > /dev/null 2>&1; then
    HEALTH_JSON="$(curl -sf --max-time 5 "${HEALTH_URL}")"
    GW_VER="$(echo "${HEALTH_JSON}" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("version","?"))' 2>/dev/null || echo '?')"
    SKILL_CT="$(echo "${HEALTH_JSON}" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("skill_count",0))' 2>/dev/null || echo '?')"
    GW_STATUS="$(echo "${HEALTH_JSON}" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("status","?"))' 2>/dev/null || echo '?')"
    echo "  ✓ Hermes Gateway up (v${GW_VER}, ${SKILL_CT} skills, status ${GW_STATUS}) on attempt ${i}"
    # A skill whose script is missing is a broken deploy, not a slow start.
    MISSING="$(echo "${HEALTH_JSON}" | python3 -c 'import sys,json; fs=json.load(sys.stdin).get("failing_skills",[]); print("\n".join("%s: %s" % (f.get("id"), f.get("reason")) for f in fs if "script not found" in f.get("reason","")))' 2>/dev/null || true)"
    if [[ -n "${MISSING}" ]]; then
      echo "  ✗ DEPLOY FAILED — enabled skills cannot run:"
      echo "${MISSING}" | sed 's/^/      /'
      exit 1
    fi
    SMOKE_OK=true
    break
  fi
  echo "  attempt ${i}/5 — waiting for gateway on ${HEALTH_URL}…"
done
if [[ "${SMOKE_OK}" != "true" ]]; then
  echo "  ✗ DEPLOY FAILED — gateway did not respond to ${HEALTH_URL} after 5 attempts"
  exit 1
fi

echo ""
echo "==> Done. Hermes Gateway v${GW_VER} running on ${REMOTE}:${GATEWAY_PORT}"
