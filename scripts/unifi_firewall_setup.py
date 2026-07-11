#!/usr/bin/env python3
"""Apply Bifrost VLAN firewall zones + policies on UCG Max (Network 10.x ZBF).

UCG Max 10.4+ uses Zone-Based Firewall. Legacy /rest/firewallrule is not writable.
Integration API keys on some UCG builds omit site UUID — session + v2 API is primary.

Requires:
  UNIFI_HOST, UNIFI_USER, UNIFI_PASS  — bifrost-agent (Super Admin, local)

Optional:
  UNIFI_API_KEY  — Integration API audit only when /sites returns site id

Usage:
  python3 scripts/unifi_firewall_setup.py audit
  python3 scripts/unifi_firewall_setup.py apply
  python3 scripts/unifi_firewall_setup.py apply --include-default-deny
  python3 scripts/unifi_firewall_setup.py remove
  python3 scripts/unifi_firewall_setup.py open-server
  python3 scripts/unifi_firewall_setup.py cleanup-probes
"""
from __future__ import annotations

import json
import os
import sys
import time
import urllib.error
import urllib.request
import ssl
from dataclasses import dataclass
from typing import Any

HOST = os.environ.get("UNIFI_HOST", "192.168.1.1")
USER = os.environ.get("UNIFI_USER", "")
PASS = os.environ.get("UNIFI_PASS", "")
API_KEY = os.environ.get("UNIFI_API_KEY", "")
SITE = "default"
# Canonical UGREEN-NAS fixed IP on Server VLAN (UniFi reservation on MAC …:4e:25).
NAS_IP = os.environ.get("NAS_IP", "192.168.10.20")
PLEX_PORTS = "32400"
SMB_PORTS = "445,139"
WRITE_DELAY_S = 0.35
CTX = ssl.create_default_context()
CTX.check_hostname = False
CTX.verify_mode = ssl.CERT_NONE

ZONE_SPECS = [
    ("Bifrost Server", "Server"),
    ("Bifrost Work", "Work"),
    ("Bifrost Family", "Family"),
    ("Bifrost IoT", "Home"),
    ("Bifrost Default", "Default"),
]

# Create order matters on UniFi ZBF: lower index = higher priority.
# Specific ALLOW (e.g. Family→NAS) MUST be created before zone REJECT (Family→Server).
POLICY_SPECS: list[dict[str, Any]] = [
    {
        "name": "Bifrost | ALLOW Work → Server",
        "action": "ALLOW",
        "src": "Work",
        "dst": "Server",
        "note": "Admin → K3s + kube-vip + NAS",
    },
    {
        "name": "Bifrost | ALLOW Family → NAS",
        "action": "ALLOW",
        "src": "Family",
        "dst_ip": NAS_IP,
        # Full NAS access (SMB/Plex/web UI/etc.) — not Server-wide.
        "dst_ports": "*",
        "note": "Family SSID → UGREEN-NAS only (must outrank REJECT Family→Server)",
    },
    {
        "name": "Bifrost | ALLOW IoT → NAS Plex",
        "action": "ALLOW",
        "src": "Home",
        "dst_ip": NAS_IP,
        "dst_ports": PLEX_PORTS,
        "note": "Home theater streaming from NAS (must outrank REJECT IoT→Server)",
    },
    {
        "name": "Bifrost | ALLOW Server → IoT",
        "action": "ALLOW",
        "src": "Server",
        "dst": "Home",
        "note": "Home Assistant / automation hub control",
    },
    {
        "name": "Bifrost | REJECT Family → Server",
        "action": "REJECT",
        "src": "Family",
        "dst": "Server",
        "note": "Family devices cannot reach servers (except NAS allow above)",
    },
    {
        "name": "Bifrost | REJECT IoT → Server",
        "action": "REJECT",
        "src": "Home",
        "dst": "Server",
        "note": "IoT cannot reach servers (except Plex allow above)",
    },
    {
        "name": "Bifrost | REJECT IoT → Family",
        "action": "REJECT",
        "src": "Home",
        "dst": "Family",
        "note": "IoT cannot reach family devices",
    },
    {
        "name": "Bifrost | REJECT Family → IoT",
        "action": "REJECT",
        "src": "Family",
        "dst": "Home",
        "note": "Family cannot lateral-scan IoT",
    },
]

