#!/usr/bin/env bash
# Ratchet for the deploy_mac_mini.sh tool-smoke bearer (TD-207 follow-up).
# Uses a fabricated token. Asserts the value is not printed and 401 says
# "token rejected" instead of a JSON parse failure.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=tool_smoke.sh
source "${SCRIPT_DIR}/tool_smoke.sh"

TOKEN="fabricated-runner-token-t2"
WRONG="fabricated-runner-token-wrong"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "${WORKDIR}"' EXIT

python3 - "${WORKDIR}/server.py" <<'PY'
import sys
from pathlib import Path
Path(sys.argv[1]).write_text(r'''
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json

TOKEN = "fabricated-runner-token-t2"

class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path != "/smoke":
            self.send_response(404)
            self.end_headers()
            return
        auth = self.headers.get("Authorization", "")
        if auth != "Bearer " + TOKEN:
            self.send_response(401)
            self.end_headers()
            self.wfile.write(b"unauthorized")
            return
        body = json.dumps({"status": "pass", "checks": []}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, fmt, *args):
        return

if __name__ == "__main__":
    ThreadingHTTPServer(("127.0.0.1", int(__import__("sys").argv[1])), Handler).serve_forever()
''')
PY

PORT="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"
python3 "${WORKDIR}/server.py" "${PORT}" &
SERVER_PID=$!
trap 'kill "${SERVER_PID}" 2>/dev/null || true; rm -rf "${WORKDIR}"' EXIT
for _ in 1 2 3 4 5 6 7 8 9 10; do
  if curl -sf -o /dev/null --max-time 1 "http://127.0.0.1:${PORT}/smoke" 2>/dev/null; then
    break
  fi
  # 401 is expected without a token; any HTTP response means the server is up.
  code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 1 "http://127.0.0.1:${PORT}/smoke" || true)"
  if [[ "${code}" == "401" || "${code}" == "200" ]]; then
    break
  fi
  sleep 0.1
done

URL="http://127.0.0.1:${PORT}/smoke"
printf 'REMEDIATION_RUNNER_TOKEN=%s\n' "${WRONG}" > "${WORKDIR}/wrong.env"
printf 'REMEDIATION_RUNNER_TOKEN=%s\n' "${TOKEN}" > "${WORKDIR}/right.env"
: > "${WORKDIR}/empty.env"

rejected="$(tool_smoke_report "${URL}" "${WORKDIR}/wrong.env")"
printf '%s\n' "${rejected}"
printf '%s\n' "${rejected}" | grep -q "token rejected"
if printf '%s\n' "${rejected}" | grep -q "could not parse smoke results"; then
  echo "401 was reported as a parse failure" >&2
  exit 1
fi
if printf '%s\n' "${rejected}" | grep -q "${WRONG}"; then
  echo "tool smoke printed the token" >&2
  exit 1
fi
if printf '%s\n' "${rejected}" | grep -q "${TOKEN}"; then
  echo "tool smoke printed the server token" >&2
  exit 1
fi

passed="$(tool_smoke_report "${URL}" "${WORKDIR}/right.env")"
printf '%s\n' "${passed}"
printf '%s\n' "${passed}" | grep -q "All tool dry-run checks passed"
if printf '%s\n' "${passed}" | grep -q "${TOKEN}"; then
  echo "tool smoke printed the token on success" >&2
  exit 1
fi

missing="$(tool_smoke_report "${URL}" "${WORKDIR}/empty.env")"
printf '%s\n' "${missing}" | grep -q "REMEDIATION_RUNNER_TOKEN missing"

DEPLOY="${SCRIPT_DIR}/deploy_mac_mini.sh"
SMOKE_SECTION="$(awk '/==> Post-deploy tool smoke/,/==> Post-deploy Nous Hermes/' "${DEPLOY}")"
printf '%s\n' "${SMOKE_SECTION}" | grep -q "tool_smoke_report"
if printf '%s\n' "${SMOKE_SECTION}" | grep -q "curl -sf --max-time 30"; then
  echo "tool smoke still uses an anonymous curl" >&2
  exit 1
fi
if printf '%s\n' "${SMOKE_SECTION}" | grep -E -q "ALERT_RELAY|PEER_RELAY_URL"; then
  echo "tool smoke section mentions relay settings" >&2
  exit 1
fi

echo "tool_smoke_test ok"
