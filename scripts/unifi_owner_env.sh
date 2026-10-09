#!/usr/bin/env bash
# Owner only. An Agent must not run the UniFi scripts.
#
# Source this file. unifi_owner_fill KEY [KEY...] exports each empty key from
# ~/.bifrost-owner/owner.env. An already-set variable wins. Values are never
# printed. Same precedence as bifrost-trade-infra/scripts/owner/owner-env.sh.
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  echo "source scripts/unifi_owner_env.sh; do not execute it" >&2
  exit 2
fi

unifi_owner_fill() {
  local file key line val
  file="${BIFROST_OWNER_ENV:-${HOME}/.bifrost-owner/owner.env}"
  for key in "$@"; do
    if [ -n "${!key-}" ]; then
      continue
    fi
    val=""
    if [ -f "$file" ]; then
      line="$(grep -E "^${key}=" "$file" | tail -1 || true)"
      val="${line#*=}"
      val="${val%\"}"
      val="${val#\"}"
      val="${val%\'}"
      val="${val#\'}"
    fi
    if [ -n "$val" ]; then
      printf -v "$key" '%s' "$val"
      export "$key"
    fi
  done
}
