#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir "$tmp/bin"
cat > "$tmp/bin/go" <<'GO'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$CALLS"
# A retry would pass, exposing any attempt to rescue the failed step.
if [[ $(wc -l < "$CALLS") -gt 1 ]]; then exit 0; fi
printf '%s\n' '{"Action":"fail","Package":"example/api","Test":"TestFailure"}' '{"Action":"fail","Package":"example/api"}'
exit 1
GO
chmod +x "$tmp/bin/go"
status=0
PATH="$tmp/bin:$PATH" CALLS="$tmp/calls" API_SCOPE_REQUEST=full API_SCOPED_PART=all \
  API_SCOPE_RESULT="$tmp/scope" FAILED_TESTS_RESULT="$tmp/failed" \
  bash "$root/scripts/ci/test_api_scoped.sh" > "$tmp/output" 2>&1 || status=$?
[[ $status == 1 ]]
[[ $(wc -l < "$tmp/calls") == 1 ]]
[[ $(cat "$tmp/failed") == example/api.TestFailure ]]
grep -q 'Test summary: FAILED' "$tmp/output"
echo 'test_api_scoped: failing tests fail without retry'
