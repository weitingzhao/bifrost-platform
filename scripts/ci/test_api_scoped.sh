#!/usr/bin/env bash
# This script and testscope must be executed from the trusted base checkout.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
for result_var in API_SCOPE_RESULT FAILED_TESTS_RESULT; do
  result_path=${!result_var:-}
  if [[ -n $result_path && $result_path != /* ]]; then printf -v "$result_var" '%s/%s' "$PWD" "$result_path"; fi
done
cd "$root/api"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
scope=full
packages=
store_run=
why='API_SCOPE_REQUEST=full'
if [[ ${API_SCOPE_REQUEST:-} != full ]]; then
  if go run ./cmd/testscope -module . "${1:-}" "${2:-}" > "$tmp/scope"; then
    while IFS='=' read -r key value; do
      case "$key" in scope) scope=$value ;; packages) packages=$value ;; store_run) store_run=$value ;; why) why=$value ;; esac
    done < "$tmp/scope"
  else
    why='testscope command failed'
  fi
fi
[[ $scope == incremental ]] || scope=full
if [[ -n ${API_SCOPE_RESULT:-} ]]; then printf '%s' "$scope" > "$API_SCOPE_RESULT"; fi
printf 'scope=%s\npackages=%s\nstore_run=%s\nwhy=%s\n' "$scope" "$packages" "$store_run" "$why"
status=0
run_tests() {
  go test -json "$@" > "$tmp/part" 2>&1 || status=1
  cat "$tmp/part"
  cat "$tmp/part" >> "$tmp/events"
}
: > "$tmp/events"
# API_SCOPED_PART (store or rest) runs one share of the selection, so CI runs the
# store package beside the rest as its full runs do (R5:5.7 CI time); unset, or
# anything else, runs both here, as before.
part=${API_SCOPED_PART:-all}
[[ $part == store || $part == rest ]] || part=all
if [[ $scope == full ]]; then
  case $part in
    store) run_tests ./internal/agentrt/store ;;
    rest)
      read -r -a others <<< "$(go list ./... | grep -v '/internal/agentrt/store$' | tr '\n' ' ')"
      run_tests "${others[@]}"
      ;;
    *) run_tests ./... ;;
  esac
else
  read -r -a selected <<< "$packages"
  rest=()
  store=
  for p in "${selected[@]}"; do
    if [[ $p == */internal/agentrt/store ]]; then store=$p; else rest+=("$p"); fi
  done
  if [[ $part != store ]] && (( ${#rest[@]} )); then run_tests "${rest[@]}"; fi
  if [[ $part != rest && -n $store ]]; then
    if [[ -n $store_run ]]; then run_tests "$store" -run "$store_run"; else run_tests "$store"; fi
  fi
fi
# Python is already used by repository CI tooling. Ignore non-JSON compiler output.
python3 - "$tmp/events" "${FAILED_TESTS_RESULT:-}" "$status" <<'PY'
import json, sys
failed = set()
for line in open(sys.argv[1]):
    try:
        event = json.loads(line)
    except ValueError:
        continue
    if event.get('Action') == 'fail' and event.get('Test'):
        failed.add(event['Package'] + '.' + event['Test'])
names = ','.join(sorted(failed))
if sys.argv[2]:
    with open(sys.argv[2], 'wb') as out:
        out.write(names.encode()[:3500])
print('Test summary: ' + ('FAILED' if sys.argv[3] != '0' else 'PASSED'))
print('Failing tests: ' + (names or '(none)'))
PY
exit "$status"
