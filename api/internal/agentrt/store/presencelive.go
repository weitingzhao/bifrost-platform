package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
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
// moment the platform could take agentd's calls again (review 527 A1b): the
// head of PlatformStream, which the API writes when its runtime database
// connects (up) and when it closes on shutdown (down), and, for the
// controller, its own start (since). A release of the platform stops it for
// tens of seconds, and a gap the platform made itself is not the host's.

// PruneSilence is how long a host handed attempt branches to delete may stay
// silent: agentd's prune budget, PruneWork (2 minutes) for asking and git and
// PruneReport (30 seconds) for its report (api/internal/agentd/prune.go),
// and 15 seconds of margin. TestPruneSilenceCoversAgentdsBudget holds it to
// those constants.
const PruneSilence = 2*time.Minute + 30*time.Second + 15*time.Second

// HostPruneStream holds a host's prune rounds: opened when its list hands out
// branches, closed when it reports or claims again.
func HostPruneStream(host string) string { return "prune:host:" + host }

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

// platformBackSQL is when, at now, the platform could last take agentd's
// calls again: the head of PlatformStream, up at its moment; down, now (the
// API is not taking calls: no host's silence counts), for at most
// PlatformDownLimit after it; NULL with no head.
func platformBackSQL(now string) string {
	return `(SELECT CASE WHEN p.head->'body'->>'platform_api' = 'down'
	    THEN LEAST(` + now + `, (p.head->>'at')::timestamptz + make_interval(secs => ` + strconv.Itoa(int(PlatformDownLimit/time.Second)) + `))
	    ELSE (p.head->>'at')::timestamptz END FROM (SELECT ` + headSQL(`'`+PlatformStream+`'`) + ` AS head) p)`
}

// hostSilentSQL is whether host h is lost at now: not alive, and silent past
// $1 seconds counted from the latest of its contact, $2 (the reader's start;
// NULL for none) and the platform's coming back.
func hostSilentSQL(now string) string {
	return `(NOT ` + hostOnlineSQL(now) + `
	    AND COALESCE(` + now + ` - GREATEST(hc.at, $2::timestamptz, ` + platformBackSQL(now) + `) > make_interval(secs => $1), true))`
}

// hostClaimLostSQL is whether host h is known not to take work at now: not
// claiming, its claim loop silent past $1 seconds counted as hostSilentSQL
// counts. Neither this nor claiming: not known yet.
func hostClaimLostSQL(now string) string {
	return `(NOT ` + hostClaimingSQL(now) + `
	    AND COALESCE(` + now + ` - GREATEST(h.last_poll_at, $2::timestamptz, ` + platformBackSQL(now) + `) > make_interval(secs => $1), true))`
}

// PlatformStream holds when the platform could take agentd's calls: a note
// when an API's runtime database connected (platform_api up) and one when
// it closed on that API's shutdown (down), the API's instance in its text.
// Its head is the platform's recovery point; nothing else is written there.
const PlatformStream = "presence:platform"

// PlatformDownLimit bounds how long a down head holds every host's silence:
// an API gone down that never came back holds it no longer.
const PlatformDownLimit = 10 * time.Minute

func platformUpText(instance string) string {
	return "Platform API " + instance + " can take agentd's calls: its runtime database connected."
}

// MarkPlatformUp records that instance, an API of the platform, can take
// agentd's calls from now: each host's silence counts from no earlier.
func MarkPlatformUp(ctx context.Context, db DB, instance string) error {
	return InTx(ctx, db, func(tx pgx.Tx) error {
		_, err := Append(ctx, tx, Event{Stream: PlatformStream, Type: "note", Actor: "controller",
			Body: map[string]any{"platform_api": "up", "text": platformUpText(instance)}})
		return err
	})
}

