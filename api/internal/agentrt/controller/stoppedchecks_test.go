package controller_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// 11-06, M-308: the controller's blocked-card pass cancels the pending check
// of a blocked task before the budget and card passes, so the only card it
// asks is the blocked task's own.
func TestStoppedChecksInBlockedCards(t *testing.T) {
	pool := setup(t)
	var impl, check string
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := store.CreateMission(ctx, tx, "controller", store.Mission{ID: "M-2", Title: "goal", Goal: "g", Deadline: time.Now().Add(time.Hour),
			Repos: []string{"bifrost-platform"}, Success: []string{store.GoalSuccess}, MaxAttempts: 9, MaxWallHours: 4}); err != nil {
			return err
		}
		if err := store.MoveMission(ctx, tx, "controller", "M-2", "active", "test"); err != nil {
			return err
		}
		spec, err := store.PutArtifact(ctx, tx, "controller", "task_spec", "text/plain", []byte("spec"))
		if err != nil {
			return err
		}
		impl, check = store.NewTaskID(), store.NewTaskID()
		for _, task := range []store.Task{
			{ID: impl, Role: "implement", Effect: "branch"},
			{ID: check, Role: "verify", Effect: "none", Subject: impl},
		} {
			task.MissionID, task.OffPlanReason, task.Class, task.Title, task.SpecArtifact, task.Repo = "M-2", "a goal", "go", "t", spec, "bifrost-platform"
			if _, err = store.ProposeTask(ctx, tx, "controller", task); err != nil {
				return err
			}
			if err = store.MarkReady(ctx, tx, "controller", task.ID); err != nil {
				return err
			}
		}
		return store.MoveTask(ctx, tx, "controller", impl, "blocked", "test")
	}))
	w := &world{questions: map[string]store.Question{}}
	src := w.sources()
	var cards []store.Card
	src.Ask = func(_ context.Context, c store.Card) (string, error) {
		var id string
		w.set(func() {
			cards = append(cards, c)
			id = fmt.Sprintf("q_%d", len(cards))
			w.questions[id] = store.Question{ID: id, Status: "open"}
		})
		return id, nil
	}
	c := &controller.Controller{DB: pool, Sources: src}
	if _, err := c.BlockedCards(ctx); err != nil {
		t.Fatal(err)
	}
	var state, reason string
	must(t, pool.QueryRow(ctx, `SELECT t.state, e.body_json->>'reason' FROM agentrt.task t JOIN agentrt.event e ON e.task_id = t.task_id
	  WHERE t.task_id = $1 AND e.event_type = 'task_state_changed' ORDER BY e.event_id DESC LIMIT 1`, check).Scan(&state, &reason))
	if want := "its subject task " + impl + " is blocked; this check can no longer decide anything"; state != "cancelled" || reason != want {
		t.Fatalf("the check is %s: %q; want cancelled: %q", state, reason, want)
	}
	w.set(func() {
		if len(cards) != 1 || cards[0].TaskID != impl {
			t.Fatalf("cards %+v; want the blocked task's only", cards)
		}
	})
}
