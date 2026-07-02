#!/usr/bin/env bash
# Read-only UniFi probe — uses UNIFI_HOST, UNIFI_USER, UNIFI_PASS from environment.
set -euo pipefail

HOST="${UNIFI_HOST:-192.168.1.1}"
USER="${UNIFI_USER:?UNIFI_USER not set}"
PASS="${UNIFI_PASS:?UNIFI_PASS not set}"

COOKIE_JAR="$(mktemp)"
trap 'rm -f "$COOKIE_JAR"' EXIT

login_resp="$(curl -sk -m 15 -c "$COOKIE_JAR" -X POST "https://${HOST}/api/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"${USER}\",\"password\":\"${PASS}\"}")"

if echo "$login_resp" | grep -q '"error"'; then
  echo "LOGIN_FAILED"
  echo "$login_resp" | python3 -m json.tool 2>/dev/null || echo "$login_resp"
  exit 1
fi

echo "LOGIN_OK"

system="$(curl -sk -m 10 "https://${HOST}/api/system")"
echo "=== SYSTEM ==="
echo "$system" | python3 -m json.tool 2>/dev/null | head -40

devices="$(curl -sk -m 15 -b "$COOKIE_JAR" "https://${HOST}/proxy/network/api/s/default/stat/device")"
echo "=== DEVICES ==="
echo "$devices" | python3 -c "
import json, sys
raw = sys.stdin.read()
try:
    data = json.loads(raw)
except json.JSONDecodeError:
    print(raw[:800])
    sys.exit(0)
items = data.get('data', data if isinstance(data, list) else [])
if not isinstance(items, list):
    print(json.dumps(data, indent=2)[:1200])
    sys.exit(0)
print(f'count={len(items)}')
for d in items:
    print(json.dumps({
        'name': d.get('name'),
        'model': d.get('model'),
        'type': d.get('type'),
        'mac': d.get('mac'),
        'ip': d.get('ip'),
        'state': d.get('state'),
        'adopted': d.get('adopted'),
        'version': d.get('version'),
    }, ensure_ascii=False))
"

sites="$(curl -sk -m 10 -b "$COOKIE_JAR" "https://${HOST}/proxy/network/api/s/default/stat/health")"
echo "=== HEALTH ==="
echo "$sites" | python3 -m json.tool 2>/dev/null | head -30
