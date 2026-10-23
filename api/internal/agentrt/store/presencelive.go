package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// When a host counts as alive (review 526 A1, A1b). Its last contact is
// host.last_poll_at: written by each claim, and by each other call its agentd
// makes (TouchHost: an attempt's heartbeat or other action, asking for its
// prune list and reporting it), so a host whose claim loop is busy elsewhere
// but whose agentd talks to the platform is not silent. agentd deletes
// attempt branches between two claims, synchronously (api/cmd/agentd/main.go,
// api/internal/agentd/prune.go): a host handed branches to delete may then
// stay silent for agentd's whole prune budget, so a prune round opened by the
// list (OpenPruneRound) keeps it alive for PruneSilence, until it reports or
// claims again (ClosePruneRound). And a host's silence is counted from no
// earlier than the moment the reader started (since): a release of the
// platform stops it for tens of seconds, and a gap the platform made itself
// is not the host's.

// PruneSilence is how long a host handed attempt branches to delete may stay
// silent: agentd's prune budget, PruneWork (2 minutes) for asking and git and
// PruneReport (30 seconds) for its report (api/internal/agentd/prune.go),
// and 15 seconds of margin. TestPruneSilenceCoversAgentdsBudget holds it to
// those constants.
const PruneSilence = 2*time.Minute + 30*time.Second + 15*time.Second

// HostPruneStream holds a host's prune rounds: opened when its list hands out
// branches, closed when it reports or claims again.
func HostPruneStream(host string) string { return "prune:host:" + host }

// pruneHead is the LATERAL of host h's prune stream's head, as ph.head.
var pruneHead = `(SELECT ` + headSQL(`'prune:host:' || h.host_id`) + ` AS head OFFSET 0) ph`

// hostOnlineSQL is whether host h (an agentrt.host row, its prune stream's
// head joined as ph) is alive at now: its last contact within $1 seconds
// (HostLostAfter), or a prune round opened within $3 seconds (PruneSilence)
// and not closed.
func hostOnlineSQL(now string) string {
	return `(COALESCE(` + now + ` - h.last_poll_at <= make_interval(secs => $1), false)
	    OR COALESCE(ph.head->'body'->>'prune_round' = 'opened' AND ` + now + ` - (ph.head->>'at')::timestamptz <= make_interval(secs => $3), false))`
}

// hostSilentSQL is whether host h is lost at now: not alive, and silent past
// $1 seconds counted from the later of its last contact and $2 (the reader's
// start; NULL for none).
func hostSilentSQL(now string) string {
	return `(NOT ` + hostOnlineSQL(now) + `
	    AND COALESCE(` + now + ` - GREATEST(h.last_poll_at, $2::timestamptz) > make_interval(secs => $1), true))`
}

// livenessArgs are $1–$3 of the SQL above.
func livenessArgs(since time.Time) []any {
	return []any{HostLostAfter.Seconds(), nullTime(since), PruneSilence.Seconds()}
}

// LivenessArgs are $1–$3 of HostsSQL, for a reader started at since.
func LivenessArgs(since time.Time) []any { return livenessArgs(since) }

// nullTime is t, or SQL NULL for the zero time.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

// TouchHost records a call of host's agentd as its last contact. It waits for
// nothing: a host row another transaction holds (a claim of the host, which
// records the poll itself, or a control being set) is passed over.
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
