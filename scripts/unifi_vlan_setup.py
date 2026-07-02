#!/usr/bin/env python3
"""Create Bifrost VLANs + apply USW port profile (phase 1: Server native VLAN 10).

Requires: UNIFI_HOST, UNIFI_USER, UNIFI_PASS
Usage:
  python3 scripts/unifi_vlan_setup.py audit
  python3 scripts/unifi_vlan_setup.py apply
  python3 scripts/unifi_vlan_setup.py apply --dry-run
"""
from __future__ import annotations

import json
import os
import sys
import urllib.request
import ssl

HOST = os.environ.get("UNIFI_HOST", "192.168.1.1")
USER = os.environ.get("UNIFI_USER", "")
PASS = os.environ.get("UNIFI_PASS", "")
SITE = "default"
CTX = ssl.create_default_context()
CTX.check_hostname = False
CTX.verify_mode = ssl.CERT_NONE

VLANS = [
    {
        "name": "Server",
        "vlan": 10,
        "ip_subnet": "192.168.10.1/24",
        "domain_name": "bifrost.lan",
        "dhcpd_enabled": True,
        "dhcpd_start": "192.168.10.200",
        "dhcpd_stop": "192.168.10.250",
        "note": "Migration pool .200-.250; assign static .10.x per topology after cutover",
    },
    {
        "name": "Work",
        "vlan": 20,
        "ip_subnet": "192.168.20.1/24",
        "domain_name": "bifrost.lan",
        "dhcpd_enabled": True,
        "dhcpd_start": "192.168.20.6",
        "dhcpd_stop": "192.168.20.254",
        "note": "SSID Bifrost (later)",
    },
    {
        "name": "Home",
        "vlan": 50,
        "ip_subnet": "192.168.50.1/24",
        "domain_name": "bifrost.lan",
        "dhcpd_enabled": True,
        "dhcpd_start": "192.168.50.6",
        "dhcpd_stop": "192.168.50.254",
        "note": "SSID Home / IoT (later)",
    },
]

# Server infrastructure — native VLAN 10 (phase 1). Mac P09 + MoCA P13-15 stay Default.
SERVER_PORTS = [2, 3, 4, 5, 6, 7, 8, 17, 21, 22, 23]
PORT_PROFILE_NAME = "Native VLAN 10 Server"


class UniFi:
    def __init__(self) -> None:
        if not USER or not PASS:
            raise SystemExit("Set UNIFI_USER and UNIFI_PASS")
        self.cookie: str | None = None
        self.csrf: str | None = None

    def _headers(self) -> dict[str, str]:
        headers = {"Content-Type": "application/json"}
        if self.cookie:
            headers["Cookie"] = self.cookie
        if self.csrf:
            headers["X-CSRF-Token"] = self.csrf
        return headers

    def _req(self, method: str, path: str, body: dict | None = None) -> dict:
        url = f"https://{HOST}{path}"
        data = None if body is None else json.dumps(body).encode()
        req = urllib.request.Request(url, data=data, headers=self._headers(), method=method)
        with urllib.request.urlopen(req, context=CTX, timeout=30) as resp:
            raw = resp.read().decode()
            if method == "POST" and path.endswith("/login"):
                self.cookie = resp.headers.get("Set-Cookie", "").split(";")[0]
                self.csrf = resp.headers.get("X-Csrf-Token") or resp.headers.get("x-csrf-token")
            updated = resp.headers.get("x-updated-csrf-token")
            if updated:
                self.csrf = updated
            return json.loads(raw) if raw else {}

    def login(self) -> None:
        self._req(
            "POST",
            "/api/auth/login",
            {"username": USER, "password": PASS},
        )

    def get(self, path: str) -> dict:
        return self._req("GET", f"/proxy/network/api/s/{SITE}{path}")

    def post(self, path: str, body: dict) -> dict:
        return self._req("POST", f"/proxy/network/api/s/{SITE}{path}", body)

    def put(self, path: str, body: dict) -> dict:
        return self._req("PUT", f"/proxy/network/api/s/{SITE}{path}", body)


def network_payload(spec: dict) -> dict:
    return {
        "name": spec["name"],
        "purpose": "corporate",
        "vlan_enabled": True,
        "vlan": spec["vlan"],
        "ip_subnet": spec["ip_subnet"],
        "domain_name": spec["domain_name"],
        "networkgroup": "LAN",
        "is_nat": True,
        "mdns_enabled": True,
        "dhcpd_enabled": spec["dhcpd_enabled"],
        "dhcpd_start": spec.get("dhcpd_start"),
        "dhcpd_stop": spec.get("dhcpd_stop"),
        "igmp_snooping": False,
    }


def audit(api: UniFi) -> None:
    nets = api.get("/rest/networkconf").get("data", [])
    profiles = api.get("/rest/portconf").get("data", [])
    devices = api.get("/stat/device").get("data", [])
    print("=== NETWORKS ===")
    for n in nets:
        if n.get("purpose") != "corporate":
            continue
        print(
            f"  {n.get('name'):8} vlan={n.get('vlan')} "
            f"subnet={n.get('ip_subnet')} dhcp={n.get('dhcpd_enabled')}"
        )
    print("=== PORT PROFILES ===")
    for p in profiles:
        print(f"  {p.get('name')} id={p.get('_id')}")
    sw = next((d for d in devices if d.get("type") == "usw"), None)
    if sw:
        overrides = {o["port_idx"]: o.get("portconf_id") for o in sw.get("port_overrides", [])}
        print(f"=== SWITCH {sw.get('name')} overrides ===")
        for port in SERVER_PORTS:
            print(f"  P{port:02d} portconf={overrides.get(port, '(auto/default)')}")


