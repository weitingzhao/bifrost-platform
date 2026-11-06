package store

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// CardTrigger is one durable reason for a landing card (CUT §11.6 v3 item 1,
// 7): the repository-stream event it comes from (Seq, unique per stream) and
// what the card is about. A trigger is listed only while it applies, read
// from the stream as it is now; the card's handler decides again under the
// business locks.
type CardTrigger struct {
	Family, Block, Batch string
	Members              []StgMemberKey
	P, BaseP             string
	Kind                 string
	Seq                  int64
	At                   time.Time
	Root, Op, Why        string
	StopGeneration       int64
	Choices              []string
	Floor                map[string]any
	// Sent is an STG block's detail.sent: the refusal was found after B5 had
	// already sent P (R5:5.7-2), so P is on STG.
	Sent map[string]any
	// Rework is a rework_landed trigger's frozen landing_rework_seen, with
	// each sibling's title (R5:5.5-18).
	Rework *ReworkView
}

// ParkedLongAfter is how long a park (other than ci_stuck and rule) lasts
// before its card.
const ParkedLongAfter = 2 * time.Hour

// LandingCardTriggers lists the triggers that apply now on repo's landing
// stream, at most one kind per event: root_failed (not declined, main_lost or
// floor, nor a red CI that sent its root back for another round), floor, ok_missing, late_landing, repo_stopped, ci_stuck and
// parked_long (not ci_stuck or rule; two hours into the park). A failed root
// whose mission is no longer active, or no_merge, has no card.
func LandingCardTriggers(ctx context.Context, tx pgx.Tx, repo string, now time.Time) ([]CardTrigger, error) {
	s, err := LandingState(ctx, tx, "landing:"+repo)
	if err != nil {
		return nil, err
	}
	return landingCardTriggers(ctx, tx, repo, now, s, nil, nil)
}

