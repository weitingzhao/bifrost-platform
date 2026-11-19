package seenfacts_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
)

// The fixture of the tests that moved here from package store (TD-582): a
// migrated database with the seed's actors and nothing else, and the readers
// a test hands the record of what was seen.

var ctx = context.Background()

func ok(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func seedOnly(ctx context.Context, owner *pgx.Conn) error {
	seeded, err := actor.Seed(ctx, owner)
	if err == nil && !seeded {
		err = errors.New("seed did nothing on an empty database")
	}
	return err
}

type world struct {
	db *agentrttest.DB
	rt *pgx.Conn
}

func newWorld(t *testing.T) *world {
	t.Helper()
	db := agentrttest.MigratedWith(t, "seeded", seedOnly)
	return &world{db: db, rt: db.Connect(t, db.Runtime)}
}

// waitFor returns once a backend of this database waits for a lock in a
// statement containing fragment, and fails the test if that has not happened
// by the deadline: a race the test means to stage did not happen.
func (w *world) waitFor(t *testing.T, fragment string, deadline time.Time) {
	t.Helper()
	w.waitForN(t, fragment, 1, deadline)
}

// waitForN is waitFor for at least n backends.
func (w *world) waitForN(t *testing.T, fragment string, want int, deadline time.Time) {
	t.Helper()
	ctx := context.Background()
	for {
		var n int
		ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database()
		  AND wait_event_type = 'Lock' AND position($1 in query) > 0`, fragment).Scan(&n))
		if n >= want {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("nothing waited for a lock in %q in time; the case proves nothing", fragment)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// answered is a full answer: option, who, when.
func answered(id, option string) seenfacts.Question {
	return seenfacts.Question{ID: id, Status: seenfacts.QuestionAnswered, Option: option, Text: "yes", AnsweredBy: "owner",
		AnsweredAt: "2026-10-14T12:00:00Z"}
}

// Readers of outside records that always return the given value.
func approvalIs(status string) func(context.Context) (seenfacts.ApprovalFact, bool) {
	return func(context.Context) (seenfacts.ApprovalFact, bool) {
		return seenfacts.ApprovalFact{Status: status, Action: "test", Tier: "test"}, true
	}
}

func questionIs(q seenfacts.Question) func(context.Context) (seenfacts.Question, bool) {
	return func(context.Context) (seenfacts.Question, bool) { return q, true }
}

// questionSeenBody is the question_seen body of q as read, checked against the
// event's vocabulary.
func questionSeenBody(id string, q seenfacts.Question) (map[string]any, error) {
	q.ID = id
	body, err := q.Body()
	if err != nil {
		return nil, err
	}
	return body, eventlog.CheckBody("question_seen", body)
}

func is(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("err = %v; want %v", err, target)
	}
}
