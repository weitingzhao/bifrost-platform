package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// When host presence cannot be judged (the coordinating thread on review 528:
// a probe that fails, or a long outage of the platform, is never silent).
// The controller judges hosts only while the platform takes agentd's calls
// (its probe says so, or some host claimed within HostLostAfter); while it
// cannot tell for longer than a while it records a policy_alert (rule
// presence_unjudged) on PresenceStream and tells the Owner in his
// conversation with the chief of staff, once; when it can tell again, a note
// there, and tells him that too. The stream holds nothing else: its head is
// the state, read the bounded way (headSQL). GET /progress says it
// (presence_unjudged), and the Console with it.

// PresenceStream holds whether host presence can be judged.
const PresenceStream = "presence:judging"

// presenceUnjudgedPhrase is the alert's phrase.
const presenceUnjudgedPhrase = "host presence cannot be judged"

// LatestClaim is the last call of any host's claim loop (the latest
// last_poll_at), zero with none: a claim within HostLostAfter is evidence the
// platform takes agentd's calls.
func LatestClaim(ctx context.Context, db DB) (time.Time, error) {
	var at *time.Time
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT max(last_poll_at) FROM agentrt.host`).Scan(&at)
	})
	if err != nil || at == nil {
		return time.Time{}, err
	}
	return *at, nil
}

// unjudgedOpen reads the head of PresenceStream: an open alert, with when it
// was recorded and its text.
func unjudgedOpen(ctx context.Context, tx pgx.Tx) (bool, time.Time, string, error) {
	var head []byte
	if err := tx.QueryRow(ctx, `SELECT `+headSQL("$1::text"), PresenceStream).Scan(&head); err != nil || head == nil {
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

// unjudgedView is GET /progress's presence_unjudged: since when presence
// cannot be judged and why, nil while it can.
func unjudgedView(ctx context.Context, tx pgx.Tx) (map[string]any, error) {
	open, at, why, err := unjudgedOpen(ctx, tx)
	if err != nil || !open {
		return nil, err
	}
	return map[string]any{"since": at.UTC().Format(time.RFC3339Nano), "why": why}, nil
}

// PresenceUnjudged records that host presence cannot be judged, why, and
// tells the Owner, unless that is what PresenceStream already says. It
// reports whether it recorded.
func PresenceUnjudged(ctx context.Context, db DB, why string) (bool, error) {
	return presenceJudging(ctx, db, false, why)
}

// PresenceJudged records that host presence can be judged again, and tells
// the Owner, when PresenceStream says it could not. It reports whether it
// recorded.
func PresenceJudged(ctx context.Context, db DB, why string) (bool, error) {
	return presenceJudging(ctx, db, true, why)
}

func presenceJudging(ctx context.Context, db DB, judged bool, why string) (bool, error) {
	did := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		did = false
		// Each lock it waits for is bounded, as the presence pass's are
		// (review 529 B-1): a lock held elsewhere fails the pass, and the
		// next one tries again.
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '`+waitsLockTimeout+`'`); err != nil {
			return err
		}
		if open, _, _, err := unjudgedOpen(ctx, tx); err != nil || open != judged {
			return err
		}
		// The seat's lock first, as the presence pass takes it.
		if err := ensureWriter(ctx, tx, Writer{Actor: Seat}, false); err != nil {
			return err
		}
		if err := lockStream(ctx, tx, PresenceStream); err != nil {
			return err
		}
		if open, _, _, err := unjudgedOpen(ctx, tx); err != nil || open != judged {
			return err
		}
		note := "From the platform, not the chief of staff: host presence cannot be judged: " + why +
			" No host is reported offline and no executor unavailable until it can be; the platform says so here when it can."
		if judged {
			if _, err := Append(ctx, tx, Event{Stream: PresenceStream, Type: "note", Actor: "controller",
				Body: map[string]any{"text": "Host presence can be judged again: " + why}}); err != nil {
				return err
			}
			note = "From the platform, not the chief of staff: host presence can be judged again: " + why
		} else if err := presenceAlert(ctx, tx, PresenceStream, "presence_unjudged", presenceUnjudgedPhrase, why); err != nil {
			return err
		}
		if _, err := postMessageAs(ctx, tx, Seat, "owner", "note", []byte(note), "", "", false); err != nil {
			return err
		}
		did = true
		return nil
	})
	return did && err == nil, err
}
