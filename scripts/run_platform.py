#!/usr/bin/env python3
"""Start bifrost-platform API + Console (dev).

Frees listen ports if occupied, then starts:
  - platform-api  default :8780  (PLATFORM_LISTEN)
  - console         default :5180  (PLATFORM_CONSOLE_PORT)

Usage (from repo root or anywhere):
  python scripts/run_platform.py
  ./scripts/run_platform.py

Install once:
  cd api && go mod tidy
  cd console && npm install
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
_API_DIR = _PROJECT_ROOT / "api"
_CONSOLE_DIR = _PROJECT_ROOT / "console"


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
        # Always apply platform-local paths from .env (API cwd=api/ breaks relatives).
        if key in _PLATFORM_DOTENV_KEYS or key not in os.environ:
            os.environ[key] = val


_PLATFORM_DOTENV_KEYS = frozenset({
    "PLATFORM_KUBECONFIG",
    "PLATFORM_CLUSTER_SYNC_SCRIPT",
    "PLATFORM_CLUSTER_SYNC_ENABLED",
    "PLATFORM_OPERATOR_TOKEN",
    "PLATFORM_LISTEN",
    "PLATFORM_CONFIG",
})


def _normalize_platform_env() -> None:
    """Expand $HOME/~ and resolve infra script paths before starting API (cwd=api/)."""
    kc = os.environ.get("PLATFORM_KUBECONFIG", "")
    if kc:
        os.environ["PLATFORM_KUBECONFIG"] = os.path.expanduser(os.path.expandvars(kc))

    script = os.environ.get("PLATFORM_CLUSTER_SYNC_SCRIPT", "").strip()
    if not script:
        default = _PROJECT_ROOT.parent / "bifrost-trade-infra" / "scripts" / "k3s" / "fetch-kubeconfig.sh"
        if default.is_file():
            os.environ["PLATFORM_CLUSTER_SYNC_SCRIPT"] = str(default.resolve())
        return

    expanded = os.path.expanduser(os.path.expandvars(script))
    candidate = Path(expanded)
    if candidate.is_file():
        os.environ["PLATFORM_CLUSTER_SYNC_SCRIPT"] = str(candidate.resolve())
        return
    if not candidate.is_absolute():
        candidate = (_PROJECT_ROOT / expanded).resolve()
    if candidate.is_file():
        os.environ["PLATFORM_CLUSTER_SYNC_SCRIPT"] = str(candidate)
        return

    sibling = _PROJECT_ROOT.parent / "bifrost-trade-infra" / "scripts" / "k3s" / "fetch-kubeconfig.sh"
    if sibling.is_file():
        os.environ["PLATFORM_CLUSTER_SYNC_SCRIPT"] = str(sibling.resolve())
        return

    # Last resort: keep configured value (API will surface a clear not-found error).


def _parse_listen_port(listen: str, default: int) -> int:
    listen = (listen or "").strip()
    if not listen:
        return default
    if listen.startswith(":"):
        return int(listen[1:])
    if ":" in listen:
        return int(listen.rsplit(":", 1)[-1])
    return int(listen)


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


def _ensure_prereqs() -> int:
    if not shutil_which("go"):
        print("Error: go not found. Install: brew install go", file=sys.stderr)
        return 1
    if not (_API_DIR / "go.mod").is_file():
        print(f"Error: missing {_API_DIR / 'go.mod'}", file=sys.stderr)
        return 1
    if not (_CONSOLE_DIR / "package.json").is_file():
        print(f"Error: missing {_CONSOLE_DIR / 'package.json'}", file=sys.stderr)
        return 1
    if not (_CONSOLE_DIR / "node_modules").is_dir():
        print("Console node_modules missing. Run: cd console && npm install", file=sys.stderr)
        return 1
    return 0


def shutil_which(cmd: str) -> str | None:
    path = os.environ.get("PATH", "")
    for part in path.split(os.pathsep):
        candidate = Path(part) / cmd
        if candidate.is_file() and os.access(candidate, os.X_OK):
            return str(candidate)
    return None


def _child_env() -> dict[str, str]:
    _normalize_platform_env()
    env = os.environ.copy()
    env["PLATFORM_PROJECT_ROOT"] = str(_PROJECT_ROOT)
    # Homebrew Go on Apple Silicon
    brew_bin = "/opt/homebrew/bin"
    if Path(brew_bin).is_dir():
        env["PATH"] = f"{brew_bin}{os.pathsep}{env.get('PATH', '')}"
    return env


def main() -> int:
    parser = argparse.ArgumentParser(description="Start bifrost-platform API + Console")
    parser.add_argument(
        "--api-only",
        action="store_true",
        help="Start platform-api only",
    )
    parser.add_argument(
        "--console-only",
        action="store_true",
        help="Start console only",
    )
    args = parser.parse_args()

    _load_dotenv()
    _normalize_platform_env()

    api_port = _parse_listen_port(os.environ.get("PLATFORM_LISTEN", ":8780"), 8780)
    console_port = int(os.environ.get("PLATFORM_CONSOLE_PORT", "5180"))

    os.environ.setdefault("PLATFORM_LISTEN", f":{api_port}")
    os.environ["PLATFORM_CONSOLE_PORT"] = str(console_port)

    if _ensure_prereqs() != 0:
        return 1

    start_api = not args.console_only
    start_console = not args.api_only

    if start_api and not _free_port(api_port, "platform-api"):
        return 1
    if start_console and not _free_port(console_port, "console"):
        return 1

    env = _child_env()
    children: list[subprocess.Popen[bytes]] = []

    def shutdown(signum: int | None = None, _frame: object | None = None) -> None:
        if signum is not None:
            print("\nShutting down...")
        for proc in children:
            if proc.poll() is None:
                proc.terminate()
        deadline = time.time() + 3.0
        for proc in children:
            while proc.poll() is None and time.time() < deadline:
                time.sleep(0.1)
        for proc in children:
            if proc.poll() is None:
                proc.kill()
        sys.exit(0 if signum is not None else 1)

    signal.signal(signal.SIGINT, shutdown)
    signal.signal(signal.SIGTERM, shutdown)

    if start_api:
        print(f"Starting platform-api on http://127.0.0.1:{api_port}")
        children.append(
            subprocess.Popen(
                ["go", "run", "./cmd/platform-api"],
                cwd=_API_DIR,
                env=env,
            )
        )

    if start_console:
        print(f"Starting console on http://127.0.0.1:{console_port}")
        children.append(
            subprocess.Popen(
                ["npm", "run", "dev", "--", "--port", str(console_port), "--strictPort"],
                cwd=_CONSOLE_DIR,
                env=env,
            )
        )

    print("Press Ctrl+C to stop.")
    while True:
        for proc in children:
            if proc.poll() is not None:
                print(f"Process exited with code {proc.returncode}", file=sys.stderr)
                shutdown()
        time.sleep(0.5)


if __name__ == "__main__":
    sys.exit(main())