// landingCardTriggers is LandingCardTriggers over repo's landing fold s
// and, for B5's repository, its fold stg when already read (nil: read it
// here), read in this same transaction (an explanation reads each once,
// R5:5.5-19 §5). With only, the root triggers are those of these roots
// alone, so an explanation reads the records of the roots it explains and
// no other (nil: every root).
func landingCardTriggers(ctx context.Context, tx pgx.Tx, repo string, now time.Time, s landing.State, stg *StgState, only map[string]bool) ([]CardTrigger, error) {
	stream := "landing:" + repo
	rows, err := tx.Query(ctx, `SELECT seq, at, body_json FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'
	  AND (body_json->'landing'->>'kind' IN ('failed', 'parked') OR body_json ? 'landing_anomaly' OR body_json ? 'landing_stop' OR body_json ? 'landing_resume')
	  ORDER BY seq`, stream)
	if err != nil {
		return nil, err
	}
	type event struct {
		seq  int64
		at   time.Time
		body map[string]any
	}
	var events []event
	for rows.Next() {
		var e event
		if err := rows.Scan(&e.seq, &e.at, &e.body); err != nil {
			rows.Close()
			return nil, err
		}
		events = append(events, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	lastFailed, lastParked := map[string]event{}, map[string]event{}
	var stop event
	var stopGen, resumedGen int64
	var out []CardTrigger
	for _, e := range events {
		if r, ok := e.body["landing"].(map[string]any); ok {
			op, _ := r["op"].(string)
			switch r["kind"] {
			case "failed":
				lastFailed[op] = e
			case "parked":
				lastParked[op] = e
			}
		}
		if a, ok := e.body["landing_anomaly"].(map[string]any); ok {
			op, _ := a["op"].(string)
			root, _ := a["root"].(string)
			o := s.Operations[op]
			switch a["kind"] {
			case "ok_missing":
				if s.LatestOperation(root) == op && o.Phase == "main_result" && o.Outcome == "ok" {
					out = append(out, CardTrigger{Kind: CardOkMissing, Seq: e.seq, At: e.at, Root: root, Op: op, Choices: []string{"confirm_main_lost", "wait"}})
				}
			case "late_landing":
				out = append(out, CardTrigger{Kind: CardLateLanding, Seq: e.seq, At: e.at, Root: root, Op: op, Choices: []string{"ack", "remind_later"}})
			}
		}
		if st, ok := e.body["landing_stop"].(map[string]any); ok {
			if g, _ := cardNumber(st["generation"]); g > stopGen {
				stop, stopGen = e, g
			}
		}
		if rs, ok := e.body["landing_resume"].(map[string]any); ok {
			if g, _ := cardNumber(rs["generation"]); g > resumedGen {
				resumedGen = g
			}
		}
	}
	if stopGen > resumedGen {
		out = append(out, CardTrigger{Kind: CardRepoStopped, Seq: stop.seq, At: stop.at, StopGeneration: stopGen, Choices: []string{"repo_resume", "keep_stopped"}})
	}
	for op, o := range s.Operations {
		if o.Task == "" || only != nil && !only[o.Task] || s.LatestOperation(o.Task) != op {
			continue
		}
		why, err := landingDisposed(ctx, tx, stream, o.Task, op)
		if err != nil {
			return nil, err
		}
		// The Owner counted it: no decision card asks again. A notice is
		// not a decision: a late reversal is still told (review 672 R-02).
		if why != "" && !(o.Phase == "reverted" && o.EverLanded) {
			continue
		}
		switch {
		case o.Phase == "failed" && o.Outcome == "declined":
			// Declined, and reworked work landed on its step with every
			// change different (R5:5.5-18): the Owner says whether it
			// delivers the root's work. Its mission need not be open: the
			// answer is step evidence only.
			t, ok, err := reworkTrigger(ctx, tx, s, repo, o.Task, op, lastFailed[op].at)
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, t)
			}
		case o.Phase == "failed" && o.Outcome == "in_main":
			// Already in main before a claim (CUT §11.8 v3 item 4): the Owner
			// counts it delivered or not; nothing else ends it.
			e, ok := lastFailed[op]
			open, err := rootMissionOpen(ctx, tx, o.Task)
			if err != nil {
				return nil, err
			}
			if ok && open {
				out = append(out, CardTrigger{Kind: CardInMain, Seq: e.seq, At: e.at, Root: o.Task, Op: op, Why: "in_main", Choices: []string{"count_as_delivered", "confirm_not_delivered"}})
			}
		case o.Phase == "reverted" && o.EverLanded:
			// Reverted within a day after its mission closed done (CUT §11.8
			// v3 item 4): the Owner is told; the mission is not reopened.
			e, ok, err := lateRevert(ctx, tx, stream, o.Task, op)
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, CardTrigger{Kind: CardLateRevert, Seq: e.seq, At: e.at, Root: o.Task, Op: op, Why: "reverted", Choices: []string{"ack", "remind_later"}})
			}
		case o.Phase == "landed":
			// Landed, and main has not shown it in effect for two hours
			// since its first such read (CUT §11.8 v3 item 4).
			open, err := rootMissionOpen(ctx, tx, o.Task)
			if err != nil {
				return nil, err
			}
			if !open {
				continue
			}
			start, ok, err := unverifiedSince(ctx, tx, stream, op)
			if err != nil {
				return nil, err
			}
			if ok && !now.Before(start.at.Add(LandedUnverifiedAfter)) {
				out = append(out, CardTrigger{Kind: CardLandedUnverified, Seq: start.seq, At: start.at, Root: o.Task, Op: op, Why: start.validity, Choices: []string{"confirm_not_delivered", "wait"}})
			}
		// in_main and reverted were found before a claim: the change is (or
		// was) in main already. B4 follows them by their main evidence; a
		// card to try again or not merge would ask the wrong question.
		case o.Phase == "failed" && !o.Retried && !slices.Contains([]string{"declined", "main_lost", "in_main", "reverted"}, o.Outcome):
			e, ok := lastFailed[op]
			if !ok {
				continue
			}
			active, err := rootMissionOpen(ctx, tx, o.Task)
			if err != nil {
				return nil, err
			}
			if !active {
				continue
			}
			// A red CI that sent its root back for another round is the
			// root's own rework, not the Owner's card (precheck.go).
			if o.Outcome == "ci_red" {
				reworked, err := landingCIReworked(ctx, tx, o.Task, op)
				if err != nil {
					return nil, err
				}
				if reworked {
					continue
				}
			}
			t := CardTrigger{Kind: CardRootFailed, Seq: e.seq, At: e.at, Root: o.Task, Op: op, Why: o.Outcome, Choices: []string{"do_not_merge", "wait"}}
			switch {
			case o.Outcome == "floor":
				t.Kind = CardFloor
				t.Floor, _ = e.body["landing_floor"].(map[string]any)
			case slices.Contains(landing.RetryableReasons, o.Outcome):
				t.Choices = []string{"try_again", "do_not_merge"}
			}
			out = append(out, t)
		case o.Phase == "parked":
			e, ok := lastParked[op]
			if !ok {
				continue
			}
			why, _ := e.body["landing"].(map[string]any)["why"].(string)
			switch {
			case why == "ci_stuck":
				out = append(out, CardTrigger{Kind: CardCIStuck, Seq: e.seq, At: e.at, Root: o.Task, Op: op, Why: why, Choices: []string{"ack", "remind_later"}})
			case why != "rule" && !now.Before(e.at.Add(ParkedLongAfter)):
				out = append(out, CardTrigger{Kind: CardParkedLong, Seq: e.seq, At: e.at, Root: o.Task, Op: op, Why: why, Choices: []string{"ack", "remind_later"}})
			}
		}
	}
	stgs, err := stgCardTriggersFrom(ctx, tx, repo, stg)
	if err != nil {
		return nil, err
	}
	out = append(out, stgs...)
	slices.SortFunc(out, func(a, b CardTrigger) int { return int(a.Seq - b.Seq) })
	return out, nil
}

