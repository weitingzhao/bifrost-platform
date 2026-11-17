//bifrost:owner owner

package store_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Regression tests for review 151.

// A5: a source is read under its record's lock. A read that a newer one
// overtakes is not recorded as a change back.
func TestOutsideReadsUnderTheLock(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	_, err := store.SeeApproval(ctx, w.rt, 16, approvalIs("pending"))
	ok(t, err)
	inside, release := make(chan struct{}), make(chan struct{})
	var released sync.Once
	t.Cleanup(func() { released.Do(func() { close(release) }) })
	slow := make(chan error, 1)
	go func() {
		_, seeErr := store.SeeApproval(ctx, pool, 16, func(context.Context) (store.ApprovalFact, bool) {
			close(inside)
			<-release
			return store.ApprovalFact{Status: "pending", Action: "test", Tier: "test"}, true
		})
		slow <- seeErr
	}()
	<-inside
	fast := make(chan error, 1)
	go func() {
		_, seeErr := store.SeeApproval(ctx, pool, 16, approvalIs(store.ApprovalExecuted))
		fast <- seeErr
	}()
	w.waitFor(t, "pg_advisory_xact_lock(742104", time.Now().Add(10*time.Second))
	released.Do(func() { close(release) })
	ok(t, <-slow)
	ok(t, <-fast)
	var statuses []string
	rows, err := w.rt.Query(ctx, `SELECT body_json->>'status' FROM agentrt.event WHERE event_type = 'approval_seen' ORDER BY event_id`)
	ok(t, err)
	statuses, err = pgx.CollectRows(rows, pgx.RowTo[string])
	ok(t, err)
	if strings.Join(statuses, ",") != "pending,executed" {
		t.Fatalf("recorded %v", statuses)
	}

	// Changes seen in order are all kept: pending, executed, pending.
	_, err = store.SeeApproval(ctx, w.rt, 16, approvalIs("pending"))
	ok(t, err)
	var n int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'approval_seen'`).Scan(&n))
	if n != 3 {
		t.Fatalf("%d changes recorded; want pending, executed, pending", n)
	}
}

// B2: an answered question is copied whole, or not at all.
func TestAnswerIsCopiedWhole(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	_, err := store.SeeQuestion(ctx, w.rt, "Q3", questionIs(store.Question{Status: store.QuestionAnswered, Option: "A", AnsweredAt: "2026-10-14T12:00:00Z"}))
	is(t, err, store.ErrState)
	_, err = store.SeeQuestion(ctx, w.rt, "Q3", questionIs(answered("Q3", "A")))
	ok(t, err)
	var copy map[string]any
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json FROM agentrt.event WHERE event_type = 'question_seen' ORDER BY event_id DESC LIMIT 1`).Scan(&copy))
	want := map[string]any{"question": "Q3", "status": "answered", "option": "A", "text": "yes", "answered_by": "owner",
		"answered_at": "2026-10-14T12:00:00Z"}
	for k, v := range want {
		if copy[k] != v {
			t.Fatalf("copy = %v", copy)
		}
	}
}
