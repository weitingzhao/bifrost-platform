#!/usr/bin/env bash
# Per-node VLAN 10 cutover: push static 192.168.10.x, then flip switch port.
# Proven on ubt-k3s-04 (NOPASSWD). Other Linux/macOS nodes need BIFROST_SUDO_PASS.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
UNIFI_HOST="${UNIFI_HOST:-192.168.1.1}"
UNIFI_USER="${UNIFI_USER:?UNIFI_USER}"
UNIFI_PASS="${UNIFI_PASS:?UNIFI_PASS}"
SSH_USER="${SSH_USER:-vision}"
SUDO_PASS="${BIFROST_SUDO_PASS:-}"
SSH_OPTS=(-o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new)

log() { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*"; }

# name|old_ip|new_ip|port|iface|os
NODES=(
  "ubt-k3s-04|192.168.1.213|192.168.10.75|2|eno1|linux"
  "ubt-k3s-05|192.168.1.93|192.168.10.77|3|eno1|linux"
  "ubt-k3s-06|192.168.1.158|192.168.10.79|4|enp2s0|linux"
  "ubt-k3s-02|192.168.1.230|192.168.10.70|23|enp4s0|linux"
  "ops-mac-agent-02|192.168.1.102|192.168.10.50|5|Ethernet|macos"
  "ops-mac-agent-01|192.168.1.144|192.168.10.52|6|Ethernet|macos"
  "UGREEN-NAS|192.168.1.190|192.168.10.20|21|eth0|nas"
)

apply_port_vlan10() {
  local port="$1"
  UNIFI_HOST="$UNIFI_HOST" UNIFI_USER="$UNIFI_USER" UNIFI_PASS="$UNIFI_PASS" \
    python3 - "$port" << 'PY'
import json, os, ssl, sys, urllib.request
port = int(sys.argv[1])
ctx = ssl.create_default_context(); ctx.check_hostname = False; ctx.verify_mode = ssl.CERT_NONE
host, user, pw = os.environ["UNIFI_HOST"], os.environ["UNIFI_USER"], os.environ["UNIFI_PASS"]
req = urllib.request.Request(
    f"https://{host}/api/auth/login",
    json.dumps({"username": user, "password": pw}).encode(),
    {"Content-Type": "application/json"},
    method="POST",
)
with urllib.request.urlopen(req, context=ctx) as r:
    cookie = r.headers.get("Set-Cookie", "").split(";")[0]
    csrf = r.headers.get("X-Csrf-Token")

def call(method, path, body=None):
    global csrf
    h = {"Content-Type": "application/json", "Cookie": cookie, "X-CSRF-Token": csrf}
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(f"https://{host}{path}", data=data, headers=h, method=method)
    with urllib.request.urlopen(req, context=ctx) as r:
        csrf = r.headers.get("x-updated-csrf-token") or csrf
        return json.loads(r.read().decode())

pid = next(p["_id"] for p in call("GET", "/proxy/network/api/s/default/rest/portconf")["data"]
           if p["name"] == "Native VLAN 10 Server")
sw = next(d for d in call("GET", "/proxy/network/api/s/default/stat/device")["data"] if d["type"] == "usw")
overrides = {o["port_idx"]: o for o in sw.get("port_overrides", [])}
overrides[port] = {"port_idx": port, "portconf_id": pid}
resp = call("PUT", f"/proxy/network/api/s/default/rest/device/{sw['_id']}",
            {"port_overrides": [overrides[k] for k in sorted(overrides)]})
if resp.get("meta", {}).get("rc") != "ok":
    raise SystemExit(resp)
call("POST", "/proxy/network/api/s/default/cmd/devmgr",
     {"cmd": "restart-port", "mac": sw["mac"], "port_idx": port})
print(f"OK port P{port:02d} -> VLAN 10")
PY
}

linux_static() {
  local host="$1" ip="$2" iface="$3"
  local sudocmd
  if ssh "${SSH_OPTS[@]}" "${SSH_USER}@${host}" 'sudo -n true' 2>/dev/null; then
    sudocmd="sudo -n"
  elif [[ -n "$SUDO_PASS" ]]; then
    sudocmd="echo '$SUDO_PASS' | sudo -S"
  else
    log "SKIP $host — no NOPASSWD and BIFROST_SUDO_PASS unset"
    return 1
  fi
  ssh "${SSH_OPTS[@]}" "${SSH_USER}@${host}" "bash -s" <<EOF
set -euo pipefail
IF=${iface}
IP=${ip}
${sudocmd} bash -c '
cat > /etc/netplan/01-bifrost-vlan10.yaml <<YAML
network:
  version: 2
  renderer: networkd
  ethernets:
    '"\${IF}"':
      dhcp4: false
      dhcp6: false
      addresses: ['"\${IP}"'/24]
      routes:
        - to: default
          via: 192.168.10.1
      nameservers:
        addresses: [192.168.10.1, 1.1.1.1]
YAML
chmod 600 /etc/netplan/01-bifrost-vlan10.yaml
rm -f /etc/netplan/01-eno1-dhcp.yaml /etc/netplan/99-bifrost-vlan10.yaml
netplan generate && netplan apply
'
EOF
}

macos_static() {
  local host="$1" ip="$2" svc="${3:-Ethernet}"
  if ssh "${SSH_OPTS[@]}" "${SSH_USER}@${host}" 'sudo -n true' 2>/dev/null; then
    ssh "${SSH_OPTS[@]}" "${SSH_USER}@${host}" \
      "sudo networksetup -setmanual '$svc' '$ip' 255.255.255.0 192.168.10.1 && sudo networksetup -setdnsservers '$svc' 192.168.10.1 1.1.1.1"
  elif [[ -n "$SUDO_PASS" ]]; then
    ssh "${SSH_OPTS[@]}" "${SSH_USER}@${host}" \
      "echo '$SUDO_PASS' | sudo -S networksetup -setmanual '$svc' '$ip' 255.255.255.0 192.168.10.1 && echo '$SUDO_PASS' | sudo -S networksetup -setdnsservers '$svc' 192.168.10.1 1.1.1.1"
  else
    log "SKIP $host — macOS needs sudo"
    return 1
  fi
}

wait_ping() {
  local ip="$1" tries="${2:-20}"
  for _ in $(seq 1 "$tries"); do
    ping -c 1 -W 1 "$ip" >/dev/null 2>&1 && return 0
    sleep 2
  done
  return 1
}

cut_node() {
  local name="$1" old_ip="$2" new_ip="$3" port="$4" iface="$5" os="$6"
  log "=== $name $old_ip -> $new_ip (P$(printf '%02d' "$port")) ==="
  if wait_ping "$new_ip" 2; then
    log "Already on $new_ip — skip"
    return 0
  fi
  if ! wait_ping "$old_ip" 15; then
    log "WARN: $old_ip unreachable — skip"
    return 1
  fi
  case "$os" in
    linux) linux_static "$old_ip" "$new_ip" "$iface" || return 1 ;;
    macos) macos_static "$old_ip" "$new_ip" "$iface" || return 1 ;;
    nas)
      log "NAS: set $new_ip in UGOS or provide SSH + BIFROST_SUDO_PASS"
      return 1
      ;;
    *) log "Unknown os $os"; return 1 ;;
  esac
  log "Static applied — switching port (expect brief outage)..."
  apply_port_vlan10 "$port"
  if wait_ping "$new_ip" 25; then
    log "OK  $name reachable at $new_ip"
    return 0
  fi
  log "FAIL $name $new_ip not pingable after port cut"
  return 1
}

main() {
  local only="${1:-}"
  local ok=0 fail=0
  for entry in "${NODES[@]}"; do
    IFS='|' read -r name old new port iface os <<<"$entry"
    if [[ -n "$only" && "$name" != "$only" ]]; then
      continue
    fi
    if cut_node "$name" "$old" "$new" "$port" "$iface" "$os"; then
      ok=$((ok + 1))
    else
      fail=$((fail + 1))
    fi
  done
  log "Done: $ok ok, $fail failed/skipped"
  [[ "$fail" -eq 0 ]]
}

main "${1:-}"
