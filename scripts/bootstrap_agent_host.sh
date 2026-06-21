#!/usr/bin/env bash
# Bootstrap Mac Mini (or any host) as Bifrost autonomous agent runner.
#
# Installs: Homebrew (if missing), Node 22, Git, Python3 (system).
# Does NOT clone repos — sync stocks workspace separately (rsync or git).
#
# Usage (on agent host via Screen Sharing):
#   curl -fsSL ...  OR  scp from Mac Pro then:
#   bash scripts/bootstrap_agent_host.sh
#
# After bootstrap:
#   1. Sync ~/Desktop/stocks (at least bifrost-platform + bifrost-trade-infra)
#   2. Copy .env from Mac Pro (CURSOR_API_KEY, tokens) — never commit
#   3. cd bifrost-platform && make dev-agent
#   4. On Mac Pro .env: REMEDIATION_RUNNER_URL=http://192.168.10.50:8781

set -euo pipefail

AGENT_IP_HINT="192.168.10.50"
REQUIRED_NODE_MAJOR=20

echo "=== Bifrost Agent Host Bootstrap ==="
echo "Hostname: $(hostname)"
echo "Expected agent LAN IP (Mini #1): $AGENT_IP_HINT"
echo ""

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "This script targets macOS (Mac Mini). For Linux, install node 20+ manually."
  exit 1
fi

install_homebrew() {
  if command -v brew >/dev/null 2>&1; then
    echo "[brew] already installed"
    return
  fi
  echo "[brew] Installing Homebrew..."
  /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
  if [[ -x /opt/homebrew/bin/brew ]]; then
    eval "$(/opt/homebrew/bin/brew shellenv)"
  elif [[ -x /usr/local/bin/brew ]]; then
    eval "$(/usr/local/bin/brew shellenv)"
  fi
}

install_packages() {
  echo "[brew] Installing node and git..."
  brew install node git
}

verify_node() {
  if ! command -v node >/dev/null 2>&1; then
    echo "Error: node not found after install"
    exit 1
  fi
  local ver major
  ver="$(node -v | sed 's/^v//')"
  major="${ver%%.*}"
  if [[ "$major" -lt "$REQUIRED_NODE_MAJOR" ]]; then
    echo "Error: node $ver < required $REQUIRED_NODE_MAJOR"
    exit 1
  fi
  echo "[ok] node $ver, npm $(npm -v), python3 $(python3 --version 2>/dev/null || echo missing), git $(git --version)"
}

print_next_steps() {
  cat <<EOF

=== Bootstrap complete ===

Next steps (Owner):

1. Sync workspace to this machine, e.g. on Mac Pro:
   rsync -avz --progress \\
     ~/Desktop/stocks/bifrost-platform \\
     ~/Desktop/stocks/bifrost-trade-infra \\
     vision@${AGENT_IP_HINT}:~/Desktop/stocks/

2. On this Mac Mini, create .env (copy from Mac Pro, do not paste secrets in chat):
   cd ~/Desktop/stocks/bifrost-platform
   cp .env.example .env
   # Edit: CURSOR_API_KEY, REMEDIATION_RUNNER_BIND=0.0.0.0
   #       PLATFORM_API_URL=http://<Mac-Pro-LAN-IP>:8780
   #       PLATFORM_OPERATOR_TOKEN=<operator token>
   #       KUBECONFIG=~/.kube/bifrost-k3s.yaml  (if cluster tools needed)

3. Start remediation runner (LAN-visible for Mac Pro platform-api):
   cd ~/Desktop/stocks/bifrost-platform
   make dev-agent

4. On Mac Pro bifrost-platform/.env add:
   REMEDIATION_RUNNER_URL=http://${AGENT_IP_HINT}:8781
   Then restart platform-api (make start).

5. Verify from Mac Pro:
   curl -s http://${AGENT_IP_HINT}:8781/health

6. Optional — install nightly launchd (after manual test):
   cp agent/schedules/com.bifrost.nightly-agent.plist ~/Library/LaunchAgents/
   # Edit plist paths if not using ~/Desktop/stocks
   launchctl load ~/Library/LaunchAgents/com.bifrost.nightly-agent.plist

EOF
}

install_homebrew
install_packages
verify_node
print_next_steps
