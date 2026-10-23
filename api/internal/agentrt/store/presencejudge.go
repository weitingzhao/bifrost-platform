package store

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// When host presence cannot be judged, or its judgment is deferred (the
// coordinating thread on reviews 528 and 530: a probe that fails, a long
// outage, or an outage the probe keeps seeing every few passes is never
// silent). Each is a switch with a stream of its own: a policy_alert when it
// turns on, a note when it turns off, each told once to the Owner in his
// conversation with the chief of staff. The stream holds nothing else: its
// head is the state, read the bounded way (headSQL). GET /progress says each
// (presence_unjudged, presence_deferred), and the Console with them.
//
// Unjudged: the controller judges hosts only while the platform takes
// agentd's calls (its probe says so, or some host claimed within
// HostLostAfter); while it cannot tell for longer than a while, nothing is
// judged.
//
// Deferred: the hosts are judged, but a host's silence counts from no
// earlier than the platform's last recovery, and when the probe keeps seeing
// outages every few passes (with several API instances its sampling of the
// Service is probabilistic: it may land on instances that are not ready
// while another serves), each recovery restarts the hosts' 30 s; a host
// silent for longer than PresenceDeferredAfter that is still not judged lost
// for that reason makes the judgment deferred.

// PresenceStream holds whether host presence can be judged; DeferredStream
// whether its judgment is deferred.
const (
	PresenceStream = "presence:judging"
	DeferredStream = "presence:deferred"
)

// PresenceDeferredAfter is how long a host may be silent, not judged lost
// because the platform's recoveries keep moving its floor, before the
// judgment is said to be deferred.
const PresenceDeferredAfter = 2 * time.Minute

// The alerts' phrases.
const (
	presenceUnjudgedPhrase = "host presence cannot be judged"
	presenceDeferredPhrase = "host presence judgment is deferred"
)

// presenceSwitch is one of the two switches: its stream, its rule and
// phrase, and what is told when it turns on (an alert) and off (a note).
type presenceSwitch struct {
	stream, rule, phrase string
	on, off, offNote     string // after "From the platform, not the chief of staff: "; offNote is the stream's note
}

var (
	unjudgedSwitch = presenceSwitch{stream: PresenceStream, rule: "presence_unjudged", phrase: presenceUnjudgedPhrase,
		on:  "host presence cannot be judged: %s No host is reported offline and no executor unavailable until it can be; the platform says so here when it can.",
		off: "host presence can be judged again: %s", offNote: "Host presence can be judged again: %s"}
	deferredSwitch = presenceSwitch{stream: DeferredStream, rule: "presence_deferred", phrase: presenceDeferredPhrase,
		on:  "host presence judgment is deferred: %s The platform says so here when the hosts are judged as usual again.",
		off: "host presence is judged as usual again: %s", offNote: "Host presence is judged as usual again: %s"}
)

