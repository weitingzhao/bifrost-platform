package controller

import (
	"time"
)

// PresenceEvery is how often the controller judges the hosts and executors
// (W-31 round 3, 3.5; CUT-online-status-2026-10-23 option A): a host is
// offline 30 seconds after its last poll (store.HostLostAfter), so a host
// that stops is recorded and told within 35 seconds, and the Console, which
// reads every 5 seconds, says it within 40.
const PresenceEvery = 5 * time.Second

// PlatformProbeLimit bounds a pass's asking whether the platform takes
// agentd's calls; no answer within it is no.
const PlatformProbeLimit = 3 * time.Second

// UnjudgedAfter is how long the platform may be seen not taking agentd's
// calls before the controller says host presence cannot be judged.
const UnjudgedAfter = 2 * time.Minute
