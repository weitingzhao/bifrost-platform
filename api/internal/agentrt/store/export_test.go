package store

import (
	"context"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"

	"github.com/jackc/pgx/v5"
)

// SetHook installs a test hook (see hook) and returns the function that
// removes it.
func SetHook(f func(point, arg string)) func() {
	testHook.Store(&f)
	return func() { testHook.Store(nil) }
}

// LimitPendingChangesForTest lowers the most plan changes needs_you lists,
// and returns the function that restores it.
func LimitPendingChangesForTest(n int) func() {
	old := planstore.PendingChangeLimit
	planstore.PendingChangeLimit = n
	return func() { planstore.PendingChangeLimit = old }
}

// ChangeBlocksForTest is what changes would hold up, read in a transaction
// of their own.
func ChangeBlocksForTest(ctx context.Context, db DB, changes []planstore.PlanChange) (any, error) {
	var out any
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		var err error
		out, err = planstore.ChangeBlocks(ctx, tx, changes)
		return err
	})
	return out, err
}

// EventTypesForTest is the closed vocabulary and the types with a body.
func EventTypesForTest() (vocabulary, written []string) { return eventlog.EventTypes() }

// QuestionSeenBodyForTest is the question_seen body of q as read, checked
// against the event's vocabulary.
func QuestionSeenBodyForTest(id string, q seenfacts.Question) (map[string]any, error) {
	q.ID = id
	body, err := q.Body()
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
