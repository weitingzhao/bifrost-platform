#!/usr/bin/env bash
# Deploy agent stack (remediation-runner + Nous Hermes MCP + nightly drift) to Mac Mini.
# Invoked by: python scripts/run_agent.py deploy · Console Operator Plane → Update primary/standby
#
# Non-interactive only: Console cannot type SSH passwords. Uses BatchMode + publickey.
# Optional: AGENT_DEPLOY_SSH_IDENTITY=/path/to/key (default: ~/.ssh/id_ed25519 then id_rsa)
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

resolve_ssh_identity() {
  if [[ -n "${AGENT_DEPLOY_SSH_IDENTITY:-}" && -f "${AGENT_DEPLOY_SSH_IDENTITY}" ]]; then
    printf '%s' "${AGENT_DEPLOY_SSH_IDENTITY}"
    return 0
  fi
  local home="${HOME:-}"
  for cand in "${home}/.ssh/id_ed25519" "${home}/.ssh/id_rsa" "${home}/.ssh/bifrost_deploy"; do
    if [[ -f "${cand}" ]]; then
      printf '%s' "${cand}"
      return 0
    fi
  done
  return 1
}

SSH_IDENTITY=""
if SSH_IDENTITY="$(resolve_ssh_identity)"; then
  echo "==> SSH identity: ${SSH_IDENTITY} (BatchMode — no password prompt)"
else
  echo "ERROR: no SSH private key found. Set AGENT_DEPLOY_SSH_IDENTITY or install ~/.ssh/id_ed25519." >&2
  echo "Console deploy cannot enter passwords — configure key-based SSH from the platform-api host to ${REMOTE}." >&2
  exit 2
fi

# Prefer publickey only; never hang waiting for a TTY password (Console has no input).
SSH_OPTS=(
  -o BatchMode=yes
  -o IdentitiesOnly=yes
  -o IdentityFile="${SSH_IDENTITY}"
  -o PreferredAuthentications=publickey
  -o PubkeyAuthentication=yes
  -o ConnectTimeout=15
  -o StrictHostKeyChecking=accept-new
)
RSYNC_SSH="ssh ${SSH_OPTS[*]}"

run_remote() {
  ssh "${SSH_OPTS[@]}" "${REMOTE}" "export PATH=${REMOTE_PATH}; $*"
}

run_scp() {
  scp "${SSH_OPTS[@]}" "$@"
}

echo "==> Preflight SSH (BatchMode) → ${REMOTE}"
if ! ssh "${SSH_OPTS[@]}" "${REMOTE}" 'echo ok' >/dev/null; then
  echo "ERROR: SSH BatchMode failed for ${REMOTE} with ${SSH_IDENTITY}." >&2
  echo "Fix: ssh-copy-id -i ${SSH_IDENTITY}.pub ${REMOTE}" >&2
  echo "Console cannot type passwords — key auth is required for Update primary/standby." >&2
  exit 2
fi

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
rsync -az -e "${RSYNC_SSH}" "${PLATFORM_LOCAL}/console/src/lib/" "${REMOTE}:${WORKSPACE_REMOTE}/bifrost-platform/console/src/lib/"
rsync -az -e "${RSYNC_SSH}" "${PLATFORM_LOCAL}/config/" "${REMOTE}:${WORKSPACE_REMOTE}/bifrost-platform/config/"
rsync -az -e "${RSYNC_SSH}" "${PLATFORM_LOCAL}/agent/drift/" "${REMOTE}:${WORKSPACE_REMOTE}/bifrost-platform/agent/drift/"
if [[ -n "${INFRA_LOCAL}" && -d "${INFRA_LOCAL}/docs" ]]; then
  run_remote "mkdir -p ${WORKSPACE_REMOTE}/bifrost-trade-infra/docs"
  rsync -az -e "${RSYNC_SSH}" "${INFRA_LOCAL}/docs/" "${REMOTE}:${WORKSPACE_REMOTE}/bifrost-trade-infra/docs/"
fi
if [[ -n "${INFRA_LOCAL}" && -d "${INFRA_LOCAL}/k8s" ]]; then
  run_remote "mkdir -p ${WORKSPACE_REMOTE}/bifrost-trade-infra/k8s"
  rsync -az -e "${RSYNC_SSH}" "${INFRA_LOCAL}/k8s/" "${REMOTE}:${WORKSPACE_REMOTE}/bifrost-trade-infra/k8s/"
fi

rsync -az --delete -e "${RSYNC_SSH}" \
  --exclude='node_modules' \
  --exclude='.env' \
  "${AGENT_SRC}/" "${REMOTE}:${REMOTE_DIR}/src/"

