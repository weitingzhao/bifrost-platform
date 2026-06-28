#!/usr/bin/env bash
# Deploy agent stack (remediation-runner + Nous Hermes MCP + nightly drift) to Mac Mini.
# Invoked by: python scripts/run_agent.py deploy
set -euo pipefail

REMOTE="${1:-vision@192.168.10.50}"
REMOTE_DIR="/Users/vision/bifrost-agent"
# Mutual-watchdog / Active-Standby config (env-driven, optional):
#   AGENT_ROLE  primary | standby   (default primary; standby disables nightly-drift)
#   PEER_SSH    vision@192.168.10.52 (peer SSH target for watchdog restart)
#   PEER_URL    http://192.168.10.52:8781 (peer runner base URL for health probe)
AGENT_ROLE="${AGENT_ROLE:-primary}"
PEER_SSH="${PEER_SSH:-}"
PEER_URL="${PEER_URL:-}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PLATFORM_LOCAL="$(cd "${SCRIPT_DIR}/../../" && pwd)"
AGENT_SRC="${PLATFORM_LOCAL}/agent/remediation"
DEPLOY_DIR="${PLATFORM_LOCAL}/agent/deploy"
INFRA_LOCAL="$(cd "${PLATFORM_LOCAL}/../bifrost-trade-infra" 2>/dev/null && pwd || echo "")"
WORKSPACE_REMOTE="${REMOTE_DIR}/workspace"
REMOTE_PATH="/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin"

run_remote() {
  ssh "${REMOTE}" "export PATH=${REMOTE_PATH}; $*"
}

echo "==> Deploying agent stack to ${REMOTE}:${REMOTE_DIR}"