DEFAULT_DENY_POLICY = {
    "name": "Bifrost | REJECT Default → Server (transition)",
    "action": "REJECT",
    "src": "Default",
    "dst": "Server",
    "note": "Hide Server from Eero/Default until decommission",
}

# Temporary cross-zone allow while AP/Eero cutover is pending (overrides ZBF Block All Traffic matrix).
TEMP_OPEN_SERVER_SPECS: list[dict[str, Any]] = [
    {
        "name": "Bifrost | TEMP ALLOW Default → Server",
        "action": "ALLOW",
        "src": "Default",
        "dst": "Server",
        "note": "Eero WiFi + Default LAN → TWS/K3s until UniFi AP rollout",
    },
    {
        "name": "Bifrost | TEMP ALLOW Work → Server",
        "action": "ALLOW",
        "src": "Work",
        "dst": "Server",
        "note": "Admin SSID → Server",
    },
    {
        "name": "Bifrost | TEMP ALLOW Family → Server",
        "action": "ALLOW",
        "src": "Family",
        "dst": "Server",
        "note": "Family SSID → Server (temporary)",
    },
    {
        "name": "Bifrost | TEMP ALLOW IoT → Server",
        "action": "ALLOW",
        "src": "Home",
        "dst": "Server",
        "note": "vision/IoT SSID → Server (temporary)",
    },
]

PROBE_NAMES = {
    "Bifrost | TEST Allow Work → Server",
    "Bifrost | PROBE reject",
    "Bifrost | PROBE Family NAS",
}


class UniFiSession:
    def __init__(self) -> None:
        if not USER or not PASS:
            raise SystemExit("Set UNIFI_USER and UNIFI_PASS (bifrost-agent local Super Admin).")
        self.cookie: str | None = None
        self.csrf: str | None = None

    def _headers(self, *, integration: bool = False) -> dict[str, str]:
        headers = {"Content-Type": "application/json"}
        if integration:
            if not API_KEY:
                raise SystemExit("UNIFI_API_KEY not set.")
            headers["X-API-KEY"] = API_KEY
        else:
            if self.cookie:
                headers["Cookie"] = self.cookie
            if self.csrf:
                headers["X-CSRF-Token"] = self.csrf
        return headers

    def request(
        self,
        method: str,
        path: str,
        body: dict | None = None,
        *,
        integration: bool = False,
    ) -> Any:
        url = f"https://{HOST}{path}"
        data = None if body is None else json.dumps(body).encode()
        req = urllib.request.Request(
            url, data=data, headers=self._headers(integration=integration), method=method
        )
        try:
            with urllib.request.urlopen(req, context=CTX, timeout=45) as resp:
                raw = resp.read().decode()
                if path.endswith("/login"):
                    self.cookie = resp.headers.get("Set-Cookie", "").split(";")[0]
                    self.csrf = resp.headers.get("X-Csrf-Token") or resp.headers.get(
                        "x-csrf-token"
                    )
                updated = resp.headers.get("x-updated-csrf-token")
                if updated:
                    self.csrf = updated
                if not raw:
                    return {}
                try:
                    return json.loads(raw)
                except json.JSONDecodeError:
                    return {"raw": raw}
        except urllib.error.HTTPError as e:
            detail = e.read().decode()
            raise RuntimeError(f"{method} {path} -> HTTP {e.code}: {detail[:900]}") from e

    def login(self) -> None:
        self.request(
            "POST",
            "/api/auth/login",
            {"username": USER, "password": PASS},
        )

    def legacy_get(self, path: str) -> dict:
        return self.request("GET", f"/proxy/network/api/s/{SITE}{path}")

    def v2_get(self, path: str) -> Any:
        return self.request("GET", f"/proxy/network/v2/api/site/{SITE}{path}")

    def v2_write(self, method: str, path: str, body: dict | None = None) -> Any:
        result = self.request("POST" if method == "POST" else method, f"/proxy/network/v2/api/site/{SITE}{path}", body)
        time.sleep(WRITE_DELAY_S)
        return result

    def v2_post(self, path: str, body: dict) -> Any:
        return self.v2_write("POST", path, body)

    def v2_delete(self, path: str) -> None:
        self.v2_write("DELETE", path, None)

    def v2_delete_optional(self, path: str) -> bool:
        """Delete v2 resource; return False if already gone (404)."""
        try:
            self.v2_write("DELETE", path, None)
            return True
        except RuntimeError as e:
            if "HTTP 404" in str(e) or "NotFound" in str(e):
                return False
            raise


