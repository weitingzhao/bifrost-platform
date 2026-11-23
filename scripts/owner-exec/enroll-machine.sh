#!/usr/bin/env bash
# One-time Owner enrollment of this machine. Uses an already provisioned
# machine credential; the authenticator seed stays in the code verifier.
set -euo pipefail
umask 077
usage() {
  echo 'usage: enroll-machine.sh HOST_ID ACCOUNT HTTPS_ORIGIN BINARY SHA256 HOST_TOKEN_FILE' >&2
  exit 2
}
[ "$#" -eq 6 ] || usage
host=$1 account=$2 origin=$3 binary=$4 expected=$5 token=$6
[[ $host =~ ^[a-z][a-z0-9-]{0,30}$ ]] || usage
[[ $account =~ ^[a-z_][a-z0-9_-]*$ ]] || usage
[[ $origin =~ ^https://[a-zA-Z0-9.-]+(:[0-9]+)?$ ]] || usage
[[ $expected =~ ^[0-9a-f]{64}$ ]] || usage
[ "$(id -u)" = 0 ] || { echo 'Run as Owner with sudo on the specified machine.' >&2; exit 2; }
[ -f "$binary" ] && [ -s "$token" ] && [ ! -L "$token" ] || usage

os=$(uname -s)
case "$os" in
  Darwin)
    actual=$(shasum -a 256 "$binary" | awk '{print $1}')
    home=$(dscl . -read "/Users/$account" NFSHomeDirectory | sed 's/^NFSHomeDirectory: //')
    root=/var/db/bifrost-owner-exec-host
    service="/Library/LaunchDaemons/com.bifrost.owner-exec-host.$host.plist"
    ;;
  Linux)
    actual=$(sha256sum "$binary" | awk '{print $1}')
    home=$(getent passwd "$account" | cut -d: -f6)
    root=/var/lib/bifrost-owner-exec-host
    service="/etc/systemd/system/bifrost-owner-exec-host-$host.service"
    ;;
  *) echo "Unsupported operating system: $os" >&2; exit 2 ;;
esac
[ "$actual" = "$expected" ] || { echo 'The worker digest does not match.' >&2; exit 2; }
[[ $home =~ ^/[-a-zA-Z0-9_./]+$ ]] && [ -d "$home" ] || usage
group=$(id -gn "$account")
[ ! -e "$service" ] || { echo 'This host is already enrolled.' >&2; exit 2; }
profile="$root/$host"
execpath="$profile/owner-exec"
cred="$profile/host-token"
install -d -o root -m 0755 "$root" "$profile"
install -o root -m 0755 "$binary" "$execpath"
install -o "$account" -g "$group" -m 0600 "$token" "$cred"

if [ "$os" = Darwin ]; then
  label="com.bifrost.owner-exec-host.$host"
  /usr/bin/python3 - "$service" "$label" "$account" "$home" "$origin" "$host" "$execpath" "$cred" <<'PY'
import plistlib
import sys

file, label, account, home, origin, host, exe, token = sys.argv[1:]
with open(file, "xb") as f:
    plistlib.dump({
        "Label": label,
        "ProgramArguments": [exe, "host"],
        "UserName": account,
        "RunAtLoad": True,
        "KeepAlive": {"SuccessfulExit": False},
        "ThrottleInterval": 15,
        "EnvironmentVariables": {
            "HOME": home, "PLATFORM_API_URL": origin, "OWNER_EXEC_HOST": host,
            "OWNER_EXEC_HOST_TOKEN_FILE": token,
        },
    }, f)
PY
  chmod 0644 "$service"
  plutil -lint "$service"
  launchctl bootstrap system "$service"
else
  cat > "$service" <<EOF
[Unit]
Description=Bifrost Owner approved command worker ($host)
Wants=network-online.target
After=network-online.target

[Service]
User=$account
Environment=HOME=$home
Environment=PLATFORM_API_URL=$origin
Environment=OWNER_EXEC_HOST=$host
Environment=OWNER_EXEC_HOST_TOKEN_FILE=$cred
ExecStart=$execpath host
Restart=on-failure
RestartSec=15
UMask=0077

[Install]
WantedBy=multi-user.target
EOF
  chmod 0644 "$service"
  systemd-analyze verify "$service"
  systemctl daemon-reload
  systemctl enable --now "bifrost-owner-exec-host-$host.service"
fi
printf 'Enrolled %s under %s. Verify with a code-approved hostname card.\n' "$host" "$account"
