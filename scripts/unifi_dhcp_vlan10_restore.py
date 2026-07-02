#!/usr/bin/env python3
"""UniFi fixed DHCP + VLAN10 port apply + link bounce for server migration."""
from __future__ import annotations

import json
import os
import ssl
import sys
import time
import subprocess
import urllib.request

HOST = os.environ.get("UNIFI_HOST", "192.168.1.1")
USER = os.environ.get("UNIFI_USER", "")
PASS = os.environ.get("UNIFI_PASS", "")
SITE = "default"
CTX = ssl.create_default_context()
CTX.check_hostname = False
CTX.verify_mode = ssl.CERT_NONE

SERVER_NODES = [
    ("ubt-k3s-01", "68:1d:ef:58:d0:5b", "192.168.10.73", 17),
    ("ubt-k3s-02", "1c:83:41:33:37:58", "192.168.10.70", 23),
    ("ubt-k3s-04", "68:1d:ef:58:d1:29", "192.168.10.75", 2),
    ("ubt-k3s-05", "68:1d:ef:60:7c:07", "192.168.10.77", 3),
    ("ubt-k3s-06", "68:1d:ef:53:ce:ef", "192.168.10.79", 4),
    ("ubt-ai-4090", "c8:7f:54:5b:b8:33", "192.168.10.60", 22),
    ("UGREEN-NAS", "6c:1f:f7:ac:4e:25", "192.168.10.20", 21),
    ("ops-mac-agent-02", "1c:f6:4c:56:76:63", "192.168.10.50", 5),
    ("ops-mac-agent-01", "1c:f6:4c:30:c9:27", "192.168.10.52", 6),
    ("TWS-Host", "68:1d:ef:58:d5:24", "192.168.10.30", 7),
    ("TWS-Sec", "68:1d:ef:60:83:31", "192.168.10.32", 8),
]

PORT_PROFILE_NAME = "Native VLAN 10 Server"
SERVER_PORTS = [2, 3, 4, 5, 6, 7, 8, 17, 21, 22, 23]


class UniFi:
    def __init__(self) -> None:
        if not USER or not PASS:
            raise SystemExit("Set UNIFI_USER and UNIFI_PASS")
        self.cookie: str | None = None
        self.csrf: str | None = None

    def _headers(self) -> dict[str, str]:
        h = {"Content-Type": "application/json"}
        if self.cookie:
            h["Cookie"] = self.cookie
        if self.csrf:
            h["X-CSRF-Token"] = self.csrf
        return h

    def _req(self, method: str, path: str, body: dict | list | None = None) -> dict:
        url = f"https://{HOST}{path}"
        data = None if body is None else json.dumps(body).encode()
        req = urllib.request.Request(url, data=data, headers=self._headers(), method=method)
        with urllib.request.urlopen(req, context=CTX, timeout=45) as resp:
            raw = resp.read().decode()
            if method == "POST" and path.endswith("/login"):
                self.cookie = resp.headers.get("Set-Cookie", "").split(";")[0]
                self.csrf = resp.headers.get("X-Csrf-Token") or resp.headers.get("x-csrf-token")
            updated = resp.headers.get("x-updated-csrf-token")
            if updated:
                self.csrf = updated
            return json.loads(raw) if raw else {}

    def login(self) -> None:
        self._req("POST", "/api/auth/login", {"username": USER, "password": PASS})

    def get(self, path: str) -> dict:
        return self._req("GET", f"/proxy/network/api/s/{SITE}{path}")

    def put(self, path: str, body: dict) -> dict:
        return self._req("PUT", f"/proxy/network/api/s/{SITE}{path}", body)

    def post(self, path: str, body: dict | list) -> dict:
        return self._req("POST", f"/proxy/network/api/s/{SITE}{path}", body)


def vlan10_id(api: UniFi) -> str:
    for n in api.get("/rest/networkconf").get("data", []):
        if n.get("vlan_enabled") and int(n.get("vlan", 0)) == 10:
            return n["_id"]
    raise SystemExit("VLAN 10 network missing")


def port_profile_id(api: UniFi) -> str:
    for p in api.get("/rest/portconf").get("data", []):
        if p.get("name") == PORT_PROFILE_NAME:
            return p["_id"]
    raise SystemExit(f"Missing port profile {PORT_PROFILE_NAME}")


def expand_vlan10_dhcp(api: UniFi, net_id: str) -> None:
    resp = api.put(
        f"/rest/networkconf/{net_id}",
        {
            "dhcpd_enabled": True,
            "dhcpd_start": "192.168.10.6",
            "dhcpd_stop": "192.168.10.254",
        },
    )
    if resp.get("meta", {}).get("rc") != "ok":
        raise SystemExit(f"DHCP update failed: {resp}")
    print("OK  VLAN 10 DHCP pool 192.168.10.6-254")


def ensure_fixed_ips(api: UniFi, net_id: str) -> None:
    users = {u.get("mac", "").lower(): u for u in api.get("/rest/user").get("data", [])}
    for name, mac, ip, _port in SERVER_NODES:
        body = {
            "name": name,
            "mac": mac,
            "use_fixedip": True,
            "fixed_ip": ip,
            "network_id": net_id,
        }
        existing = users.get(mac.lower())
        if existing:
            resp = api.put(f"/rest/user/{existing['_id']}", body)
        else:
            resp = api.post("/rest/user", body)
        if resp.get("meta", {}).get("rc") != "ok":
            raise SystemExit(f"Fixed IP failed for {name}: {resp}")
        print(f"OK  DHCP fixed {name} {mac} -> {ip}")


def apply_vlan10_ports(api: UniFi, pid: str) -> None:
    sw = next(d for d in api.get("/stat/device").get("data", []) if d.get("type") == "usw")
    overrides = {o["port_idx"]: o for o in sw.get("port_overrides", [])}
    for port in SERVER_PORTS:
        overrides[port] = {"port_idx": port, "portconf_id": pid}
    resp = api.put(
        f"/rest/device/{sw['_id']}",
        {"port_overrides": [overrides[k] for k in sorted(overrides)]},
    )
    if resp.get("meta", {}).get("rc") != "ok":
        raise SystemExit(f"Port apply failed: {resp}")
    print(f"OK  Applied VLAN 10 profile to P{SERVER_PORTS}")


def bounce_ports(api: UniFi) -> None:
    sw = next(d for d in api.get("/stat/device").get("data", []) if d.get("type") == "usw")
    sw_mac = sw["mac"]
    for port in SERVER_PORTS:
        api.post("/cmd/devmgr", {"cmd": "restart-port", "mac": sw_mac, "port_idx": port})
        print(f"OK  restart-port P{port:02d}")
        time.sleep(2)


def verify_pings() -> None:
    print("=== Ping verification ===")
    ok = 0
    for name, _mac, ip, _port in SERVER_NODES:
        r = subprocess.run(["ping", "-c", "1", "-W", "2", ip], capture_output=True)
        status = "OK" if r.returncode == 0 else "FAIL"
        if r.returncode == 0:
            ok += 1
        print(f"  {status:4} {name:18} {ip}")
    print(f"Reachable: {ok}/{len(SERVER_NODES)}")


def main() -> None:
    api = UniFi()
    api.login()
    net_id = vlan10_id(api)
    expand_vlan10_dhcp(api, net_id)
    ensure_fixed_ips(api, net_id)
    apply_vlan10_ports(api, port_profile_id(api))
    print("Waiting 8s for VLAN tagging...")
    time.sleep(8)
    bounce_ports(api)
    print("Waiting 25s for DHCP renew...")
    time.sleep(25)
    verify_pings()


if __name__ == "__main__":
    main()
