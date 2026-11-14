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
// contact, as hc.at: the later of its claim loop's last call and the freshest
// heartbeat of its leased or running attempts (stale ones have stopped).
var hostLiveJoins = `CROSS JOIN LATERAL (SELECT ` + headSQL(`'prune:host:' || h.host_id`) + ` AS head OFFSET 0) ph
	  CROSS JOIN LATERAL (SELECT GREATEST(h.last_poll_at, (SELECT max(t.heartbeat_at) FROM agentrt.attempt t
	    WHERE t.host_id = h.host_id AND t.state IN ('leased','running'))) AS at OFFSET 0) hc`

// pruneOpenSQL is whether host h's prune round, opened within $3 seconds
// (PruneSilence), is not closed at now.
func pruneOpenSQL(now string) string {
	return `COALESCE(ph.head->'body'->>'prune_round' = 'opened' AND ` + now + ` - (ph.head->>'at')::timestamptz <= make_interval(secs => $3), false)`
}

// hostOnlineSQL is whether host h is alive at now: its contact within $1
// seconds (HostLostAfter), or its prune round open.
func hostOnlineSQL(now string) string {
	return `(COALESCE(` + now + ` - hc.at <= make_interval(secs => $1), false) OR ` + pruneOpenSQL(now) + `)`
}

// hostClaimingSQL is whether host h takes work at now: its claim loop's last
// call within $1 seconds, or its prune round open.
func hostClaimingSQL(now string) string {
	return `(COALESCE(` + now + ` - h.last_poll_at <= make_interval(secs => $1), false) OR ` + pruneOpenSQL(now) + `)`
}

// hostSilentSQL is whether host h is lost at now: not alive, and silent past
// $1 seconds counted from the later of its contact and $2 (when the platform
// could take agentd's calls again, as the reader saw it; NULL for none).
func hostSilentSQL(now string) string {
	return `(NOT ` + hostOnlineSQL(now) + `
	    AND COALESCE(` + now + ` - GREATEST(hc.at, $2::timestamptz) > make_interval(secs => $1), true))`
}

// hostClaimLostSQL is whether host h is known not to take work at now: not
// claiming, its claim loop silent past $1 seconds counted as hostSilentSQL
// counts. Neither this nor claiming: not known yet.
func hostClaimLostSQL(now string) string {
	return `(NOT ` + hostClaimingSQL(now) + `
	    AND COALESCE(` + now + ` - GREATEST(h.last_poll_at, $2::timestamptz) > make_interval(secs => $1), true))`
}

// livenessArgs are $1–$3 of the SQL above.
func livenessArgs(since time.Time) []any {
	return []any{HostLostAfter.Seconds(), nullTime(since), PruneSilence.Seconds()}
}

// HostsArgs are $1–$3 of HostsSQL for a reader that saw the platform take
// agentd's calls again at since (zero: no such moment).
func HostsArgs(since time.Time) []any { return livenessArgs(since) }

// nullTime is t, or SQL NULL for the zero time.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
