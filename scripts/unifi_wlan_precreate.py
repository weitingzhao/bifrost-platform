"""Pre-create Bifrost WiFi SSIDs on UniFi (disabled until AP adoption).

Requires: UNIFI_HOST, UNIFI_USER, UNIFI_PASS
Optional: BIFROST_WIFI_PASS — WPA2 passphrase (or set password in UniFi Portal after create)

Usage:
  python3 scripts/unifi_wlan_precreate.py audit
  python3 scripts/unifi_wlan_precreate.py apply
  python3 scripts/unifi_wlan_precreate.py apply --enable   # only after AP ready + password set
"""
from __future__ import annotations

import json
import os
import ssl
import sys
import urllib.error
import urllib.request

HOST = os.environ.get("UNIFI_HOST", "192.168.1.1")
USER = os.environ.get("UNIFI_USER", "")
PASS = os.environ.get("UNIFI_PASS", "")
WIFI_PASS = os.environ.get("BIFROST_WIFI_PASS", "CHANGE-ME-set-in-unifi-portal")
SITE = "default"
CTX = ssl.create_default_context()
CTX.check_hostname = False
CTX.verify_mode = ssl.CERT_NONE

SSID_SPECS = [
    ("Bifrost", 20, "Admin — K3s/Ops access"),
    ("Family", 30, "Family devices — no Server"),
    # Exact Eero SSID spelling (case-sensitive) so IoT auto-reconnects on cutover.
    ("vision", 50, "IoT — Ring/Echo/sensors (same name+pass as Eero)"),
]


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

    def call(self, method: str, path: str, body: dict | None = None) -> dict | list:
        url = f"https://{HOST}{path}"
        data = None if body is None else json.dumps(body).encode()
        req = urllib.request.Request(url, data=data, headers=self._headers(), method=method)
        try:
            with urllib.request.urlopen(req, context=CTX, timeout=45) as resp:
                raw = resp.read().decode()
                if method == "POST" and path.endswith("/login"):
                    self.cookie = resp.headers.get("Set-Cookie", "").split(";")[0]
                    self.csrf = resp.headers.get("X-Csrf-Token") or resp.headers.get("x-csrf-token")
                updated = resp.headers.get("x-updated-csrf-token")
                if updated:
                    self.csrf = updated
                return json.loads(raw) if raw else {}
        except urllib.error.HTTPError as e:
            detail = e.read().decode()[:900]
            raise SystemExit(f"HTTP {e.code} {method} {path}: {detail}") from e

    def login(self) -> None:
        self.call("POST", "/api/auth/login", {"username": USER, "password": PASS})

    def get(self, path: str) -> dict:
        return self.call("GET", f"/proxy/network/api/s/{SITE}{path}")  # type: ignore[return-value]

    def v2_get(self, path: str) -> list | dict:
        return self.call("GET", f"/proxy/network/v2/api/site/{SITE}{path}")

    def post(self, path: str, body: dict) -> dict:
        return self.call("POST", f"/proxy/network/api/s/{SITE}{path}", body)  # type: ignore[return-value]

    def put(self, path: str, body: dict) -> dict:
        return self.call("PUT", f"/proxy/network/api/s/{SITE}{path}", body)  # type: ignore[return-value]


def vlan_network_ids(api: UniFi) -> dict[int, str]:
    out: dict[int, str] = {}
    for n in api.get("/rest/networkconf").get("data", []):
        if n.get("vlan_enabled") and n.get("vlan") is not None:
            out[int(n["vlan"])] = n["_id"]
    for _name, vid, _note in SSID_SPECS:
        if vid not in out:
            raise SystemExit(f"VLAN {vid} missing on controller — run VLAN setup first")
    return out


def default_ap_group_id(api: UniFi) -> str:
    """UCG Network 10.x exposes AP groups on v2; required for wlanconf create."""
    groups = api.v2_get("/apgroups")
    if not isinstance(groups, list):
        raise SystemExit(f"Unexpected apgroups payload: {groups}")
    for g in groups:
        if g.get("attr_hidden_id") == "default" or g.get("name") == "All APs":
            return g["_id"]
    if groups:
        return groups[0]["_id"]
    raise SystemExit("No AP groups found — adopt at least one AP first")


