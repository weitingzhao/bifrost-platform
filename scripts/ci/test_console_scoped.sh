#!/usr/bin/env bash
# Run from the trusted base checkout, like test_api_scoped.sh.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
if [[ -n ${CONSOLE_CACHE_DIR:-} ]]; then
  mkdir -p "$CONSOLE_CACHE_DIR"
  export CONSOLE_CACHE_DIR=$(cd "$CONSOLE_CACHE_DIR" && pwd)
fi
cd "$root/api"
tmp=$(mktemp -d)
config_tmp=
trap 'rm -rf "$tmp"; if [[ -n $config_tmp ]]; then rm -rf "$config_tmp"; fi' EXIT
scope=full
related=
tests=
lint=
build=true
why='CONSOLE_SCOPE_REQUEST=full'
if [[ ${CONSOLE_SCOPE_REQUEST:-} != full ]]; then
  if go run ./cmd/testscope -module . -console "${1:-}" "${2:-}" > "$tmp/scope"; then
    while IFS='=' read -r key value; do
      case "$key" in
        console_scope) scope=$value ;; console_related) related=$value ;;
        console_tests) tests=$value ;; console_lint) lint=$value ;;
        console_build) build=$value ;; why) why=$value ;;
      esac
    done < "$tmp/scope"
  else
    why='testscope command failed'
  fi
fi
case "$scope" in full|incremental|none) ;; *) scope=full; why='invalid selector result' ;; esac
if [[ $scope == full ]]; then build=true; fi
printf 'console_scope=%s\nconsole_related=%s\nconsole_tests=%s\nconsole_lint=%s\nconsole_build=%s\nwhy=%s\n' "$scope" "$related" "$tests" "$lint" "$build" "$why"
[[ $scope != none ]] || exit 0
cd "$root"
type_args=()
lint_cache=(--cache --cache-location "$root/console/node_modules/.cache/eslint/")
vitest_args=()
vite_args=()
if [[ -n ${CONSOLE_CACHE_DIR:-} ]]; then
  type_args=(--incremental --tsBuildInfoFile "$CONSOLE_CACHE_DIR/type-check.tsbuildinfo")
  lint_cache=(--cache --cache-location "$CONSOLE_CACHE_DIR/eslint/")
  # Vite and Vitest expose cacheDir through configuration, not CLI flags.
  # Pass temporary configuration wrappers via --config; never edit repo configs.
  config_tmp=$(mktemp -d "$root/console/.scoped-config.XXXXXX")
  for tool in vite vitest; do
    cat > "$config_tmp/$tool.mjs" <<JS
import original from '../$tool.config.ts'
export default { ...original, cacheDir: process.env.CONSOLE_CACHE_DIR + '/$tool' }
JS
  done
  vitest_args=(--config "$config_tmp/vitest.mjs")
  vite_args=(--config "$config_tmp/vite.mjs")
fi
npm --prefix console run type-check -- "${type_args[@]}"
cd "$root/console"
if [[ $scope == full ]]; then
  node_modules/.bin/eslint "${lint_cache[@]}" src
  node_modules/.bin/vitest run "${vitest_args[@]}"
else
  read -r -a lint_files <<< "$lint"
  read -r -a related_files <<< "$related"
  read -r -a test_files <<< "$tests"
  # Selector paths are relative to the repository; tools run in console/.
  for i in "${!lint_files[@]}"; do lint_files[$i]=${lint_files[$i]#console/}; done
  for i in "${!related_files[@]}"; do related_files[$i]=${related_files[$i]#console/}; done
  for i in "${!test_files[@]}"; do test_files[$i]=${test_files[$i]#console/}; done
  if (( ${#lint_files[@]} )); then node_modules/.bin/eslint "${lint_cache[@]}" "${lint_files[@]}"; fi
  if (( ${#related_files[@]} )); then node_modules/.bin/vitest related --run --passWithNoTests "${vitest_args[@]}" "${related_files[@]}"; fi
  if (( ${#test_files[@]} )); then node_modules/.bin/vitest run "${vitest_args[@]}" "${test_files[@]}"; fi
fi
cd "$root"
if [[ $build == true ]]; then npm --prefix console run build -- "${vite_args[@]}"; fi
