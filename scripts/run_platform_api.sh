#!/usr/bin/env bash
# Launch platform-api for bdev sessions.
# Loads bifrost-platform/.env, prefers api/bin/platform-api, then exec's so
# bdev-supervise waits on the API process (not a Python parent).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
API_DIR="$ROOT/api"
BIN="$API_DIR/bin/platform-api"

eval "$(
  ROOT="$ROOT" python3 - <<'PY'
import os, shlex
from pathlib import Path

root = Path(os.environ["ROOT"])
env_path = root / ".env"
keys = {
    "PLATFORM_KUBECONFIG", "PLATFORM_CLUSTER_SYNC_SCRIPT", "PLATFORM_CLUSTER_SYNC_ENABLED",
    "PLATFORM_METRICS_SERVER_SCRIPT", "PLATFORM_METRICS_SERVER_ENABLED",
    "PLATFORM_OPERATOR_TOKEN", "PLATFORM_ADMIN_TOKEN", "PLATFORM_LISTEN",
    "PLATFORM_CONSOLE_HOST", "PLATFORM_CONSOLE_PORT", "PLATFORM_CONFIG",
    "OPS_VIEWER_ENV", "REMEDIATION_RUNNER_URL", "REMEDIATION_RUNNER_STANDBY_URL",
    "REMEDIATION_RUNNER_PORT", "REMEDIATION_RUNNER_BIND", "REMEDIATION_RUNNER_AUTOSTART",
    "REMEDIATION_CWD", "REMEDIATION_MODEL", "CURSOR_API_KEY", "PLATFORM_API_URL",
    "GIT_BRIDGE_URL", "SATELLITE_PROBE_BRIDGE_URL", "HERMES_GATEWAY_URL",
    "PLATFORM_PROJECT_ROOT",
}
if env_path.is_file():
    for line in env_path.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, _, val = line.partition("=")
        key, val = key.strip(), val.strip().strip('"').strip("'")
        if key in keys or key.startswith("PLATFORM_") or key.startswith("REMEDIATION_") or key.startswith("OPS_"):
            os.environ[key] = val

os.environ.setdefault("OPS_VIEWER_ENV", "dev")
os.environ.setdefault("PLATFORM_LISTEN", ":8780")
os.environ["PLATFORM_PROJECT_ROOT"] = str(root)

kc = os.environ.get("PLATFORM_KUBECONFIG", "")
if kc:
    os.environ["PLATFORM_KUBECONFIG"] = os.path.expanduser(os.path.expandvars(kc))

brew = "/opt/homebrew/bin"
if os.path.isdir(brew):
    os.environ["PATH"] = brew + os.pathsep + os.environ.get("PATH", "")

export_keys = sorted(
    k for k in os.environ
    if k in keys or k.startswith("PLATFORM_") or k.startswith("REMEDIATION_")
    or k.startswith("OPS_") or k in ("PATH", "CURSOR_API_KEY", "HOME")
)
for k in export_keys:
    print(f"export {shlex.quote(k)}={shlex.quote(os.environ[k])}")
PY
)"

cd "$API_DIR"

listen="${PLATFORM_LISTEN:-:8780}"
port="${listen##*:}"
if command -v lsof >/dev/null 2>&1; then
  pids=$(lsof -i ":${port}" -t 2>/dev/null || true)
  if [[ -n "${pids:-}" ]]; then
    echo "[run_platform_api] freeing :${port} (PIDs ${pids//$'\n'/ })"
    # shellcheck disable=SC2086
    kill -TERM $pids 2>/dev/null || true
    sleep 0.5
    pids=$(lsof -i ":${port}" -t 2>/dev/null || true)
    if [[ -n "${pids:-}" ]]; then
      # shellcheck disable=SC2086
      kill -KILL $pids 2>/dev/null || true
      sleep 0.3
    fi
  fi
fi

if [[ -x "$BIN" ]]; then
  echo "[run_platform_api] exec binary $BIN on ${PLATFORM_LISTEN}"
  exec "$BIN"
fi

if ! command -v go >/dev/null 2>&1; then
  echo "[run_platform_api] go not found and no binary at $BIN" >&2
  exit 1
fi
echo "[run_platform_api] exec go run (tip: make build-api) on ${PLATFORM_LISTEN}"
exec go run ./cmd/platform-api
