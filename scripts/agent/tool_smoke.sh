#!/usr/bin/env bash
# Post-deploy GET /smoke for the remediation runner.
# Reads REMEDIATION_RUNNER_TOKEN from an env file and sends it as a bearer.
# Never prints the token. HTTP 401 is reported as "token rejected".
# Sourced by deploy_mac_mini.sh. Does not touch ALERT_RELAY or PEER_RELAY_URL.

tool_smoke_read_token() {
  local env_file="$1"
  local line token=""
  [[ -f "${env_file}" ]] || return 1
  while IFS= read -r line || [[ -n "${line}" ]]; do
    case "${line}" in
      REMEDIATION_RUNNER_TOKEN=*)
        token="${line#REMEDIATION_RUNNER_TOKEN=}"
        token="${token%$'\r'}"
        ;;
    esac
  done < "${env_file}"
  token="${token#\"}"
  token="${token%\"}"
  token="${token#\'}"
  token="${token%\'}"
  [[ -n "${token}" ]] || return 1
  printf '%s' "${token}"
}

tool_smoke_report() {
  local url="$1"
  local env_file="$2"
  local token http_code body_file
  if ! token="$(tool_smoke_read_token "${env_file}")"; then
    echo "  ⚠ REMEDIATION_RUNNER_TOKEN missing in ${env_file} (non-blocking)"
    return 0
  fi
  body_file="$(mktemp)"
  http_code="$(
    curl -sS -o "${body_file}" -w '%{http_code}' --max-time 30 \
      -H "Authorization: Bearer ${token}" \
      "${url}" 2>/dev/null || true
  )"
  if [[ -z "${http_code}" ]]; then
    http_code="000"
  fi
  if [[ "${http_code}" == "401" ]]; then
    echo "  ⚠ token rejected"
    rm -f "${body_file}"
    return 0
  fi
  if [[ "${http_code}" != "200" ]]; then
    echo "  ⚠ tool smoke HTTP ${http_code} (non-blocking)"
    rm -f "${body_file}"
    return 0
  fi
  local smoke_status
  smoke_status="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("status","unknown"))' "${body_file}" 2>/dev/null || echo 'unknown')"
  if [[ "${smoke_status}" == "pass" ]]; then
    echo "  ✓ All tool dry-run checks passed"
  else
    echo "  ⚠ Some checks failed (non-blocking):"
    python3 -c '
import json, sys
data = json.load(open(sys.argv[1]))
for c in data.get("checks", []):
    mark = "✓" if c.get("status") == "pass" else "✗"
    detail = c.get("detail") or ""
    suffix = f" — {detail}" if detail else ""
    print(f"    {mark} {c.get(\"label\",\"?\")}{suffix}")
' "${body_file}" 2>/dev/null || echo "    (could not parse smoke results)"
  fi
  rm -f "${body_file}"
  return 0
}