// LateRevertWithin is how long after a mission closes done the reconciler
// still reads its landings, and a reversal is told as late_revert.
const LateRevertWithin = 24 * time.Hour

// lateRevert is op's reversal record when it came after its root's mission
// closed done, within LateRevertWithin of the close.
func lateRevert(ctx context.Context, tx pgx.Tx, stream, root, op string) (unverifiedRead, bool, error) {
	var closed *time.Time
	var mission string
	if err := tx.QueryRow(ctx, `SELECT m.mission_id, (SELECT max(e.at) FROM agentrt.event e WHERE e.mission_id = m.mission_id AND e.event_type = 'mission_state_changed' AND e.body_json->>'to' = 'done')
	  FROM agentrt.task t JOIN agentrt.mission m USING (mission_id) WHERE t.task_id = $1 AND m.state = 'done'`, root).Scan(&mission, &closed); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return unverifiedRead{}, false, err
	}
	if closed == nil {
		return unverifiedRead{}, false, nil
	}
	// Closed from landing evidence: told only for an operation the close
	// counted delivered (review 672 R-02).
	lc, err := readLandingClose(ctx, tx, mission)
	if err != nil {
		return unverifiedRead{}, false, err
	}
	if lc != nil {
		counted := false
		for _, x := range lc.Roots {
			if x["root"] == root && x["op"] == op && x["class"] == RootDelivered {
				counted = true
			}
		}
		if !counted {
			return unverifiedRead{}, false, nil
		}
	}
	var r unverifiedRead
	err = tx.QueryRow(ctx, `SELECT seq, at FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'
	  AND body_json->'landing'->>'kind' = 'reverted' AND body_json->'landing'->>'op' = $2 ORDER BY seq DESC LIMIT 1`, stream, op).Scan(&r.seq, &r.at)
	if errors.Is(err, pgx.ErrNoRows) {
		return unverifiedRead{}, false, nil
	}
	if err != nil {
		return unverifiedRead{}, false, err
	}
	return r, r.at.After(*closed) && !r.at.After(closed.Add(LateRevertWithin)), nil
}

// LandedUnverifiedAfter is how long a landing's main evidence stays not
// effective before its card.
const LandedUnverifiedAfter = 2 * time.Hour

type unverifiedRead struct {
	seq      int64
	at       time.Time
	validity string
}

// unverifiedSince is the first main evidence of op that is not effective
// after its last effective one (the start of the current run of
// not-effective reads), with that read's event seq; ok false when the
// latest read is effective or there is none.
func unverifiedSince(ctx context.Context, tx pgx.Tx, stream, op string) (unverifiedRead, bool, error) {
	rows, err := tx.Query(ctx, `SELECT seq, at, COALESCE(body_json->'landing_main'->>'validity', ''), COALESCE((body_json->'landing_main'->>'has_r')::boolean, false)
	  FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note' AND body_json->'landing_main'->>'op' = $2
	  AND NOT (body_json->'landing_main' ? 'stg_evidence') ORDER BY seq`, stream, op)
	if err != nil {
		return unverifiedRead{}, false, err
	}
	defer rows.Close()
	var start unverifiedRead
	open := false
	for rows.Next() {
		var r unverifiedRead
		var hasR bool
		if err := rows.Scan(&r.seq, &r.at, &r.validity, &hasR); err != nil {
			return unverifiedRead{}, false, err
		}
		if r.validity == "" {
			r.validity = "unknown"
			if !hasR {
				r.validity = "absent"
			}
		}
		switch {
		case r.validity == "effective":
			open = false
		case !open:
			start, open = r, true
		}
	}
	return start, open, rows.Err()
}

// rootMissionOpen: the root's mission is active and not no_merge, so a
// decision about the root can still be carried out.
func rootMissionOpen(ctx context.Context, tx pgx.Tx, root string) (bool, error) {
	var open bool
	err := tx.QueryRow(ctx, `SELECT m.state = 'active' AND COALESCE(m.scope_json->>'no_merge','') = '' FROM agentrt.task t JOIN agentrt.mission m USING (mission_id) WHERE t.task_id = $1`, root).Scan(&open)
	return open, err
}
