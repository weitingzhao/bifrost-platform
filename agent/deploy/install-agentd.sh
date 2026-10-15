#!/usr/bin/env bash
# Installs agentd as a launchd daemon that runs as its own system user on an
# agent host (design v5 §5.2; review 163 A8: that user has no MCP server, so
# the agent CLIs it runs load none). The Owner runs it with sudo; it changes
# the host's standing configuration, so no agent runs it.
#
#   sudo install-agentd.sh <user> <agentd binary> <platform agentrt URL> <host id> <tools> <git base> [<PATH>]
#   install-agentd.sh --render <plist> <user> <agentd binary> <platform agentrt URL> <host id> <tools> <git base> [<PATH>]
#
# --render only writes the plist (to check it, and for the tests); it needs
# no root. The user's agentd token must be in <config>/agentd.token and the
# git token, if any, in <config>/agentd-git.token: regular files the user
# owns, mode 0600, and so must cursor's API key, if any, in <config>/cursor-api.key
# (cursor's own login lives in a keychain a daemon cannot unlock). <config> is
# ~<user>/.config/bifrost and the workspaces and
# the log go to ~<user>/agent-work/agentd (AGENTD_CONFIG_DIR and
# AGENTD_ROOT_DIR override them). AGENTD_GIT_PUSH_BASE, when set, is where
# attempt branches go (<git base> is then where work is cloned from, a mirror
# that takes no push). <PATH> is where agentd finds git, codex and
# cursor-agent; by default ~<user>/.local/bin, then Homebrew's and the
# system's. Every value goes into the plist through plutil, one key at a
# time: no text substitution, so no character in a path or URL can change
# the file (review 164 B3). The new plist is checked before it replaces the
# old one.
set -euo pipefail

usage() {
  echo "usage: sudo $0 <user> <agentd binary> <platform agentrt URL> <host id> <tools> <git base> [<PATH>]" >&2
  echo "       $0 --render <plist> <same arguments>" >&2
  exit 2
}
die() {
  echo "install-agentd: $*" >&2
  exit 1
}

out=""
if [ "${1:-}" = "--render" ]; then
  [ "$#" -ge 2 ] || usage
  out=$2
  shift 2
fi
[ "$#" -eq 6 ] || [ "$#" -eq 7 ] || usage
user=$1 bin=$2 platform=$3 host=$4 tools=$5 git_base=$6

home=$(dscl . -read "/Users/$user" NFSHomeDirectory 2>/dev/null | sed -n 's/^NFSHomeDirectory: //p')
[ -n "$home" ] || die "no user $user"
path=${7:-"$home/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin"}
config=${AGENTD_CONFIG_DIR:-"$home/.config/bifrost"}
push_base=${AGENTD_GIT_PUSH_BASE:-}
root=${AGENTD_ROOT_DIR:-"$home/agent-work/agentd"}

# The binary by its absolute path: launchd starts it from another directory.
[ -f "$bin" ] && [ -x "$bin" ] || die "not an executable file: $bin"
bin="$(cd "$(dirname "$bin")" && pwd -P)/$(basename "$bin")"

# credential <file> <required|optional>: a regular file the user owns, mode
# 0600. An optional one that is not there returns 1.
credential() {
  if [ ! -e "$1" ] && [ ! -L "$1" ]; then
    [ "$2" = required ] && die "missing: $1"
    return 1
  fi
  { [ -f "$1" ] && [ ! -L "$1" ]; } || die "$1 is not a regular file"
  [ "$(stat -f %Su "$1")" = "$user" ] || die "$1 is not owned by $user"
  [ "$(stat -f %Lp "$1")" = 600 ] || die "$1 must be mode 0600"
}
token="$config/agentd.token"
credential "$token" required
git_token="$config/agentd-git.token"
credential "$git_token" optional || git_token=""
cursor_key="$config/cursor-api.key"
credential "$cursor_key" optional || cursor_key=""

label=com.bifrost.agentd
render() {
  local f=$1 kv
  plutil -create xml1 "$f"
  plutil -insert Label -string "$label" "$f"
  plutil -insert UserName -string "$user" "$f"
  plutil -insert InitGroups -bool true "$f"
  plutil -insert ProgramArguments -array "$f"
  plutil -insert ProgramArguments.0 -string "$bin" "$f"
  plutil -insert WorkingDirectory -string "$root" "$f"
  plutil -insert EnvironmentVariables -dictionary "$f"
  for kv in "PATH=$path" "HOME=$home" "USER=$user" "LOGNAME=$user" \
    "AGENTD_PLATFORM=$platform" "AGENTD_HOST=$host" "AGENTD_TOKEN_FILE=$token" "AGENTD_TOOLS=$tools" \
    "AGENTD_GIT_BASE=$git_base" "AGENTD_GIT_PUSH_BASE=$push_base" "AGENTD_GIT_TOKEN_FILE=$git_token" "AGENTD_CURSOR_KEY_FILE=$cursor_key" "AGENTD_ROOT=$root"; do
    plutil -insert "EnvironmentVariables.${kv%%=*}" -string "${kv#*=}" "$f"
  done
  plutil -insert RunAtLoad -bool true "$f"
  plutil -insert KeepAlive -bool true "$f"
  plutil -insert ThrottleInterval -integer 10 "$f"
  plutil -insert StandardOutPath -string "$root/agentd.log" "$f"
  plutil -insert StandardErrorPath -string "$root/agentd.log" "$f"
  plutil -lint "$f" >/dev/null
}

if [ -n "$out" ]; then
  tmp=$(mktemp "$(dirname "$out")/.agentd-plist.XXXXXX")
  trap 'rm -f "$tmp"' EXIT
  render "$tmp"
  mv -f "$tmp" "$out"
  trap - EXIT
  echo "rendered: $out"
  exit 0
fi

[ "$(id -u)" = 0 ] || die "run it with sudo (it installs a system launchd daemon that runs as $user)"
plist="/Library/LaunchDaemons/$label.plist"
sudo -u "$user" mkdir -p -m 700 "$root"
tmp=$(mktemp "/Library/LaunchDaemons/.$label.XXXXXX")
trap 'rm -f "$tmp"' EXIT
render "$tmp"
chown root:wheel "$tmp"
chmod 644 "$tmp"
launchctl bootout "system/$label" 2>/dev/null || true
mv -f "$tmp" "$plist"
trap - EXIT
launchctl bootstrap system "$plist"
echo "agentd installed: $plist, running as $user (log: $root/agentd.log)"
