package store

import (
	"context"
	"encoding/json"
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
