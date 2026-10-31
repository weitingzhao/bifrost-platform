package store_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// TD-463 ratchet: neither evidence nor paid tasks without an Owner request;
// concurrent clicks/dispatch create exactly one request and one task per item.
func TestAssessmentRequiresOwnerRequest(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	for _, kind := range []string{store.AssessCard, store.AssessApproval} {
		item := store.AssessItem{Kind: kind, ID: "463", Title: "Assessment opt-in", CreatedAt: time.Now(), Options: store.ApprovalOptions()}
		if _, fresh, err := store.AssessmentQuery(ctx, w.rt, item, false); err != nil || fresh {
			t.Fatalf("unrequested evidence: %v %v", fresh, err)
		}
		if task, err := store.GiveAssessment(ctx, w.rt, item, store.Evidence{}, nil, false); err != nil || task != "" {
			t.Fatalf("unrequested task: %q %v", task, err)
		}
		for _, actor := range []string{"controller", "seat:chief_of_staff", "thread:test"} {
			err := store.RequestAssessment(ctx, w.rt, store.Writer{Actor: actor}, kind, item.ID, func(context.Context) error { return nil })
			if !errors.Is(err, store.ErrNotOwner) {
				t.Fatalf("%s: %v", actor, err)
			}
		}
		err := store.RequestAssessment(ctx, w.rt, store.Writer{Actor: "owner"}, kind, item.ID,
			func(context.Context) error { return fmt.Errorf("%w: item is closed", store.ErrState) })
		if !errors.Is(err, store.ErrState) {
			t.Fatalf("closed initial request: %v", err)
		}
		raw, err := store.ReadAssessment(ctx, w.rt, kind, item.ID)
		ok(t, err)
		if string(raw) != "[]" {
			t.Fatalf("refused request left events: %s", raw)
		}
		var wg sync.WaitGroup
		errs := make(chan error, 4)
		for range 4 {
			db := w.db.Connect(t, w.db.Runtime)
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs <- store.RequestAssessment(ctx, db, store.Writer{Actor: "owner"}, kind, item.ID, func(context.Context) error { return nil })
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			ok(t, err)
		}
		if _, fresh, err := store.AssessmentQuery(ctx, w.rt, item, false); err != nil || !fresh {
			t.Fatalf("requested evidence: %v %v", fresh, err)
		}
		tasks := make(chan string, 4)
		errs = make(chan error, 4)
		for range 4 {
			db := w.db.Connect(t, w.db.Runtime)
			wg.Add(1)
			go func() {
				defer wg.Done()
				task, err := store.GiveAssessment(ctx, db, item, store.Evidence{}, nil, false)
				tasks <- task
				errs <- err
			}()
		}
		wg.Wait()
		close(tasks)
		close(errs)
		for err := range errs {
			ok(t, err)
		}
		n := 0
		for task := range tasks {
			if task != "" {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("%s: dispatched %d times", kind, n)
		}
		var requests int
		ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = $1 AND body_json ? 'assessment_requested'`, store.AssessStream(kind, item.ID)).Scan(&requests))
		if requests != 1 {
			t.Fatalf("%d request events", requests)
		}
		if _, fresh, err := store.AssessmentQuery(ctx, w.rt, item, false); err != nil || fresh {
			t.Fatalf("dispatch repeated: %v %v", fresh, err)
		}
		// A repeated click is still rejected when the item has since closed.
		err = store.RequestAssessment(ctx, w.rt, store.Writer{Actor: "owner"}, kind, item.ID, func(context.Context) error { return fmt.Errorf("%w: item is closed", store.ErrState) })
		if !errors.Is(err, store.ErrState) {
			t.Fatalf("closed replay: %v", err)
		}
	}
}

// Serial: FreezeEventClock.
func TestAssessmentKeepsLegacyResultsAndBoundsQueueWait(t *testing.T) {
	w := newKernelWorld(t)
	item := store.AssessItem{Kind: store.AssessApproval, ID: "464"}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.Append(ctx, tx, store.Event{Stream: store.AssessStream(item.Kind, item.ID), Type: "note", Actor: "controller",
			Body: map[string]any{"text": "AI assessment requested from the seat", "ask": map[string]any{"options": []string{"approve", "reject"}}}})
		return err
	}))
	requestAssessment(t, w.rt, item)
	if _, fresh, err := store.AssessmentQuery(ctx, w.rt, item, false); err != nil || fresh {
		t.Fatalf("legacy was requeued: %v %v", fresh, err)
	}
	item.ID = "465"
	unfreeze := store.FreezeEventClock(time.Now().Add(-store.AssessTimeout - time.Minute))
	requestAssessment(t, w.rt, item)
	unfreeze()
	_, err := store.EndAssessments(ctx, w.rt)
	ok(t, err)
	if _, why := w.assessed(t, item.Kind, item.ID); why == "" {
		t.Fatal("expired request has no reason")
	}
	if task, err := store.GiveAssessment(ctx, w.rt, item, store.Evidence{}, nil, false); err != nil || task != "" {
		t.Fatalf("expired request dispatched: %q %v", task, err)
	}
}
