#!/usr/bin/env bash
# Installs agentd as a launchd agent of the current user (design v5 §5.2).
# The Owner runs it on an agent host; it changes the host's standing
# configuration, so no agent runs it.
#
#   install-agentd.sh <agentd binary> <platform agentrt URL> <host id> <tools> <git base>
#
# The host's agentd token must already be in ~/.config/bifrost/agentd.token
# and the git token (optional) in ~/.config/bifrost/agentd-git.token, both
# mode 0600; this script only names those files.
set -euo pipefail

if [ "$#" -ne 5 ]; then
  echo "usage: $0 <agentd binary> <platform agentrt URL> <host id> <tools> <git base>" >&2
  exit 2
fi
bin=$1 platform=$2 host=$3 tools=$4 git_base=$5
here=$(cd "$(dirname "$0")" && pwd)
root="$HOME/agent-work/agentd"
token="$HOME/.config/bifrost/agentd.token"
git_token="$HOME/.config/bifrost/agentd-git.token"
plist="$HOME/Library/LaunchAgents/com.bifrost.agentd.plist"

[ -x "$bin" ] || { echo "not executable: $bin" >&2; exit 1; }
for f in "$token"; do
  [ -f "$f" ] || { echo "missing: $f" >&2; exit 1; }
  [ "$(stat -f %Lp "$f")" = 600 ] || { echo "$f must be mode 0600" >&2; exit 1; }
done
[ -f "$git_token" ] || git_token=""

mkdir -p "$root" "$(dirname "$plist")"
sed -e "s#__AGENTD__#$bin#g" -e "s#__HOME__#$HOME#g" -e "s#__ROOT__#$root#g" \
    -e "s#__PLATFORM__#$platform#g" -e "s#__HOST__#$host#g" -e "s#__TOKEN_FILE__#$token#g" \
    -e "s#__TOOLS__#$tools#g" -e "s#__GIT_BASE__#$git_base#g" -e "s#__GIT_TOKEN_FILE__#$git_token#g" \
    "$here/com.bifrost.agentd.plist.template" > "$plist"
plutil -lint "$plist"

launchctl bootout "gui/$(id -u)/com.bifrost.agentd" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$plist"
echo "agentd installed: $plist (log: $root/agentd.log)"