@dataclass
class NetworkRef:
    name: str
    mongo_id: str
    uuid: str
    vlan: int | None


def load_networks(api: UniFiSession) -> dict[str, NetworkRef]:
    data = api.legacy_get("/rest/networkconf").get("data", [])
    out: dict[str, NetworkRef] = {}
    for n in data:
        if n.get("purpose") != "corporate":
            continue
        name = n.get("name", "")
        out[name] = NetworkRef(
            name=name,
            mongo_id=n["_id"],
            uuid=n.get("external_id") or "",
            vlan=n.get("vlan"),
        )
    return out


def list_v2_zones(api: UniFiSession) -> list[dict]:
    resp = api.v2_get("/firewall/zone")
    return resp if isinstance(resp, list) else []


def list_v2_policies(api: UniFiSession) -> list[dict]:
    resp = api.v2_get("/firewall-policies")
    return resp if isinstance(resp, list) else []


def zone_name_for_network(net_name: str) -> str:
    for zname, nname in ZONE_SPECS:
        if nname == net_name:
            return zname
    raise KeyError(net_name)


def find_zone_for_spec(zones: list[dict], zone_name: str) -> dict | None:
    exact = [z for z in zones if z.get("name") == zone_name]
    if exact:
        return exact[0]
    # Legacy typo from earlier manual setup
    if zone_name == "Bifrost Family":
        legacy = [z for z in zones if str(z.get("name", "")).startswith("Bifrost Family")]
        if legacy:
            return legacy[0]
    return None


def ensure_v2_zone(
    api: UniFiSession,
    zones: list[dict],
    zone_name: str,
    network: NetworkRef,
) -> str:
    zone = find_zone_for_spec(zones, zone_name)
    net_id = network.mongo_id
    if zone and zone.get("network_ids") == [net_id]:
        return zone["_id"]

    if zone:
        print(f"  ~ rebind zone {zone_name} (network {network.name})")
        api.v2_delete(f"/firewall/zone/{zone['_id']}")
        zones = [z for z in zones if z["_id"] != zone["_id"]]

    created = api.v2_post(
        "/firewall/zone",
        {"name": zone_name, "network_ids": [net_id]},
    )
    zone_id = created.get("_id")
    if not zone_id:
        raise RuntimeError(f"Zone create failed for {zone_name}: {created}")
    print(f"  + zone {zone_name} ({network.name} VLAN {network.vlan}) id={zone_id}")
    return zone_id


def v2_action(action: str) -> str:
    if action == "ALLOW":
        return "ALLOW"
    if action in ("REJECT", "BLOCK"):
        return "BLOCK"
    raise ValueError(action)


def endpoint_any(zone_id: str) -> dict[str, Any]:
    return {
        "zone_id": zone_id,
        "matching_target": "ANY",
        "port_matching_type": "ANY",
        "match_opposite_ports": False,
    }


def build_v2_policy(spec: dict, zone_ids: dict[str, str]) -> dict[str, Any]:
    src_zone = zone_ids[zone_name_for_network(spec["src"])]
    action = v2_action(spec["action"])
    allow = action == "ALLOW"

    if "dst" in spec:
        dst_zone = zone_ids[zone_name_for_network(spec["dst"])]
        destination: dict[str, Any] = endpoint_any(dst_zone)
    else:
        dst_zone = zone_ids["Bifrost Server"]
        ports = spec.get("dst_ports", "")
        any_ports = ports in ("", "*", "any", None)
        destination = {
            "zone_id": dst_zone,
            "matching_target": "IP",
            "matching_target_type": "SPECIFIC",
            "ips": [spec["dst_ip"]],
            "port_matching_type": "ANY" if any_ports else "SPECIFIC",
            "match_opposite_ports": False,
            "match_opposite_ips": False,
        }
        if not any_ports:
            destination["port"] = ports

    return {
        "name": spec["name"],
        "enabled": True,
        "action": action,
        "ip_version": "IPV4",
        "protocol": "all",
        "source": endpoint_any(src_zone),
        "destination": destination,
        "schedule": {"mode": "ALWAYS"},
        "logging": False,
        "create_allow_respond": allow,
    }


