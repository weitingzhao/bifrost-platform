#!/usr/bin/env python3
"""Bifrost agent host tooling — single entry (mirrors run_platform.py).

The remediation runner and Hermes gateway are retired (OWNER_LAW #95); there is
no `start` subcommand any more. A subcommand is required.

Subcommands:
  drift       Layer 1–3 scan, no report file
  deploy      deploy the host services to Mac Mini [user@host]
  bootstrap   one-time Mac Mini host bootstrap (run on Mini via Screen Sharing)

Usage:
  python scripts/run_agent.py drift
  python scripts/run_agent.py deploy vision@192.168.10.50
  python scripts/run_agent.py bootstrap
"""

from __future__ import annotations

import argparse
import os
import subprocess
import sys
from pathlib import Path

_PROJECT_ROOT = Path(__file__).resolve().parent.parent
_AGENT_SCRIPTS = _PROJECT_ROOT / "scripts" / "agent"

# Keys the agent-host scripts read from .env (file value wins over the shell).
_DOTENV_KEYS = frozenset({
    "PLATFORM_API_URL",
    "PLATFORM_OPERATOR_TOKEN",
    "KUBECONFIG",
    "BIFROST_AGENT_ROOT",
})


def _load_dotenv() -> None:
    env_path = _PROJECT_ROOT / ".env"
    if not env_path.is_file():
        return
    for line in env_path.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, _, val = line.partition("=")
        key = key.strip()
        val = val.strip().strip('"').strip("'")
        if not key:
            continue
        if key in _DOTENV_KEYS or key not in os.environ:
            os.environ[key] = val


def _run_shell(script_name: str, *args: str) -> int:
    _load_dotenv()
    kc = os.environ.get("KUBECONFIG", "")
    if kc:
        os.environ["KUBECONFIG"] = os.path.expanduser(os.path.expandvars(kc))
    script = _AGENT_SCRIPTS / script_name
    if not script.is_file():
        print(f"Error: missing {script}", file=sys.stderr)
        return 1
    proc = subprocess.run(
        ["bash", str(script), *args],
        cwd=_PROJECT_ROOT,
        env=os.environ.copy(),
        check=False,
    )
    return proc.returncode


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Bifrost agent host tooling (single entry)",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=__doc__,
    )
    sub = parser.add_subparsers(dest="command", required=True)

    sub.add_parser("drift", help="Layer 1–3 drift scan").set_defaults(
        func=lambda _a: _run_shell("drift_scan.sh")
    )

    deploy_p = sub.add_parser("deploy", help="deploy to Mac Mini agent host")
    deploy_p.add_argument("remote", nargs="?", default="vision@192.168.10.50")
    deploy_p.set_defaults(func=lambda a: _run_shell("deploy_mac_mini.sh", a.remote))

    sub.add_parser("bootstrap", help="one-time Mac Mini bootstrap (run on Mini)").set_defaults(
        func=lambda _a: _run_shell("bootstrap_host.sh")
    )

    args = parser.parse_args()
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
