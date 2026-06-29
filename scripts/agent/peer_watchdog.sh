#!/usr/bin/env bash
# Mutual watchdog (dual Mac Mini self-healing).
#
# Probes the peer agent runner over the LAN; if it is unreachable after a few
# attempts, restarts the peer runner via passwordless SSH (launchctl kickstart).
# Installed by deploy_mac_mini.sh, invoked every 60s by the
# com.bifrost.peer-watchdog launchd agent.
#
# Required env (from config/env.sh + config/env.local.sh):
#   PEER_AGENT_URL   e.g. http://192.168.10.52:8781   (peer runner base URL)
#   PEER_AGENT_SSH   e.g. vision@192.168.10.52        (peer SSH target)
# Optional:
#   BIFROST_AGENT_ROOT (default: $HOME/bifrost-agent)
set -uo pipefail

AGENT_ROOT="${BIFROST_AGENT_ROOT:-$HOME/bifrost-agent}"
LOG="${AGENT_ROOT}/logs/peer-watchdog.log"
mkdir -p "$(dirname "$LOG")"

ts() { date '+%Y-%m-%dT%H:%M:%S%z'; }
log() { echo "[$(ts)] $*" >> "$LOG"; }

PEER_URL="${PEER_AGENT_URL:-}"
PEER_SSH="${PEER_AGENT_SSH:-}"

if [[ -z "$PEER_URL" || -z "$PEER_SSH" ]]; then
  log "SKIP: PEER_AGENT_URL or PEER_AGENT_SSH not set"
  exit 0
fi

probe() {
  curl -fsS -m 8 "${PEER_URL%/}/health" >/dev/null 2>&1
}

# 3 attempts, 5s apart, to avoid flapping on transient network blips.
for _ in 1 2 3; do
  if probe; then
    exit 0
  fi
  sleep 5
done

log "PEER DOWN: ${PEER_URL} unreachable after 3 attempts — restarting via ${PEER_SSH}"
OUT="$(ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ConnectTimeout=8 \
  "${PEER_SSH}" \
  'launchctl kickstart -k "gui/$(id -u)/com.bifrost.remediation-runner"' 2>&1)"
RC=$?
log "restart rc=${RC} out=${OUT}"

sleep 5
if probe; then
  log "RECOVERED: ${PEER_URL} healthy after restart"
else
  log "STILL DOWN: ${PEER_URL} not healthy after restart (manual attention needed)"
fi
exit 0
