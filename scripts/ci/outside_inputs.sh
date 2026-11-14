#!/usr/bin/env bash
# Prints "export NAME=value" lines: the git object id of everything a Go test may
# read outside the Go module (api/). api/internal/outsideinput explains why; its
# Env function and this script must give an input the same name.
#
#   eval "$(bash scripts/ci/outside_inputs.sh)"     before go test, in CI
#
# This repository: one variable per top-level entry but api, and
# BIFROST_INPUT_ROOT for all of them together. Each git checkout beside this
# one (bifrost-trade-infra, bifrost-ui): the whole tree, each top-level entry and
# each entry one level below. Prints nothing for a checkout it cannot read: the
# variable stays unset and Go caches as it did before.
#
# In CI since 2026-11-14 (task bifrost-ci-platform-test-api): the first run with
# it, ci-platform-kz4dm, exported 388 variables.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
clean() { printf '%s' "$1" | tr 'a-z' 'A-Z' | tr -c 'A-Z0-9' '_'; }
if tree=$(git -C "$root" ls-tree HEAD 2>/dev/null); then
  outside=$(printf '%s\n' "$tree" | awk -F'\t' '$2 != "api"')
  printf '%s\n' "$outside" | while IFS=$'\t' read -r meta name; do
    [ -n "$name" ] || continue
    printf 'export BIFROST_INPUT_%s=%s\n' "$(clean "$name")" "${meta##* }"
  done
  printf 'export BIFROST_INPUT_ROOT=%s\n' "$(printf '%s\n' "$outside" | git hash-object --stdin)"
fi
for repo in "$root"/../*/; do
  repo=${repo%/}
  [ "$(cd "$repo" && pwd)" != "$root" ] || continue
  top=$(git -C "$repo" rev-parse --show-toplevel 2>/dev/null) || continue
  [ "$top" = "$(cd "$repo" && pwd -P)" ] || [ "$top" = "$(cd "$repo" && pwd)" ] || continue
  name=$(clean "$(basename "$repo")")
  whole=$(git -C "$repo" rev-parse 'HEAD^{tree}' 2>/dev/null) || continue
  printf 'export BIFROST_INPUT_%s__=%s\n' "$name" "$whole"
  git -C "$repo" ls-tree HEAD | while IFS=$'\t' read -r meta entry; do
    printf 'export BIFROST_INPUT_%s__%s=%s\n' "$name" "$(clean "$entry")" "${meta##* }"
    case "$meta" in *" tree "*) ;; *) continue ;; esac
    git -C "$repo" ls-tree HEAD "$entry/" | while IFS=$'\t' read -r meta2 entry2; do
      printf 'export BIFROST_INPUT_%s__%s=%s\n' "$name" "$(clean "$entry2")" "${meta2##* }"
    done
  done
done
