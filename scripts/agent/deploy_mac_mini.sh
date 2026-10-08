#!/usr/bin/env bash
# Deploy agent stack (remediation-runner + Nous Hermes MCP) to Mac Mini,
# and on the primary the Nous Hermes dashboard's launchd job (ai.hermes.dashboard).
# Invoked by: python scripts/run_agent.py deploy · Console Operator Plane → Update primary/standby
#
# Non-interactive only: Console cannot type SSH passwords. Uses BatchMode + publickey.
# Optional: AGENT_DEPLOY_SSH_IDENTITY=/path/to/key (default: ~/.ssh/id_ed25519 then id_rsa)
set -euo pipefail

# resolve_relay_config decides ALERT_RELAY and PEER_RELAY_URL for one host.
# It does not SSH. Stdout is exactly two lines:
#   ALERT_RELAY=on|off
#   PEER_RELAY_URL=<value or empty>
# Warnings go to stderr and start with WARNING:.
#
#   --plane-file PATH | --no-plane-file     remote env.operator-plane.sh
#   --local-file PATH | --no-local-file     remote env.local.sh
#   --disable-alert-relay                   force ALERT_RELAY=off
#
# An unset ALERT_RELAY / empty-or-unset PEER_RELAY_URL keeps the remote file.
# Writing off over a remote "on" requires --disable-alert-relay. A missing
# remote file uses the historical default (off / empty) and warns.
# Test entry (exits before any deploy): deploy_mac_mini.sh --resolve-relay-config ...
_relay_norm() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]'
}

_relay_has_export() {
  grep -qE "^export ${2}=" "$1" 2>/dev/null
}

_relay_extract_export() {
  local line val
  line="$(grep -E "^export ${2}=" "$1" 2>/dev/null | tail -n 1 || true)"
  [[ -n "${line}" ]] || return 0
  val="${line#export ${2}=}"
  val="${val%$'\r'}"
  case "${val}" in
    '"'*'"')
      val="${val#\"}"
      val="${val%\"}"
      ;;
    "'"*"'")
      val="${val#\'}"
      val="${val%\'}"
      ;;
  esac
  printf '%s' "${val}"
}

