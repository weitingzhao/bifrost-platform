#!/usr/bin/env python3
"""Layer 2 API probe drift scan (no LLM).

Probes platform-api L0 read routes and remediation runner health.
Compares HTTP status + minimal JSON shape against expectations.

Stdout: markdown report. Exit 0 if all probes pass, 1 if any failure.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.error
import urllib.request
from dataclasses import dataclass
from datetime import datetime
from typing import Any

DEFAULT_PLATFORM_API = "http://127.0.0.1:8780"
DEFAULT_RUNNER_URL = "http://127.0.0.1:8781"


@dataclass
class Probe:
    name: str
    url: str
    required_keys: tuple[str, ...] = ()
    expect_status: int = 200
    optional: bool = False  # warn only — e.g. remediation bridge when runner is off-cluster
    assert_field: tuple[str, str] | None = None  # (json_key, expected_value) — fail if mismatch


@dataclass
class ProbeResult:
    probe: Probe
    ok: bool
    status: int | None
    detail: str
    warning: bool = False


def build_probes(platform_base: str, runner_base: str) -> list[Probe]:
    base = platform_base.rstrip("/")
    return [
        Probe("platform-api /health", f"{base}/health"),
        Probe("GET /api/v1/context", f"{base}/api/v1/context", ("milestones",)),
        Probe("GET /api/v1/matrix", f"{base}/api/v1/matrix"),
        Probe("GET /api/v1/topology", f"{base}/api/v1/topology"),
        Probe("GET /api/v1/environments", f"{base}/api/v1/environments"),
        Probe("GET /api/v1/cluster", f"{base}/api/v1/cluster", optional=True),
        Probe(
            "cluster reachability (kubeconfig secret)",
            f"{base}/api/v1/cluster",
            assert_field=("reachability", "ok"),
            optional=False,
        ),
        Probe("GET /api/v1/cluster/nodes", f"{base}/api/v1/cluster/nodes", optional=True),
        Probe("GET /api/v1/mcp/tools", f"{base}/api/v1/mcp/tools"),
        Probe("GET /api/v1/mcp/status", f"{base}/api/v1/mcp/status"),
        Probe(
            "GET /api/v1/remediation/health",
            f"{base}/api/v1/remediation/health",
            ("status",),
            optional=True,
        ),
        Probe(
            "remediation-runner /health",
            f"{runner_base.rstrip('/')}/health",
            ("status", "service"),
        ),
    ]


def fetch_json(url: str, timeout: float = 8.0) -> tuple[int | None, Any | None, str]:
    req = urllib.request.Request(url, headers={"Accept": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            raw = resp.read().decode("utf-8", errors="replace")
            status = resp.status
            try:
                return status, json.loads(raw), ""
            except json.JSONDecodeError:
                return status, None, "response is not valid JSON"
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", errors="replace")[:200]
        return exc.code, None, f"HTTP {exc.code}: {body}"
    except urllib.error.URLError as exc:
        reason = getattr(exc, "reason", exc)
        return None, None, str(reason)
    except TimeoutError:
        return None, None, f"timeout after {timeout}s"
    except OSError as exc:
        return None, None, str(exc)


def run_probe(probe: Probe) -> ProbeResult:
    status, data, err = fetch_json(probe.url)
    if status is None:
        return ProbeResult(probe, False, None, err, probe.optional)
    if status != probe.expect_status:
        return ProbeResult(
            probe, False, status, err or f"expected HTTP {probe.expect_status}", probe.optional
        )
    if data is None:
        return ProbeResult(probe, False, status, err or "empty JSON body", probe.optional)
    for key in probe.required_keys:
        if key not in data:
            return ProbeResult(probe, False, status, f"missing JSON key `{key}`", probe.optional)
    if probe.assert_field is not None:
        key, expected = probe.assert_field
        actual = data.get(key) if isinstance(data, dict) else None
        if str(actual) != expected:
            return ProbeResult(
                probe,
                False,
                status,
                f"`{key}` is `{actual}`, expected `{expected}`",
                probe.optional,
            )
    return ProbeResult(probe, True, status, "ok", False)


def format_report(results: list[ProbeResult], platform_base: str, runner_base: str) -> str:
    hard_failures = [r for r in results if not r.ok and not r.warning]
    warnings = [r for r in results if not r.ok and r.warning]
    passes = [r for r in results if r.ok]
    stamp = datetime.now().isoformat(timespec="seconds")
    lines = [
        "## Layer 2 — API probe scan",
        f"- Platform API: `{platform_base}`",
        f"- Runner: `{runner_base}`",
        f"- Probed at: {stamp}",
        f"- Probes: {len(passes)} pass, {len(hard_failures)} fail, {len(warnings)} warn",
        "",
    ]
    if not hard_failures and not warnings:
        lines.append("All API probes passed.")
        return "\n".join(lines)

    if warnings:
        lines.append("### Warnings (optional probes)")
        for r in warnings:
            status = r.status if r.status is not None else "—"
            lines.append(f"- **{r.probe.name}** — HTTP {status} — {r.detail}")
        lines.append("")

    if hard_failures:
        lines.append("### Failures")
        for r in hard_failures:
            status = r.status if r.status is not None else "—"
            lines.append(f"- **{r.probe.name}** — HTTP {status} — {r.detail}")
    return "\n".join(lines)


def main() -> int:
    parser = argparse.ArgumentParser(description="Layer 2 API probe drift scan")
    parser.add_argument(
        "--platform-api",
        default=os.environ.get("PLATFORM_API_URL", DEFAULT_PLATFORM_API),
        help="platform-api base URL",
    )
    parser.add_argument(
        "--runner-url",
        default=os.environ.get("REMEDIATION_RUNNER_URL", DEFAULT_RUNNER_URL),
        help="remediation runner base URL",
    )
    args = parser.parse_args()

    probes = build_probes(args.platform_api, args.runner_url)
    results = [run_probe(p) for p in probes]
    print(format_report(results, args.platform_api, args.runner_url))
    return 1 if any(not r.ok and not r.warning for r in results) else 0


if __name__ == "__main__":
    sys.exit(main())
