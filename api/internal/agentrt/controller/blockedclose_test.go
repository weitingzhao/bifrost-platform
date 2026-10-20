package controller_test

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The Owner 2026-10-20: "Close mission" on one card closes the mission, and
// the same pass withdraws its other open cards, so their places in the
// question store are free for that pass's new cards; the next pass reads them
// withdrawn and closes them. The cards recommend closing a mission this old.
func TestBlockedCardsCloseAMission(t *testing.T) {
	pool := setup(t)
	var tasks []string
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
		for range 2 {
			id := store.NewTaskID()
			if _, err = store.ProposeTask(ctx, tx, "controller", store.Task{ID: id, MissionID: "M-2", OffPlanReason: "a goal",
				Role: "implement", Class: "go", Title: "t", SpecArtifact: spec, Effect: "branch", Repo: "bifrost-platform"}); err != nil {
				return err
			}
			if err = store.MarkReady(ctx, tx, "controller", id); err != nil {
				return err
			}
			tasks = append(tasks, id)
		}
		// Five days old on a budget of four hours.
		if _, err = store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test"}}); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE agentrt.mission SET created_at = clock_timestamp() - interval '5 days' WHERE mission_id = 'M-2'`)
		return err
	}))
	w := &world{questions: map[string]store.Question{}}
	src := w.sources()
	var cards []store.Card
	var withdrawn []string
	src.Ask = func(_ context.Context, c store.Card) (string, error) {
		var id string
		w.set(func() {
			cards = append(cards, c)
			id = fmt.Sprintf("q_%d", len(cards))
			w.questions[id] = store.Question{ID: id, Status: "open"}
		})
		return id, nil
	}
	src.Withdraw = func(_ context.Context, id, _ string) error {
		w.set(func() {
			withdrawn = append(withdrawn, id)
			w.questions[id] = store.Question{ID: id, Status: "withdrawn"}
		})
		return nil
	}
	c := &controller.Controller{DB: pool, Sources: src}
	if _, err := c.BlockedCards(ctx); err != nil {
		t.Fatal(err)
	}
	w.set(func() {
		if len(cards) != 2 || cards[0].Recommended != 3 || cards[0].RecommendedWhy != "started 5 days ago, used 30× its 4 hours, nothing has run" {
			t.Fatalf("cards %+v", cards)
		}
		w.questions["q_1"] = store.Question{ID: "q_1", Status: store.QuestionAnswered, Option: store.CardCloseKey,
			AnsweredBy: "owner", AnsweredAt: "2026-10-20T15:00:00Z"}
	})
	if _, err := c.BlockedCards(ctx); err != nil {
		t.Fatal(err)
	}
	var state string
	must(t, pool.QueryRow(ctx, `SELECT state FROM agentrt.mission WHERE mission_id = 'M-2'`).Scan(&state))
	w.set(func() {
		if state != "stopped" || !slices.Equal(withdrawn, []string{"q_2"}) || len(cards) != 2 {
			t.Fatalf("M-2 is %s; withdrew %v; %d cards", state, withdrawn, len(cards))
		}
	})
	if _, err := c.BlockedCards(ctx); err != nil {
		t.Fatal(err)
	}
	var open int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event o WHERE o.event_type = 'owner_wait_opened' AND NOT EXISTS (
	  SELECT 1 FROM agentrt.event c WHERE c.event_type = 'owner_wait_closed' AND c.body_json->>'question_id' = o.body_json->>'question_id')`).Scan(&open))
	w.set(func() {
		if open != 0 || len(withdrawn) != 1 {
			t.Fatalf("%d cards still open; withdrew %v", open, withdrawn)
		}
	})
}

// TD-390: the controller answers a blocked card by the card-auto-answer rule
// in force, with the option the card recommends, once it has waited; with no
// rule it answers nothing.
func TestAnswerByRuleInTheController(t *testing.T) {
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
		_, err = tx.Exec(ctx, `UPDATE agentrt.mission SET created_at = clock_timestamp() - interval '5 days' WHERE mission_id = 'M-2'`)
		return err
	}))
	w := &world{questions: map[string]store.Question{}}
	src := w.sources()
	var card store.Card
	src.Ask = func(_ context.Context, c store.Card) (string, error) {
		w.set(func() { card = c })
		return "q_8", nil
	}
	type answer struct{ id, option, rule string }
	var answers []answer
	src.Cards = func(context.Context) ([]store.RuntimeCard, error) {
		var out []store.RuntimeCard
		w.set(func() {
			if card.Key != "" && len(answers) == 0 {
				out = []store.RuntimeCard{{ID: "q_8", TaskID: card.TaskID, Recommended: string(rune('a' + card.Recommended - 1)),
					CreatedAt: time.Now().Add(-31 * time.Minute)}}
			}
		})
		return out, nil
	}
	src.RuleAnswer = func(_ context.Context, id, option, rule string, _ time.Time, _ string) error {
		w.set(func() { answers = append(answers, answer{id, option, rule}) })
		return nil
	}
	c := &controller.Controller{DB: pool, Sources: src}
	if _, err := c.BlockedCards(ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := c.AnswerByRule(ctx); err != nil || n != 0 {
		t.Fatalf("with no rule: answered %d (%v)", n, err)
	}
	_, err := pool.Exec(ctx, agentrttest.CardAutoRule)
	must(t, err)
	if n, err := c.AnswerByRule(ctx); err != nil || n != 1 {
		t.Fatalf("answered %d (%v)", n, err)
	}
	w.set(func() {
		if len(answers) != 1 || answers[0] != (answer{"q_8", "c", "rule card-auto-answer v1"}) {
			t.Fatalf("answers %+v", answers)
		}
	})
	if n, err := (&controller.Controller{DB: pool, Sources: w.sources()}).AnswerByRule(ctx); err != nil || n != 0 {
		t.Fatalf("with no card lister: answered %d (%v)", n, err)
	}
}
