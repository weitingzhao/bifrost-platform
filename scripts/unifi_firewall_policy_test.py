#!/usr/bin/env python3
"""Offline check of the ZBF policy payloads. No network, and the Owner env file is not read."""
from __future__ import annotations

import os
import re
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
# The baseline module loads the Owner env at import; point it at a path that does not exist.
os.environ["BIFROST_OWNER_ENV"] = str(HERE / "no-such-owner-env")
sys.path.insert(0, str(HERE))
import unifi_firewall_setup as fw  # noqa: E402

VPN_RULE = "Bifrost | ALLOW VPN → Ops VIP"
FAMILY_RULE = "Bifrost | ALLOW Family → Trade VIP"


def main() -> int:
    zone_ids = {name: f"zone-{i}" for i, (name, _) in enumerate(fw.ZONE_SPECS)}
    zone_ids[fw.predefined_zone_ref("vpn")] = "zone-vpn"
    by_name = {spec["name"]: spec for spec in fw.POLICY_SPECS}
    failures: list[str] = []

    vpn = fw.build_v2_policy(by_name[VPN_RULE], zone_ids)
    want_source = {
        "zone_id": "zone-vpn",
        "matching_target": "IP",
        "matching_target_type": "SPECIFIC",
        "ips": ["192.168.2.0/24"],
        "port_matching_type": "ANY",
        "match_opposite_ports": False,
        "match_opposite_ips": False,
    }
    want_destination = {
        "zone_id": zone_ids["Bifrost Server"],
        "matching_target": "IP",
        "matching_target_type": "SPECIFIC",
        "ips": ["192.168.10.100"],
        "port_matching_type": "SPECIFIC",
        "match_opposite_ports": False,
        "match_opposite_ips": False,
        "port": "443",
    }
    if vpn["source"] != want_source:
        failures.append(f"VPN rule source: {vpn['source']}")
    if vpn["destination"] != want_destination:
        failures.append(f"VPN rule destination: {vpn['destination']}")
    if (vpn["protocol"], vpn["action"], vpn["create_allow_respond"]) != ("tcp", "ALLOW", True):
        failures.append("VPN rule must be a TCP ALLOW with a return rule")

    family = fw.build_v2_policy(by_name[FAMILY_RULE], zone_ids)
    if (
        family["protocol"] != "all"
        or family["source"] != fw.endpoint_any(zone_ids["Bifrost Family"])
        or family["destination"].get("port") != "80,443"
    ):
        failures.append(f"existing rule payload changed: {family}")

    go_source = (HERE.parent / "api/internal/network/service.go").read_text(encoding="utf-8")
    block = go_source.split("var expectedPolicyNames = []string{", 1)[1].split("}", 1)[0]
    go_names = set(re.findall(r'"([^"]+)"', block))
    py_names = {spec["name"] for spec in fw.POLICY_SPECS} | {fw.DEFAULT_DENY_POLICY["name"]}
    if go_names != py_names:
        failures.append(
            "service.go expectedPolicyNames differs: "
            f"only in Go {sorted(go_names - py_names)}, only in script {sorted(py_names - go_names)}"
        )

    for line in failures:
        print(f"FAIL {line}", file=sys.stderr)
    if failures:
        return 1
    print("ok: VPN rule payload, existing rule payload, and the Go audit list match the baseline")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
