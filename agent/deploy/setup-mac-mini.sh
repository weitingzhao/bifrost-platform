#!/usr/bin/env bash
# Backward-compatible — use: python scripts/run_agent.py deploy
ROOT="$(cd "$(dirname "$0")/../../" && pwd)"
exec python3 "${ROOT}/scripts/run_agent.py" deploy "$@"
