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
guard_packages=
guard_run=
why='API_SCOPE_REQUEST=full'
if [[ ${API_SCOPE_REQUEST:-} != full ]]; then
  if go run ./cmd/testscope -module . "${1:-}" "${2:-}" > "$tmp/scope"; then
    while IFS='=' read -r key value; do
      case "$key" in scope) scope=$value ;; packages) packages=$value ;; store_run) store_run=$value ;; guard_packages) guard_packages=$value ;; guard_run) guard_run=$value ;; why) why=$value ;; esac
    done < "$tmp/scope"
  else
    why='testscope command failed'
  fi
fi
[[ $scope == incremental ]] || scope=full
if [[ -n ${API_SCOPE_RESULT:-} ]]; then printf '%s' "$scope" > "$API_SCOPE_RESULT"; fi
printf 'scope=%s\npackages=%s\nstore_run=%s\nguard_packages=%s\nguard_run=%s\nwhy=%s\n' "$scope" "$packages" "$store_run" "$guard_packages" "$guard_run" "$why"
status=0
run_tests() {
  go test -json -count=1 -timeout=1500s "$@" > "$tmp/part" 2>&1 || status=1
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
  if [[ $part != store && -n $guard_packages ]]; then
    read -r -a guards <<< "$guard_packages"
    run_tests "${guards[@]}" -run "$guard_run"
  fi
  if [[ $part != rest && -n $store ]]; then
    if [[ -n $store_run ]]; then run_tests "$store" -run "$store_run"; else run_tests "$store"; fi
  fi
fi
# A failed top-level test is run once more on its own (R5 phase 2, 2026-11-06: timing
# tests of untouched packages failed landings under CI load and sent good changes
# back). It passes only if every failed test passes the second time and no package
# failed without a failing test (build error, panic, timeout): those stay red.
if (( status != 0 )); then
  retry_plan="$(python3 - "$tmp/events" <<'PY'
import json, sys
tests, pkg_fail = {}, set()
for line in open(sys.argv[1]):
    try:
        e = json.loads(line)
    except ValueError:
        continue
    if e.get('Action') != 'fail':
        continue
    t = e.get('Test')
    if t:
        if '/' not in t:
            tests.setdefault(e['Package'], set()).add(t)
    else:
        pkg_fail.add(e['Package'])
if pkg_fail - set(tests) or not tests:
    print('none')
else:
    for p, ts in sorted(tests.items()):
        print(p + ' ^(' + '|'.join(sorted(ts)) + ')$')
PY
)"
  if [[ $retry_plan != none && -n $retry_plan ]]; then
    echo "==> retrying the failed tests once:"
    echo "$retry_plan"
    retry_status=0
    : > "$tmp/retry"
    while read -r pkg pattern; do
      go test -json -count=1 -timeout=1500s "$pkg" -run "$pattern" >> "$tmp/retry" 2>&1 || retry_status=1
    done <<< "$retry_plan"
    cat "$tmp/retry"
    if (( retry_status == 0 )); then
      echo "==> flaky: every failed test passed on its retry; the run passes"
      status=0
      # The first failures are kept in the log above; the result names none.
      : > "$tmp/events"
    else
      cat "$tmp/retry" >> "$tmp/events"
    fi
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
