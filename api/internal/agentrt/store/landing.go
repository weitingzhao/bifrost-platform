package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// LandingState folds only the authoritative stream, never mission mirrors.
// Writers call it after Append has locked the stream, in the same transaction.
func LandingState(ctx context.Context, tx pgx.Tx, stream string) (landing.State, error) {
	rows, err := tx.Query(ctx, `SELECT at, body_json->'landing' FROM agentrt.event WHERE event_stream_id=$1 AND event_type='note' AND body_json ? 'landing' ORDER BY seq`, stream)
	if err != nil {
		return landing.State{}, err
	}
	defer rows.Close()
	entries := []landing.Entry{}
	for rows.Next() {
		var at time.Time
		var raw []byte
		if err := rows.Scan(&at, &raw); err != nil {
			return landing.State{}, err
		}
		v, err := canon.Parse(raw)
		if err != nil {
			return landing.State{}, err
		}
		r, err := landing.Parse(v)
		if err != nil {
			return landing.State{}, err
		}
		entries = append(entries, landing.Entry{At: at, Record: r})
	}
	if err := rows.Err(); err != nil {
		return landing.State{}, err
	}
	return landing.Fold(entries)
}
func checkLandingAppend(ctx context.Context, tx pgx.Tx, ev Event, at time.Time) error {
	value, exists := ev.Body["landing"]
	if !exists {
		return nil
	}
	if ev.Type != "note" {
		return fmt.Errorf("landing requires note")
	}
	r, err := landing.Parse(value)
	if err != nil {
		return err
	}
	k := r.String("kind")
	switch k {
	case "stg_pending", "stg_snapshot", "stg_started", "stg_result", "stg_verified", "stg_failed":
		return stgError("legacy", k, "legacy STG kinds are read-only; write note.stg_release v1")
	}
	if (k == "retry" || k == "rehearsed" || k == "discarded" || k == "awaiting_grant") && ev.Actor != "controller" {
		return fmt.Errorf("landing transition requires controller")
	}
	if k == "reserved" {
		parts := strings.Split(ev.Stream, ":")
		if len(parts) != 3 || parts[0] != "landing-quota" || (parts[1] != "dev" && parts[1] != "business") || parts[2] != at.UTC().Format("2006-01-02") {
			return fmt.Errorf("reservation requires today's tenant quota stream")
		}
		state, err := LandingState(ctx, tx, "landing:bifrost-platform")
		if err != nil {
			return err
		}
		op, ok := state.Operations[r.String("op")]
		if !ok || !op.Reservable(r.String("root")) {
			return fmt.Errorf("reservation requires a live prepared operation")
		}
	} else if ev.Stream != "landing:bifrost-platform" {
		return fmt.Errorf("landing record requires repository stream; mission mirrors are text only")
	}
	s, err := LandingState(ctx, tx, ev.Stream)
	if err != nil {
		return err
	}
	if k == "failed" && s.Operations[r.String("op")].Phase == "" && ev.Actor != "controller" {
		return fmt.Errorf("born-terminal landing failure requires controller")
	}
	if k == "failed" && r.String("why") == "main_lost" && ev.Actor != "controller" {
		return fmt.Errorf("failed{main_lost} requires controller (an Owner answer)")
	}
	// A delayed preparer cannot charge authorization waiting to execution.
	// Keep this at append time rather than changing historical Fold semantics;
	// main_result{unknown}'s reconciler still has its separate bound meaning.
	if o := s.Operations[r.String("op")]; k == "failed" && o.Phase == "awaiting_grant" &&
		(r.String("why") == "bound" || r.String("why") == "approval_expired") &&
		landing.RootBound(s, o.Task, at, false) != r.String("why") {
		return fmt.Errorf("waiting operation has not reached its current budget")
	}
	if k == "main_intent" && s.Operations[r.String("op")].Phase == "awaiting_grant" {
		return fmt.Errorf("waiting operation requires grant consumption; not installed in C-4")
	}
	if k == "failed" && r.String("why") == "declined" && s.Operations[r.String("op")].Phase == "awaiting_grant" {
		request, ok := ev.Body["landing_grant_request"].(map[string]any)
		if !ok || request["op"] != r.String("op") || request["status"] != "rejected" || request["approval_id"] == "" {
			return fmt.Errorf("declining an authorization wait requires its rejected approval in the same event")
		}
	}
	return s.Apply(landing.Entry{At: at, Record: r})
}

