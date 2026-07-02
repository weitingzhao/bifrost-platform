#!/usr/bin/env bash
# Restore 192.168.10.x on server ports after VLAN 10 cutover.
# 1) Revert USW server ports to Default (L2 VLAN 1) for SSH on 192.168.1.x
# 2) Push netplan / macOS network static IPs
# 3) Re-apply Native VLAN 10 port profile
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
UNIFI_HOST="${UNIFI_HOST:-192.168.1.1}"
UNIFI_USER="${UNIFI_USER:?UNIFI_USER not set}"
UNIFI_PASS="${UNIFI_PASS:?UNIFI_PASS not set}"
SSH_USER="${SSH_USER:-vision}"
SUDO_PASS="${BIFROST_SUDO_PASS:-}"
SSH_OPTS=(-o BatchMode=yes -o ConnectTimeout=8 -o StrictHostKeyChecking=accept-new)

log() { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*"; }

revert_ports() {
  log "Reverting server ports to Default (clear overrides)..."
  UNIFI_HOST="$UNIFI_HOST" UNIFI_USER="$UNIFI_USER" UNIFI_PASS="$UNIFI_PASS" \
    python3 "$SCRIPT_DIR/unifi_vlan_setup.py" revert-ports
}

apply_vlan10_ports() {
  log "Re-applying Native VLAN 10 on server ports..."
  UNIFI_HOST="$UNIFI_HOST" UNIFI_USER="$UNIFI_USER" UNIFI_PASS="$UNIFI_PASS" \
    python3 "$SCRIPT_DIR/unifi_vlan_setup.py" apply-ports-only
}

wait_ping() {
  local ip="$1" tries="${2:-30}"
  for _ in $(seq 1 "$tries"); do
    if ping -c 1 -W 1 "$ip" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  return 1
}

linux_netplan() {
  local host="$1" ip="$2"
  if [[ -z "$SUDO_PASS" ]]; then
    log "ERROR: BIFROST_SUDO_PASS not set — cannot run netplan on $host"
    return 1
  fi
  ssh "${SSH_OPTS[@]}" "${SSH_USER}@${host}" "sudo -S bash -s" <<EOF
$SUDO_PASS
set -euo pipefail
TARGET_IP="${ip}"
IF=\$(ip -o -4 route show to default 2>/dev/null | awk '{print \$3}' | head -1)
if [[ -z "\$IF" ]]; then
  IF=\$(ip -o link show | awk -F': ' '{print \$2}' | grep -E '^(en|eth)' | head -1)
fi
FILE="/etc/netplan/99-bifrost-vlan10.yaml"
cat > "\$FILE" <<YAML
network:
  version: 2
  ethernets:
    \${IF}:
      dhcp4: false
      dhcp6: false
      addresses: [\${TARGET_IP}/24]
      routes:
        - to: default
          via: 192.168.10.1
      nameservers:
        addresses: [192.168.10.1, 1.1.1.1]
YAML
chmod 600 "\$FILE"
# Remove cloud-init dhcp overrides if present
for f in /etc/netplan/50-cloud-init.yaml /etc/netplan/00-installer-config.yaml; do
  [[ -f "\$f" ]] && mv "\$f" "\${f}.bak.\$(date +%s)" || true
done
netplan generate
netplan apply
echo "OK \$(hostname) \${IF} -> \${TARGET_IP}"
ip -4 addr show dev "\$IF" | grep inet
EOF
}

macos_static() {
  local host="$1" ip="$2"
  ssh "${SSH_OPTS[@]}" "${SSH_USER}@${host}" "bash -s" <<EOF
set -euo pipefail
IP="${ip}"
GW="192.168.10.1"
SVC=\$(networksetup -listallhardwareports | awk -F': ' '
  /Hardware Port:/{hp=\$2}
  /Device: en/{print hp; exit}
')
if [[ -z "\$SVC" ]]; then SVC="Ethernet"; fi
echo "Service=\$SVC -> \$IP"
sudo networksetup -setmanual "\$SVC" "\$IP" 255.255.255.0 "\$GW"
sudo networksetup -setdnsservers "\$SVC" "\$GW" 1.1.1.1
echo "OK \$(hostname) \$IP"
EOF
}

configure_node() {
  local name="$1" old_ip="$2" new_ip="$3" os="$4"
  log "Configure $name ($old_ip -> $new_ip) os=$os"
  if ! wait_ping "$old_ip" 20; then
    log "WARN: $name $old_ip not pingable — skip"
    return 1
  fi
  case "$os" in
    linux) linux_netplan "$old_ip" "$new_ip" ;;
    macos) macos_static "$old_ip" "$new_ip" ;;
    windows)
      log "SKIP Windows $name — set $new_ip manually in adapter settings"
      return 0
      ;;
    nas)
      if ssh "${SSH_OPTS[@]}" "${SSH_USER}@${old_ip}" "hostname" >/dev/null 2>&1; then
        linux_netplan "$old_ip" "$new_ip"
      else
        log "WARN: NAS SSH unavailable — set $new_ip in UGOS UI"
        return 1
      fi
      ;;
    *) log "Unknown os $os"; return 1 ;;
  esac
  sleep 2
  if wait_ping "$new_ip" 15; then
    log "OK $name reachable at $new_ip (still on Default VLAN until port re-apply)"
    return 0
  fi
  log "NOTE $name $new_ip not pingable yet (may need VLAN 10 port)"
  return 0
}

