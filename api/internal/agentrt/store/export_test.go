package store

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// SetHook installs a test hook (see hook) and returns the function that
// removes it.
func SetHook(f func(point, arg string)) func() {
	testHook.Store(&f)
	return func() { testHook.Store(nil) }
}

// FreezeEventClock makes every event written from now on carry the time at,
// and returns the function that undoes it.
func FreezeEventClock(at time.Time) func() {
	eventClock = func() time.Time { return at }
	return func() { eventClock = time.Now }
}

// MoveTaskForTest moves a task as the kernel does: the fixture that leases a
// given task directly (LeaseAttempt) moves it to leased with it.
func MoveTaskForTest(ctx context.Context, tx pgx.Tx, actor, taskID, to string) error {
	t, err := readTask(ctx, tx, taskID, true)
	if err != nil {
		return err
	}
	return moveTask(ctx, tx, actor, t, to, "test")
}

// LiveForTest is a live attempt as the projection reads one: its state,
// whether its host is known, when the host last polled, and its elapsed
// seconds.
func LiveForTest(id, state string, hostKnown bool, polled *time.Time, elapsed *int64, started time.Time) *ProgressAttempt {
	a := &ProgressAttempt{AttemptID: id, HostID: "h1", State: state, hostKnown: hostKnown, hostPolled: polled, ElapsedS: elapsed, started: started}
	if !started.IsZero() {
		a.StartedAt = started.UTC().Format(time.RFC3339Nano)
	}
	return a
}

// SortLiveForTest is the projection's order of live attempts.
func SortLiveForTest(list []*ProgressAttempt) []string {
	m := map[string]*ProgressAttempt{}
	for _, a := range list {
		m[a.AttemptID] = a
	}
	var ids []string
	for _, a := range sortedLive(m) {
		ids = append(ids, a.AttemptID)
	}
	return ids
}

// LimitPendingChangesForTest lowers the most plan changes needs_you lists,
// and returns the function that restores it.
func LimitPendingChangesForTest(n int) func() {
	old := pendingChangeLimit
	pendingChangeLimit = n
	return func() { pendingChangeLimit = old }
}

// WorstForTest is the status a mission shows of its tasks' statuses.
func WorstForTest(statuses ...string) string { return worst(statuses) }

// ChangeBlocksForTest is what changes would hold up, read in a transaction
// of their own.
func ChangeBlocksForTest(ctx context.Context, db DB, changes []PlanChange) (any, error) {
	var out any
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		var err error
		out, err = changeBlocks(ctx, tx, changes)
		return err
	})
	return out, err
}

// DecidePageForTest lowers how many waiting missions a decision pass reads
// at a time, and returns the function that restores it.
func DecidePageForTest(n int) func() {
	old := decidePageSize
	decidePageSize = n
	return func() { decidePageSize = old }
}

// ProposeUnboundedForTest proposes a mission as a platform before the
// proposal bounds did (review 320 A1): the same key and request, no bounds.
func ProposeUnboundedForTest(ctx context.Context, db DB, w Writer, key string, p MissionProposal) (map[string]any, error) {
	p, err := p.check()
	if err != nil {
		return nil, err
	}
	return idempotentAs(ctx, db, w, "missions", key, p.request(), nil, func(tx pgx.Tx) (map[string]any, error) {
		return proposeMissionTx(ctx, tx, w, p, key, nil)
	})
}

// EventTypesForTest is the closed vocabulary and the types with a body.
func EventTypesForTest() (vocabulary, written []string) {
	for t := range eventTypes {
		vocabulary = append(vocabulary, t)
	}
	for t := range bodies {
		written = append(written, t)
	}
	return vocabulary, written
}

// BlockedCardWhyForTest is the longest why a card's recommendation has.
var BlockedCardWhyForTest = strings.Repeat("w", RecommendWhyMax)

// BlockedPageForTest sets how many blocked tasks a pass reads at a time and
// returns the function that restores it.
func BlockedPageForTest(n int) func() {
	old := blockedPage.Swap(int64(n))
	return func() { blockedPage.Store(old) }
}

// ProposeIssueMissionForTest proposes a mission as take_issue would (its
// work id an issue's, outside the dev envelope) but passing ask, which
// take_issue never does: askMission refuses a BI- mission's card (review
// 522 A1).
func ProposeIssueMissionForTest(ctx context.Context, tx pgx.Tx, w Writer, workID string, ask Ask) (map[string]any, error) {
	p, err := MissionProposal{WorkID: workID, Title: "t", Goal: "g", Success: []string{"s"}, MaxAttempts: 100, MaxWallHours: 48,
		issueTake: true}.check()
	if err != nil {
		return nil, err
	}
	return proposeMissionTx(ctx, tx, w, p, "k-ask", ask)
}

// ProgressAtForTest is the projection at at, a host's silence counted from no
// earlier than since.
func ProgressAtForTest(ctx context.Context, db DB, at, since time.Time) (map[string]any, error) {
	return progressAt(ctx, db, at, since)
}

// QuestionSeenBodyForTest is the question_seen body of q as read, checked
// against the event's vocabulary.
func QuestionSeenBodyForTest(id string, q Question) (map[string]any, error) {
	body, _, err := questionSeenBody(id, q)
	if err != nil {
		return nil, err
	}
	return body, checkBody("question_seen", body)
}

// SeenCopy is an approval_seen or question_seen event as the external facts
// test reads it back: Ref is the approval number or question id.
type SeenCopy struct {
	Ref, Status, Option, AnsweredBy, AnsweredAt, TaskID string
	At                                                  time.Time
	EventID                                             int64
}

// SeenCopiesForTest reads the approval_seen or question_seen copies of refs,
// in event order.
func SeenCopiesForTest(ctx context.Context, tx pgx.Tx, typ, key string, refs []string) ([]SeenCopy, error) {
	var tasks []string
	rows, err := tx.Query(ctx, `SELECT event_id,at,COALESCE(task_id,''),COALESCE(body_json->>$2,''),COALESCE(body_json->>'status',''),COALESCE(body_json->>'option',''),COALESCE(body_json->>'answered_by',''),COALESCE(body_json->>'answered_at','') FROM agentrt.event WHERE event_type=$1 AND (body_json->>$2=ANY($3) OR task_id=ANY($4)) ORDER BY event_id`, typ, key, refs, tasks)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (SeenCopy, error) {
		var c SeenCopy
		return c, r.Scan(&c.EventID, &c.At, &c.TaskID, &c.Ref, &c.Status, &c.Option, &c.AnsweredBy, &c.AnsweredAt)
	})
}