def wlan_payload(
    name: str,
    net_id: str,
    usergroup_id: str,
    wlangroup_id: str,
    ap_group_id: str,
    enabled: bool,
) -> dict:
    return {
        "name": name,
        "enabled": enabled,
        "hide_ssid": False,
        "security": "wpapsk",
        "wpa_mode": "wpa2",
        "wpa_enc": "ccmp",
        "x_passphrase": WIFI_PASS,
        "networkconf_id": net_id,
        "usergroup_id": usergroup_id,
        "wlangroup_id": wlangroup_id,
        "ap_group_ids": [ap_group_id],
        "wlan_band": "both",
        "mac_filter_enabled": False,
        "is_guest": False,
        "schedule": [],
        "pmf_mode": "optional",
    }


def audit(api: UniFi) -> None:
    nets = vlan_network_ids(api)
    print("=== VLAN networks ===")
    for name, vid, note in SSID_SPECS:
        print(f"  VLAN {vid:>2} {name:8} net={nets[vid][:8]}… — {note}")
    print("\n=== WLAN ===")
    by_name = {w.get("name"): w for w in api.get("/rest/wlanconf").get("data", [])}
    for name, vid, note in SSID_SPECS:
        w = by_name.get(name)
        if not w:
            print(f"  MISSING  {name} (VLAN {vid})")
            continue
        print(
            f"  OK       {name} enabled={w.get('enabled')} "
            f"vlan_net={w.get('networkconf_id','')[:8]}…"
        )


def apply(api: UniFi, enable: bool) -> None:
    nets = vlan_network_ids(api)
    usergroup_id = api.get("/rest/usergroup")["data"][0]["_id"]
    wlangroup_id = next(
        g["_id"] for g in api.get("/rest/wlangroup")["data"] if g.get("name") == "Default"
    )
    ap_group_id = default_ap_group_id(api)
    existing = {w.get("name"): w for w in api.get("/rest/wlanconf").get("data", [])}
    for name, vid, note in SSID_SPECS:
        body = wlan_payload(
            name, nets[vid], usergroup_id, wlangroup_id, ap_group_id, enabled=enable
        )
        if name in existing:
            wid = existing[name]["_id"]
            # Do not overwrite Portal-set password unless BIFROST_WIFI_PASS is explicitly set.
            if "BIFROST_WIFI_PASS" not in os.environ:
                body.pop("x_passphrase", None)
            resp = api.put(f"/rest/wlanconf/{wid}", body)
            action = "UPDATE"
        else:
            resp = api.post("/rest/wlanconf", body)
            action = "CREATE"
        if resp.get("meta", {}).get("rc") != "ok":
            raise SystemExit(f"{action} {name} failed: {resp}")
        state = "enabled" if enable else "disabled"
        print(f"OK  {action} {name} → VLAN {vid} ({state}) — {note}")
    if not enable:
        print(
            "\nSSIDs are disabled — safe alongside Eero.\n"
            "Next: UniFi Portal → Settings → WiFi → set password on each SSID "
            "(vision = current Eero password), then enable when ready."
        )


def main() -> None:
    cmd = sys.argv[1] if len(sys.argv) > 1 else "audit"
    enable = "--enable" in sys.argv
    api = UniFi()
    api.login()
    if cmd == "audit":
        audit(api)
        return
    if cmd == "apply":
        # UniFi requires at least one adopted AP (ApGroup) before wlanconf POST succeeds.
        devices = api.get("/stat/device").get("data", [])
        aps = [d for d in devices if d.get("type") in ("uap", "uap-ng", "uap-xg")]
        if not aps:
            print("SKIP wlanconf create — no AP adopted yet (api.err.ApGroupMissing).")
            print("     Run `python3 scripts/unifi_wlan_precreate.py apply` after first U7 Pro is adopted.")
            audit(api)
            return
        apply(api, enable=enable)
        print()
        audit(api)
        return
    raise SystemExit(f"Unknown command: {cmd}")


if __name__ == "__main__":
    main()
