package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seatproto"
)

// An assessment's report and its record (TD-391; assess.go). The report is
// one JSON object; it is taken only whole: an option of the item, a
// confidence, two to four reasons each citing refs the spec gave (each ref is
// recorded as the record it names, so the Console links to it), and what the
// seat could not check. What the report says is advice, recorded on the
// item's stream and read by nothing that acts.

// Bounds of an assessment.
const (
	minReasons        = 2
	maxReasons        = 4
	maxReasonRunes    = 300
	maxReasonRefs     = 4
	maxUnchecked      = 5
	maxUncheckedRunes = 200
)

// Confidences an assessment states.
var confidences = []string{"high", "medium", "low"}

// approvalOptions are an approval's options as an assessment names them.
var approvalOptions = []AssessOption{{Key: "approve", Label: "Approve"}, {Key: "reject", Label: "Reject"}}

// ApprovalOptions are an approval's options for its assessment.
func ApprovalOptions() []AssessOption { return slices.Clone(approvalOptions) }

// assessState is what an item's stream says: whether it was asked (when,
// its task, its option keys and refs) and its outcome, an assessment (the
// option it recommends) or why there is none.
type assessState struct {
	asked       bool
	askedAt     time.Time
	task        string
	options     []string
	refs        map[string]any
	done        bool
	recommended string
	unavailable string
}

// assessStateOf reads the item's stream; the caller holds it when it acts on
// what it reads.
func assessStateOf(ctx context.Context, tx pgx.Tx, stream string) (assessState, error) {
	var st assessState
	rows, err := tx.Query(ctx, `SELECT COALESCE(task_id, ''), body_json, at FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'
	  ORDER BY seq`, stream)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		var task string
		var body map[string]any
		var at time.Time
		if err = rows.Scan(&task, &body, &at); err != nil {
			return st, err
		}
		if ask, ok := body["ask"].(map[string]any); ok && !st.asked {
			st.asked, st.askedAt, st.task = true, at, task
			st.refs, _ = ask["refs"].(map[string]any)
			if keys, ok := ask["options"].([]any); ok {
				for _, k := range keys {
					if s, ok := k.(string); ok {
						st.options = append(st.options, s)
					}
				}
			}
		}
		if a, ok := body["assessment"].(map[string]any); ok && !st.done {
			st.done = true
			st.recommended, _ = a["recommended"].(string)
		}
		if why, ok := body["unavailable"].(string); ok && !st.done {
			st.done, st.unavailable = true, why
		}
	}
	return st, rows.Err()
}

// lets says whether a rule may answer a card that recommends rec on what its
// assessment says now, and why: when none was asked, none can be had, or it
// agrees; not while it may still come, nor when it recommends another option
// (the Owner's, TD-391).
func (st assessState) lets(rec string, now time.Time) (bool, string) {
	switch {
	case !st.asked:
		return true, "no AI assessment was asked"
	case st.done && st.unavailable != "":
		return true, "its AI assessment is unavailable (" + st.unavailable + ")"
	case st.done:
		return st.recommended == rec, "its AI assessment agrees"
	case st.late(now):
		return true, "no AI assessment came within " + timeoutText()
	}
	return false, ""
}

// late says whether an assessment asked and not in has run past its time.
func (st assessState) late(now time.Time) bool {
	return st.asked && !st.done && now.Sub(st.askedAt) >= AssessTimeout
}

func timeoutText() string { return fmt.Sprintf("%d minutes", int(AssessTimeout/time.Minute)) }

// noneInTime is why an assessment past its time has none.
func noneInTime() string { return "none came within " + timeoutText() }

// cardAssessment reads a card's assessment, its stream held.
func cardAssessment(ctx context.Context, tx pgx.Tx, question string) (assessState, error) {
	stream := AssessStream(AssessCard, question)
	if err := lockStream(ctx, tx, stream); err != nil {
		return assessState{}, err
	}
	return assessStateOf(ctx, tx, stream)
}

// assessmentOfTask is the stream of the item task's mission assesses, "" for
// a task of any other mission.
func assessmentOfTask(ctx context.Context, tx pgx.Tx, t taskRow) (string, error) {
	var stream string
	err := tx.QueryRow(ctx, `SELECT COALESCE(scope_json->>'assessment_of', '') FROM agentrt.mission WHERE mission_id = $1`, t.mission).Scan(&stream)
	return stream, err
}