resolve_relay_config() {
  local plane_file="" local_file="" disable=0
  local have_plane=0 have_local=0
  local remote_alert="" remote_peer=""
  local plane_known=0 local_known=0
  local alert="off" peer="" explicit="" remote_norm=""

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --plane-file)
        [[ $# -ge 2 ]] || { echo "ERROR: --plane-file needs a path" >&2; return 2; }
        plane_file="$2"
        have_plane=1
        shift 2
        ;;
      --no-plane-file)
        have_plane=0
        plane_file=""
        shift
        ;;
      --local-file)
        [[ $# -ge 2 ]] || { echo "ERROR: --local-file needs a path" >&2; return 2; }
        local_file="$2"
        have_local=1
        shift 2
        ;;
      --no-local-file)
        have_local=0
        local_file=""
        shift
        ;;
      --disable-alert-relay)
        disable=1
        shift
        ;;
      *)
        echo "ERROR: resolve_relay_config: unknown argument: $1" >&2
        return 2
        ;;
    esac
  done

  if [[ "${have_plane}" -eq 1 && -n "${plane_file}" && -f "${plane_file}" ]]; then
    if _relay_has_export "${plane_file}" ALERT_RELAY; then
      remote_alert="$(_relay_extract_export "${plane_file}" ALERT_RELAY)"
      plane_known=1
    fi
  fi
  if [[ "${have_local}" -eq 1 && -n "${local_file}" && -f "${local_file}" ]]; then
    if _relay_has_export "${local_file}" PEER_RELAY_URL; then
      remote_peer="$(_relay_extract_export "${local_file}" PEER_RELAY_URL)"
      local_known=1
    fi
  fi

  if [[ "${disable}" -eq 1 ]]; then
    alert="off"
    echo "WARNING: !!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!" >&2
    echo "WARNING: !! --disable-alert-relay is turning ALERT_RELAY OFF." >&2
    echo "WARNING: !! Owner pages and heartbeats will 404 until you redeploy" >&2
    echo "WARNING: !! with ALERT_RELAY=on and without --disable-alert-relay." >&2
    echo "WARNING: !!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!" >&2
  elif [[ "${ALERT_RELAY+x}" == "x" ]]; then
    explicit="$(_relay_norm "${ALERT_RELAY}")"
    remote_norm="$(_relay_norm "${remote_alert}")"
    if [[ "${explicit}" == "on" ]]; then
      alert="on"
    elif [[ "${explicit}" == "off" ]]; then
      if [[ "${remote_norm}" == "on" ]]; then
        alert="on"
        echo "WARNING: ALERT_RELAY=off in the deployer environment does not turn the relay off." >&2
        echo "WARNING: Keeping remote ALERT_RELAY=on. Pass --disable-alert-relay to disable it." >&2
      else
        alert="off"
        if [[ "${plane_known}" -eq 0 ]]; then
          echo "WARNING: no remote env.operator-plane.sh ALERT_RELAY; defaulting ALERT_RELAY=off." >&2
          echo "WARNING: export ALERT_RELAY=on before deploy if this host should page the owner." >&2
        fi
      fi
    elif [[ "${plane_known}" -eq 1 ]]; then
      if [[ "${remote_norm}" == "on" || "${remote_norm}" == "off" ]]; then
        alert="${remote_norm}"
        echo "WARNING: ignoring invalid ALERT_RELAY=${ALERT_RELAY}; keeping remote ALERT_RELAY=${alert}." >&2
      else
        alert="off"
        echo "WARNING: invalid ALERT_RELAY=${ALERT_RELAY}; defaulting ALERT_RELAY=off." >&2
      fi
    else
      alert="off"
      echo "WARNING: invalid ALERT_RELAY=${ALERT_RELAY}; defaulting ALERT_RELAY=off." >&2
    fi
  else
    remote_norm="$(_relay_norm "${remote_alert}")"
    if [[ "${plane_known}" -eq 1 && "${remote_norm}" == "on" ]]; then
      alert="on"
    elif [[ "${plane_known}" -eq 1 && "${remote_norm}" == "off" ]]; then
      alert="off"
    else
      alert="off"
      echo "WARNING: no remote env.operator-plane.sh ALERT_RELAY; defaulting ALERT_RELAY=off." >&2
      echo "WARNING: export ALERT_RELAY=on before deploy if this host should page the owner." >&2
    fi
  fi

  if [[ "${PEER_RELAY_URL+x}" == "x" && -n "${PEER_RELAY_URL}" ]]; then
    peer="${PEER_RELAY_URL}"
  elif [[ "${local_known}" -eq 1 ]]; then
    peer="${remote_peer}"
  else
    peer=""
    echo "WARNING: no remote env.local.sh PEER_RELAY_URL; defaulting PEER_RELAY_URL to empty." >&2
    echo "WARNING: on the standby, export PEER_RELAY_URL=http://192.168.10.50:8783/api/v1/alerts/relay and redeploy." >&2
  fi

  printf 'ALERT_RELAY=%s\n' "${alert}"
  printf 'PEER_RELAY_URL=%s\n' "${peer}"
}

if [[ "${BASH_SOURCE[0]}" != "$0" ]]; then
  return 0
