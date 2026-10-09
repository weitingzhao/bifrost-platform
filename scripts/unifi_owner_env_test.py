#!/usr/bin/env python3
"""Fake owner.env. Passing output must not contain the value."""
from __future__ import annotations

import hashlib
import io
import os
import sys
import tempfile
from contextlib import redirect_stdout
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import unifi_owner_env


def main() -> int:
    secret = "sekrit-unifi-value"
    with tempfile.TemporaryDirectory() as tmp:
        path = Path(tmp) / "owner.env"
        path.write_text(f"UNIFI_PASS={secret}\nUNIFI_USER=owner-user\n", encoding="utf-8")
        os.environ["BIFROST_OWNER_ENV"] = str(path)
        os.environ.pop("UNIFI_PASS", None)
        os.environ.pop("UNIFI_USER", None)
        os.environ["UNIFI_HOST"] = "already"
        buf = io.StringIO()
        with redirect_stdout(buf):
            unifi_owner_env.load()
        if secret in buf.getvalue():
            print("FAIL load printed a value", file=sys.stderr)
            return 1
        got = hashlib.sha256(os.environ.get("UNIFI_PASS", "").encode()).hexdigest()
        want = hashlib.sha256(secret.encode()).hexdigest()
        if got != want or os.environ.get("UNIFI_HOST") != "already" or os.environ.get("UNIFI_USER") != "owner-user":
            print("FAIL load did not apply precedence", file=sys.stderr)
            return 1
    print("ok: unifi owner env prefers the environment and does not print values")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
