#!/usr/bin/env bash
# MoCA VLAN 802.1Q pass-through test — run AFTER hours (brief Eero WiFi blip on MoCA path).
#
# Prereq: AP not required. Tests whether MoCA adapters forward tagged frames for AP rollout.
# Safe rollback: revert P13-P15 to Default + restart-port.
#
# Usage (from bifrost-platform, with UNIFI_* set):
#   ./scripts/unifi_moca_vlan_test.sh audit
#   ./scripts/unifi_moca_vlan_test.sh apply-trunk   # P13-P15 → Trunk All LANs
#   ./scripts/unifi_moca_vlan_test.sh revert
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
UNIFI_HOST="${UNIFI_HOST:-192.168.1.1}"
UNIFI_USER="${UNIFI_USER:?UNIFI_USER}"
UNIFI_PASS="${UNIFI_PASS:?UNIFI_PASS}"
MOCA_PORTS=(13 14 15)
TRUNK_PROFILE="${TRUNK_PROFILE:-Trunk All LANs}"

python3 <<'PY' "${1:-audit}"
import json, os, ssl, sys, urllib.request
cmd = sys.argv[1]
HOST = os.environ["UNIFI_HOST"]
USER = os.environ["UNIFI_USER"]
PASS = os.environ["UNIFI_PASS"]
PORTS = [13, 14, 15]
TRUNK = os.environ.get("TRUNK_PROFILE", "Trunk All LANs")
ctx = ssl.create_default_context(); ctx.check_hostname=False; ctx.verify_mode=ssl.CERT_NONE

def login():
    req = urllib.request.Request(f"https://{HOST}/api/auth/login",
        json.dumps({"username":USER,"password":PASS}).encode(),
        {"Content-Type":"application/json"}, method="POST")
    with urllib.request.urlopen(req, context=ctx, timeout=15) as r:
        return r.headers.get("Set-Cookie","").split(";")[0], r.headers.get("X-Csrf-Token")

cookie, csrf = login()

def api(method, path, body=None):
    global csrf
    h={"Content-Type":"application/json","Cookie":cookie,"X-CSRF-Token":csrf}
    data=None if body is None else json.dumps(body).encode()
    req=urllib.request.Request(f"https://{HOST}{path}", data=data, headers=h, method=method)
    with urllib.request.urlopen(req, context=ctx, timeout=30) as r:
        csrf = r.headers.get("x-updated-csrf-token") or csrf
        return json.loads(r.read().decode())

profiles = {p["name"]: p["_id"] for p in api("GET","/proxy/network/api/s/default/rest/portconf")["data"]}
sw = next(d for d in api("GET","/proxy/network/api/s/default/stat/device")["data"] if d["type"]=="usw")

if cmd == "audit":
    print("MoCA ports:")
    for idx in PORTS:
        p = next(x for x in sw["port_table"] if x.get("port_idx")==idx)
        prof = next((n for n,i in profiles.items() if i==p.get("portconf_id")), "Default")
        print(f"  P{idx:02d} profile={prof} up={p.get('up')} speed={p.get('speed')}M")
    print(f"\nTrunk profile available: {TRUNK in profiles}")
    sys.exit(0)

if cmd == "apply-trunk":
    pid = profiles.get(TRUNK)
    if not pid:
        raise SystemExit(f"Missing profile: {TRUNK}")
    overrides = {o["port_idx"]: o for o in sw.get("port_overrides", [])}
    for port in PORTS:
        overrides[port] = {"port_idx": port, "portconf_id": pid}
    resp = api("PUT", f"/proxy/network/api/s/default/rest/device/{sw['_id']}",
               {"port_overrides": [overrides[k] for k in sorted(overrides)]})
    if resp.get("meta", {}).get("rc") != "ok":
        raise SystemExit(resp)
    for port in PORTS:
        api("POST","/proxy/network/api/s/default/cmd/devmgr",
            {"cmd":"restart-port","mac":sw["mac"],"port_idx":port})
        print(f"OK  P{port:02d} -> {TRUNK}")
    print("\nVerify: Eero still works; optional — connect test device expecting VLAN 20 DHCP after SSID enable.")
    sys.exit(0)

if cmd == "revert":
    overrides = [o for o in sw.get("port_overrides", []) if o.get("port_idx") not in PORTS]
    resp = api("PUT", f"/proxy/network/api/s/default/rest/device/{sw['_id']}", {"port_overrides": overrides})
    if resp.get("meta", {}).get("rc") != "ok":
        raise SystemExit(resp)
    for port in PORTS:
        api("POST","/proxy/network/api/s/default/cmd/devmgr",
            {"cmd":"restart-port","mac":sw["mac"],"port_idx":port})
        print(f"OK  P{port:02d} reverted to Default")
    sys.exit(0)

raise SystemExit(f"Unknown: {cmd}")
PY