def integration_sites_have_id(api: UniFiSession) -> bool:
    if not API_KEY:
        return False
    try:
        resp = api.request(
            "GET",
            "/proxy/network/integration/v1/sites",
            integration=True,
        )
        data = resp.get("data", []) if isinstance(resp, dict) else []
        return bool(data and data[0].get("id"))
    except RuntimeError:
        return False


def audit(api: UniFiSession) -> None:
    ver = api.legacy_get("/stat/sysinfo")["data"][0].get("version")
    print(f"Controller: {HOST}  Network {ver}")
    print(f"Auth: session ({USER})  Integration key usable: {integration_sites_have_id(api)}\n")

    networks = load_networks(api)
    print("Networks:")
    for name, ref in sorted(networks.items(), key=lambda x: (x[1].vlan or 0, x[0])):
        print(f"  {name:8} VLAN {ref.vlan or 1:>2}  mongo={ref.mongo_id}")

    zones = list_v2_zones(api)
    print(f"\nV2 firewall zones ({len(zones)}):")
    for z in zones:
        if z.get("name", "").startswith("Bifrost") or z.get("name") == "Internal":
            nets = z.get("network_ids") or []
            flag = "OK" if nets else "EMPTY"
            print(f"  [{flag:5}] {z.get('name')}  networks={len(nets)}  id={z.get('_id')}")

    policies = list_v2_policies(api)
    bifrost = [p for p in policies if str(p.get("name", "")).startswith("Bifrost |")]
    print(f"\nV2 Bifrost policies: {len(bifrost)}")
    for p in sorted(bifrost, key=lambda x: x.get("name", "")):
        print(f"  - {p.get('name')}  action={p.get('action')}")

    missing_zones = []
    for zone_name, net_name in ZONE_SPECS:
        z = find_zone_for_spec(zones, zone_name)
        net = networks.get(net_name)
        if not net:
            missing_zones.append(f"{zone_name}: network {net_name} missing")
            continue
        if not z or (z.get("network_ids") or []) != [net.mongo_id]:
            missing_zones.append(f"{zone_name}: not bound to {net_name}")

    if missing_zones:
        print("\nGaps (apply will fix):")
        for g in missing_zones:
            print(f"  ! {g}")
    else:
        print("\nZone bindings: complete")

    print("\nPlanned policies (apply order):")
    for i, spec in enumerate(POLICY_SPECS, 1):
        dst = spec.get("dst") or spec.get("dst_ip", "?")
        print(f"  {i:2}. [{spec['action']:6}] {spec['src']} → {dst}  — {spec.get('note','')}")
    print("  opt. [REJECT] Default → Server  (--include-default-deny)")


def apply(api: UniFiSession, *, include_default_deny: bool) -> None:
    networks = load_networks(api)
    for _, net_name in ZONE_SPECS:
        if net_name not in networks:
            raise SystemExit(f"Missing network: {net_name}")

    zones = list_v2_zones(api)
    zone_ids: dict[str, str] = {}
    print("Ensuring firewall zones (v2 session API)…")
    for zone_name, net_name in ZONE_SPECS:
        zone_ids[zone_name] = ensure_v2_zone(api, zones, zone_name, networks[net_name])
        zones = list_v2_zones(api)

    specs = list(POLICY_SPECS)
    if include_default_deny:
        specs.append(DEFAULT_DENY_POLICY)

    existing = {p.get("name") for p in list_v2_policies(api)}
    print("\nCreating firewall policies…")
    for spec in specs:
        if spec["name"] in existing:
            print(f"  = skip (exists) {spec['name']}")
            continue
        body = build_v2_policy(spec, zone_ids)
        created = api.v2_post("/firewall-policies", body)
        pid = created.get("_id") if isinstance(created, dict) else "?"
        print(f"  + {spec['name']} id={pid}")

    print("\nDone. Run `python3 scripts/unifi_firewall_setup.py audit` to review.")