KUBECONFIG_LOCAL="${KUBECONFIG:-$HOME/.kube/bifrost-k3s.yaml}"
if [[ -f "${KUBECONFIG_LOCAL}" ]]; then
  run_remote "mkdir -p ~/.kube"
  run_scp "${KUBECONFIG_LOCAL}" "${REMOTE}:~/.kube/bifrost-k3s.yaml"
  run_remote "chmod 600 ~/.kube/bifrost-k3s.yaml"
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
# Optional AGENT_PLATFORM_API_URL → PLATFORM_API_URL on the Mini:
# Point remediation runners at Mac Pro :8780 (or any platform-api that serves
# /checklist/signals) when cluster NodePort lags behind. Preserves env.local
# sourcing from env.sh — do not remove AGENT_ROLE / peer watchdog lines.
_PLATFORM_API_LINE=""
if [[ -n "${AGENT_PLATFORM_API_URL:-}" ]]; then
  _PLATFORM_API_LINE="export PLATFORM_API_URL=${AGENT_PLATFORM_API_URL}"
fi
run_remote "cat > ${REMOTE_DIR}/config/env.local.sh << 'ENVEOF'
# Managed by deploy_mac_mini.sh — role + mutual-watchdog peer config.
# Optional PLATFORM_API_URL: checklist AI Check / report_checklist_signals need
# a platform-api that exposes /api/v1/checklist/signals (often Mac Pro :8780).
export AGENT_ROLE=${AGENT_ROLE}
export PEER_AGENT_SSH=${PEER_SSH}
export PEER_AGENT_URL=${PEER_URL}
${_PLATFORM_API_LINE}
ENVEOF
echo '  wrote env.local.sh (role=${AGENT_ROLE} peer_ssh=${PEER_SSH} peer_url=${PEER_URL})'"
unset _PLATFORM_API_LINE

# Ensure env.sh sources env.local.sh (override PLATFORM_API_URL etc.)
run_remote "
  if [ -f ${REMOTE_DIR}/config/env.sh ] && ! grep -q 'env.local.sh' ${REMOTE_DIR}/config/env.sh; then
    printf '\n[ -f \"\$HOME/bifrost-agent/config/env.local.sh\" ] && source \"\$HOME/bifrost-agent/config/env.local.sh\"\n' >> ${REMOTE_DIR}/config/env.sh
    echo '  appended env.local.sh source to env.sh'
  fi
"

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
    run_scp -q "${TMP_OUT}" "${REMOTE}:${REMOTE_DIR}/config/.env"
    run_remote "chmod 600 ${REMOTE_DIR}/config/.env"
    echo "  remote .env updated"
    rm -f "${TMP_OUT}"
  fi
  rm -f "${TMP_ENV}"
fi

echo "==> Installing launchd + nightly_drift.sh"
run_scp "${DEPLOY_DIR}/com.bifrost.remediation-runner.plist" "${REMOTE}:~/Library/LaunchAgents/"
run_scp "${DEPLOY_DIR}/com.bifrost.nightly-drift.plist" "${REMOTE}:~/Library/LaunchAgents/"
run_scp "${SCRIPT_DIR}/nightly_drift.sh" "${REMOTE}:${REMOTE_DIR}/nightly_drift.sh"
run_remote "chmod +x ${REMOTE_DIR}/nightly_drift.sh"

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
  run_scp "${SCRIPT_DIR}/peer_watchdog.sh" "${REMOTE}:${REMOTE_DIR}/peer_watchdog.sh"
  run_remote "chmod +x ${REMOTE_DIR}/peer_watchdog.sh"
  run_scp "${DEPLOY_DIR}/com.bifrost.peer-watchdog.plist" "${REMOTE}:~/Library/LaunchAgents/"
  run_remote "launchctl bootout gui/\$(id -u)/com.bifrost.peer-watchdog 2>/dev/null || true"
  run_remote "launchctl bootstrap gui/\$(id -u) ~/Library/LaunchAgents/com.bifrost.peer-watchdog.plist"
else
  echo "==> No PEER_SSH/PEER_URL — skipping peer watchdog install"
fi

echo "==> Syncing Bifrost MCP server (for Nous Hermes Agent)"
MCP_SRC="${PLATFORM_LOCAL}/mcp/platform"
run_remote "mkdir -p ${REMOTE_DIR}/mcp-platform"
rsync -az --delete -e "${RSYNC_SSH}" \
  --exclude='node_modules' \
  "${MCP_SRC}/" "${REMOTE}:${REMOTE_DIR}/mcp-platform/"
run_remote "cd ${REMOTE_DIR}/mcp-platform && npm install --no-audit --no-fund"

echo "==> Pin Hermes tool_search=off (keep L0 MCP tools eager: verify_mission_snapshot / verify_payload)"
run_remote 'export PATH="$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:$PATH"; if command -v hermes >/dev/null 2>&1; then hermes config set tools.tool_search.enabled off; else echo "  skip — hermes CLI not on PATH"; fi'

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
