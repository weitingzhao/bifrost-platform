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
    echo "  ✓ Hermes Gateway healthy (v${GW_VER}, ${SKILL_CT} skills) on attempt ${i}"
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
