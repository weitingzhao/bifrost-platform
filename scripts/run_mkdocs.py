#!/usr/bin/env python3
"""Start MkDocs dev server for bifrost-platform.

Default: http://127.0.0.1:8060
Port: DOCS_PORT env or --port / -p
"""

from __future__ import annotations

import argparse
import os
import signal
import subprocess
import sys
import time

_PROJECT_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
_DEFAULT_PORT = 8060


def _symlink_file(link_path: str, target_path: str) -> None:
    if not os.path.isfile(target_path):
        return
    if os.path.islink(link_path) or os.path.lexists(link_path):
        return
    os.makedirs(os.path.dirname(link_path), exist_ok=True)
    os.symlink(target_path, link_path)


def _ensure_doc_symlinks() -> None:
    """Expose repo-root README and agent/mcp READMEs under docs/."""
    docs = os.path.join(_PROJECT_ROOT, "docs")
    _symlink_file(os.path.join(docs, "overview.md"), os.path.join(_PROJECT_ROOT, "README.md"))
    _symlink_file(
        os.path.join(docs, "agent", "README.md"),
        os.path.join(_PROJECT_ROOT, "agent", "README.md"),
    )
    _symlink_file(
        os.path.join(docs, "mcp", "README.md"),
        os.path.join(_PROJECT_ROOT, "mcp", "README.md"),
    )


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
        return [int(x) for x in (out.stdout or "").strip().splitlines() if x.strip()]
    except (subprocess.TimeoutExpired, ValueError, FileNotFoundError):
        return []


def _kill_pids(pids: list[int], sig: int = signal.SIGTERM) -> None:
    for pid in pids:
        try:
            os.kill(pid, sig)
        except (ProcessLookupError, PermissionError):
            pass


def _free_port(port: int, wait_sec: float = 0.6) -> bool:
    pids = _pids_on_port(port)
    if not pids:
        return True
    print(f"Port {port} in use by PIDs {pids}; sending SIGTERM...")
    _kill_pids(pids, signal.SIGTERM)
    time.sleep(wait_sec)
    still = _pids_on_port(port)
    if still:
        print(f"Still in use by {still}; sending SIGKILL...")
        _kill_pids(still, signal.SIGKILL)
        time.sleep(wait_sec)
    return len(_pids_on_port(port)) == 0


def main() -> int:
    parser = argparse.ArgumentParser(description="MkDocs serve — bifrost-platform")
    parser.add_argument(
        "-p",
        "--port",
        type=int,
        default=int(os.environ.get("DOCS_PORT", str(_DEFAULT_PORT))),
        help=f"Listen port (default {_DEFAULT_PORT} or DOCS_PORT)",
    )
    parser.add_argument(
        "-a",
        "--addr",
        default="127.0.0.1",
        help="Listen address (default 127.0.0.1)",
    )
    args = parser.parse_args()

    os.chdir(_PROJECT_ROOT)
    _ensure_doc_symlinks()

    try:
        import mkdocs  # noqa: F401
    except ImportError:
        print(
            "Error: mkdocs not installed.\n"
            "  ./scripts/start_docs.sh   # creates .venv-docs and installs deps",
            file=sys.stderr,
        )
        return 1

    if not _free_port(args.port):
        print(f"Could not free port {args.port}. Run: lsof -i :{args.port}", file=sys.stderr)
        return 1

    addr_spec = f"{args.addr}:{args.port}"
    print(f"Starting MkDocs at http://{addr_spec} (repo: {_PROJECT_ROOT})")
    return subprocess.run(
        [sys.executable, "-m", "mkdocs", "serve", "-a", addr_spec],
        cwd=_PROJECT_ROOT,
    ).returncode


if __name__ == "__main__":
    sys.exit(main())
