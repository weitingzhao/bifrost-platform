#!/usr/bin/env bash
# Install LaunchAgent: auto-mount NAS at Mac login + retry every 5 min until up.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_MOUNT_SCRIPT="${SCRIPT_DIR}/mount_nas.sh"
INSTALL_DIR="${HOME}/Library/Scripts"
MOUNT_SCRIPT="${INSTALL_DIR}/mount_nas.sh"
PLIST_LABEL="com.bifrost.mount-nas"
PLIST_PATH="${HOME}/Library/LaunchAgents/${PLIST_LABEL}.plist"

mkdir -p "${INSTALL_DIR}" "${HOME}/Library/LaunchAgents"
cp "${REPO_MOUNT_SCRIPT}" "${MOUNT_SCRIPT}"
chmod +x "${MOUNT_SCRIPT}"

cat > "${PLIST_PATH}" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>${PLIST_LABEL}</string>
  <key>ProgramArguments</key>
  <array>
    <string>${MOUNT_SCRIPT}</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>StartInterval</key>
  <integer>300</integer>
  <key>StandardOutPath</key>
  <string>${HOME}/Library/Logs/mount-nas.log</string>
  <key>StandardErrorPath</key>
  <string>${HOME}/Library/Logs/mount-nas.log</string>
</dict>
</plist>
EOF

launchctl bootout "gui/$(id -u)/${PLIST_LABEL}" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "${PLIST_PATH}"
launchctl enable "gui/$(id -u)/${PLIST_LABEL}"

echo "Installed ${PLIST_PATH}"
echo "Logs: ~/Library/Logs/mount-nas.log"
echo "Test now: ${MOUNT_SCRIPT}"
