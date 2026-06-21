#!/usr/bin/env python3
"""Start bifrost-platform remediation Agent sidecar (dev).

Frees listen port if occupied, loads repo .env, then starts:
  - remediation runner  default :8781  (REMEDIATION_RUNNER_PORT)

Usage (from repo root or anywhere):
  python scripts/run_remediation_agent.py
  ./scripts/run_remediation_agent.py

Install once:
  cd agent/remediation && npm install
"""

from __future__ import annotations

import argparse
import os
import signal
import subprocess
import sys
import time
from pathlib import Path

_PROJECT_ROOT = Path(__file__).resolve().parent.parent
_AGENT_DIR = _PROJECT_ROOT / "agent" / "remediation"
_DEFAULT_INFRA = _PROJECT_ROOT.parent / "bifrost-trade-infra"

_REMEDIATION_DOTENV_KEYS = frozenset({
    "CURSOR_API_KEY",
    "REMEDIATION_RUNNER_URL",
    "REMEDIATION_RUNNER_PORT",
    "REMEDIATION_RUNNER_BIND",
    "REMEDIATION_CWD",
    "REMEDIATION_MODEL",
    "PLATFORM_API_URL",
    "PLATFORM_OPERATOR_TOKEN",
    "PLATFORM_ADMIN_TOKEN",
    "NIGHTLY_SKIP_CLUSTER",
    "KUBECONFIG",
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
        if key in _REMEDIATION_DOTENV_KEYS or key not in os.environ:
            os.environ[key] = val


def _normalize_remediation_env() -> None:
    port = os.environ.get("REMEDIATION_RUNNER_PORT", "8781").strip() or "8781"
    os.environ["REMEDIATION_RUNNER_PORT"] = port

    api_url = os.environ.get("PLATFORM_API_URL", "http://127.0.0.1:8780").strip()
    os.environ.setdefault("PLATFORM_API_URL", api_url or "http://127.0.0.1:8780")

    kc = os.environ.get("KUBECONFIG", "")
    if kc:
        os.environ["KUBECONFIG"] = os.path.expanduser(os.path.expandvars(kc))

    cwd = os.environ.get("REMEDIATION_CWD", "").strip()
    if not cwd:
        if _DEFAULT_INFRA.is_dir():
            os.environ["REMEDIATION_CWD"] = str(_DEFAULT_INFRA.resolve())
    else:
        expanded = os.path.expanduser(os.path.expandvars(cwd))
        candidate = Path(expanded)
        if not candidate.is_absolute():
            candidate = (_PROJECT_ROOT / expanded).resolve()
        if candidate.is_dir():
            os.environ["REMEDIATION_CWD"] = str(candidate)
        elif _DEFAULT_INFRA.is_dir():
            os.environ["REMEDIATION_CWD"] = str(_DEFAULT_INFRA.resolve())

    # Sidecar reads PLATFORM_OPERATOR_TOKEN; fall back to dev placeholder from platform-auth.yaml.
    if not os.environ.get("PLATFORM_OPERATOR_TOKEN", "").strip():
        os.environ.setdefault("PLATFORM_OPERATOR_TOKEN", "platform-operator-dev")


def _parse_port(raw: str, default: int) -> int:
    value = (raw or "").strip()
    if not value:
        return default
    if value.startswith(":"):
        return int(value[1:])
    if "://" in value:
        from urllib.parse import urlparse

        parsed = urlparse(value)
        if parsed.port is not None:
            return parsed.port
        return 443 if parsed.scheme == "https" else 80
    if ":" in value:
        return int(value.rsplit(":", 1)[-1])
    return int(value)


def _pids_on_port(port: int) -> list[int]:
    try:
        out = subprocess.run(
            ["lsof", "-i", f":{port}", "-t"],
            capture_output=True,
            text=True,
            timeout=5,
            check=False,
        )
        if out.returncode != 0:
            return []
        return [int(x) for x in out.stdout.strip().splitlines() if x.strip()]
    except (subprocess.TimeoutExpired, ValueError, FileNotFoundError):
        return []


def _kill_pids(pids: list[int], sig: int) -> None:
    for pid in pids:
        try:
            os.kill(pid, sig)
        except (ProcessLookupError, PermissionError):
            pass


def _free_port(port: int, label: str, wait_sec: float = 0.6) -> bool:
    pids = _pids_on_port(port)
    if not pids:
        return True
    print(f"[{label}] Port {port} in use by PIDs {pids}; SIGTERM...")
    _kill_pids(pids, signal.SIGTERM)
    time.sleep(wait_sec)
    still = _pids_on_port(port)
    if still:
        print(f"[{label}] Port {port} still in use by {still}; SIGKILL...")
        _kill_pids(still, signal.SIGKILL)
        time.sleep(wait_sec)
    remaining = _pids_on_port(port)
    if remaining:
        print(f"Error: could not free port {port} (PIDs {remaining})", file=sys.stderr)
        return False
    print(f"[{label}] Port {port} is free.")
    return True


def _which(cmd: str) -> str | None:
    path = os.environ.get("PATH", "")
    for part in path.split(os.pathsep):
        candidate = Path(part) / cmd
        if candidate.is_file() and os.access(candidate, os.X_OK):
            return str(candidate)
    return None


def _ensure_prereqs(install: bool) -> int:
    if _which("node") is None:
        print("Error: node not found. Install Node.js 20+.", file=sys.stderr)
        return 1
    if _which("npm") is None:
        print("Error: npm not found.", file=sys.stderr)
        return 1
    if not (_AGENT_DIR / "package.json").is_file():
        print(f"Error: missing {_AGENT_DIR / 'package.json'}", file=sys.stderr)
        return 1
    if not (_AGENT_DIR / "node_modules").is_dir():
        if not install:
            print(
                "Error: agent/remediation/node_modules missing. "
                "Run: cd agent/remediation && npm install\n"
                "Or rerun with --install",
                file=sys.stderr,
            )
            return 1
        print("[remediation-agent] Installing npm dependencies...")
        proc = subprocess.run(["npm", "install"], cwd=_AGENT_DIR, check=False)
        if proc.returncode != 0:
            print("Error: npm install failed", file=sys.stderr)
            return proc.returncode
    return 0


def _print_startup_summary(port: int) -> None:
    has_key = bool(os.environ.get("CURSOR_API_KEY", "").strip())
    bind = os.environ.get("REMEDIATION_RUNNER_BIND", "127.0.0.1").strip() or "127.0.0.1"
    print(f"Starting remediation agent on http://{bind}:{port}")
    print(f"  cwd:              {os.environ.get('REMEDIATION_CWD', '(unset)')}")
    print(f"  platform-api:     {os.environ.get('PLATFORM_API_URL', 'http://127.0.0.1:8780')}")
    print(f"  CURSOR_API_KEY:   {'set' if has_key else 'MISSING — jobs will fail until configured'}")
    if not has_key:
        print("  hint: add CURSOR_API_KEY to bifrost-platform/.env")
    print("Press Ctrl+C to stop.")


def main() -> int:
    parser = argparse.ArgumentParser(description="Start bifrost remediation Agent sidecar")
    parser.add_argument(
        "--install",
        action="store_true",
        help="Run npm install if node_modules is missing",
    )
    parser.add_argument(
        "--no-watch",
        action="store_true",
        help="Use tsx src/server.ts instead of tsx watch (no hot reload)",
    )
    args = parser.parse_args()

    _load_dotenv()
    _normalize_remediation_env()

    port = _parse_port(os.environ.get("REMEDIATION_RUNNER_PORT", "8781"), 8781)
    os.environ["REMEDIATION_RUNNER_PORT"] = str(port)

    if _ensure_prereqs(install=args.install) != 0:
        return 1

    if not _free_port(port, "remediation-agent"):
        return 1

    npm_script = "start" if args.no_watch else "dev"
    env = os.environ.copy()
    env["PLATFORM_PROJECT_ROOT"] = str(_PROJECT_ROOT)

    _print_startup_summary(port)

    child = subprocess.Popen(
        ["npm", "run", npm_script],
        cwd=_AGENT_DIR,
        env=env,
    )

    def shutdown(signum: int | None = None, _frame: object | None = None) -> None:
        if signum is not None:
            print("\nShutting down remediation agent...")
        if child.poll() is None:
            child.terminate()
            deadline = time.time() + 3.0
            while child.poll() is None and time.time() < deadline:
                time.sleep(0.1)
            if child.poll() is None:
                child.kill()
        sys.exit(0 if signum is not None else 1)

    signal.signal(signal.SIGINT, shutdown)
    signal.signal(signal.SIGTERM, shutdown)

    while True:
        if child.poll() is not None:
            print(f"Process exited with code {child.returncode}", file=sys.stderr)
            shutdown()
        time.sleep(0.5)


if __name__ == "__main__":
    sys.exit(main())
