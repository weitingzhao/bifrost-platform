package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The Console's blocked-card fixture (console/src/api/__fixtures__/question-blocked-card.testdb.json):
// GET /api/v1/questions/{id} of the card below, as the Owner's Console reads it. Off unless asked
// for; it writes the file, and the Console's tests read it:
//
//	CONSOLE_QUESTION_FIXTURE_WRITE=1 go test ./internal/server/ -run TestBlockedCardOnTheStore
const consoleBlockedCardFixture = "../../../console/src/api/__fixtures__/question-blocked-card.testdb.json"

// A blocked task's card on the real question store and a real database (the
// Owner 2026-10-20): three options, the third closing the mission; the
// recommended one keyed in the store, its why the option's detail and the
// body's first line. A card made by a pass whose transaction then failed is
// found again by its key on the next pass although its words changed since
// (its mission is older now): the runtime records that card, not an error on
// every pass. The answer "close" ends the mission.
func TestBlockedCardOnTheStore(t *testing.T) {
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	if _, err := store.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	conn := db.Connect(t, db.Runtime)
	if _, err := conn.Exec(ctx, agentrttest.DevBudgetRule); err != nil {
		t.Fatal(err)
	}
	task := store.NewTaskID()
	legacy := func(stmt string) {
		t.Helper()
		if err := store.InTx(ctx, conn, func(tx pgx.Tx) error {
			if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
				Body: map[string]any{"source": "test"}}); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, stmt)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.InTx(ctx, conn, func(tx pgx.Tx) error {
		if _, err := store.CreateMission(ctx, tx, "controller", store.Mission{ID: "M-3", Title: "goal", Goal: "a goal",
			Deadline: time.Now().Add(24 * time.Hour), Repos: []string{"bifrost-platform"}, Success: []string{store.GoalSuccess}}); err != nil {
			return err
		}
		if err := store.MoveMission(ctx, tx, "controller", "M-3", "active", "test"); err != nil {
			return err
		}
		spec, err := store.PutArtifact(ctx, tx, "controller", "task_spec", "text/markdown", []byte("# task\n"))
		if err != nil {
			return err
		}
		if _, err = store.ProposeTask(ctx, tx, "controller", store.Task{ID: task, MissionID: "M-3", OffPlanReason: "a goal", Role: "implement",
			Effect: "branch", Repo: "bifrost-platform", Class: "go", Title: "STG walk: a check that always fails", SpecArtifact: spec}); err != nil {
			return err
		}
		return store.MarkReady(ctx, tx, "controller", task)
	}); err != nil {
		t.Fatal(err)
	}
	legacy(`UPDATE agentrt.mission SET created_at = clock_timestamp() - interval '30 hours 10 minutes',
	  budget_json = '{"max_attempts": 9, "max_wall_hours": 2}' WHERE mission_id = 'M-3'`)
	if n, err := store.BlockOverBudget(ctx, conn); err != nil || n != 1 {
		t.Fatalf("blocked %d (%v)", n, err)
	}

	rt := newQuestionRuntime(filepath.Join(t.TempDir(), "questions"), nil)
	ask, read, ack := rt.agentRuntimeAsk(), rt.agentRuntimeQuestion(), rt.agentRuntimeAck()
	var made string
	failing := func(ctx context.Context, c store.Card) (string, error) {
		id, err := ask(ctx, c)
		if err != nil {
			return "", err
		}
		made = id
		return "", errors.New("the database went away after the card was made")
	}
	if n, err := store.AskBlocked(ctx, conn, failing); n != 0 || err == nil || made == "" {
		t.Fatalf("the first pass: asked %d (%v), card %q", n, err, made)
	}
	first, ok, err := rt.svc.Lookup(made)
	why := "started 30 hours ago, used 15× its 2 hours, nothing has run"
	if err != nil || !ok || len(first.Options) != 3 || first.Options[0].Label != store.BlockedRetry || first.Options[1].Label != store.BlockedDrop ||
		first.Options[2].Label != "Close mission M-3" || first.Recommended != "c" || first.Options[2].Detail != why ||
		first.Options[0].Detail != "" || first.Options[1].Detail != "" || first.TaskID != task || first.OwnerDuty != "goal" ||
		!strings.HasPrefix(first.Body, "Recommended: Close mission M-3 — "+why+"\n\n") {
		t.Fatalf("the card %+v %v %v", first, ok, err)
	}

	// Three days on, the card's words differ; the next pass records the card
	// the first made.
	legacy(`UPDATE agentrt.mission SET created_at = created_at - interval '3 days' WHERE mission_id = 'M-3'`)
	if n, err := store.AskBlocked(ctx, conn, ask); err != nil || n != 1 {
		t.Fatalf("the next pass: asked %d (%v)", n, err)
	}
	if q, ok := read(ctx, made); !ok || q.Status != "open" || q.Options != 3 {
		t.Fatalf("the card reads %+v %v; want open with 3 options", q, ok)
	}
	var opened string
	if err = conn.QueryRow(ctx, `SELECT body_json->>'question_id' FROM agentrt.event WHERE event_type = 'owner_wait_opened' AND task_id = $1`,
		task).Scan(&opened); err != nil || opened != made {
		t.Fatalf("recorded card %q (%v); want %s", opened, err, made)
	}

	if os.Getenv("CONSOLE_QUESTION_FIXTURE_WRITE") == "1" {
		res := rt.svc.Get(ctx, "owner-console", "admin", "admin", made)
		raw, err := json.MarshalIndent(map[string]any{"status": res.Code, "body": res.Body}, "", "  ")
		if err != nil || res.Code != 200 {
			t.Fatalf("the fixture: %d %v", res.Code, err)
		}
		if err = os.WriteFile(consoleBlockedCardFixture, append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	answer, _ := json.Marshal(map[string]any{"option": "c", "channel": "console"})
	if res := rt.svc.Answer(ctx, "owner-console", "admin", "admin", made, answer); res.Code >= 300 {
		t.Fatalf("answer = %d %v", res.Code, res.Body)
	}
	if n, err := store.DecideBlocked(ctx, conn, read, ack); err != nil || n != 1 {
		t.Fatalf("decided %d (%v)", n, err)
	}
	var mission, state string
	if err = conn.QueryRow(ctx, `SELECT m.state, t.state FROM agentrt.mission m JOIN agentrt.task t ON t.mission_id = m.mission_id
	  WHERE t.task_id = $1`, task).Scan(&mission, &state); err != nil || mission != "stopped" || state != "cancelled" {
		t.Fatalf("M-3 is %s, its task %s (%v)", mission, state, err)
	}
	if got, ok, err := rt.svc.Lookup(made); err != nil || !ok || got.Answer == nil || got.Answer.Read == nil {
		t.Fatalf("the answer was not acknowledged: %+v %v %v", got.Answer, ok, err)
	}
}

// Review 439 A3: only a card that says so (a blocked card) is taken for the
// card the store holds under its key, and only when that card names the same
// task and options; a plan change retried under its key with other content,
// or a card naming another task or other options, stays a conflict.
func TestOnlyTheSameCardIsTakenByItsKey(t *testing.T) {
	rt := newQuestionRuntime(filepath.Join(t.TempDir(), "questions"), nil)
	ctx := context.Background()
	ask := rt.agentRuntimeAsk()
	pc := store.Card{Key: strings.Repeat("ab", 16), Title: "Plan change to R2: a new item", Body: "A thread proposes P1"}
	first, err := ask(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	pc.Body = "A thread proposes P2"
	if id, err := ask(ctx, pc); err == nil || id != "" {
		t.Fatalf("a plan change's key with other content took card %q (%v); want a conflict", id, err)
	}
	task := "t_01M5192P0JPMJAP63ZKRPXJ2VV"
	blocked := store.Card{Key: strings.Repeat("cd", 16), Title: "Blocked in M-3: t", Body: "started 30 hours ago",
		Options: []string{store.BlockedRetry, store.BlockedDrop, "Close mission M-3"}, Recommended: 3, RecommendedWhy: "why",
		Meanwhile: "the task stays blocked", OwnerDuty: "goal", TaskID: task, Urgency: "when you can", SameByKey: true}
	card, err := ask(ctx, blocked)
	if err != nil || card == first {
		t.Fatalf("the blocked card: %q %v", card, err)
	}
	later := blocked
	later.Body = "started 4 days ago"
	if id, err := ask(ctx, later); err != nil || id != card {
		t.Fatalf("the same card in other words: %q %v; want %s", id, err, card)
	}
	for name, edit := range map[string]func(*store.Card){
		"another task":    func(c *store.Card) { c.TaskID = "t_01M5192P0JPMJAP63ZKRPXJ2VW" },
		"another mission": func(c *store.Card) { c.Options = []string{store.BlockedRetry, store.BlockedDrop, "Close mission M-4"} },
		"not asked so":    func(c *store.Card) { c.SameByKey = false },
	} {
		other := later
		edit(&other)
		if id, err := ask(ctx, other); err == nil || id != "" {
			t.Errorf("%s: took card %q (%v); want a conflict", name, id, err)
		}
	}
}