verify_all() {
  log "=== Verification ==="
  local ok=0 fail=0
  while read -r ip name; do
    if wait_ping "$ip" 3; then
      log "  OK  $name $ip"
      ok=$((ok + 1))
    else
      log "  FAIL $name $ip"
      fail=$((fail + 1))
    fi
  done <<'IPS'
192.168.10.73 ubt-k3s-01
192.168.10.70 ubt-k3s-02
192.168.10.75 ubt-k3s-04
192.168.10.77 ubt-k3s-05
192.168.10.79 ubt-k3s-06
192.168.10.20 UGREEN-NAS
192.168.10.60 ubt-ai-4090
192.168.10.50 ops-mac-agent-02
192.168.10.52 ops-mac-agent-01
192.168.10.160 TWS-Host
192.168.10.172 TWS-Sec
IPS
  log "Reachable: $ok / $((ok + fail))"
}

main() {
  revert_ports
  log "Waiting for Default VLAN connectivity..."
  sleep 8

  # name old_ip new_ip os
  local nodes=(
    "ubt-k3s-01|192.168.1.153|192.168.10.73|linux"
    "ubt-k3s-02|192.168.1.230|192.168.10.70|linux"
    "ubt-k3s-04|192.168.1.215|192.168.10.75|linux"
    "ubt-k3s-05|192.168.1.93|192.168.10.77|linux"
    "ubt-k3s-06|192.168.1.158|192.168.10.79|linux"
    "ubt-ai-4090|192.168.1.147|192.168.10.60|linux"
    "ops-mac-agent-02|192.168.1.102|192.168.10.50|macos"
    "ops-mac-agent-01|192.168.1.144|192.168.10.52|macos"
    "UGREEN-NAS|192.168.1.190|192.168.10.20|nas"
    "TWS-Host|192.168.1.160|192.168.10.160|windows"
    "TWS-Sec|192.168.1.172|192.168.10.172|windows"
  )

  local failed=0
  for entry in "${nodes[@]}"; do
    IFS='|' read -r name old new os <<<"$entry"
    configure_node "$name" "$old" "$new" "$os" || failed=$((failed + 1))
  done

  apply_vlan10_ports
  log "Waiting for VLAN 10 port profile to propagate..."
  sleep 10
  verify_all

  if [[ "$failed" -gt 0 ]]; then
    log "Completed with $failed node(s) needing manual follow-up"
    exit 1
  fi
  log "All server static IPs restored on VLAN 10"
}

main "$@"
