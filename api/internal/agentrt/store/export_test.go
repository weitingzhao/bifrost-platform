package store

import (
	"context"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"

	"github.com/jackc/pgx/v5"
)

// SetHook installs a test hook (see hook) and returns the function that
// removes it.
func SetHook(f func(point, arg string)) func() {
	testHook.Store(&f)
	return func() { testHook.Store(nil) }
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

// EventTypesForTest is the closed vocabulary and the types with a body.
func EventTypesForTest() (vocabulary, written []string) { return eventlog.EventTypes() }

// BlockedCardWhyForTest is the longest why a card's recommendation has.
var BlockedCardWhyForTest = strings.Repeat("w", RecommendWhyMax)

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
