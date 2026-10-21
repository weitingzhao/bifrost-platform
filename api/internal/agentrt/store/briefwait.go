package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// A brief no host takes (review 487 A1). The chief of staff's job lists who
// may answer (job:chief_of_staff v2: one candidate, exec:claude on
// claude-opus-5-5); while none can take a brief (no host offers that
// executor with a matching model, its account's pools cool down, or a
// reading is at the routing rule's quota stop), a message, a scheduled brief
// or a nudge waits ready, and the seat that would tell the Owner what waits
// is what does not run. Its wait is timed like any ready task's (waits.go,
// task_ready); once that is recorded, the platform tells the Owner itself,
// once a wait: a note in his conversation with the seat (his Chief page
// reads that one), saying it is the platform's, which brief waits, what it
// answers and what the platform can tell of why. The brief stays ready and
// is answered when a host takes it. An assessment ends itself (assess.go).
//
// The note is posted in a transaction of its own, after the wait's record
// commits, with the seat's lock first as AcceptBrief takes it, and no row of
// the brief or its mission referenced or locked: AcceptBrief holds the
// brief's row while it waits for the seat's lock. That it was told is a note
// on the Owner's mailbox stream (briefWaitTold), which the next pass reads,
// so a telling that failed is tried again and one that committed is not.

// briefWaitTold is the text of the record that the Owner was told of a
// brief's wait: the task and when its wait began (the record's since). It is
// formatted alike in Go and by the query (format's %s).
const briefWaitTold = "Told the Owner: brief task %s, ready since %s, waits with no host taking it."

// briefWaitsPerPass bounds how many waits one pass tells.
const briefWaitsPerPass = 3

// briefWait is a recorded task_ready wait of a brief, to tell.
type briefWait struct {
	task, since string
	limitS      int64
}

