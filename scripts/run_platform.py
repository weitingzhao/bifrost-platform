#!/usr/bin/env python3
"""Start bifrost-platform API + Console (dev).

Frees listen ports if occupied, then starts:
  - platform-api  default :8780  (PLATFORM_LISTEN)
  - console         default :5180  (PLATFORM_CONSOLE_PORT)

Usage (from repo root or anywhere):
  python scripts/run_platform.py
  python scripts/run_platform.py --api-only
  python scripts/run_platform.py --console-only
  ./scripts/run_platform.py

Prefer prebuilt API binary when present:
  make build-api   # → api/bin/platform-api
  (falls back to `go run` if the binary is missing)

For split bdev sessions (recommended):
  bdev restart platform-api
  bdev restart platform-console

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
_API_BIN = _API_DIR / "bin" / "platform-api"
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
    "PLATFORM_METRICS_SERVER_SCRIPT",
    "PLATFORM_METRICS_SERVER_ENABLED",
    "PLATFORM_OPERATOR_TOKEN",
    "PLATFORM_ADMIN_TOKEN",
    "PLATFORM_LISTEN",
    "PLATFORM_CONSOLE_HOST",
    "PLATFORM_CONSOLE_PORT",
    "PLATFORM_CONFIG",
    "OPS_VIEWER_ENV",
    "REMEDIATION_RUNNER_URL",
    "REMEDIATION_RUNNER_STANDBY_URL",
    "REMEDIATION_RUNNER_PORT",
    "REMEDIATION_RUNNER_BIND",
    "REMEDIATION_RUNNER_AUTOSTART",
    "REMEDIATION_CWD",
    "REMEDIATION_MODEL",
    "CURSOR_API_KEY",
    "PLATFORM_API_URL",
    "GIT_BRIDGE_URL",
    "SATELLITE_PROBE_BRIDGE_URL",
    "HERMES_GATEWAY_URL",
})


def _normalize_platform_env() -> None:
    """Expand $HOME/~ and resolve infra script paths before starting API (cwd=api/)."""
    # Local Fleet Desk seat: default DEV so make start never inherits clusters.yaml prod pin.
    # Prod in-cluster uses KUBERNETES_SERVICE_HOST + yaml viewer_env (or OPS_VIEWER_ENV=prod).
    if not os.environ.get("OPS_VIEWER_ENV", "").strip():
        os.environ["OPS_VIEWER_ENV"] = "dev"

    kc = os.environ.get("PLATFORM_KUBECONFIG", "")
    if kc:
        os.environ["PLATFORM_KUBECONFIG"] = os.path.expanduser(os.path.expandvars(kc))

    script = os.environ.get("PLATFORM_CLUSTER_SYNC_SCRIPT", "").strip()
    if not script:
        default = _PROJECT_ROOT.parent / "bifrost-trade-infra" / "scripts" / "k3s" / "fetch-kubeconfig.sh"
        if default.is_file():
            os.environ["PLATFORM_CLUSTER_SYNC_SCRIPT"] = str(default.resolve())
    else:
        expanded = os.path.expanduser(os.path.expandvars(script))
        candidate = Path(expanded)
        if candidate.is_file():
            os.environ["PLATFORM_CLUSTER_SYNC_SCRIPT"] = str(candidate.resolve())
        elif not candidate.is_absolute():
            candidate = (_PROJECT_ROOT / expanded).resolve()
            if candidate.is_file():
                os.environ["PLATFORM_CLUSTER_SYNC_SCRIPT"] = str(candidate)
        else:
            sibling = _PROJECT_ROOT.parent / "bifrost-trade-infra" / "scripts" / "k3s" / "fetch-kubeconfig.sh"
            if sibling.is_file():
                os.environ["PLATFORM_CLUSTER_SYNC_SCRIPT"] = str(sibling.resolve())

    _resolve_metrics_server_script()


def _resolve_metrics_server_script() -> None:
    script = os.environ.get("PLATFORM_METRICS_SERVER_SCRIPT", "").strip()
    default = _PROJECT_ROOT.parent / "bifrost-trade-infra" / "scripts" / "k3s" / "install-metrics-server.sh"
    if not script:
        if default.is_file():
            os.environ["PLATFORM_METRICS_SERVER_SCRIPT"] = str(default.resolve())
        return

    expanded = os.path.expanduser(os.path.expandvars(script))
    candidate = Path(expanded)
    if candidate.is_file():
        os.environ["PLATFORM_METRICS_SERVER_SCRIPT"] = str(candidate.resolve())
        return
    if not candidate.is_absolute():
        candidate = (_PROJECT_ROOT / expanded).resolve()
    if candidate.is_file():
        os.environ["PLATFORM_METRICS_SERVER_SCRIPT"] = str(candidate)
        return
    if default.is_file():
        os.environ["PLATFORM_METRICS_SERVER_SCRIPT"] = str(default.resolve())


def _parse_listen_port(listen: str, default: int) -> int:
    listen = (listen or "").strip()
    if not listen:
        return default
    if listen.startswith(":"):
        return int(listen[1:])
    if ":" in listen:
        return int(listen.rsplit(":", 1)[-1])
    return int(listen)


def _lan_urls(console_port: int) -> list[str]:
    """Prefer Bifrost LAN (192.168.10.x) when listing remote console URLs."""
    try:
        out = subprocess.check_output(["ifconfig"], text=True, stderr=subprocess.DEVNULL)
    except (OSError, subprocess.CalledProcessError):
        return []
    ips: list[str] = []
    for line in out.splitlines():
        line = line.strip()
        if not line.startswith("inet "):
            continue
        parts = line.split()
        if len(parts) < 2:
            continue
        ip = parts[1]
        if ip.startswith("127.") or ":" in ip:
            continue
        ips.append(ip)
    preferred = [ip for ip in ips if ip.startswith("192.168.10.")]
    others = [ip for ip in ips if ip not in preferred]
    return [f"http://{ip}:{console_port}" for ip in preferred + others]


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


def _runner_autostart_enabled() -> bool:
    flag = os.environ.get("REMEDIATION_RUNNER_AUTOSTART", "1").strip().lower()
    if flag in ("0", "false", "no", "off"):
        return False
    url = os.environ.get("REMEDIATION_RUNNER_URL", "").strip()
    if not url:
        os.environ.setdefault("REMEDIATION_RUNNER_URL", "http://127.0.0.1:8781")
        return True
    lowered = url.lower()
    return "127.0.0.1" in lowered or "localhost" in lowered


def _remediation_runner_port() -> int:
    url = os.environ.get("REMEDIATION_RUNNER_URL", "http://127.0.0.1:8781").strip()
    if not url:
        return 8781
    if "://" not in url:
        url = f"http://{url}"
    from urllib.parse import urlparse

    parsed = urlparse(url)
    if parsed.port is not None:
        return parsed.port
    return 8781


def _spawn_remediation_runner(env: dict[str, str]) -> subprocess.Popen[bytes] | None:
    if not _runner_autostart_enabled():
        return None
    port = _remediation_runner_port()
    if _pids_on_port(port):
        print(f"Remediation runner already on :{port} (skip autostart)")
        return None
    remediation_dir = _PROJECT_ROOT / "agent" / "remediation"
    agent_script = _PROJECT_ROOT / "scripts" / "run_agent.py"
    if not remediation_dir.is_dir() or not agent_script.is_file():
        return None
    if not (remediation_dir / "node_modules").is_dir():
        print(
            "Remediation runner not installed — Agent Desk needs `make dev-agent` "
            "(or cd agent/remediation && npm install)"
        )
        return None
    print(f"Starting remediation runner on :{port} (sidecar)")
    return subprocess.Popen(
        [sys.executable, str(agent_script), "start", "--no-watch"],
        cwd=_PROJECT_ROOT,
        env=env,
    )


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
    parser.add_argument(
        "--no-runner",
        action="store_true",
        help="Do not autostart remediation runner (use a separate bdev session)",
    )
    args = parser.parse_args()

    _load_dotenv()
    _normalize_platform_env()

    api_port = _parse_listen_port(os.environ.get("PLATFORM_LISTEN", ":8780"), 8780)
    console_port = int(os.environ.get("PLATFORM_CONSOLE_PORT", "5180"))
    # Default loopback-only. Set PLATFORM_CONSOLE_HOST=0.0.0.0 for Bifrost LAN access.
    console_host = (os.environ.get("PLATFORM_CONSOLE_HOST") or "127.0.0.1").strip() or "127.0.0.1"

    os.environ.setdefault("PLATFORM_LISTEN", f":{api_port}")
    os.environ["PLATFORM_CONSOLE_PORT"] = str(console_port)
    os.environ["PLATFORM_CONSOLE_HOST"] = console_host

    if _ensure_prereqs() != 0:
        return 1

    start_api = not args.console_only
    start_console = not args.api_only

    if start_api and not _free_port(api_port, "platform-api"):
        return 1
    if start_console and not _free_port(console_port, "console"):
        return 1

    env = _child_env()
    os.environ.setdefault("REMEDIATION_RUNNER_URL", "http://127.0.0.1:8781")
    children: list[subprocess.Popen[bytes]] = []

    runner_proc: subprocess.Popen[bytes] | None = None
    if not args.no_runner:
        runner_proc = _spawn_remediation_runner(env)
        if runner_proc is not None:
            children.append(runner_proc)

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

    lan_urls = _lan_urls(console_port) if console_host in ("0.0.0.0", "::", "*") else []

    api_proc: subprocess.Popen[bytes] | None = None
    console_proc: subprocess.Popen[bytes] | None = None

    def spawn_api() -> subprocess.Popen[bytes]:
        listen = os.environ["PLATFORM_LISTEN"]
        if _API_BIN.is_file() and os.access(_API_BIN, os.X_OK):
            print(f"Starting platform-api (binary {_API_BIN}) on {listen}")
            return subprocess.Popen(
                [str(_API_BIN)],
                cwd=_API_DIR,
                env=env,
            )
        print(f"Starting platform-api (go run) on {listen}")
        print("  Tip: `make build-api` for faster restarts via api/bin/platform-api")
        return subprocess.Popen(
            ["go", "run", "./cmd/platform-api"],
            cwd=_API_DIR,
            env=env,
        )

    def spawn_console() -> subprocess.Popen[bytes]:
        print(f"Starting console on http://{console_host}:{console_port}")
        return subprocess.Popen(
            [
                "npm",
                "run",
                "dev",
                "--",
                "--host",
                console_host,
                "--port",
                str(console_port),
                "--strictPort",
            ],
            cwd=_CONSOLE_DIR,
            env=env,
        )

    if start_api:
        # PLATFORM_LISTEN=:8780 binds all interfaces (needed for remote /api via Vite proxy
        # and direct API clients on the Bifrost LAN).
        api_proc = spawn_api()
        children.append(api_proc)

    if start_console:
        console_proc = spawn_console()
        children.append(console_proc)

    print(f"Local console: http://127.0.0.1:{console_port}")
    for url in lan_urls:
        print(f"LAN console:   {url}")
    print("Press Ctrl+C to stop.")

    api_backoff = 1.0
    while True:
        # API crash used to tear down Vite too (run_platform treated any child exit
        # as fatal). Restart API in-place so Console stays up over LAN / bdev.
        if start_api and api_proc is not None and api_proc.poll() is not None:
            code = api_proc.returncode
            print(
                f"[platform-api] exited with code {code}; restarting in {api_backoff:.0f}s…",
                file=sys.stderr,
            )
            time.sleep(api_backoff)
            api_backoff = min(api_backoff * 2, 30.0)
            if not _free_port(api_port, "platform-api"):
                print("[platform-api] could not free listen port; giving up", file=sys.stderr)
                shutdown()
            try:
                children.remove(api_proc)
            except ValueError:
                pass
            api_proc = spawn_api()
            children.append(api_proc)
            continue

        if start_api and api_proc is not None and api_proc.poll() is None:
            api_backoff = 1.0

        if start_console and console_proc is not None and console_proc.poll() is not None:
            print(f"[console] exited with code {console_proc.returncode}", file=sys.stderr)
            shutdown()

        if runner_proc is not None and runner_proc.poll() is not None:
            # Remediation runner is optional; do not take down platform.
            print(
                f"[remediation-runner] exited with code {runner_proc.returncode} (ignored)",
                file=sys.stderr,
            )
            try:
                children.remove(runner_proc)
            except ValueError:
                pass
            runner_proc = None

        time.sleep(0.5)


if __name__ == "__main__":
    sys.exit(main())
