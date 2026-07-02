#!/usr/bin/env bash
# Audit UniFi networks + switch ports (read-only).
set -euo pipefail

HOST="${UNIFI_HOST:-192.168.1.1}"
USER="${UNIFI_USER:?UNIFI_USER not set}"
PASS="${UNIFI_PASS:?UNIFI_PASS not set}"

COOKIE_JAR="$(mktemp)"
trap 'rm -f "$COOKIE_JAR"' EXIT

curl -sk -m 15 -c "$COOKIE_JAR" -X POST "https://${HOST}/api/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"${USER}\",\"password\":\"${PASS}\"}" >/dev/null

echo "=== NETWORKS (networkconf) ==="
curl -sk -m 15 -b "$COOKIE_JAR" "https://${HOST}/proxy/network/api/s/default/rest/networkconf" \
  | python3 -c "
import json,sys
d=json.load(sys.stdin)
for n in d.get('data',[]):
    print(json.dumps({
        '_id': n.get('_id'),
        'name': n.get('name'),
        'purpose': n.get('purpose'),
        'vlan': n.get('vlan'),
        'vlan_enabled': n.get('vlan_enabled'),
        'ip_subnet': n.get('ip_subnet'),
        'dhcpd_enabled': n.get('dhcpd_enabled'),
        'dhcpd_start': n.get('dhcpd_start'),
        'dhcpd_stop': n.get('dhcpd_stop'),
        'is_nat': n.get('is_nat'),
        'domain_name': n.get('domain_name'),
    }, ensure_ascii=False))
"

echo "=== USW PORT TABLE ==="
curl -sk -m 15 -b "$COOKIE_JAR" "https://${HOST}/proxy/network/api/s/default/stat/device" \
  | python3 -c "
import json,sys
d=json.load(sys.stdin)
for dev in d.get('data',[]):
    if dev.get('type') != 'usw':
        continue
    print('switch:', dev.get('name'), dev.get('model'), dev.get('ip'))
    for p in sorted(dev.get('port_table',[]), key=lambda x: x.get('port_idx',0)):
        print(json.dumps({
            'port': p.get('port_idx'),
            'name': p.get('name'),
            'enable': p.get('enable'),
            'up': p.get('up'),
            'speed': p.get('speed'),
            'mac': p.get('mac_table', [{}])[0].get('mac') if p.get('mac_table') else None,
            'native_networkconf_id': p.get('native_networkconf_id'),
            'portconf_id': p.get('portconf_id'),
        }, ensure_ascii=False))
"

echo "=== PORT PROFILES ==="
curl -sk -m 15 -b "$COOKIE_JAR" "https://${HOST}/proxy/network/api/s/default/rest/portconf" \
  | python3 -c "
import json,sys
d=json.load(sys.stdin)
for p in d.get('data',[]):
    print(json.dumps({
        '_id': p.get('_id'),
        'name': p.get('name'),
        'native_networkconf_id': p.get('native_networkconf_id'),
        'vlan': p.get('vlan'),
        'tagged_vlan_mgmt': p.get('tagged_vlan_mgmt'),
    }, ensure_ascii=False))
"