fi
# prod_viewer_export prints the line that gives the Mini's operator plane the
# PROD viewer token: "export PLATFORM_VIEWER_TOKEN=<value>", read from
# PLATFORM_PROD_VIEWER_TOKEN in the infra .env. config/platform-auth.yaml maps
# PLATFORM_VIEWER_TOKEN to the viewer role, so PROD callers can read viewer
# routes there: the nightly maintainer reconcile (GET /agent/launchd) and
# viewer requests PROD platform-api proxies to the plane. The Mac Pro's own
# PLATFORM_VIEWER_TOKEN is never sent (ops-arch 2026-10-08, option A).
#
#   prod_viewer_export <infra-env> [<other-tokens-file>]
#
# Prints nothing when the key is absent or empty. Exits 3, printing nothing on
# stdout, when the value equals any *_TOKEN already in <other-tokens-file>:
# LoadAuth rejects two roles with one token, and the plane would then deny every
# authenticated route. The value never goes to stderr.
# Test entry (exits before any deploy): deploy_mac_mini.sh --prod-viewer-export ...
_env_value() {
  local val="$1"
  val="${val%$'\r'}"
  case "${val}" in
    '"'*'"') val="${val#\"}"; val="${val%\"}" ;;
    "'"*"'") val="${val#\'}"; val="${val%\'}" ;;
  esac
  printf '%s' "${val}"
}

