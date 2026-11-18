#!/usr/bin/env bash
# Bootstrap Mac Mini (or any host) as Bifrost autonomous agent runner.
#
# Installs: Homebrew (if missing), Node 22, Git, Python3 (system).
# Does NOT clone repos — sync stocks workspace separately (rsync or git).
#
# Usage (on agent host via Screen Sharing):
#   curl -fsSL ...  OR  scp from Mac Pro then:
#   bash scripts/agent/bootstrap_host.sh
#
# After bootstrap:
#   1. Sync ~/Desktop/stocks (at least bifrost-platform + bifrost-trade-infra)
#   2. Deploy the host services: python scripts/run_agent.py deploy <user@host>
#      (the remediation runner and Hermes gateway are retired; see deploy_mac_mini.sh)

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

2. Deploy the host services (git-bridge, probe bridge, operator plane) from the Mac Pro:
   cd ~/Desktop/stocks/bifrost-platform
   python scripts/run_agent.py deploy vision@${AGENT_IP_HINT}
   (The remediation runner and Hermes gateway are retired and no longer installed.)

3. Verify from Mac Pro:
   curl -s http://${AGENT_IP_HINT}:8783/health

EOF
}

install_homebrew
install_packages
verify_node
print_next_steps
