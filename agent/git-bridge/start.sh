#!/usr/bin/env bash
# Start git-bridge, killing any previous instance on the same port first.
# Usage:
#   ./start.sh          # foreground (Ctrl+C to stop)
#   ./start.sh daemon    # background daemon via double-fork (survives terminal close)
#   ./start.sh stop      # stop daemon
#   ./start.sh status    # check if running
set -euo pipefail

PORT="${GIT_BRIDGE_PORT:-8785}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LOG_DIR="${SCRIPT_DIR}/logs"
PID_FILE="${SCRIPT_DIR}/logs/git-bridge.pid"

kill_port() {
  local pids
  pids=$(lsof -i ":${PORT}" -t 2>/dev/null || true)
  if [[ -n "${pids}" ]]; then
    echo "[git-bridge] Killing process on :${PORT} (PIDs: ${pids})"
    kill -9 ${pids} 2>/dev/null || true
    sleep 1
  fi
}

cd "${SCRIPT_DIR}"
export NODE_PATH="${SCRIPT_DIR}/node_modules"

case "${1:-foreground}" in
  daemon)
    kill_port
    mkdir -p "${LOG_DIR}"
    python3 -c "
import os, sys, subprocess
# double-fork to fully detach from terminal
if os.fork() > 0: sys.exit(0)
os.setsid()
if os.fork() > 0: sys.exit(0)
# redirect stdio
devnull = os.open(os.devnull, os.O_RDWR)
logpath = '${LOG_DIR}/git-bridge.log'
logfd = os.open(logpath, os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o644)
os.dup2(devnull, 0)
os.dup2(logfd, 1)
os.dup2(logfd, 2)
os.close(devnull)
os.close(logfd)
# write pid
p = subprocess.Popen(
    ['/opt/homebrew/bin/node', '--import', 'tsx/esm', 'src/server.ts'],
    cwd='${SCRIPT_DIR}',
    env={**os.environ, 'NODE_PATH': '${SCRIPT_DIR}/node_modules'},
)
with open('${PID_FILE}', 'w') as f: f.write(str(p.pid))
p.wait()
"
    sleep 3
    if lsof -i ":${PORT}" -t >/dev/null 2>&1; then
      PID=$(cat "${PID_FILE}" 2>/dev/null || lsof -i ":${PORT}" -t | head -1)
      echo "[git-bridge] Daemon started (PID ${PID}, port ${PORT})"
      echo "[git-bridge] Logs: ${LOG_DIR}/git-bridge.log"
    else
      echo "[git-bridge] ERROR: daemon failed to start" >&2
      tail -5 "${LOG_DIR}/git-bridge.log" 2>/dev/null
      exit 1
    fi
    ;;
  stop)
    kill_port
    rm -f "${PID_FILE}"
    echo "[git-bridge] Stopped"
    ;;
  status)
    if lsof -i ":${PORT}" -t >/dev/null 2>&1; then
      PID=$(lsof -i ":${PORT}" -t | head -1)
      echo "[git-bridge] Running (PID ${PID}, port ${PORT})"
    else
      echo "[git-bridge] Not running"
    fi
    ;;
  foreground|*)
    kill_port
    exec node --import tsx/esm src/server.ts
    ;;
esac
