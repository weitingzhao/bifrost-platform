package store_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// One read of a round: its steps in ordinal
// order, a step's accepted and landed task as events in time, and a card
// voided and asked again as one card of two attempts.
func TestReadRoundTimelineStepsEventsAndCards(t *testing.T) {
	t.Parallel()
	ctx := ownClock(t)
	w, c := dbWorld(t)
	task := dbAccepted(t, w, c, s2, "bifrost-platform")
	dbOp(t, w, c, task, dbNewA, "landed")

	// The landing card of the operation: the first asking (q1) expires and
	// is closed, the second (q2) is answered.
	opts := map[string]any{"a": "try_again", "b": "do_not_merge"}
	seeQuestion := func(id string, q store.Question) {
		t.Helper()
		c.dbTick()
		q.ID = id
		_, err := store.SeeQuestion(ctx, w.rt, id, func(context.Context) (store.Question, bool) { return q, true })
		ok(t, err)
	}
	card := func(name string, body map[string]any) {
		t.Helper()
		c.dbTick()
		ok(t, putCard(w, "controller", cardStream, name, body))
	}
	first, second := store.CardPrepID(store.CardRootFailed, 7, 1), store.CardPrepID(store.CardRootFailed, 7, 2)
	card("landing_card_prepared", preparedCard(task, store.CardRootFailed, 7, 1, opts))
	card("landing_card_ask_started", map[string]any{"prep": first})
	card("landing_card", map[string]any{"prep": first, "question_id": "q1"})
	seeQuestion("q1", store.Question{Status: "open"})
	seeQuestion("q1", store.Question{Status: "expired"})
	card("landing_card_closed", map[string]any{"prep": first, "question_id": "q1", "why": "expired"})
	card("landing_card_prepared", preparedCard(task, store.CardRootFailed, 7, 2, opts))
	card("landing_card_ask_started", map[string]any{"prep": second})
	card("landing_card", map[string]any{"prep": second, "question_id": "q2"})
	seeQuestion("q2", store.Question{Status: "open"})
	answeredAt := c.at.Add(10 * time.Second).UTC().Format(time.RFC3339Nano)
	seeQuestion("q2", store.Question{Status: store.QuestionAnswered, Option: "a", AnsweredBy: "owner", AnsweredAt: answeredAt})
	card("landing_card_acked", map[string]any{"prep": second, "question_id": "q2"})

	before := w.footprint(t)
	out, err := store.ReadRoundTimeline(ctx, w.rt, root, "")
	ok(t, err)
	if before != w.footprint(t) {
		t.Fatal("read wrote state")
	}
	if out["schema"] != "agentrt.plan_timeline_response/v1" || out["root"] != root || out["as_of"] == "" {
		t.Fatalf("response: %v", out)
	}
	if ws := out["warnings"].([]string); len(ws) != 0 {
		t.Fatalf("warnings: %v", ws)
	}
	if u := out["unbound_cards"].([]store.StepCard); len(u) != 0 {
		t.Fatalf("unbound cards: %+v", u)
	}
	steps := out["steps"].([]map[string]any)
	var order []string
	for _, s := range steps {
		order = append(order, s["plan_node_id"].(string))
	}
	if want := []string{root, item, s1, s2, s3, dbStep}; !slices.Equal(order, want) {
		t.Fatalf("order %v, want %v", order, want)
	}
	step := steps[3]
	if step["label"] != "2.1-2" || step["title"] != "s2" || step["node_kind"] != "step" || step["state"] == "" {
		t.Fatalf("step: %v", step)
	}

	// The task's moves, in time: accepted before landed, the landing naming
	// the task.
	var kinds []string
	for _, e := range step["events"].([]store.StepEvent) {
		kinds = append(kinds, e.Kind)
		if e.Kind == "landed" && (e.TaskID != task || e.Reason != commitB || e.Source != "note.landing") {
			t.Fatalf("landed: %+v", e)
		}
	}
	accepted, landed := slices.Index(kinds, "accepted"), slices.Index(kinds, "landed")
	if accepted < 0 || landed < accepted || !slices.Contains(kinds, "task_leased") || !slices.Contains(kinds, "submitted") {
		t.Fatalf("events: %v", kinds)
	}

	// One card, bound by its operation, with both askings.
	cards := step["cards"].([]store.StepCard)
	if len(cards) != 1 {
		t.Fatalf("cards: %+v", cards)
	}
	got := cards[0]
	if got.Kind != store.StepCardLanding || got.Via != store.StepViaLandingOp || got.TaskID != task || len(got.Attempts) != 2 {
		t.Fatalf("card: %+v", got)
	}
	a1, a2 := got.Attempts[0], got.Attempts[1]
	if a1.N != 1 || a1.QuestionID != "q1" || a1.VoidReason != "expired" || a1.ClosedAt.IsZero() {
		t.Fatalf("first attempt: %+v", a1)
	}
	if a2.N != 2 || a2.QuestionID != "q2" || a2.VoidReason != "" || a2.Result == nil || a2.Result.Status != store.QuestionAnswered ||
		a2.Result.Choice != "try_again" || a2.Result.AnsweredBy != "owner" {
		t.Fatalf("second attempt: %+v %+v", a2, a2.Result)
	}
	for _, s := range steps {
		if s["plan_node_id"] == s2 {
			continue
		}
		if len(s["cards"].([]store.StepCard)) != 0 || slices.ContainsFunc(s["events"].([]store.StepEvent), func(e store.StepEvent) bool { return e.TaskID != "" }) {
			t.Fatalf("%v has another step's work: %v", s["plan_node_id"], s)
		}
	}

	// One step of the round: only that step.
	one, err := store.ReadRoundTimeline(ctx, w.rt, root, s2)
	ok(t, err)
	if only := one["steps"].([]map[string]any); len(only) != 1 || only[0]["plan_node_id"] != s2 || len(only[0]["cards"].([]store.StepCard)) != 1 {
		t.Fatalf("one step: %v", only)
	}

	// An unknown round is not found; a step outside the round is invalid.
	if _, err := store.ReadRoundTimeline(ctx, w.rt, "R-none", ""); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("unknown root: %v", err)
	}
	if _, err := store.ReadRoundTimeline(ctx, w.rt, item, s1+"-none"); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("step outside: %v", err)
	}
}
