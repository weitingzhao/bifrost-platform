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
#   PEER_RELAY_URL   e.g. http://192.168.10.50:8783/api/v1/alerts/relay — watch the
#                    peer's ntfy alert relay (TD-248). Set only on the Mini that does
#                    NOT run the relay. With NTFY_TOPIC (+ NTFY_URL) from config/.env,
#                    pages the Owner when the relay is unreachable or not enabled for
#                    RELAY_DOWN_MIN minutes (default 5), hourly while it stays down,
#                    and once when it is back. The relay pages its own missing
#                    Alertmanager heartbeat; this covers the relay itself.
set -uo pipefail

AGENT_ROOT="${BIFROST_AGENT_ROOT:-$HOME/bifrost-agent}"
LOG="${AGENT_ROOT}/logs/peer-watchdog.log"
mkdir -p "$(dirname "$LOG")"

ts() { date '+%Y-%m-%dT%H:%M:%S%z'; }
log() { echo "[$(ts)] $*" >> "$LOG"; }

PEER_URL="${PEER_AGENT_URL:-}"
PEER_SSH="${PEER_AGENT_SSH:-}"

# --- peer alert relay (TD-248) -------------------------------------------------
RELAY_URL="${PEER_RELAY_URL:-}"
STATE_DIR="${AGENT_ROOT}/state"
DOWN_FILE="${STATE_DIR}/relay-down-since"
PAGE_FILE="${STATE_DIR}/relay-last-page"

ntfy() { # title priority tags body
  curl -fsS -m 10 -H "Title: $1" -H "Priority: $2" -H "Tags: $3" \
    -d "$4" "${NTFY_URL:-https://ntfy.sh}/${NTFY_TOPIC}" >/dev/null 2>&1
}

watch_relay() {
  [[ -n "$RELAY_URL" ]] || return 0
  if [[ -z "${NTFY_TOPIC:-}" ]]; then
    log "RELAY WATCH SKIP: PEER_RELAY_URL set but NTFY_TOPIC missing"
    return 0
  fi
  mkdir -p "$STATE_DIR"
  local now body ok=0
  now="$(date +%s)"
  for _ in 1 2 3; do
    body="$(curl -fsS -m 8 "$RELAY_URL" 2>/dev/null)" && [[ "$body" == *'"enabled":true'* ]] && { ok=1; break; }
    sleep 5
  done
  if [[ "$ok" == 1 ]]; then
    if [[ -f "$DOWN_FILE" ]]; then
      if [[ -f "$PAGE_FILE" ]]; then
        ntfy "RESOLVED alert relay on the peer Mini is back" 2 white_check_mark \
          "${RELAY_URL} answers again; pages from Alertmanager reach you again." \
          && log "RELAY RECOVERED: paged resolved"
      fi
      rm -f "$DOWN_FILE" "$PAGE_FILE"
    fi
    return 0
  fi
  [[ -f "$DOWN_FILE" ]] || echo "$now" > "$DOWN_FILE"
  local since last=0
  since="$(cat "$DOWN_FILE")"
  [[ -f "$PAGE_FILE" ]] && last="$(cat "$PAGE_FILE")"
  if (( now - since < ${RELAY_DOWN_MIN:-5} * 60 )); then
    return 0
  fi
  if (( last > 0 && now - last < 3600 )); then
    return 0
  fi
  if ntfy "CRITICAL alert relay on the peer Mini is down" 5 rotating_light,skull \
    "${RELAY_URL} has not answered (or is not enabled) for $(( (now - since) / 60 )) min. Alertmanager pages go through it: until it is back, no alert and no heartbeat page reaches you."; then
    echo "$now" > "$PAGE_FILE"
    log "RELAY DOWN: paged (down since $(date -r "$since" '+%H:%M'))"
  else
    log "RELAY DOWN: ntfy publish failed"
  fi
}
watch_relay

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
