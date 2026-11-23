# Named-machine Owner command execution

Use `owner_run_command` with `params.host` to select an enrolled machine. The
Console shows the host and the exact command before asking for the existing
six-digit code. The cluster verifier issues a short-lived, single-claim
ticket. The worker on that machine polls outbound over HTTPS and reports
the result through the existing approval lease. Hostless cluster cards
continue to use the existing execution path.

## One-time enrollment

1. Bring up the platform and its existing verifier with the same dedicated
   ticket key. Only the platform approval service and the verifier receive
   that key. The verifier keeps its existing authenticator seed.
2. Provision one different executor credential per selected machine using
   the platform's existing auth mechanism, with principal name `host-<id>`
   and role `executor`. The machine receives only its own credential.
   Initial IDs are `work-mac`, `ops-mac-agent-01`, `ops-mac-agent-02` and
   `ubt-k3s-06`. Additional macOS and Linux machines use the same worker.
3. On each machine, run the checked worker installer once with the Owner
   selecting its execution account. It uses a system LaunchDaemon on macOS
   or a systemd service on Linux. It verifies the supplied release binary
   digest and installs the host's credential with mode 0600.

```text
enroll-machine.sh HOST_ID ACCOUNT HTTPS_ORIGIN BINARY SHA256 HOST_TOKEN_FILE
```

Check each newly enrolled host with a code-approved `hostname` card and
confirm its execution and result on the card before sending a real command.
The machine must be awake and able to reach the HTTPS platform origin. The
command has the permissions of the account selected during enrollment.
Commands that themselves require a terminal or an interactive key
unlock still need their application-specific noninteractive setup before
they can complete headlessly.