// ObserveLandingEnabled is called when the loop first observes a current
// authorized rule. Append's timestamp, not effective_at, becomes the fence.
// Later versions and repeated observations never move it.
//
// The rule is selected once the stream lock is held, at that clock (CUT §9,
// review 656 B-R1): the fence is never written on a rule that ran out while
// this waited. now is the pass's; it no longer decides.
//
// It returns the stream's fence (State.Fence), zero when there is none yet:
// on PROD (env "prod") enabled is recorded only after the live mode, so the
// two are complete together (review 672 B-02).
func ObserveLandingEnabled(ctx context.Context, tx pgx.Tx, env, tenant, repo string, now time.Time) (time.Time, error) {
	stream := "landing:" + repo
	if err := lockStream(ctx, tx, stream); err != nil {
		return time.Time{}, err
	}
	rule, err := CurrentRule(ctx, tx, "landing:"+tenant, DecisionAt())
	if err != nil {
		return time.Time{}, err
	}
	if rule == nil || rule.Landing == nil {
		return time.Time{}, fmt.Errorf("no current landing rule")
	}
	if _, ok := rule.Landing.Repo(repo); !ok {
		return time.Time{}, fmt.Errorf("repo not enabled")
	}
	s, err := LandingState(ctx, tx, stream)
	if err != nil {
		return time.Time{}, err
	}
	if !s.EnabledAt.IsZero() {
		return s.Fence(), nil
	}
	if env == "prod" && s.LiveAt.IsZero() {
		return time.Time{}, nil
	}
	a, err := Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", Body: map[string]any{"text": "Landing enabled", "landing": map[string]any{"v": landing.Version, "kind": "enabled", "op": "enabled:" + tenant, "repo": repo, "rule_version": rule.Version}}})
	if err != nil {
		return time.Time{}, err
	}
	s.EnabledAt = a.At
	return s.Fence(), nil
}

// EnsureLandingMode records, once, that the live landing is wired for repo
// (CUT §11.8 v3 item 1): PROD workers write it before their first claim.
// It returns when it was recorded.
func EnsureLandingMode(ctx context.Context, db DB, repo string) (time.Time, error) {
	stream := "landing:" + repo
	var at time.Time
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		if err := lockStream(ctx, tx, stream); err != nil {
			return err
		}
		s, err := LandingState(ctx, tx, stream)
		if err != nil {
			return err
		}
		if !s.LiveAt.IsZero() {
			at = s.LiveAt
			return nil
		}
		a, err := Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", Body: map[string]any{"text": "Landing live mode",
			"landing": map[string]any{"v": landing.Version, "kind": "mode", "op": "mode:live", "repo": repo}}})
		at = a.At
		return err
	})
	return at, err
}

// LandingLiveAt is when the live mode was recorded on repo's stream, zero
// when it never was.
func LandingLiveAt(ctx context.Context, db DB, repo string) (time.Time, error) {
	var at time.Time
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		s, err := LandingState(ctx, tx, "landing:"+repo)
		at = s.LiveAt
		return err
	})
	return at, err
}

// ReparkLanding moves a parked operation to a new park for why, in one
// transaction under the stream's lock (review 672 R-05): it re-reads the
// operation and, if it is still the park observed (parked for was, its last
// record at seen), ends it and starts the new one together. The time of the
// last record tells the park observed from a later one with the same reason
// (review 675 R-05: window, ci_stuck, window again). Nothing is written
// otherwise; ok reports whether it was.
func ReparkLanding(ctx context.Context, db DB, repo, op, was string, seen time.Time, why string, retryAt time.Time, text string) (ok bool, err error) {
	stream := "landing:" + repo
	err = InTx(ctx, db, func(tx pgx.Tx) error {
		ok = false
		if err := lockStream(ctx, tx, stream); err != nil {
			return err
		}
		s, err := LandingState(ctx, tx, stream)
		if err != nil {
			return err
		}
		if o, has := s.Operations[op]; !has || o.Phase != "parked" || o.Why != was || !o.LastAt.Equal(seen) || was == why {
			return nil
		}
		for _, r := range []map[string]any{
			{"v": landing.Version, "kind": "unparked", "op": op},
			{"v": landing.Version, "kind": "parked", "op": op, "why": why, "retry_at": retryAt.Format(time.RFC3339Nano)},
		} {
			if _, err := Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", Body: map[string]any{"text": text + r["kind"].(string), "landing": r}}); err != nil {
				return err
			}
		}
		ok = true
		return nil
	})
	return ok, err
}