// LatestClaim is the last call of any host's claim loop (the latest
// last_poll_at), zero with none: a claim within HostLostAfter is evidence the
// platform takes agentd's calls.
func LatestClaim(ctx context.Context, db DB) (time.Time, error) {
	var at *time.Time
	err := inPresenceTx(ctx, db, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT max(last_poll_at) FROM agentrt.host`).Scan(&at)
	})
	if err != nil || at == nil {
		return time.Time{}, err
	}
	return *at, nil
}

// presenceRollbackLimit bounds the rollback of a presence transaction that
// failed (review 530 B-1): it runs after the pass's own deadline may have
// passed, and a rollback that gets no answer within it is given up, its
// connection closed by pgx, which discards a connection an operation's
// context cut. No goroutine is left waiting on it.
const presenceRollbackLimit = 2 * time.Second

// inPresenceTx is InTx for the presence pass: everything within ctx, and the
// rollback after an error within presenceRollbackLimit, so the pass returns
// within its deadline and that limit whatever the database answers.
func inPresenceTx(ctx context.Context, db DB, fn func(pgx.Tx) error) error {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), presenceRollbackLimit)
		defer cancel()
		_ = tx.Rollback(rctx)
	}()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// switchOpen reads the head of a switch's stream: an open alert, with when it
// was recorded and its text.
func switchOpen(ctx context.Context, tx pgx.Tx, stream string) (bool, time.Time, string, error) {
	var head []byte
	if err := tx.QueryRow(ctx, `SELECT `+headSQL("$1::text"), stream).Scan(&head); err != nil || head == nil {
		return false, time.Time{}, "", err
	}
	var h struct {
		Type string    `json:"type"`
		At   time.Time `json:"at"`
		Body struct {
			Artifact string `json:"artifact_id"`
		} `json:"body"`
	}
	if err := json.Unmarshal(head, &h); err != nil || h.Type != "policy_alert" {
		return false, time.Time{}, "", err
	}
	var text []byte
	if err := tx.QueryRow(ctx, `SELECT body FROM agentrt.artifact_blob WHERE artifact_id = $1`, h.Body.Artifact).Scan(&text); err != nil {
		return false, time.Time{}, "", err
	}
	return true, h.At, string(text), nil
}

// switchView is GET /progress's view of a switch: since when it is on and
// why, nil while it is off.
func switchView(ctx context.Context, tx pgx.Tx, stream string) (map[string]any, error) {
	open, at, why, err := switchOpen(ctx, tx, stream)
	if err != nil || !open {
		return nil, err
	}
	return map[string]any{"since": at.UTC().Format(time.RFC3339Nano), "why": why}, nil
}

// unjudgedView and deferredView are GET /progress's presence_unjudged and
// presence_deferred.
func unjudgedView(ctx context.Context, tx pgx.Tx) (map[string]any, error) {
	return switchView(ctx, tx, PresenceStream)
}

func deferredView(ctx context.Context, tx pgx.Tx) (map[string]any, error) {
	return switchView(ctx, tx, DeferredStream)
}

// PresenceUnjudged records that host presence cannot be judged, why, and
// tells the Owner, unless that is what PresenceStream already says. It
// reports whether it recorded.
func PresenceUnjudged(ctx context.Context, db DB, why string) (bool, error) {
	return turnSwitch(ctx, db, unjudgedSwitch, true, why)
}

// PresenceJudged records that host presence can be judged again, and tells
// the Owner, when PresenceStream says it could not. It reports whether it
// recorded.
func PresenceJudged(ctx context.Context, db DB, why string) (bool, error) {
	return turnSwitch(ctx, db, unjudgedSwitch, false, why)
}

// turnSwitch turns sw on or off, why, and tells the Owner, unless it already
// is. Each lock it waits for is bounded, as the presence pass's are (review
// 529 B-1): a lock held elsewhere fails the pass, and the next one tries
// again.
func turnSwitch(ctx context.Context, db DB, sw presenceSwitch, on bool, why string) (bool, error) {
	did := false
	err := inPresenceTx(ctx, db, func(tx pgx.Tx) error {
		did = false
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '`+waitsLockTimeout+`'`); err != nil {
			return err
		}
		if open, _, _, err := switchOpen(ctx, tx, sw.stream); err != nil || open == on {
			return err
		}
		// The seat's lock first, as the presence pass takes it.
		if err := ensureWriter(ctx, tx, Writer{Actor: Seat}, false); err != nil {
			return err
		}
		if err := lockStream(ctx, tx, sw.stream); err != nil {
			return err
		}
		if open, _, _, err := switchOpen(ctx, tx, sw.stream); err != nil || open == on {
			return err
		}
		note := fmt.Sprintf(sw.on, why)
		if on {
			if err := presenceAlert(ctx, tx, sw.stream, sw.rule, sw.phrase, why); err != nil {
				return err
			}
		} else {
			if _, err := Append(ctx, tx, Event{Stream: sw.stream, Type: "note", Actor: "controller",
				Body: map[string]any{"text": fmt.Sprintf(sw.offNote, why)}}); err != nil {
				return err
			}
			note = fmt.Sprintf(sw.off, why)
		}
		if _, err := postMessageAs(ctx, tx, Seat, "owner", "note", []byte("From the platform, not the chief of staff: "+note), "", "", false); err != nil {
			return err
		}
		did = true
		return nil
	})
	return did && err == nil, err
}

// PresenceDeferral judges whether host presence is deferred at now (zero:
// the database's clock), the hosts' silence counted from since, given the
// platform's recent recovery points as the controller saw them: a watched
// host once heard from, not alive, neither judged lost nor already told
// offline, silent longer than PresenceDeferredAfter and across at least two
// recoveries (one
// is an outage the hosts are given their 30 s after, as they should be;
// several are a floor that keeps moving). It turns the deferred switch on or
// off accordingly, naming the probe; it reports whether it turned it.
func PresenceDeferral(ctx context.Context, db DB, now, since time.Time, recoveries []time.Time, probe string) (bool, error) {
	var hosts []string
	var silent time.Duration
	err := inPresenceTx(ctx, db, func(tx pgx.Tx) error {
		if len(recoveries) < 2 {
			return nil
		}
		if now.IsZero() {
			if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
				return err
			}
		}
		var doc []byte
		if err := tx.QueryRow(ctx, HostsSQL("$4::timestamptz"), append(livenessArgs(since), now)...).Scan(&doc); err != nil {
			return err
		}
		var hs []HostPresence
		if err := json.Unmarshal(doc, &hs); err != nil {
			return err
		}
		streams := make([]string, 0, len(hs))
		for _, h := range hs {
			streams = append(streams, HostPresenceStream(h.HostID))
		}
		open, err := openAlerts(ctx, tx, streams)
		if err != nil {
			return err
		}
		for _, h := range hs {
			if _, told := open[HostPresenceStream(h.HostID)]; told || !h.watched() || h.Online || h.ReportedOffline {
				continue
			}
			// A host never heard from is not being deferred by recoveries:
			// what presence says of it stays as it is.
			if h.ContactAt == nil {
				continue
			}
			s, across := now.Sub(*h.ContactAt), 0
			for _, r := range recoveries {
				if r.After(*h.ContactAt) {
					across++
				}
			}
			if s > PresenceDeferredAfter && across >= 2 {
				hosts, silent = append(hosts, h.HostID), max(silent, s)
			}
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	if len(hosts) == 0 {
		return turnSwitch(ctx, db, deferredSwitch, false, "every host is judged as usual.")
	}
	slices.Sort(hosts)
	return turnSwitch(ctx, db, deferredSwitch, true, fmt.Sprintf("%s silent for up to %s but not judged: the platform's health probe (%s) "+
		"keeps failing every few passes, and each time it answers again the hosts' 30 s start over. With several API instances the probe's "+
		"sampling of the Service (5 fresh connections a pass) is probabilistic, so this can last; the hosts are judged once the probe "+
		"stays answered for 30 s.", strings.Join(hosts, ", "), silent.Truncate(time.Second), probe))
}