prod_viewer_export() {
  local infra_env="$1" others="${2:-}" line val other
  line="$(grep -E '^(export )?PLATFORM_PROD_VIEWER_TOKEN=' "${infra_env}" 2>/dev/null | tail -n 1 || true)"
  [[ -n "${line}" ]] || return 0
  val="$(_env_value "${line#*=}")"
  [[ -n "${val}" ]] || return 0
  if [[ -n "${others}" && -f "${others}" ]]; then
    while IFS= read -r other; do
      [[ -n "${other}" ]] || continue
      if [[ "$(_env_value "${other#*=}")" == "${val}" ]]; then
        echo "ERROR: PLATFORM_PROD_VIEWER_TOKEN equals ${other%%=*} on the Mini; the plane would refuse its auth file" >&2
        return 3
      fi
    done < <(grep -E '^(export )?[A-Z0-9_]*_TOKEN=' "${others}" || true)
  fi
  printf 'export PLATFORM_VIEWER_TOKEN=%s\n' "${val}"
}

if [[ "${1:-}" == "--prod-viewer-export" ]]; then
  shift
  prod_viewer_export "$@"
  exit $?
fi

if [[ "${1:-}" == "--resolve-relay-config" ]]; then
  shift
  resolve_relay_config "$@"
  exit 0
fi

DISABLE_ALERT_RELAY=0
_RELAY_POSITIONAL=()
for _relay_arg in "$@"; do
  case "${_relay_arg}" in
    --disable-alert-relay) DISABLE_ALERT_RELAY=1 ;;
    --*)
      echo "ERROR: unknown option: ${_relay_arg}" >&2
      exit 2
      ;;
    *) _RELAY_POSITIONAL+=("${_relay_arg}") ;;
  esac
done
if [[ ${#_RELAY_POSITIONAL[@]} -gt 1 ]]; then
  echo "ERROR: unexpected argument: ${_RELAY_POSITIONAL[1]}" >&2
  exit 1
fi
if [[ ${#_RELAY_POSITIONAL[@]} -eq 1 ]]; then
  REMOTE="${_RELAY_POSITIONAL[0]}"
else
  REMOTE="vision@192.168.10.50"
fi
unset _relay_arg

REMOTE_DIR="/Users/vision/bifrost-agent"
# Mutual-watchdog / Active-Standby config (env-driven, optional):
#   AGENT_ROLE  primary | standby   (default primary)
#   PEER_SSH    vision@192.168.10.52 (peer SSH target for watchdog restart)
#   PEER_URL    http://192.168.10.52:8781 (peer runner base URL for health probe)
#   PEER_RELAY_URL http://192.168.10.50:8783/api/v1/alerts/relay — set only when
#               deploying the Mini that does NOT run the alert relay (TD-248).
#               When unset (or set empty), the remote env.local.sh value is kept.
# Operator plane (L-1 Go binary, optional — skipped when Go is absent):
#   OPERATOR_PLANE_PORT       8783
#   OPERATOR_PLANE_AUTOPILOT  on | off (default off — platform-workers still owns patrol)
#   PLATFORM_LAN_HOST         192.168.10.40 (this host, as the Minis address it)
#   ALERT_RELAY               on | off — when unset, the remote env.operator-plane.sh
#                             value is kept. Default off only if that file has no
#                             value (warned). on→off requires --disable-alert-relay.
#                             On for exactly one Mini: the one Alertmanager's
#                             owner-ntfy receivers point at (TD-209).
#                             NTFY_TOPIC / ALERT_RELAY_TOKEN come from this host's .env.
AGENT_ROLE="${AGENT_ROLE:-primary}"
PEER_SSH="${PEER_SSH:-}"
PEER_URL="${PEER_URL:-}"
OPERATOR_PLANE_PORT="${OPERATOR_PLANE_PORT:-8783}"
OPERATOR_PLANE_AUTOPILOT="${OPERATOR_PLANE_AUTOPILOT:-off}"
PLATFORM_LAN_HOST="${PLATFORM_LAN_HOST:-192.168.10.40}"
HERMES_GATEWAY_REMOTE="${HERMES_GATEWAY_REMOTE:-192.168.10.52}"
# macOS gates local-network access per executable, and a launchd job does not
# inherit it: a LAN address that works from a shell answers "no route to host"
# in the plane. Loopback and the host's own address are exempt, so never send a
# host across the LAN to reach a service it is already running.
if [[ "$(echo "${REMOTE}" | cut -d@ -f2)" == "${HERMES_GATEWAY_REMOTE}" ]]; then
  HERMES_GATEWAY_URL_FOR_REMOTE="http://127.0.0.1:8782"
else
  HERMES_GATEWAY_URL_FOR_REMOTE="http://${HERMES_GATEWAY_REMOTE}:8782"
fi
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PLATFORM_LOCAL="$(cd "${SCRIPT_DIR}/../../" && pwd)"
# The runner binds 0.0.0.0. Refuse to deploy until the shared bearer key is present.
# This checks the key name only; the value is not printed.
if [[ ! -f "${PLATFORM_LOCAL}/.env" ]] || ! grep -qE '^REMEDIATION_RUNNER_TOKEN=.+' "${PLATFORM_LOCAL}/.env"; then
  echo "ERROR: REMEDIATION_RUNNER_TOKEN is missing or empty in ${PLATFORM_LOCAL}/.env" >&2
  echo "Add that key (distinct from PLATFORM_OPERATOR_TOKEN) before deploying a non-loopback runner." >&2
  exit 1
fi
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

echo "==> Resolving ALERT_RELAY and PEER_RELAY_URL (keep remote values unless set)"
_relay_plane_snap="$(mktemp)"
_relay_local_snap="$(mktemp)"
_relay_args=()
if run_remote "test -f ${REMOTE_DIR}/config/env.operator-plane.sh"; then
  run_remote "cat ${REMOTE_DIR}/config/env.operator-plane.sh" > "${_relay_plane_snap}"
  _relay_args+=(--plane-file "${_relay_plane_snap}")
else
  _relay_args+=(--no-plane-file)
fi
if run_remote "test -f ${REMOTE_DIR}/config/env.local.sh"; then
  run_remote "cat ${REMOTE_DIR}/config/env.local.sh" > "${_relay_local_snap}"
  _relay_args+=(--local-file "${_relay_local_snap}")
else
  _relay_args+=(--no-local-file)
fi
if [[ "${DISABLE_ALERT_RELAY}" -eq 1 ]]; then
  _relay_args+=(--disable-alert-relay)
fi
_relay_rc=0
_relay_out="$(resolve_relay_config "${_relay_args[@]}")" || _relay_rc=$?
rm -f "${_relay_plane_snap}" "${_relay_local_snap}"
unset _relay_plane_snap _relay_local_snap _relay_args
if [[ "${_relay_rc}" -ne 0 ]]; then
  exit "${_relay_rc}"
fi
ALERT_RELAY="$(printf '%s\n' "${_relay_out}" | sed -n 's/^ALERT_RELAY=//p')"
PEER_RELAY_URL="$(printf '%s\n' "${_relay_out}" | sed -n 's/^PEER_RELAY_URL=//p')"
unset _relay_out _relay_rc
if [[ "${ALERT_RELAY}" != "on" && "${ALERT_RELAY}" != "off" ]]; then
  echo "ERROR: resolved ALERT_RELAY='${ALERT_RELAY}' is not on or off" >&2
  exit 1
fi
echo "  effective for ${REMOTE}: ALERT_RELAY=${ALERT_RELAY} PEER_RELAY_URL=${PEER_RELAY_URL:-<empty>}"

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
export PEER_RELAY_URL=${PEER_RELAY_URL}
${_PLATFORM_API_LINE}
ENVEOF
echo '  wrote env.local.sh (role=${AGENT_ROLE} peer_ssh=${PEER_SSH} peer_url=${PEER_URL} peer_relay=${PEER_RELAY_URL})'"
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
  grep -E '^(CURSOR_API_KEY|PLATFORM_OPERATOR_TOKEN|PLATFORM_ADMIN_TOKEN|GIT_BRIDGE_URL|NTFY_URL|NTFY_TOPIC|ALERT_RELAY_TOKEN|REMEDIATION_RUNNER_TOKEN)=' "${PLATFORM_LOCAL}/.env" > "${TMP_ENV}" || true
  if [[ -s "${TMP_ENV}" ]]; then
    TMP_OUT="$(mktemp)"
    while IFS= read -r line; do
      if [[ "${line}" == export* ]]; then
        echo "${line}" >> "${TMP_OUT}"
      else
        echo "export ${line}" >> "${TMP_OUT}"
      fi
    done < "${TMP_ENV}"
    _viewer_rc=0
    _viewer_line=""
    if [[ -n "${INFRA_LOCAL}" && -f "${INFRA_LOCAL}/.env" ]]; then
      _viewer_line="$(prod_viewer_export "${INFRA_LOCAL}/.env" "${TMP_OUT}")" || _viewer_rc=$?
    fi
    if [[ "${_viewer_rc}" -ne 0 ]]; then
      rm -f "${TMP_OUT}" "${TMP_ENV}"
      exit "${_viewer_rc}"
    fi
    if [[ -n "${_viewer_line}" ]]; then
      printf '%s\n' "${_viewer_line}" >> "${TMP_OUT}"
      echo "  PROD viewer token added as PLATFORM_VIEWER_TOKEN"
    else
      echo "  WARNING: no PLATFORM_PROD_VIEWER_TOKEN in ${INFRA_LOCAL:-<no infra checkout>}/.env — PROD callers cannot read viewer routes on this plane" >&2
    fi
    unset _viewer_line _viewer_rc
    run_scp -q "${TMP_OUT}" "${REMOTE}:${REMOTE_DIR}/config/.env"
    run_remote "chmod 600 ${REMOTE_DIR}/config/.env"
    echo "  remote .env updated"
    rm -f "${TMP_OUT}"
  fi
  rm -f "${TMP_ENV}"
fi

echo "==> Installing remediation-runner launchd"
run_scp "${DEPLOY_DIR}/com.bifrost.remediation-runner.plist" "${REMOTE}:~/Library/LaunchAgents/"

run_remote "launchctl bootout gui/\$(id -u)/com.bifrost.remediation-runner 2>/dev/null || true"
run_remote "launchctl bootstrap gui/\$(id -u) ~/Library/LaunchAgents/com.bifrost.remediation-runner.plist"

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

echo "==> Operator plane (L-1)"
if command -v go >/dev/null 2>&1; then
  # Build for the Mini, not for whatever this host is.
  PLANE_ARCH="$(run_remote 'uname -m' | tr -d '\r')"
  case "${PLANE_ARCH}" in
    arm64) PLANE_GOARCH=arm64 ;;
    x86_64) PLANE_GOARCH=amd64 ;;
    *) echo "  ERROR: unknown remote arch '${PLANE_ARCH}'" >&2; exit 1 ;;
  esac
  echo "  building darwin/${PLANE_GOARCH}"
  make -C "${PLATFORM_LOCAL}" build-operator-plane GOOS=darwin GOARCH="${PLANE_GOARCH}" >/dev/null
  run_remote "mkdir -p ${REMOTE_DIR}/operator-plane-data"
  # Plane-only env: kept out of env.local.sh so the Node runner's environment
  # does not change when the plane's does.
  run_remote "cat > ${REMOTE_DIR}/config/env.operator-plane.sh << 'ENVEOF'
# Managed by deploy_mac_mini.sh — operator plane (L-1) only.
# git-bridge and the satellite probe bridge run on the platform host, so the
# Minis address it by LAN IP; the 127.0.0.1 in that host's .env means itself.
export PLATFORM_CONFIG=${REMOTE_DIR}/workspace/bifrost-platform/config/environments.yaml
export PLATFORM_DATA_DIR=${REMOTE_DIR}/operator-plane-data
export OPERATOR_PLANE_LISTEN=:${OPERATOR_PLANE_PORT}
export OPERATOR_PLANE_AUTOPILOT=${OPERATOR_PLANE_AUTOPILOT}
export ALERT_RELAY=${ALERT_RELAY}
export GIT_BRIDGE_URL=http://${PLATFORM_LAN_HOST}:8785
export SATELLITE_PROBE_BRIDGE_URL=http://${PLATFORM_LAN_HOST}:8786
export HERMES_GATEWAY_URL=${HERMES_GATEWAY_URL_FOR_REMOTE}
export NOUS_HERMES_URL=http://192.168.10.50:9119
ENVEOF
echo '  wrote env.operator-plane.sh (port=${OPERATOR_PLANE_PORT} autopilot=${OPERATOR_PLANE_AUTOPILOT} alert_relay=${ALERT_RELAY})'"
  # Stop first: macOS refuses to overwrite a running executable.
  run_remote "launchctl bootout gui/\$(id -u)/com.bifrost.operator-plane 2>/dev/null || true"
  run_scp "${PLATFORM_LOCAL}/api/bin/operator-plane" "${REMOTE}:${REMOTE_DIR}/operator-plane"
  run_remote "chmod +x ${REMOTE_DIR}/operator-plane"
  run_scp "${DEPLOY_DIR}/com.bifrost.operator-plane.plist" "${REMOTE}:~/Library/LaunchAgents/"
  run_remote "launchctl bootstrap gui/\$(id -u) ~/Library/LaunchAgents/com.bifrost.operator-plane.plist"

  PLANE_URL="http://$(echo "${REMOTE}" | cut -d@ -f2):${OPERATOR_PLANE_PORT}/health"
  PLANE_OK=false
  for i in 1 2 3 4 5; do
    sleep 2
    if PLANE_JSON="$(curl -sf --max-time 5 "${PLANE_URL}" 2>/dev/null)"; then
      echo "  ✓ operator-plane healthy: ${PLANE_JSON}"
      PLANE_OK=true
      break
    fi
    echo "  attempt ${i}/5 — waiting for operator-plane on ${PLANE_URL}…"
  done
  if [[ "${PLANE_OK}" != "true" ]]; then
    echo "  ✗ operator-plane did not answer ${PLANE_URL}" >&2
    run_remote "tail -20 ${REMOTE_DIR}/logs/operator-plane.err 2>/dev/null || true"
    exit 1
  fi
else
  echo "  skip — no Go toolchain on this host; the Node agent stack above is unaffected"
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

echo "==> Nous Hermes dashboard (launchd ai.hermes.dashboard — primary only)"
# :9119 lives beside the gateway it reports on: the primary, and only where Nous
# Hermes is installed (the standby has none). Its gateway already has a launchd
# job of its own (ai.hermes.gateway, from `hermes gateway install`).
if [[ "${AGENT_ROLE}" != "standby" ]] && run_remote 'test -x "$HOME/.hermes/hermes-agent/venv/bin/python"'; then
  run_scp "${DEPLOY_DIR}/ai.hermes.dashboard.plist" "${REMOTE}:~/Library/LaunchAgents/"
  run_remote "launchctl bootout gui/\$(id -u)/ai.hermes.dashboard 2>/dev/null || true"
  # A dashboard started by hand (from an SSH session) holds :9119 and would
  # leave the launchd job crash-looping on the port: stop it — only if it is
  # one. Anything else on the port is reported, and the job is not loaded.
  DASH_PORT_STATE="$(run_remote '
    for i in 1 2 3 4 5 6 7 8 9 10; do
      pid=$(/usr/sbin/lsof -t -i :9119 -sTCP:LISTEN 2>/dev/null | head -1)
      [ -z "$pid" ] && { echo free; exit 0; }
      case "$(ps -o command= -p "$pid")" in
        *"hermes dashboard"*|*"hermes_cli.main dashboard"*) kill "$pid"; sleep 1 ;;
        *) echo "held:$pid"; exit 0 ;;
      esac
    done
    echo "held:$(/usr/sbin/lsof -t -i :9119 -sTCP:LISTEN 2>/dev/null | head -1)"')"
  if [[ "${DASH_PORT_STATE}" == "free" ]]; then
    run_remote "launchctl bootstrap gui/\$(id -u) ~/Library/LaunchAgents/ai.hermes.dashboard.plist"
    echo "  loaded ai.hermes.dashboard"
  else
    echo "  ⚠ :9119 is held by something other than the Hermes dashboard (${DASH_PORT_STATE}) — ai.hermes.dashboard not loaded" >&2
  fi
else
  echo "  skip — standby, or Nous Hermes is not installed on ${REMOTE}"
fi

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
# shellcheck source=tool_smoke.sh
source "${SCRIPT_DIR}/tool_smoke.sh"
tool_smoke_report "http://$(echo "${REMOTE}" | cut -d@ -f2):${RUNNER_PORT}/smoke" "${PLATFORM_LOCAL}/.env"

echo "==> Post-deploy Nous Hermes Agent health probe"
# /api/status is public on the Hermes dashboard; the endpoints behind basic auth
# (/api/env, /api/config, /api/sessions …) are not needed here, so the probe sends
# no credentials. Never put a default password here — this repo is public.
HERMES_DASHBOARD_PORT="${HERMES_DASHBOARD_PORT:-9119}"
HERMES_DASHBOARD_URL="http://$(echo "${REMOTE}" | cut -d@ -f2):${HERMES_DASHBOARD_PORT}/api/status"
HERMES_OK=false
for i in 1 2 3; do
  sleep 2
  HERMES_JSON="$(curl -sf --max-time 5 "${HERMES_DASHBOARD_URL}" 2>/dev/null || echo '')"
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
if [[ "${HERMES_OK}" == "true" ]]; then
  echo "    Nous Hermes Agent v${HERMES_VER} → http://$(echo "${REMOTE}" | cut -d@ -f2):${HERMES_DASHBOARD_PORT} (gateway: ${HERMES_GW})"
fi
if [[ -n "${PEER_SSH}" && -n "${PEER_URL}" ]]; then
  echo "    Peer watchdog every 60s → ${PEER_URL} (restart via ${PEER_SSH})"
fi

# .50 must be paging and .52 must still know where the relay is. A deploy that
# rewrote ALERT_RELAY=off (2026-10-07) is not a successful deploy.
echo "==> Alert relay effective values"
_relay_user="${REMOTE%%@*}"
if [[ -z "${_relay_user}" || "${_relay_user}" == "${REMOTE}" ]]; then
  _relay_user="vision"
fi
_relay_primary="${ALERT_RELAY_PRIMARY_HOST:-192.168.10.50}"
_relay_standby="${ALERT_RELAY_STANDBY_HOST:-192.168.10.52}"
_relay_port="${OPERATOR_PLANE_PORT:-8783}"

_relay_dump_host() {
  local target="$1"
  ssh "${SSH_OPTS[@]}" "${target}" \
    "grep -E '^export ALERT_RELAY=' ${REMOTE_DIR}/config/env.operator-plane.sh 2>/dev/null || echo 'export ALERT_RELAY=(missing)'; grep -E '^export PEER_RELAY_URL=' ${REMOTE_DIR}/config/env.local.sh 2>/dev/null || echo 'export PEER_RELAY_URL=(missing)'" \
    || echo "export ALERT_RELAY=(ssh-failed)
export PEER_RELAY_URL=(ssh-failed)"
}

_relay_primary_dump="$(_relay_dump_host "${_relay_user}@${_relay_primary}")"
_relay_standby_dump="$(_relay_dump_host "${_relay_user}@${_relay_standby}")"
echo "  ${_relay_primary}"
printf '%s\n' "${_relay_primary_dump}" | sed 's/^/    /'
echo "  ${_relay_standby}"
printf '%s\n' "${_relay_standby_dump}" | sed 's/^/    /'

_relay_health="$(curl -sf --max-time 5 "http://${_relay_primary}:${_relay_port}/health" 2>/dev/null || true)"
echo "  ${_relay_primary}  GET :${_relay_port}/health ${_relay_health:-<no response>}"

_relay_fail=0
if [[ "${_relay_health}" != *'"alert_relay":true'* && "${_relay_health}" != *'"alert_relay": true'* ]]; then
  echo "ERROR: ${_relay_primary} GET :${_relay_port}/health must show alert_relay:true (got: ${_relay_health:-no response})" >&2
  _relay_fail=1
fi

_relay_peer_line="$(printf '%s\n' "${_relay_standby_dump}" | grep 'PEER_RELAY_URL=' | tail -n 1 || true)"
_relay_peer_val="${_relay_peer_line#export PEER_RELAY_URL=}"
_relay_peer_val="${_relay_peer_val#\"}"
_relay_peer_val="${_relay_peer_val%\"}"
_relay_peer_val="${_relay_peer_val#\'}"
_relay_peer_val="${_relay_peer_val%\'}"
if [[ -z "${_relay_peer_val}" || "${_relay_peer_val}" == "(missing)" || "${_relay_peer_val}" == "(ssh-failed)" || "${_relay_peer_val}" == "${_relay_peer_line}" ]]; then
  echo "ERROR: ${_relay_standby} ${REMOTE_DIR}/config/env.local.sh must contain a non-empty PEER_RELAY_URL" >&2
  _relay_fail=1
fi

if [[ "${_relay_fail}" -ne 0 ]]; then
  cat >&2 <<EOF
How to fix:
  On ${_relay_primary}, redeploy with ALERT_RELAY=on and do not pass --disable-alert-relay.
  ${REMOTE_DIR}/config/env.operator-plane.sh must export ALERT_RELAY=on, then restart
  the operator-plane launchd job and confirm:
    curl -sf http://${_relay_primary}:${_relay_port}/health
  shows "alert_relay":true. The plane also needs NTFY_TOPIC and ALERT_RELAY_TOKEN
  in ${REMOTE_DIR}/config/.env; without them the process stays up with alert_relay false.
  On ${_relay_standby}, redeploy with
    PEER_RELAY_URL=http://${_relay_primary}:${_relay_port}/api/v1/alerts/relay
  so ${REMOTE_DIR}/config/env.local.sh contains that export.
EOF
  exit 1
fi