// TellWaitingBriefs tells the Owner of each brief still ready whose ready
// wait of now passed its limit (WaitLimits recorded it) and was not told
// yet; it returns how many it told. It reads from the ready briefs, not from
// a window of the records, so a telling that failed is tried again however
// long ago the wait was recorded (review 490 A1). A pass is bounded in time
// and in each lock it waits for, as WaitLimits is (review 490 B1).
func TellWaitingBriefs(ctx context.Context, db DB, now time.Time) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, waitsPassTimeout)
	defer cancel()
	var waits []briefWait
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '`+waitsLockTimeout+`'`); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `WITH r AS (SELECT t.task_id, (SELECT max(e.at) FROM agentrt.event e WHERE e.task_id = t.task_id
		      AND e.event_type = 'task_state_changed' AND e.body_json->>'to' = 'ready') AS since
		    FROM agentrt.task t WHERE t.state = 'ready' AND t.role = 'brief' AND t.task_class <> 'assessment')
		  SELECT r.task_id, w.body_json->>'since', (w.body_json->>'limit_s')::bigint
		  FROM r JOIN LATERAL (SELECT w.body_json, w.at FROM agentrt.event w WHERE w.task_id = r.task_id
		      AND w.event_type = 'wait_limit_passed' AND w.body_json->>'wait' = '`+WaitTaskReady+`'
		      AND (w.body_json->>'since')::timestamptz = r.since ORDER BY w.event_id LIMIT 1) w ON true
		  WHERE NOT EXISTS (SELECT 1 FROM agentrt.event n WHERE n.event_stream_id = $1 AND n.event_type = 'note'
		    AND n.body_json->>'text' = format($2, r.task_id, w.body_json->>'since'))
		  ORDER BY w.at, r.task_id LIMIT $3`, MailboxTopic("owner"), briefWaitTold, briefWaitsPerPass)
		if err != nil {
			return err
		}
		waits, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (briefWait, error) {
			var b briefWait
			return b, r.Scan(&b.task, &b.since, &b.limitS)
		})
		return err
	})
	if err != nil {
		return 0, err
	}
	told := 0
	var errs []error
	for _, b := range waits {
		did, err := tellWaitingBrief(ctx, db, b)
		if err != nil {
			errs = append(errs, fmt.Errorf("brief %s: %w", b.task, err))
		}
		if did {
			told++
		}
	}
	return told, errors.Join(errs...)
}

// tellWaitingBrief posts the note of one wait and records that it was told,
// unless that is recorded or the brief's wait ended meanwhile.
func tellWaitingBrief(ctx context.Context, db DB, b briefWait) (bool, error) {
	did := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		did = false
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '`+waitsLockTimeout+`'`); err != nil {
			return err
		}
		if err := ensureWriter(ctx, tx, Writer{Actor: Seat}, false); err != nil {
			return err
		}
		marker := fmt.Sprintf(briefWaitTold, b.task, b.since)
		var seen bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'
		  AND body_json->>'text' = $2)`, MailboxTopic("owner"), marker).Scan(&seen); err != nil || seen {
			return err
		}
		// Still ready, and ready since the wait's own start: a brief taken,
		// failed and readied again meanwhile is a wait of its own (review 490
		// C1). A plain read: no row of the brief is locked here.
		var same bool
		if err := tx.QueryRow(ctx, `SELECT t.state = 'ready' AND (SELECT max(e.at) FROM agentrt.event e WHERE e.task_id = t.task_id
		  AND e.event_type = 'task_state_changed' AND e.body_json->>'to' = 'ready') = $2::timestamptz
		  FROM agentrt.task t WHERE t.task_id = $1`, b.task, b.since).Scan(&same); err != nil || !same {
			return err
		}
		text, err := waitingBriefText(ctx, tx, b)
		if err != nil {
			return err
		}
		if _, err = postMessageAs(ctx, tx, Seat, "owner", "note", []byte(text), "", "", false); err != nil {
			return err
		}
		if _, err = Append(ctx, tx, Event{Stream: MailboxTopic("owner"), Type: "note", Actor: "controller",
			Body: map[string]any{"text": marker}}); err != nil {
			return err
		}
		did = true
		return nil
	})
	return did && err == nil, err
}

// waitingBriefText is the note: whose it is, the brief, what it answers and
// why no host took it, as far as the platform can tell.
func waitingBriefText(ctx context.Context, tx pgx.Tx, b briefWait) (string, error) {
	var mission, tenant string
	var route []string
	if err := tx.QueryRow(ctx, `SELECT t.mission_id, m.tenant, ARRAY(SELECT jsonb_array_elements_text(t.route_json))
	  FROM agentrt.task t JOIN agentrt.mission m ON m.mission_id = t.mission_id WHERE t.task_id = $1`, b.task).Scan(&mission, &tenant, &route); err != nil {
		return "", err
	}
	answers, err := briefAnswers(ctx, tx, b.task)
	if err != nil {
		return "", err
	}
	why, err := whyNoHostTakes(ctx, tx, route, tenant)
	if err != nil {
		return "", err
	}
	var s strings.Builder
	fmt.Fprintf(&s, "From the platform, not the chief of staff: brief task %s (mission %s) has been ready since %s and no host took it "+
		"within its limit of %s, so the chief of staff has not answered %s.\n\n", b.task, mission, b.since,
		(time.Duration(b.limitS) * time.Second).String(), answers)
	s.WriteString("Why, as far as the platform can tell: " + why + "\n\n")
	s.WriteString("The brief stays ready and is answered as usual once a host takes it.")
	return s.String(), nil
}

// briefAnswers names what the brief answers: the controller's note it was
// made from (a scheduled brief or a nudge, quoted), or someone's message.
func briefAnswers(ctx context.Context, tx pgx.Tx, task string) (string, error) {
	var id, from string
	var body []byte
	err := tx.QueryRow(ctx, `SELECT m.message_id, m.from_actor_id, b.body FROM agentrt.message m
	  JOIN agentrt.artifact_blob b ON b.artifact_id = m.body_artifact_id
	  WHERE m.task_id = $1 AND m.to_actor_id = $2 ORDER BY m.created_at LIMIT 1`, task, Seat).Scan(&id, &from, &body)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "what it was made for", nil
	case err != nil:
		return "", err
	case fromController(from):
		return strconv.Quote(oneLineRunes(string(body), 200)), nil
	}
	return from + "'s message " + id, nil
}

// whyNoHostTakes says what the platform knows that keeps work from a claim of
// a task with route for tenant, and no more (review 490 A2): the stops,
// pauses and drains in force that a claim reads (global, and each host's);
// the route's candidates still active or on probation, each with its
// account's cooldown or its quota reading at the routing rule's stop, as the
// claim reads them (claim.go, pick.go); and, for a candidate with neither,
// that why no host took it is not known here: whether a host offering its
// executor and model asked, or the hosts' slots were full, the platform does
// not record.
func whyNoHostTakes(ctx context.Context, tx pgx.Tx, route []string, tenant string) (string, error) {
	var parts []string
	rows, err := tx.Query(ctx, `SELECT control_kind, scope, COALESCE(scope_ref, '') FROM agentrt.control
	  WHERE cleared_at IS NULL AND control_kind IN ('stop','pause','drain') AND scope IN ('global','host')
	  ORDER BY scope, scope_ref, control_kind`)
	if err != nil {
		return "", err
	}
	type control struct{ kind, scope, ref string }
	controls, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (control, error) {
		var c control
		return c, r.Scan(&c.kind, &c.scope, &c.ref)
	})
	if err != nil {
		return "", err
	}
	for _, c := range controls {
		if c.scope == "global" {
			parts = append(parts, fmt.Sprintf("A %s is in force for every host: no host takes new work while it holds.", c.kind))
		} else {
			parts = append(parts, fmt.Sprintf("A %s is in force on host %s: that host takes no new work while it holds.", c.kind, c.ref))
		}
	}
	rows, err = tx.Query(ctx, `SELECT executor_actor_id, model_provider, model_pattern, status FROM agentrt.job_candidate
	  WHERE job_candidate_id = ANY($1) AND status IN ('active','probation') ORDER BY array_position($1, job_candidate_id)`, route)
	if err != nil {
		return "", err
	}
	type cand struct{ executor, provider, pattern, status string }
	cands, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (cand, error) {
		var c cand
		return c, r.Scan(&c.executor, &c.provider, &c.pattern, &c.status)
	})
	if err != nil {
		return "", err
	}
	if len(cands) == 0 {
		parts = append(parts, "No candidate of the chief of staff's job may take it now: the route lists none still active or on probation.")
		return strings.Join(parts, " "), nil
	}
	quota, err := quotaByTool(ctx, tx)
	if err != nil {
		return "", err
	}
	stop, err := quotaStop(ctx, tx, tenant)
	if err != nil {
		return "", err
	}
	for _, c := range cands {
		tool := toolOf(c.executor)
		var until *time.Time
		if err = tx.QueryRow(ctx, `SELECT max(cooldown_until) FROM agentrt.vendor_pool WHERE cooldown_until > clock_timestamp()
		  AND vendor_pool_id LIKE $1`, tool+":"+DefaultAccount+":%").Scan(&until); err != nil {
			return "", err
		}
		who := fmt.Sprintf("%s (%s, model %s, %s)", c.executor, c.provider, c.pattern, c.status)
		switch q, read := quota[tool]; {
		case until != nil:
			parts = append(parts, fmt.Sprintf("%s: the %s account's pools cool down until %s.", who, tool, until.UTC().Format(time.RFC3339)))
		case read && q.read && q.used >= stop:
			parts = append(parts, fmt.Sprintf("%s: its pool reads %.0f%% used, at or past the routing rule's stop of %.0f%%.", who, q.used*100, stop*100))
		default:
			parts = append(parts, fmt.Sprintf("%s: the platform finds no cooldown and no quota stop for it. Why no host took it is not "+
				"known here: the platform does not record whether a host offering %s with a model matching %q asked (a host offers what "+
				"its AGENTD_TOOLS say), or whether the hosts' slots were full.", who, c.executor, c.pattern))
		}
	}
	return strings.Join(parts, " "), nil
}
