package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
)

// keyedStore answers asks as the question store does for a blocked card:
// a key it holds with the same options is that card (SameByKey), the same
// key with other options a conflict.
type keyedStore struct {
	mu   sync.Mutex
	opts map[string][]string
}

func (k *keyedStore) ask(_ context.Context, c store.Card) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.opts == nil {
		k.opts = map[string][]string{}
	}
	if have, ok := k.opts[c.Key]; ok && !slices.Equal(have, c.Options) {
		return "", errors.New("the question store answered 409: idempotency key reused with different content")
	}
	k.opts[c.Key] = c.Options
	return "q_" + c.Key[:8], nil
}

// Review 440 A1: a card the store made for a pass that then failed, and the
// task moved to another mission before the next pass (a plan change rebinds
// its step): the next pass asks a card of its own, naming the mission the
// task is in now, rather than meeting the old card's key on every pass.
func TestMovedTaskGetsACardOfItsOwn(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-2", "active", "test") }))
	moved := w.ready(t, store.Task{}) // on step s1 of M-1
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.FailWorkForTest(ctx, tx, moved) }))
	var k keyedStore
	failing := func(ctx context.Context, c store.Card) (string, error) {
		if _, err := k.ask(ctx, c); err != nil {
			return "", err
		}
		return "", errors.New("the database went away after the card was made")
	}
	if n, err := store.AskBlocked(ctx, w.rt, failing); n != 0 || err == nil {
		t.Fatalf("the failing pass: asked %d (%v)", n, err)
	}
	plan := &asker{}
	_, err := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "k1", root, "rebind", []store.PlanChange{{Op: "rebind_step", Node: s1, MissionID: "M-2"}}, plan.ask)
	ok(t, err)
	plan.answer(plan.lastID(), store.QuestionAnswered, store.CardApplyKey)
	if n, err := store.DecidePlanChanges(ctx, w.rt, plan.question, nil); err != nil || n != 1 {
		t.Fatalf("the rebind: %d %v", n, err)
	}
	if n, err := store.AskBlocked(ctx, w.rt, k.ask); err != nil || n != 1 {
		t.Fatalf("the next pass: asked %d (%v)", n, err)
	}
	var opened string
	ok(t, w.rt.QueryRow(ctx, `SELECT mission_id FROM agentrt.event WHERE event_type = 'owner_wait_opened' AND task_id = $1`, moved).Scan(&opened))
	if opened != "M-2" || len(k.opts) != 2 {
		t.Fatalf("the card was asked in %s; the store holds %d cards", opened, len(k.opts))
	}
}

// Review 440 B1: the pass after a close reads every leftover task, past its
// first page.
func TestCancelAfterCloseReadsEveryPage(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	a := &asker{}
	w.closingCard(t, a)
	if n, err := store.DecideBlocked(ctx, w.rt, a.question, nil); err != nil || n != 1 {
		t.Fatalf("decided %d (%v)", n, err)
	}
	var late []string
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		for range 101 {
			id := store.NewTaskID()
			if _, err := store.ProposeTask(ctx, tx, "controller", store.Task{ID: id, MissionID: "M-3", OffPlanReason: "a goal", Role: "implement",
				Effect: "branch", Repo: "bifrost-platform", Class: "go", Title: "t", SpecArtifact: w.spec}); err != nil {
				return err
			}
			late = append(late, id)
		}
		return nil
	}))
	if n, err := store.CancelAfterClose(ctx, w.rt); err != nil || n != 101 {
		t.Fatalf("cancelled %d (%v); want all 101", n, err)
	}
	var left int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.task WHERE mission_id = 'M-3' AND state <> 'cancelled'`).Scan(&left))
	if left != 0 {
		t.Fatalf("%d tasks of M-3 left", left)
	}
}

// Review 440 C1: a task committed after the close locked the mission's tasks
// and before it read them again under the mission's lock is not among those
// locked: the close starts again and cancels it too.
// Serial: it holds the package's test hook (SetHook).
func TestCloseMissionReadsItsTasksAgain(t *testing.T) {
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	a := &asker{}
	w.closingCard(t, a)
	late := store.NewTaskID()
	tries := 0
	unhook := store.SetHook(func(point, arg string) {
		if point != "close: tasks locked" || arg != "M-3" {
			return
		}
		tries++
		if tries == 1 {
			ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
				_, err := store.ProposeTask(ctx, tx, "controller", store.Task{ID: late, MissionID: "M-3", OffPlanReason: "a goal", Role: "implement",
					Effect: "branch", Repo: "bifrost-platform", Class: "go", Title: "t", SpecArtifact: w.spec})
				return err
			}))
		}
	})
	defer unhook()
	pool := w.db.Pool(t, w.db.Runtime)
	if n, err := store.DecideBlocked(ctx, pool, a.question, nil); err != nil || n != 1 {
		t.Fatalf("decided %d (%v)", n, err)
	}
	if tries != 2 || w.state(t, "task", late) != "cancelled" {
		t.Fatalf("%d tries; the late task is %s", tries, w.state(t, "task", late))
	}
}

// Review 440 C2: four-byte characters in every free part (a repository,
// title, reason, words, the failed command and its output) still make a card
// the question store takes.
func TestBlockedCardFitsFourByteText(t *testing.T) {
	t.Parallel()
	wide := strings.Repeat("𝄞", 300)
	report, err := json.Marshal(map[string]any{"commands": []map[string]any{{"argv": []string{"sh", "-c", wide}, "exit_code": 1,
		"tail": strings.Repeat("𝄞", 100)}}})
	ok(t, err)
	budget := "One round needs up to 3 attempts (implementation, verification, review); mission M-3 has used 1000000 of its 1000000 attempts " +
		"and 123456.7 of its 100000 hours, so this answer also raises its budget to 1000003 attempts and 223457 hours, recorded as yours."
	for _, words := range []string{"", wide} {
		body := store.BlockedCardBodyFullForTest(wide, "deterministic", string(report), wide, wide, words, budget, strings.Repeat("w", store.RecommendWhyMax))
		if n := questions.BodyBytes(body); n > questions.BodyLimit || !strings.Contains(body, "Close mission M-3: every task") {
			t.Fatalf("%d bytes (the store takes %d):\n%s", n, questions.BodyLimit, body)
		}
	}
}