def cleanup_probes(api: UniFiSession) -> None:
    policies = list_v2_policies(api)
    deleted = 0
    for p in policies:
        name = p.get("name", "")
        if name in PROBE_NAMES or name.startswith("Bifrost | PROBE"):
            api.v2_delete(f"/firewall-policies/{p['_id']}")
            print(f"  - deleted {name}")
            deleted += 1
    if not deleted:
        print("No probe/test policies found.")
    else:
        print(f"Removed {deleted} probe policy(ies).")


def remove_bifrost_policies(api: UniFiSession) -> None:
    """Delete all Bifrost ZBF policies — restores cross-VLAN routing until re-apply."""
    deleted_total = 0
    for _ in range(20):
        policies = list_v2_policies(api)
        bifrost = [p for p in policies if str(p.get("name", "")).startswith("Bifrost |")]
        if not bifrost:
            break
        if deleted_total == 0:
            print(f"Removing {len(bifrost)} Bifrost firewall policy(ies)…")
        for p in sorted(bifrost, key=lambda x: x.get("name", "")):
            name = p.get("name", "")
            pid = p.get("_id")
            if not pid:
                continue
            if api.v2_delete_optional(f"/firewall-policies/{pid}"):
                print(f"  - deleted {name}")
                deleted_total += 1
            else:
                print(f"  = skip (gone) {name}")
    if deleted_total == 0:
        print("No Bifrost firewall policies found (already open).")
        return
    print(f"\nRemoved {deleted_total} policy(ies). Cross-VLAN traffic is no longer blocked by Bifrost ZBF.")
    print("Re-enable later: python3 scripts/unifi_firewall_setup.py apply --include-default-deny")


def open_server_access(api: UniFiSession) -> None:
    """Add TEMP ALLOW policies so all VLAN zones can reach Server (overrides ZBF matrix blocks)."""
    networks = load_networks(api)
    zones = list_v2_zones(api)
    zone_ids: dict[str, str] = {}
    for zone_name, net_name in ZONE_SPECS:
        net = networks.get(net_name)
        if not net:
            raise SystemExit(f"Missing network: {net_name}")
        z = find_zone_for_spec(zones, zone_name)
        if not z:
            zone_ids[zone_name] = ensure_v2_zone(api, zones, zone_name, net)
            zones = list_v2_zones(api)
        else:
            zone_ids[zone_name] = z["_id"]

    existing = {p.get("name") for p in list_v2_policies(api)}
    print("Adding temporary Server access policies (override ZBF Block All Traffic matrix)…")
    for spec in TEMP_OPEN_SERVER_SPECS:
        if spec["name"] in existing:
            print(f"  = skip (exists) {spec['name']}")
            continue
        body = build_v2_policy(spec, zone_ids)
        created = api.v2_post("/firewall-policies", body)
        pid = created.get("_id") if isinstance(created, dict) else "?"
        print(f"  + {spec['name']} id={pid}")
    print("\nDone. Test: ping 192.168.10.30 from WiFi/Default LAN.")
    print("Re-lock later: remove TEMP policies + run apply --include-default-deny")


def main() -> None:
    cmd = sys.argv[1] if len(sys.argv) > 1 else "audit"
    include_default = "--include-default-deny" in sys.argv
    api = UniFiSession()
    api.login()
    if cmd == "audit":
        audit(api)
    elif cmd == "apply":
        apply(api, include_default_deny=include_default)
    elif cmd == "cleanup-probes":
        cleanup_probes(api)
    elif cmd == "remove":
        remove_bifrost_policies(api)
    elif cmd == "open-server":
        open_server_access(api)
    else:
        raise SystemExit(
            f"Unknown command: {cmd}  (audit | apply | remove | open-server | cleanup-probes)"
        )


if __name__ == "__main__":
    main()