// acceptAssessment records the report of a submitted assessment task (t,
// locked; its submitted attempt's conclusion and report, aerr when it has
// none): the assessment when it is one, else why there is none; either way
// the task ends. One that comes after its item's outcome is recorded (none
// came in time) is not used. It returns "assessed", "unavailable", "late" or
// "" when there was nothing to do.
func acceptAssessment(ctx context.Context, tx pgx.Tx, t taskRow, stream, attempt, conclusion, report string, aerr error) (string, error) {
	if t.state != "submitted" || aerr != nil {
		return "", nil
	}
	if err := lockStream(ctx, tx, stream); err != nil {
		return "", err
	}
	st, err := assessStateOf(ctx, tx, stream)
	if err != nil {
		return "", err
	}
	if st.done || !st.asked || st.task != t.id {
		return "late", endAssessmentTask(ctx, tx, t, "its assessment came after "+stream+" had its outcome, or was not its ask")
	}
	// Past its time a report is not taken, whoever reaches the stream first
	// (review 448 A1): the same outcome as EndAssessments'.
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return "", err
	}
	if st.late(now) {
		return "unavailable", recordUnavailable(ctx, tx, t, stream, noneInTime())
	}
	switch {
	case conclusion != "done":
		return "unavailable", recordUnavailable(ctx, tx, t, stream, fmt.Sprintf("its attempt ended %q", conclusion))
	case report == "":
		return "unavailable", recordUnavailable(ctx, tx, t, stream, "its attempt handed in nothing")
	}
	var body []byte
	if err = tx.QueryRow(ctx, `SELECT body FROM agentrt.artifact_blob WHERE artifact_id = $1`, report).Scan(&body); err != nil {
		return "", err
	}
	text := strings.TrimSpace(strings.ToValidUTF8(string(body), "?"))
	if alerts := seatproto.Alerts(text); len(alerts) > 0 {
		if err = tellOwnerOfAlerts(ctx, tx, t.mission, t.id, "", "assessing "+stream, alerts); err != nil {
			return "", err
		}
	}
	a, why := parseAssessment(seatproto.WithoutAlerts(text), st.options, st.refs)
	if why != "" {
		return "unavailable", recordUnavailable(ctx, tx, t, stream, "its report is not an assessment: "+why)
	}
	var executor string
	if err = tx.QueryRow(ctx, `SELECT executor_actor_id FROM agentrt.attempt WHERE attempt_id = $1`, attempt).Scan(&executor); err != nil {
		return "", err
	}
	a["by"], a["task"], a["report_artifact_id"] = executor, t.id, report
	if _, err = Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", MissionID: t.mission, TaskID: t.id,
		Body: map[string]any{"text": fmt.Sprintf("AI assessment: recommends %s, %s confidence", a["recommended"], a["confidence"]),
			"assessment": a}}); err != nil {
		return "", err
	}
	return "assessed", moveTask(ctx, tx, "controller", t, "accepted", "assessment recorded on "+stream)
}

// recordUnavailable records why the item has no assessment and ends its
// task.
func recordUnavailable(ctx context.Context, tx pgx.Tx, t taskRow, stream, why string) error {
	why = oneLineRunes(why, assessFieldRunes)
	if _, err := Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", MissionID: t.mission, TaskID: t.id,
		Body: map[string]any{"text": "AI assessment unavailable: " + why, "unavailable": why}}); err != nil {
		return err
	}
	return endAssessmentTask(ctx, tx, t, "the assessment is unavailable: "+why)
}

// endAssessmentTask cancels a submitted assessment task whose report is not
// used, and stops its mission.
func endAssessmentTask(ctx context.Context, tx pgx.Tx, t taskRow, why string) error {
	if err := moveTaskBy(ctx, tx, "controller", t, "cancelled", why, closeMoves); err != nil {
		return err
	}
	return stopAssessment(ctx, tx, t.mission, why)
}

