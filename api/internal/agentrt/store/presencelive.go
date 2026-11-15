package store

import (
	"time"
)

// When a host counts as alive, and when as taking work (review 526 A1, A1b;
// review 527 A3). Two facts are kept apart. last_poll_at is the last call of
// agentd's claim loop: each claim, and asking for and reporting its prune
// list, which that loop does between two claims (TouchHost); a host takes
// work (claiming) while that is recent. Its contact is the later of that and
// the freshest heartbeat of its leased or running attempts (hostLiveJoins),
// which their own goroutines send: a host is alive (online) while its
// contact is recent, so a host whose claim loop has stopped while an attempt
// still reports is online and takes no work, and its executors are not
// available. agentd deletes attempt branches between two claims,
// synchronously (api/cmd/agentd/main.go, api/internal/agentd/prune.go): a
// host handed branches to delete may then stay silent for agentd's whole
// prune budget, so a prune round opened by the list (OpenPruneRound) keeps it
// alive and taking work for PruneSilence, until it reports or claims again
// (ClosePruneRound). And a host's silence is counted from no earlier than the
// moment the platform could take agentd's calls again, as the reader saw it
// (since; review 528 A1b): for the API, the moment its runtime routes became
// ready; for the controller, the later of its own start and the moment its
// probe of the API saw the platform take calls again (controller/presence.go).
// A release or an outage of the platform stops it for as long as it lasts,
// and a gap the platform made itself is not the host's.

// PruneSilence is how long a host handed attempt branches to delete may stay
// silent: agentd's prune budget, PruneWork (2 minutes) for asking and git and
// PruneReport (30 seconds) for its report (api/internal/agentd/prune.go),
// and 15 seconds of margin. TestPruneSilenceCoversAgentdsBudget holds it to
// those constants.
const PruneSilence = 2*time.Minute + 30*time.Second + 15*time.Second

// hostLiveJoins join to host h its prune stream's head, as ph.head, and its
// contact, as hc.at: its claim loop's last call (no attempt runs any more
// to heartbeat for it).
var hostLiveJoins = `CROSS JOIN LATERAL (SELECT ` + headSQL(`'prune:host:' || h.host_id`) + ` AS head OFFSET 0) ph
	  CROSS JOIN LATERAL (SELECT h.last_poll_at AS at OFFSET 0) hc`

// nullTime is t, or SQL NULL for the zero time.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
