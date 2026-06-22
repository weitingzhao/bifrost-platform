#!/usr/bin/env bash
# Deploy remediation-runner + nightly drift to Mac Mini. Invoked by: python scripts/run_agent.py deploy
set -euo pipefail

REMOTE="${1:-vision@192.168.10.50}"
REMOTE_DIR="/Users/vision/bifrost-agent"
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

if [[ -f "${PLATFORM_LOCAL}/.env" ]]; then
  echo "==> Syncing CURSOR_API_KEY + PLATFORM_OPERATOR_TOKEN to remote .env"
  TMP_ENV="$(mktemp)"
  grep -E '^(CURSOR_API_KEY|PLATFORM_OPERATOR_TOKEN)=' "${PLATFORM_LOCAL}/.env" > "${TMP_ENV}" || true
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
run_remote "launchctl bootout gui/\$(id -u)/com.bifrost.nightly-drift 2>/dev/null || true"
run_remote "launchctl bootstrap gui/\$(id -u) ~/Library/LaunchAgents/com.bifrost.nightly-drift.plist"

echo ""
echo "==> Done. Nightly 3:00 AM → ${REMOTE_DIR}/nightly_drift.sh"