// MarkPlatformDown records that instance stops taking agentd's calls (its
// shutdown), when the platform's latest record is instance's own coming up:
// an API that a newer one already replaced (a rolling update starts the new
// one first) records nothing. It reports whether it recorded.
func MarkPlatformDown(ctx context.Context, db DB, instance string) (bool, error) {
	did := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		did = false
		if err := lockStream(ctx, tx, PlatformStream); err != nil {
			return err
		}
		var head []byte
		if err := tx.QueryRow(ctx, `SELECT `+headSQL("$1::text"), PlatformStream).Scan(&head); err != nil || head == nil {
			return err
		}
		var h struct {
			Body struct {
				Up   string `json:"platform_api"`
				Text string `json:"text"`
			} `json:"body"`
		}
		if err := json.Unmarshal(head, &h); err != nil || h.Body.Up != "up" || h.Body.Text != platformUpText(instance) {
			return err
		}
		_, err := Append(ctx, tx, Event{Stream: PlatformStream, Type: "note", Actor: "controller", Body: map[string]any{"platform_api": "down",
			"text": "Platform API " + instance + " stopped taking agentd's calls: it shut down. No host's silence counts until an API is up again."}})
		did = err == nil
		return err
	})
	return did && err == nil, err
}

// livenessArgs are $1–$3 of the SQL above.
func livenessArgs(since time.Time) []any {
	return []any{HostLostAfter.Seconds(), nullTime(since), PruneSilence.Seconds()}
}

// HostsArgs are $1–$3 of HostsSQL for a reader with no start of its own
// (the API: the platform's coming back is its own).
func HostsArgs() []any { return livenessArgs(time.Time{}) }

// nullTime is t, or SQL NULL for the zero time.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

// TouchHost records a call of host's agentd claim loop other than a claim
// (asking for and reporting its prune list) as its last poll. It waits for
// no row: a host row another transaction holds (a claim of the host, which
// records the poll itself, or a control being set) is passed over. An
// attempt's calls are not recorded here: their heartbeats are the host's
// contact as they are (hostLiveJoins).
func TouchHost(ctx context.Context, db DB, host string) error {
	return InTx(ctx, db, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE agentrt.host SET last_poll_at = clock_timestamp()
		  WHERE host_id = (SELECT host_id FROM agentrt.host WHERE host_id = $1 FOR NO KEY UPDATE SKIP LOCKED)`, host)
		return err
	})
}

// pruneRound reads the head of host's prune stream: whether a round is open.
func pruneRound(ctx context.Context, tx pgx.Tx, host string) (bool, error) {
	var head []byte
	if err := tx.QueryRow(ctx, `SELECT `+headSQL("$1::text"), HostPruneStream(host)).Scan(&head); err != nil || head == nil {
		return false, err
	}
	var h struct {
		Body struct {
			Round string `json:"prune_round"`
		} `json:"body"`
	}
	return h.Body.Round == "opened", json.Unmarshal(head, &h)
}

// OpenPruneRound records that host was handed n attempt branches to delete:
// it may be silent for PruneSilence from now.
func OpenPruneRound(ctx context.Context, db DB, host string, n int) error {
	return InTx(ctx, db, func(tx pgx.Tx) error {
		_, err := Append(ctx, tx, Event{Stream: HostPruneStream(host), Type: "note", Actor: "controller", Body: map[string]any{
			"prune_round": "opened", "text": fmt.Sprintf("Host %s was handed %s to delete; it may be silent for up to %s while it deletes them.",
				host, plural(n, "1 attempt branch", strconv.Itoa(n)+" attempt branches"), PruneSilence)}})
		return err
	})
}

// ClosePruneRound records that host's open prune round ended (why: it
// reported, or it claims again); nothing when none is open. The stream is
// locked before its head is read again, so a round closes once.
func ClosePruneRound(ctx context.Context, db DB, host, why string) error {
	return InTx(ctx, db, func(tx pgx.Tx) error {
		if open, err := pruneRound(ctx, tx, host); err != nil || !open {
			return err
		}
		if err := lockStream(ctx, tx, HostPruneStream(host)); err != nil {
			return err
		}
		if open, err := pruneRound(ctx, tx, host); err != nil || !open {
			return err
		}
		_, err := Append(ctx, tx, Event{Stream: HostPruneStream(host), Type: "note", Actor: "controller",
			Body: map[string]any{"prune_round": "closed", "text": "Host " + host + "'s prune round ended: " + why + "."}})
		return err
	})
}