def ensure_networks(api: UniFi, dry_run: bool) -> dict[int, str]:
    existing = api.get("/rest/networkconf").get("data", [])
    by_vlan: dict[int, dict] = {}
    for n in existing:
        if n.get("vlan_enabled") and n.get("vlan") is not None:
            by_vlan[int(n["vlan"])] = n

    ids: dict[int, str] = {}
    for spec in VLANS:
        vid = spec["vlan"]
        if vid in by_vlan:
            ids[vid] = by_vlan[vid]["_id"]
            print(f"OK  VLAN {vid} ({spec['name']}) already exists id={ids[vid]}")
            continue
        payload = network_payload(spec)
        print(f"CREATE VLAN {vid} ({spec['name']}) -> {spec['ip_subnet']}")
        if dry_run:
            ids[vid] = f"dry-run-vlan-{vid}"
            continue
        resp = api.post("/rest/networkconf", payload)
        if resp.get("meta", {}).get("rc") != "ok":
            raise SystemExit(f"Failed VLAN {vid}: {resp}")
        ids[vid] = resp["data"][0]["_id"]
        print(f"  created id={ids[vid]}")
    return ids


def ensure_port_profile(api: UniFi, network_id: str, dry_run: bool) -> str:
    profiles = api.get("/rest/portconf").get("data", [])
    for p in profiles:
        if p.get("name") == PORT_PROFILE_NAME:
            print(f"OK  port profile exists id={p['_id']}")
            return p["_id"]
    print(f"CREATE port profile {PORT_PROFILE_NAME}")
    if dry_run:
        return "dry-run-portconf"
    payload = {
        "name": PORT_PROFILE_NAME,
        "native_networkconf_id": network_id,
        "setting_preference": "manual",
        "op_mode": "switch",
    }
    resp = api.post("/rest/portconf", payload)
    if resp.get("meta", {}).get("rc") != "ok":
        raise SystemExit(f"Failed port profile: {resp}")
    pid = resp["data"][0]["_id"]
    print(f"  created id={pid}")
    return pid


def get_switch(api: UniFi) -> dict:
    devices = api.get("/stat/device").get("data", [])
    sw = next((d for d in devices if d.get("type") == "usw"), None)
    if not sw:
        raise SystemExit("No USW found")
    return sw


def get_port_profile_id(api: UniFi) -> str:
    for p in api.get("/rest/portconf").get("data", []):
        if p.get("name") == PORT_PROFILE_NAME:
            return p["_id"]
    raise SystemExit(f"Port profile {PORT_PROFILE_NAME} not found — run apply first")


def get_vlan10_network_id(api: UniFi) -> str:
    for n in api.get("/rest/networkconf").get("data", []):
        if n.get("vlan_enabled") and int(n.get("vlan", 0)) == 10:
            return n["_id"]
    raise SystemExit("VLAN 10 network not found")


def apply_server_ports(api: UniFi, portconf_id: str, dry_run: bool) -> None:
    sw = get_switch(api)
    device_id = sw["_id"]
    existing = {o["port_idx"]: o for o in sw.get("port_overrides", [])}
    for port in SERVER_PORTS:
        existing[port] = {
            "port_idx": port,
            "portconf_id": portconf_id,
        }
    overrides = [existing[k] for k in sorted(existing)]
    print(f"APPLY port profile to P{SERVER_PORTS} on {sw.get('name')}")
    if dry_run:
        return
    resp = api.put(
        f"/rest/device/{device_id}",
        {
            "port_overrides": overrides,
            "override_inform_host": sw.get("override_inform_host", False),
        },
    )
    if resp.get("meta", {}).get("rc") != "ok":
        raise SystemExit(f"Failed port overrides: {resp}")
    print("  port overrides applied — server links on VLAN 10")


def revert_server_ports(api: UniFi, dry_run: bool) -> None:
    sw = get_switch(api)
    device_id = sw["_id"]
    server_set = set(SERVER_PORTS)
    overrides = [o for o in sw.get("port_overrides", []) if o.get("port_idx") not in server_set]
    print(f"REVERT server ports P{SERVER_PORTS} to Default on {sw.get('name')}")
    if dry_run:
        return
    resp = api.put(
        f"/rest/device/{device_id}",
        {
            "port_overrides": overrides,
            "override_inform_host": sw.get("override_inform_host", False),
        },
    )
    if resp.get("meta", {}).get("rc") != "ok":
        raise SystemExit(f"Failed revert port overrides: {resp}")
    print("  server ports reverted to Default VLAN")


def main() -> None:
    cmd = sys.argv[1] if len(sys.argv) > 1 else "audit"
    dry_run = "--dry-run" in sys.argv
    api = UniFi()
    api.login()
    if cmd == "audit":
        audit(api)
        return
    if cmd == "apply":
        ids = ensure_networks(api, dry_run)
        portconf = ensure_port_profile(api, ids[10], dry_run)
        apply_server_ports(api, portconf, dry_run)
        print()
        audit(api)
        print()
        print("NEXT: run scripts/restore_server_vlan10_ips.sh to push static 192.168.10.x")
        return
    if cmd == "revert-ports":
        revert_server_ports(api, dry_run)
        return
    if cmd == "apply-ports-only":
        portconf = get_port_profile_id(api)
        apply_server_ports(api, portconf, dry_run)
        return
    raise SystemExit(f"Unknown command: {cmd}")


if __name__ == "__main__":
    main()