# config/logs/workspace only — preserve jobs/reports symlinks to NAS
run_remote "mkdir -p ${REMOTE_DIR}/{config,logs,workspace}"
run_remote "
  for d in jobs reports; do
    p='${REMOTE_DIR}/'\$d
    if [ -L \"\$p\" ]; then
      if [ -e \"\$p\" ]; then
        echo \"  keep symlink \$p -> \$(readlink \"\$p\")\"
      else
        echo \"  WARN: broken symlink \$p -> \$(readlink \"\$p\") (NAS not mounted?)\"
        mkdir -p '${REMOTE_DIR}/jobs-local'
      fi
    elif [ ! -e \"\$p\" ]; then
      mkdir -p \"\$p\"
    fi
  done
"

echo "==> Syncing drift-scan workspace"
run_remote "mkdir -p ${WORKSPACE_REMOTE}/bifrost-platform/console/src/lib ${WORKSPACE_REMOTE}/bifrost-platform/config ${WORKSPACE_REMOTE}/bifrost-platform/agent"
rsync -az "${PLATFORM_LOCAL}/console/src/lib/" "${REMOTE}:${WORKSPACE_REMOTE}/bifrost-platform/console/src/lib/"
rsync -az "${PLATFORM_LOCAL}/config/" "${REMOTE}:${WORKSPACE_REMOTE}/bifrost-platform/config/"
rsync -az "${PLATFORM_LOCAL}/agent/drift/" "${REMOTE}:${WORKSPACE_REMOTE}/bifrost-platform/agent/drift/"
if [[ -n "${INFRA_LOCAL}" && -d "${INFRA_LOCAL}/docs" ]]; then
  run_remote "mkdir -p ${WORKSPACE_REMOTE}/bifrost-trade-infra/docs"
  rsync -az "${INFRA_LOCAL}/docs/" "${REMOTE}:${WORKSPACE_REMOTE}/bifrost-trade-infra/docs/"
fi
if [[ -n "${INFRA_LOCAL}" && -d "${INFRA_LOCAL}/k8s" ]]; then
  run_remote "mkdir -p ${WORKSPACE_REMOTE}/bifrost-trade-infra/k8s"
  rsync -az "${INFRA_LOCAL}/k8s/" "${REMOTE}:${WORKSPACE_REMOTE}/bifrost-trade-infra/k8s/"
fi

rsync -az --delete \
  --exclude='node_modules' \
  --exclude='.env' \
  "${AGENT_SRC}/" "${REMOTE}:${REMOTE_DIR}/src/"

KUBECONFIG_LOCAL="${KUBECONFIG:-$HOME/.kube/bifrost-k3s.yaml}"
if [[ -f "${KUBECONFIG_LOCAL}" ]]; then
  ssh "${REMOTE}" "mkdir -p ~/.kube"
  scp "${KUBECONFIG_LOCAL}" "${REMOTE}:~/.kube/bifrost-k3s.yaml"
  ssh "${REMOTE}" "chmod 600 ~/.kube/bifrost-k3s.yaml"
  echo "  kubeconfig synced"
else
  echo "  WARNING: kubeconfig not found at ${KUBECONFIG_LOCAL}"
fi

echo "==> Installing npm dependencies"
run_remote "cd ${REMOTE_DIR}/src && npm install --no-audit --no-fund"

echo "==> config/env.sh (install template only if missing)"
run_remote "
  if [ ! -f ${REMOTE_DIR}/config/env.sh ]; then
    cat > ${REMOTE_DIR}/config/env.sh << 'ENVEOF'
export KUBECONFIG=\$HOME/.kube/bifrost-k3s.yaml
export REMEDIATION_RUNNER_PORT=8781
export REMEDIATION_RUNNER_BIND=0.0.0.0
export PLATFORM_API_URL=http://192.168.10.73:30878
export REMEDIATION_RUNNER_URL=http://127.0.0.1:8781
export BIFROST_AGENT_ROOT=\$HOME/bifrost-agent
export REMEDIATION_CWD=\$HOME/bifrost-agent/workspace/bifrost-trade-infra
export REMEDIATION_JOBS_DIR=\$HOME/bifrost-agent/jobs
# If jobs -> NAS is unmounted, runner auto-falls back to ~/bifrost-agent/jobs-local
[ -f \"\$HOME/bifrost-agent/config/env.local.sh\" ] && source \"\$HOME/bifrost-agent/config/env.local.sh\"
ENVEOF
    echo '  wrote env.sh'
  else
    echo '  kept existing env.sh'
  fi
"

echo "==> config/env.local.sh (role + peer watchdog config, always rewritten)"
run_remote "cat > ${REMOTE_DIR}/config/env.local.sh << 'ENVEOF'
# Managed by deploy_mac_mini.sh — role + mutual-watchdog peer config.
export AGENT_ROLE=${AGENT_ROLE}
export PEER_AGENT_SSH=${PEER_SSH}
export PEER_AGENT_URL=${PEER_URL}
ENVEOF
echo '  wrote env.local.sh (role=${AGENT_ROLE} peer_ssh=${PEER_SSH} peer_url=${PEER_URL})'"

if [[ -f "${PLATFORM_LOCAL}/.env" ]]; then
  echo "==> Syncing secrets + bridge config to remote .env"
  TMP_ENV="$(mktemp)"
  grep -E '^(CURSOR_API_KEY|PLATFORM_OPERATOR_TOKEN|PLATFORM_ADMIN_TOKEN|GIT_BRIDGE_URL)=' "${PLATFORM_LOCAL}/.env" > "${TMP_ENV}" || true
  if [[ -s "${TMP_ENV}" ]]; then
    TMP_OUT="$(mktemp)"
    while IFS= read -r line; do
      if [[ "${line}" == export* ]]; then
        echo "${line}" >> "${TMP_OUT}"
      else
        echo "export ${line}" >> "${TMP_OUT}"
      fi
    done < "${TMP_ENV}"
    scp -q "${TMP_OUT}" "${REMOTE}:${REMOTE_DIR}/config/.env"
    run_remote "chmod 600 ${REMOTE_DIR}/config/.env"
    echo "  remote .env updated"
    rm -f "${TMP_OUT}"
  fi
  rm -f "${TMP_ENV}"
fi

echo "==> Installing launchd + nightly_drift.sh"
scp "${DEPLOY_DIR}/com.bifrost.remediation-runner.plist" "${REMOTE}:~/Library/LaunchAgents/"
scp "${DEPLOY_DIR}/com.bifrost.nightly-drift.plist" "${REMOTE}:~/Library/LaunchAgents/"
scp "${SCRIPT_DIR}/nightly_drift.sh" "${REMOTE}:${REMOTE_DIR}/nightly_drift.sh"
ssh "${REMOTE}" "chmod +x ${REMOTE_DIR}/nightly_drift.sh"

run_remote "launchctl bootout gui/\$(id -u)/com.bifrost.remediation-runner 2>/dev/null || true"
run_remote "launchctl bootstrap gui/\$(id -u) ~/Library/LaunchAgents/com.bifrost.remediation-runner.plist"

# nightly-drift runs on the PRIMARY only; standby keeps it disabled to avoid
# duplicate scans / NAS write contention.
if [[ "${AGENT_ROLE}" == "standby" ]]; then
  echo "==> AGENT_ROLE=standby — disabling nightly-drift on this host"
  run_remote "launchctl bootout gui/\$(id -u)/com.bifrost.nightly-drift 2>/dev/null || true"
else
  run_remote "launchctl bootout gui/\$(id -u)/com.bifrost.nightly-drift 2>/dev/null || true"
  run_remote "launchctl bootstrap gui/\$(id -u) ~/Library/LaunchAgents/com.bifrost.nightly-drift.plist"
fi

# Mutual watchdog — only install if peer config is provided.
if [[ -n "${PEER_SSH}" && -n "${PEER_URL}" ]]; then
  echo "==> Installing peer watchdog (peer=${PEER_URL})"
  scp "${SCRIPT_DIR}/peer_watchdog.sh" "${REMOTE}:${REMOTE_DIR}/peer_watchdog.sh"
  ssh "${REMOTE}" "chmod +x ${REMOTE_DIR}/peer_watchdog.sh"
  scp "${DEPLOY_DIR}/com.bifrost.peer-watchdog.plist" "${REMOTE}:~/Library/LaunchAgents/"
  run_remote "launchctl bootout gui/\$(id -u)/com.bifrost.peer-watchdog 2>/dev/null || true"
  run_remote "launchctl bootstrap gui/\$(id -u) ~/Library/LaunchAgents/com.bifrost.peer-watchdog.plist"
else
  echo "==> No PEER_SSH/PEER_URL — skipping peer watchdog install"
fi

echo "==> Syncing Bifrost MCP server (for Nous Hermes Agent)"
MCP_SRC="${PLATFORM_LOCAL}/mcp/platform"
run_remote "mkdir -p ${REMOTE_DIR}/mcp-platform"
rsync -az --delete \
  --exclude='node_modules' \
  "${MCP_SRC}/" "${REMOTE}:${REMOTE_DIR}/mcp-platform/"
run_remote "cd ${REMOTE_DIR}/mcp-platform && npm install --no-audit --no-fund"

echo "==> Post-deploy health smoke"
RUNNER_PORT="${RUNNER_PORT:-8781}"
HEALTH_URL="http://$(echo "${REMOTE}" | cut -d@ -f2):${RUNNER_PORT}/health"
SMOKE_OK=false
for i in 1 2 3 4 5; do
  sleep 2
  if curl -sf --max-time 5 "${HEALTH_URL}" > /dev/null 2>&1; then
    HEALTH_JSON="$(curl -sf --max-time 5 "${HEALTH_URL}")"
    RUNNER_VER="$(echo "${HEALTH_JSON}" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("version","?"))' 2>/dev/null || echo '?')"
    echo "  ✓ Runner healthy (v${RUNNER_VER}) on attempt ${i}"
    SMOKE_OK=true
    break
  fi
  echo "  attempt ${i}/5 — waiting for runner on ${HEALTH_URL}…"
done
if [[ "${SMOKE_OK}" != "true" ]]; then
  echo "  ✗ SMOKE FAILED — runner did not respond to ${HEALTH_URL} after 5 attempts"
  exit 1
fi

echo "==> Post-deploy tool smoke"
SMOKE_URL="http://$(echo "${REMOTE}" | cut -d@ -f2):${RUNNER_PORT}/smoke"
SMOKE_JSON="$(curl -sf --max-time 30 "${SMOKE_URL}" 2>/dev/null || echo '{}')"
SMOKE_STATUS="$(echo "${SMOKE_JSON}" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("status","unknown"))' 2>/dev/null || echo 'unknown')"
if [[ "${SMOKE_STATUS}" == "pass" ]]; then
  echo "  ✓ All tool dry-run checks passed"
else
  echo "  ⚠ Some checks failed (non-blocking):"
  echo "${SMOKE_JSON}" | python3 -c '
import sys, json
data = json.load(sys.stdin)
for c in data.get("checks", []):
    mark = "✓" if c["status"] == "pass" else "✗"
    detail = f" — {c.get(\"detail\",\"\")}" if c.get("detail") else ""
    print(f"    {mark} {c[\"label\"]}{detail}")
' 2>/dev/null || echo "    (could not parse smoke results)"
fi

echo "==> Post-deploy Nous Hermes Agent health probe"
HERMES_DASHBOARD_PORT="${HERMES_DASHBOARD_PORT:-9119}"
HERMES_DASHBOARD_URL="http://$(echo "${REMOTE}" | cut -d@ -f2):${HERMES_DASHBOARD_PORT}/api/status"
HERMES_OK=false
for i in 1 2 3; do
  sleep 2
  HERMES_JSON="$(curl -sf --max-time 5 -u "${NOUS_HERMES_USER:-bifrost}:${NOUS_HERMES_PASS:-bifrost-ops-2026}" "${HERMES_DASHBOARD_URL}" 2>/dev/null || echo '')"
  if [[ -n "${HERMES_JSON}" ]]; then
    HERMES_VER="$(echo "${HERMES_JSON}" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("version","?"))' 2>/dev/null || echo '?')"
    HERMES_GW="$(echo "${HERMES_JSON}" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("gateway_state","?"))' 2>/dev/null || echo '?')"
    echo "  ✓ Nous Hermes Agent v${HERMES_VER} (gateway: ${HERMES_GW}) on attempt ${i}"
    HERMES_OK=true
    break
  fi
  echo "  attempt ${i}/3 — waiting for Hermes dashboard on port ${HERMES_DASHBOARD_PORT}…"
done
if [[ "${HERMES_OK}" != "true" ]]; then
  echo "  ⚠ Nous Hermes Agent dashboard not reachable (non-blocking)"
fi

echo ""
echo "==> Done. role=${AGENT_ROLE} version=${RUNNER_VER}"
if [[ "${AGENT_ROLE}" != "standby" ]]; then
  echo "    Nightly 3:00 AM → ${REMOTE_DIR}/nightly_drift.sh"
fi
if [[ "${HERMES_OK}" == "true" ]]; then
  echo "    Nous Hermes Agent v${HERMES_VER} → http://$(echo "${REMOTE}" | cut -d@ -f2):${HERMES_DASHBOARD_PORT} (gateway: ${HERMES_GW})"
fi
if [[ -n "${PEER_SSH}" && -n "${PEER_URL}" ]]; then
  echo "    Peer watchdog every 60s → ${PEER_URL} (restart via ${PEER_SSH})"
fi
