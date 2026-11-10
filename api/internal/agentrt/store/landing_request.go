package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// LandingGrantRequest is an observation of an approval, never a grant. Its
// source is the workers' approval reader, outside every PostgreSQL transaction.
type LandingGrantRequest struct {
	Op, ApprovalID, Status, ParamsHash string
	// R is the request's R parameter: the controller's own request reads it
	// (landing_grant_request.go). Not part of the recorded observation.
	R              string
	ApprovalNumber int64
	SeenAt         time.Time
}

func (r LandingGrantRequest) body() map[string]any {
	return map[string]any{"v": "1", "op": r.Op, "approval_id": r.ApprovalID, "approval_number": r.ApprovalNumber,
		"status": r.Status, "params_hash": r.ParamsHash, "seen_at": r.SeenAt.UTC().Format(time.RFC3339Nano)}
}

var landingRequestID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_:./-]{0,199}$`)
var landingRequestHash = regexp.MustCompile(`^[0-9a-f]{64}$`)

func badLandingGrantRequest(v any) string {
	m, ok := v.(map[string]any)
	if !ok || len(m) != 7 || m["v"] != "1" {
		return "landing_grant_request requires a closed v1 object"
	}
	for _, k := range []string{"op", "approval_id", "status", "params_hash", "seen_at"} {
		if s, ok := m[k].(string); !ok || s == "" {
			return "landing_grant_request needs " + k
		}
	}
	if !landingRequestID.MatchString(m["op"].(string)) || !landingRequestID.MatchString(m["approval_id"].(string)) || !landingRequestHash.MatchString(m["params_hash"].(string)) {
		return "landing_grant_request invalid identity or params_hash"
	}
	if n, ok := cardNumber(m["approval_number"]); !ok || n < 1 {
		return "landing_grant_request needs approval_number"
	}
	if !slices.Contains([]string{"pending", "approved", "running", "executed", "failed", "rejected", "expired", "unknown"}, m["status"].(string)) {
		return "landing_grant_request invalid status"
	}
	if at, err := time.Parse(time.RFC3339Nano, m["seen_at"].(string)); err != nil || at.IsZero() {
		return "landing_grant_request invalid seen_at"
	}
	return ""
}

// Append enforces the provenance and the rejection/failure pairing, including
// direct internal callers. Late rejection observes the old op only. Unknown or
// expired approvals never prove that a grant has been revoked or is absent.
func checkLandingRequestAppend(ctx context.Context, tx pgx.Tx, ev Event, at time.Time) error {
	v, has := ev.Body["landing_grant_request"]
	r, hasLanding := ev.Body["landing"].(map[string]any)
	if !has {
		return nil
	}
	if why := badLandingGrantRequest(v); why != "" {
		return fmt.Errorf("%s", why)
	}
	m := v.(map[string]any)
	if ev.Type != "note" || ev.Actor != "controller" || !landing.RepoStream(ev.Stream) || !landing.GrantSupported(strings.TrimPrefix(ev.Stream, "landing:")) {
		return invalid("landing request requires controller repository stream")
	}
	seen, _ := time.Parse(time.RFC3339Nano, m["seen_at"].(string))
	if seen.After(at) {
		return invalid("landing request observed in the future")
	}
	s, err := LandingState(ctx, tx, ev.Stream)
	if err != nil {
		return err
	}
	op := m["op"].(string)
	o, exists := s.Operations[op]
	if !exists || ev.TaskID != o.Task || o.Task == "" {
		return invalid("landing request needs its operation's root")
	}
	// One identity cannot move between operations or change its parameter hash.
	var mismatch, duplicate bool
	err = tx.QueryRow(ctx, `SELECT
	  EXISTS (SELECT 1 FROM agentrt.event WHERE event_stream_id=$1 AND body_json->'landing_grant_request'->>'approval_id'=$2
	    AND (body_json->'landing_grant_request'->>'op'<>$3 OR body_json->'landing_grant_request'->>'params_hash'<>$4
	      OR (body_json->'landing_grant_request'->>'approval_number')::bigint<>$5)),
	  EXISTS (SELECT 1 FROM agentrt.event WHERE event_stream_id=$1 AND body_json->'landing_grant_request'->>'approval_id'=$2
	    AND body_json->'landing_grant_request'->>'status'=$6)`, ev.Stream, m["approval_id"], op, m["params_hash"], m["approval_number"], m["status"]).Scan(&mismatch, &duplicate)
	if err != nil {
		return err
	}
	if mismatch || duplicate {
		return invalid("landing request identity conflict or duplicate")
	}
	declines := m["status"] == "rejected" && s.LatestOperation(o.Task) == op && o.Phase == "awaiting_grant"
	if declines != hasLanding || hasLanding && (r["op"] != op || r["kind"] != "failed" || r["why"] != "declined") {
		return invalid("rejection ends only its latest waiting operation, in the same event")
	}
	return nil
}

// landingRequestRecorded reads, without the stream lock, whether this exact
// observation is already in the stream and nothing under its approval id
// disagrees with it. A yes stays true: events cannot be changed or removed
// (the event guards), and every append of an observation passes
// checkLandingRequestAppend under the stream lock, which refuses a second
// identity for an approval id. Anything this read cannot match exactly,
// such as a field of another JSON type, counts as a disagreement and goes
// to the locked check.
func landingRequestRecorded(ctx context.Context, db DB, r LandingGrantRequest) (bool, error) {
	var same, mismatch bool
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		repo, err := grantRepoOf(ctx, tx, r.Op)
		if err != nil {
			if errors.Is(err, ErrInvalid) {
				return invalid("unknown landing operation")
			}
			return err
		}
		return tx.QueryRow(ctx, `SELECT
		  coalesce(bool_or(jsonb_typeof(g->'status')='string' AND g->>'status'=$2), false),
		  coalesce(bool_or(jsonb_typeof(g->'op') IS DISTINCT FROM 'string' OR g->>'op' IS DISTINCT FROM $3
		    OR jsonb_typeof(g->'params_hash') IS DISTINCT FROM 'string' OR g->>'params_hash' IS DISTINCT FROM $4
		    OR jsonb_typeof(g->'approval_number') IS DISTINCT FROM 'number' OR g->>'approval_number' IS DISTINCT FROM $5), false)
		FROM (SELECT body_json->'landing_grant_request' AS g FROM agentrt.event
		  WHERE event_stream_id=$6 AND body_json->'landing_grant_request'->>'approval_id'=$1) seen`,
			r.ApprovalID, r.Status, r.Op, r.ParamsHash, strconv.FormatInt(r.ApprovalNumber, 10), grantStream(repo)).Scan(&same, &mismatch)
	})
	return same && !mismatch, err
}

// RecordLandingGrantRequest deduplicates (approval_id,status) under the stream
// lock. A rejected latest wait ends atomically; stale-generation decisions
// remain observations. Retrying after an uncertain commit is a safe no-op.
func RecordLandingGrantRequest(ctx context.Context, db DB, r LandingGrantRequest) (bool, error) {
	if why := badLandingGrantRequest(r.body()); why != "" {
		return false, invalid("%s", why)
	}
	// The pass offers every landing approval each round, most of them long
	// recorded. A recorded one is settled here, before any fold or lock, so it
	// never queues behind a landing that holds the stream (TD-514). A mismatch
	// goes on to the locked check below, which refuses it.
	recorded, err := landingRequestRecorded(ctx, db, r)
	if err != nil || recorded {
		return false, err
	}
	var root, stream string
	err = InTx(ctx, db, func(tx pgx.Tx) error {
		repo, err := grantRepoOf(ctx, tx, r.Op)
		if err != nil {
			if errors.Is(err, ErrInvalid) {
				return invalid("unknown landing operation")
			}
			return err
		}
		stream = grantStream(repo)
		s, err := LandingState(ctx, tx, stream)
		if err != nil {
			return err
		}
		root = s.Operations[r.Op].Task
		return nil
	})
	if err != nil {
		return false, err
	}
	if root == "" {
		return false, invalid("unknown landing operation")
	}
	wrote := false
	err = InTx(ctx, db, func(tx pgx.Tx) error {
		wrote = false
		mission, err := lockWaitingRootIn(ctx, tx, root, stream)
		if err != nil {
			return err
		}
		var existing map[string]any
		rows, err := tx.Query(ctx, `SELECT body_json->'landing_grant_request' FROM agentrt.event WHERE event_stream_id=$2
		  AND body_json->'landing_grant_request'->>'approval_id'=$1`, r.ApprovalID, stream)
		if err != nil {
			return err
		}
		duplicate := false
		for rows.Next() {
			if err = rows.Scan(&existing); err != nil {
				rows.Close()
				return err
			}
			n, _ := cardNumber(existing["approval_number"])
			if existing["op"] != r.Op || existing["params_hash"] != r.ParamsHash || n != r.ApprovalNumber {
				rows.Close()
				return invalid("landing request identity conflict")
			}
			duplicate = duplicate || existing["status"] == r.Status
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if duplicate {
			return nil
		}
		s, err := LandingState(ctx, tx, stream)
		if err != nil {
			return err
		}
		o := s.Operations[r.Op]
		body := map[string]any{"text": "Landing approval observed: " + r.Status, "landing_grant_request": r.body()}
		if r.Status == "rejected" && s.LatestOperation(root) == r.Op && o.Phase == "awaiting_grant" {
			body["landing"] = map[string]any{"v": landing.Version, "kind": "failed", "op": r.Op, "why": "declined"}
		}
		_, err = Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", MissionID: mission, TaskID: root, Body: body})
		wrote = err == nil
		return err
	})
	return wrote, err
}

// SettledLandingGrantRequests are the pending requests whose operation in s
// no longer waits on a grant: it went on without one (a rule that clears
// it, landing_rule_clear, or a newer operation of its task) or ended. No
// grant can be written for them any more (the executor refuses not_waiting),
// so the controller withdraws them from the Owner's list rather than leave
// them to expire there. An operation s does not know is left alone.
func SettledLandingGrantRequests(s landing.State, requests []LandingGrantRequest) []LandingGrantRequest {
	var out []LandingGrantRequest
	for _, r := range requests {
		o, known := s.Operations[r.Op]
		if r.Status != "pending" || !known || o.Task == "" || o.Phase == "awaiting_grant" && s.LatestOperation(o.Task) == r.Op {
			continue
		}
		out = append(out, r)
	}
	return out
}
