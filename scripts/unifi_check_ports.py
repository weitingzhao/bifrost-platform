#!/usr/bin/env python3
"""Check specific USW ports via stat JSON files or live curl."""
import json
import sys
from collections import defaultdict
from pathlib import Path

dev = json.loads(Path("/tmp/dev.json").read_text())
sta = json.loads(Path("/tmp/sta.json").read_text())


def port_detail(idx: int) -> dict:
    for d in dev.get("data", []):
        if d.get("type") != "usw":
            continue
        for p in d.get("port_table", []):
            if p.get("port_idx") == idx:
                sp = p.get("speed")
                if sp == 2500:
                    link = "2.5G"
                elif sp:
                    link = f"{sp}M"
                else:
                    link = "down"
                lc = p.get("last_connection") or {}
                return {
                    "up": p.get("up"),
                    "link": link,
                    "media": p.get("media"),
                    "mac_table_count": p.get("mac_table_count"),
                    "link_down_count": p.get("link_down_count"),
                    "last_mac": lc.get("mac"),
                    "last_ip": lc.get("ip"),
                }
    return {}


ports = defaultdict(list)
for c in sta.get("data", []):
    if not c.get("is_wired"):
        continue
    sw = c.get("sw_port")
    if sw not in (1, 15):
        continue
    ports[sw].append(
        {
            "ip": c.get("ip") or "?",
            "hostname": c.get("hostname") or "-",
            "mac": c.get("mac") or "",
        }
    )

check = [int(x) for x in sys.argv[1:]] if len(sys.argv) > 1 else [1, 15]
for p in check:
    d = port_detail(p)
    print(f"=== P{p:02d} ===")
    print(f"  Link: {d.get('link')}  up={d.get('up')}  media={d.get('media')}")
    print(f"  mac_table_count={d.get('mac_table_count')}  link_down_count={d.get('link_down_count')}")
    print(f"  last_connection: {d.get('last_ip')} {d.get('last_mac')}")
    clients = ports.get(p, [])
    print(f"  wired clients ({len(clients)}):")
    for c in clients:
        print(f"    {c['ip']:>15}  {c['hostname']}  {c['mac']}")
    print()
