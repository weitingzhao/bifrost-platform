package controller_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// cardAutoFiveSeconds is STG's card-auto-answer (v3's wait), for blocked
// cards, as if the Owner had signed it.
const cardAutoFiveSeconds = `INSERT INTO agentrt.rule (rule_id, rule_version, body_json, body_text, direction, signed_by, signature,
    effective_at, expires_at) VALUES ('card-auto-answer', 1,
    '{"after_seconds":5,"direction":"initial","effective_at":"2026-01-01T00:00:00Z","expires_at":"2100-01-01T00:00:00Z","kinds":["blocked"],"rule_id":"card-auto-answer","rule_version":1}',
    E'rule_id: card-auto-answer\nrule_version: 1\ndirection: initial\neffective_at: 2026-01-01T00:00:00Z\nexpires_at: 2100-01-01T00:00:00Z\nafter_seconds: 5\nkinds: [blocked]\n',
    'initial', 'test', 'test', '2026-01-01T00:00:00Z', '2100-01-01T00:00:00Z')`

// TD-402: a card opened just after a pass of the card rule loop is answered
// within its rule's wait and a second (at five seconds a pass, the TD-393
// walk measured 9.8). A fake clock: passes come every CardRuleEvery after
// one at 0, the card opens a quarter second after it, and each pass sees the
// card as old as the clock says.
func TestACardOpenedJustAfterAPassIsAnsweredWithinItsWaitAndASecond(t *testing.T) {
	pool := setup(t)
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
		id := store.NewTaskID()
		if _, err = store.ProposeTask(ctx, tx, "controller", store.Task{ID: id, MissionID: "M-2", OffPlanReason: "a goal",
			Role: "implement", Class: "go", Title: "t", SpecArtifact: spec, Effect: "branch", Repo: "bifrost-platform"}); err != nil {
			return err
		}
		if err = store.MarkReady(ctx, tx, "controller", id); err != nil {
			return err
		}
		if _, err = store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test"}}); err != nil {
			return err
		}
		// Past its budget's hours, so the pass blocks it and asks its card.
		_, err = tx.Exec(ctx, `UPDATE agentrt.mission SET created_at = clock_timestamp() - interval '5 hours' WHERE mission_id = 'M-2'`)
		return err
	}))
	w := &world{questions: map[string]store.Question{}}
	src := w.sources()
	var card store.Card
	src.Ask = func(_ context.Context, c store.Card) (string, error) {
		w.set(func() { card = c })
		return "q_8", nil
	}
	const opened = 250 * time.Millisecond // after the pass at 0
	var mu sync.Mutex
	var clock time.Duration
	answered := false
	src.Cards = func(context.Context) ([]store.RuntimeCard, error) {
		mu.Lock()
		defer mu.Unlock()
		if answered {
			return nil, nil
		}
		var c store.Card
		w.set(func() { c = card })
		return []store.RuntimeCard{{ID: "q_8", TaskID: c.TaskID, Recommended: string(rune('a' + c.Recommended - 1)),
			CreatedAt: time.Now().Add(-(clock - opened))}}, nil
	}
	src.RuleAnswer = func(context.Context, string, string, string, time.Time, string) error {
		mu.Lock()
		defer mu.Unlock()
		answered = true
		return nil
	}
	c := &controller.Controller{DB: pool, Sources: src}
	if _, err := c.BlockedCards(ctx); err != nil {
		t.Fatal(err)
	}
	var task string
	if w.set(func() { task = card.TaskID }); task == "" {
		t.Fatal("no card was asked")
	}
	_, err := pool.Exec(ctx, cardAutoFiveSeconds)
	must(t, err)
	const after = 5 * time.Second
	var waited time.Duration
	for k := 1; waited == 0; k++ {
		mu.Lock()
		clock = time.Duration(k) * controller.CardRuleEvery
		now := clock
		mu.Unlock()
		if now-opened > after+2*time.Second {
			t.Fatalf("not answered %v after it opened (a pass every %v)", now-opened, controller.CardRuleEvery)
		}
		n, err := c.AnswerByRule(ctx)
		must(t, err)
		if n == 1 {
			waited = now - opened
		}
	}
	if waited < after || waited > after+time.Second {
		t.Fatalf("answered %v after it opened; want within %v and %v", waited, after, after+time.Second)
	}
}
