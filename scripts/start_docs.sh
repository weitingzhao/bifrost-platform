#!/usr/bin/env bash
# Start bifrost-platform MkDocs — fixed default http://127.0.0.1:8060
#
# Usage:
#   ./scripts/start_docs.sh
#
# Infra handbook (Goal, roadmap, migration): http://127.0.0.1:8050/
#   cd ../bifrost-trade-infra && ./scripts/start_docs.sh

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

VENV="$ROOT/.venv-docs"
PY="$VENV/bin/python"
PORT=8060

if [[ ! -x "$PY" ]]; then
  echo "Creating docs virtualenv at .venv-docs ..."
  python3 -m venv "$VENV"
fi

if ! "$PY" -c "import mkdocs" 2>/dev/null; then
  echo "Installing MkDocs dependencies ..."
  "$PY" -m pip install -U pip
  "$PY" -m pip install -r requirements-docs.txt
fi

exec "$PY" scripts/run_mkdocs.py -p "$PORT"