// stopAssessment stops an assessment's mission that has not ended.
func stopAssessment(ctx context.Context, tx pgx.Tx, mission, why string) error {
	var state string
	if err := tx.QueryRow(ctx, `SELECT state FROM agentrt.mission WHERE mission_id = $1 FOR NO KEY UPDATE`, mission).Scan(&state); err != nil {
		return err
	}
	if !slices.Contains([]string{"proposed", "active", "paused"}, state) {
		return nil
	}
	return MoveMission(ctx, tx, "controller", mission, "stopped", why)
}

// assessmentReport is the report's shape; any other field is refused.
type assessmentReport struct {
	Recommended string `json:"recommended"`
	Confidence  string `json:"confidence"`
	Reasons     []struct {
		Text     string   `json:"text"`
		Evidence []string `json:"evidence"`
	} `json:"reasons"`
	Unchecked []string `json:"unchecked"`
}

// parseAssessment reads a report as an assessment of an item with these
// option keys and refs: the record to keep, or why it is none. A reason
// whose refs are not all the spec's is dropped; fewer than two good reasons
// is no assessment.
func parseAssessment(text string, options []string, refs map[string]any) (map[string]any, string) {
	raw, why := jsonOf(text)
	if why != "" {
		return nil, why
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	var r assessmentReport
	if err := dec.Decode(&r); err != nil {
		return nil, "not the assessment's shape: " + oneLineRunes(err.Error(), 120)
	}
	// One object and nothing after it (review 448 B1): a second, a
	// correction or a contradiction, is not chosen between.
	var more json.RawMessage
	if err := dec.Decode(&more); !errors.Is(err, io.EOF) {
		return nil, "more than one JSON object"
	}
	if !slices.Contains(options, r.Recommended) {
		return nil, fmt.Sprintf("recommended %q is not one of %v", oneLineRunes(r.Recommended, 40), options)
	}
	if !slices.Contains(confidences, r.Confidence) {
		return nil, fmt.Sprintf("confidence %q is not one of %v", oneLineRunes(r.Confidence, 40), confidences)
	}
	if len(r.Reasons) > maxReasons {
		return nil, fmt.Sprintf("%d reasons; at most %d", len(r.Reasons), maxReasons)
	}
	var reasons []any
	dropped := 0
	for _, rs := range r.Reasons {
		said := oneLineRunes(rs.Text, maxReasonRunes+1)
		var cited []any
		ok := said != "" && utf8.RuneCountInString(said) <= maxReasonRunes && len(rs.Evidence) >= 1 && len(rs.Evidence) <= maxReasonRefs
		seen := map[string]bool{}
		for _, e := range rs.Evidence {
			ref, found := refs[strings.TrimSpace(e)].(map[string]any)
			if !found {
				ok = false
				break
			}
			if !seen[strings.TrimSpace(e)] {
				seen[strings.TrimSpace(e)] = true
				cited = append(cited, refRecord(strings.TrimSpace(e), ref))
			}
		}
		if !ok {
			dropped++
			continue
		}
		reasons = append(reasons, map[string]any{"text": said, "evidence": cited})
	}
	if len(reasons) < minReasons {
		return nil, fmt.Sprintf("%d reasons with evidence from its spec; at least %d (%d dropped)", len(reasons), minReasons, dropped)
	}
	if len(r.Unchecked) > maxUnchecked {
		return nil, fmt.Sprintf("%d unchecked; at most %d", len(r.Unchecked), maxUnchecked)
	}
	unchecked := []any{}
	for _, u := range r.Unchecked {
		if u = oneLineRunes(u, maxUncheckedRunes); u != "" {
			unchecked = append(unchecked, u)
		}
	}
	out := map[string]any{"recommended": r.Recommended, "confidence": r.Confidence, "reasons": reasons, "unchecked": unchecked}
	if dropped > 0 {
		out["dropped_reasons"] = dropped
	}
	return out, ""
}

// refRecord is a cited ref as recorded: the ref and the record it names.
func refRecord(ref string, rec map[string]any) map[string]any {
	out := map[string]any{"ref": ref}
	for k, v := range rec {
		out[k] = v
	}
	return out
}

// jsonOf is the JSON object a report holds: the inside of its one ```
// fence when it has one, else from its first { to its last }. A second
// fence, or an object outside the one fence, is more than one object
// (review 450 B1: the first of two fences was taken, the second ignored);
// why says so.
func jsonOf(text string) (raw, why string) {
	if strings.Contains(text, "```") {
		if strings.Count(text, "```") != 2 {
			return "", "more than one JSON object"
		}
		i := strings.Index(text, "```")
		j := i + 3 + strings.Index(text[i+3:], "```")
		inside, outside := text[i+3:j], text[:i]+text[j+3:]
		if strings.Contains(outside, "{") {
			return "", "more than one JSON object"
		}
		if nl := strings.IndexByte(inside, '\n'); nl >= 0 {
			inside = inside[nl+1:]
		}
		text = inside
	}
	i, j := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if i < 0 || j < i {
		return "", "no JSON object"
	}
	return text[i : j+1], ""
}

// EndAssessments records unavailable every assessment that can no longer
// come: its task blocked, cancelled or otherwise ended without one, or none
// within AssessTimeout of its ask; its task is cancelled (a live attempt
// with it) and its mission stopped. It returns how many it ended.
func EndAssessments(ctx context.Context, db DB) (int, error) {
	type open struct{ mission, stream string }
	var list []open
	if err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT mission_id, scope_json->>'assessment_of' FROM agentrt.mission
		  WHERE scope_json ? 'assessment_of' AND state IN ('proposed','active','paused') ORDER BY created_at, mission_id`)
		if err != nil {
			return err
		}
		list, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (open, error) {
			var o open
			return o, r.Scan(&o.mission, &o.stream)
		})
		return err
	}); err != nil {
		return 0, err
	}
	n := 0
	var errs []error
	for _, o := range list {
		did, err := endAssessment(ctx, db, o.mission, o.stream)
		if err != nil {
			errs = append(errs, fmt.Errorf("assessment %s: %w", o.stream, err))
		}
		if did {
			n++
		}
	}
	return n, errors.Join(errs...)
}

// endAssessment ends one assessment's mission when its assessment can no
// longer come. Locks as a mission's close takes them: its live attempts, its
// tasks (a task held by another pass is left to the next), the item's
// stream, the mission.
func endAssessment(ctx context.Context, db DB, mission, stream string) (bool, error) {
	did := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		did = false
		attempts, err := liveOf(ctx, tx, mission, "FOR UPDATE OF a")
		if err != nil {
			return err
		}
		left, err := unfinishedOf(ctx, tx, mission, "FOR NO KEY UPDATE NOWAIT")
		if err != nil {
			return err
		}
		if err = lockStream(ctx, tx, stream); err != nil {
			return err
		}
		st, err := assessStateOf(ctx, tx, stream)
		if err != nil {
			return err
		}
		var now time.Time
		if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
		why := ""
		if st.done {
			// The outcome is recorded: an accepted assessment's mission is
			// closed as done (close.go); anything left after one that is
			// unavailable is cancelled with its mission.
			if st.unavailable == "" || len(left) == 0 {
				return nil
			}
			why = "the assessment is unavailable: " + st.unavailable
		} else {
			var state, reason string
			if err = tx.QueryRow(ctx, `SELECT t.state, COALESCE((SELECT e.body_json->>'reason' FROM agentrt.event e WHERE e.task_id = t.task_id
			    AND e.event_type = 'task_state_changed' ORDER BY e.event_id DESC LIMIT 1), '')
			  FROM agentrt.task t WHERE t.task_id = $1`, st.task).Scan(&state, &reason); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			switch {
			case !st.asked:
				why = "it was never asked"
			case state == "blocked":
				why = "its task was blocked: " + oneLineRunes(reason, 200)
			case slices.Contains([]string{"cancelled", "rejected", "accepted"}, state):
				why = "its task ended " + state + " without one"
			case st.late(now):
				why = noneInTime()
			default:
				return nil
			}
			why = oneLineRunes(why, assessFieldRunes)
			if _, err = Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", MissionID: mission, TaskID: st.task,
				Body: map[string]any{"text": "AI assessment unavailable: " + why, "unavailable": why}}); err != nil {
				return err
			}
			why = "the assessment is unavailable: " + why
		}
		if err = cancelAll(ctx, tx, attempts, left, why); err != nil {
			return err
		}
		did = true
		return stopAssessment(ctx, tx, mission, why)
	})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "55P03" {
		return false, nil // a task held by another pass: the next pass looks again
	}
	return did, err
}
