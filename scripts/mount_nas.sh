#!/usr/bin/env bash
# Mount UGREEN NAS share at login (uses Keychain creds saved from Finder).
# Install: scripts/install_mount_nas_launchagent.sh
set -euo pipefail

NAS_HOST="${NAS_HOST:-192.168.10.20}"
NAS_SHARE="${NAS_SHARE:-personal_folder}"
MOUNT_POINT="/Volumes/${NAS_SHARE}"
SMB_URL="smb://${NAS_HOST}/${NAS_SHARE}"
MAX_WAIT_SEC="${MAX_WAIT_SEC:-120}"
POLL_SEC="${POLL_SEC:-3}"

log() { printf '[mount-nas] %s\n' "$*"; }

if mount | grep -q " on ${MOUNT_POINT} "; then
  log "already mounted at ${MOUNT_POINT}"
  exit 0
fi

log "waiting for ${NAS_HOST} (up to ${MAX_WAIT_SEC}s)…"
elapsed=0
while (( elapsed < MAX_WAIT_SEC )); do
  if ping -c 1 -W 1 "${NAS_HOST}" >/dev/null 2>&1; then
    break
  fi
  sleep "${POLL_SEC}"
  elapsed=$((elapsed + POLL_SEC))
done

if ! ping -c 1 -W 2 "${NAS_HOST}" >/dev/null 2>&1; then
  log "NAS unreachable — will retry on next launchd interval"
  exit 1
fi

log "mounting ${SMB_URL} → ${MOUNT_POINT}"
/usr/bin/open "${SMB_URL}"

for _ in $(seq 1 20); do
  if mount | grep -q " on ${MOUNT_POINT} "; then
    log "ok: $(mount | grep " on ${MOUNT_POINT} ")"
    exit 0
  fi
  sleep 1
done

log "mount command sent but ${MOUNT_POINT} not visible yet"
exit 1
