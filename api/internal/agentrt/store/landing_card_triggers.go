package store

import (
	"context"
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
	Kind           string
	Seq            int64
	At             time.Time
	Root, Op, Why  string
	StopGeneration int64
	Choices        []string
}

// ParkedLongAfter is how long a park (other than ci_stuck and rule) lasts
// before its card.
const ParkedLongAfter = 2 * time.Hour

// LandingCardTriggers lists the triggers that apply now on repo's landing
// stream, at most one kind per event: root_failed (not declined, main_lost or
// floor), floor, ok_missing, late_landing, repo_stopped, ci_stuck and
// parked_long (not ci_stuck or rule; two hours into the park). A failed root
// whose mission is no longer active, or no_merge, has no card.
func LandingCardTriggers(ctx context.Context, tx pgx.Tx, repo string, now time.Time) ([]CardTrigger, error) {
	stream := "landing:" + repo
	s, err := LandingState(ctx, tx, stream)
	if err != nil {
		return nil, err
	}
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
		if o.Task == "" || s.LatestOperation(o.Task) != op {
			continue
		}
		switch {
		case o.Phase == "failed" && !o.Retried && o.Outcome != "declined" && o.Outcome != "main_lost":
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
			t := CardTrigger{Kind: CardRootFailed, Seq: e.seq, At: e.at, Root: o.Task, Op: op, Why: o.Outcome, Choices: []string{"do_not_merge", "wait"}}
			switch {
			case o.Outcome == "floor":
				t.Kind = CardFloor
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
	slices.SortFunc(out, func(a, b CardTrigger) int { return int(a.Seq - b.Seq) })
	return out, nil
}

// rootMissionOpen: the root's mission is active and not no_merge, so a
// decision about the root can still be carried out.
func rootMissionOpen(ctx context.Context, tx pgx.Tx, root string) (bool, error) {
	var open bool
	err := tx.QueryRow(ctx, `SELECT m.state = 'active' AND COALESCE(m.scope_json->>'no_merge','') = '' FROM agentrt.task t JOIN agentrt.mission m USING (mission_id) WHERE t.task_id = $1`, root).Scan(&open)
	return open, err
}
