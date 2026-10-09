"""Load UniFi credentials for Owner scripts.

Owner only. An Agent must not run the UniFi scripts.

Precedence matches bifrost-trade-infra/scripts/owner/owner-env.sh: an existing
environment variable wins, otherwise ~/.bifrost-owner/owner.env (override with
BIFROST_OWNER_ENV). Values are never printed.
"""
from __future__ import annotations

import os
from pathlib import Path

KEYS = ("UNIFI_HOST", "UNIFI_USER", "UNIFI_PASS", "UNIFI_API_KEY")


def owner_env_path() -> Path:
    raw = os.environ.get("BIFROST_OWNER_ENV")
    if raw:
        return Path(raw)
    return Path.home() / ".bifrost-owner" / "owner.env"


def load(keys: tuple[str, ...] = KEYS) -> None:
    missing = [key for key in keys if not os.environ.get(key)]
    path = owner_env_path()
    if not missing or not path.is_file():
        return
    found: dict[str, str] = {}
    for line in path.read_text(encoding="utf-8").splitlines():
        if not line or line.lstrip().startswith("#") or "=" not in line:
            continue
        key, _, raw = line.partition("=")
        key = key.strip()
        val = raw.strip()
        if len(val) >= 2 and val[0] == val[-1] and val[0] in ("'", '"'):
            val = val[1:-1]
        if key:
            found[key] = val
    for key in missing:
        if found.get(key):
            os.environ[key] = found[key]
