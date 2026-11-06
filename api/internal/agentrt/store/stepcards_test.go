package store_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

var stepT0 = time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)

func stepAt(min int) time.Time { return stepT0.Add(time.Duration(min) * time.Minute) }

// oneStepCard is the only card under a key.
func oneStepCard(t *testing.T, out map[string][]store.StepCard, key string) store.StepCard {
	t.Helper()
	if len(out[key]) != 1 {
		t.Fatalf("under %q: %+v (all %+v)", key, out[key], out)
	}
	return out[key][0]
}

// A plan_node_binding row names the step of an approval and of a question.
func TestFoldStepCardsBinding(t *testing.T) {
	t.Parallel()
	out := store.FoldStepCards(store.StepCardsInput{
		Bindings: []store.StepBinding{{PlanNodeID: "S1", Kind: "approval", Ref: "17", EventID: 1}, {PlanNodeID: "S2", Kind: "question", Ref: "Q3", EventID: 2}},
		Approvals: []store.SeenCopy{{Ref: "17", Status: "pending", At: stepAt(1), EventID: 3},
			{Ref: "17", Status: "approved", At: stepAt(5), EventID: 4}},
		Questions: []store.SeenCopy{{Ref: "Q3", Status: "open", At: stepAt(2), EventID: 5}},
	})
	a := oneStepCard(t, out, "S1")
	if a.Kind != store.StepCardApproval || a.Ref != "17" || a.Via != store.StepViaBinding || len(a.Attempts) != 1 {
		t.Fatalf("approval %+v", a)
	}
	if at := a.Attempts[0]; at.N != 1 || !at.OpenedAt.Equal(stepAt(1)) || !at.ClosedAt.Equal(stepAt(5)) || at.Result.Status != "approved" || at.EventID != 4 {
		t.Fatalf("approval attempt %+v", at)
	}
	q := oneStepCard(t, out, "S2")
	if q.Kind != store.StepCardQuestion || q.Via != store.StepViaBinding || q.Attempts[0].QuestionID != "Q3" || !q.Attempts[0].ClosedAt.IsZero() {
		t.Fatalf("question %+v", q)
	}
}

// A question copy naming a task on a step gives that task's step, with the
// answer as its result.
func TestFoldStepCardsTask(t *testing.T) {
	t.Parallel()
	out := store.FoldStepCards(store.StepCardsInput{
		Tasks: map[string]string{"T1": "S4"},
		Questions: []store.SeenCopy{{Ref: "Q9", Status: "open", TaskID: "T1", At: stepAt(1), EventID: 1},
			{Ref: "Q9", Status: "answered", Option: "a", AnsweredBy: "owner", AnsweredAt: stepAt(3).Format(time.RFC3339Nano), TaskID: "T1", At: stepAt(4), EventID: 2}},
	})
	q := oneStepCard(t, out, "S4")
	if q.Via != store.StepViaTask || q.TaskID != "T1" {
		t.Fatalf("question %+v", q)
	}
	if r := q.Attempts[0].Result; r == nil || r.Option != "a" || r.AnsweredBy != "owner" {
		t.Fatalf("result %+v", q.Attempts[0])
	}
}

// A binding beats the task: the bound step wins over the task's.
func TestFoldStepCardsBindingBeatsTask(t *testing.T) {
	t.Parallel()
	out := store.FoldStepCards(store.StepCardsInput{
		Bindings:  []store.StepBinding{{PlanNodeID: "S-bound", Kind: "question", Ref: "Q1", EventID: 1}},
		Tasks:     map[string]string{"T1": "S-task"},
		Questions: []store.SeenCopy{{Ref: "Q1", Status: "open", TaskID: "T1", At: stepAt(1), EventID: 2}},
	})
	if len(out["S-task"]) != 0 {
		t.Fatalf("also under the task's step: %+v", out)
	}
	if q := oneStepCard(t, out, "S-bound"); q.Via != store.StepViaBinding {
		t.Fatalf("question %+v", q)
	}
}

func landingCard(n int, question string) store.CardRecord {
	return store.CardRecord{Repo: "bifrost-platform", Kind: store.CardRootFailed, Root: "r1", Op: "op-1", TriggerSeq: 7, N: n,
		Prep: store.CardPrepID(store.CardRootFailed, 7, n), Options: map[string]string{"a": "try_again", "b": "do_not_merge"},
		AskStarted: true, AskStartedAt: stepAt(10 * n), Bound: question != "", BoundAt: stepAt(10*n + 1), QuestionID: question}
}

// A landing card finds its step through its op's task; a card voided and
// opened again is two attempts, the void reason on the first, the answer on
// the second.
func TestFoldStepCardsLandingOpReopened(t *testing.T) {
	t.Parallel()
	first := landingCard(1, "Q-a")
	first.Closed, first.ClosedWhy = true, "expired"
	second := landingCard(2, "Q-b")
	second.Acked = true
	out := store.FoldStepCards(store.StepCardsInput{
		Tasks: map[string]string{"T7": "S7"},
		Ops:   map[string]landing.Operation{"op-1": {Task: "T7"}},
		Cards: []store.StepCardRecord{{Card: second, AckedAt: stepAt(30), EventID: 21}, {Card: first, ClosedAt: stepAt(15), EventID: 11}},
		Questions: []store.SeenCopy{{Ref: "Q-a", Status: "expired", At: stepAt(15), EventID: 12},
			{Ref: "Q-b", Status: "answered", Option: "b", AnsweredBy: "owner", AnsweredAt: stepAt(25).Format(time.RFC3339Nano), At: stepAt(26), EventID: 22}},
	})
	c := oneStepCard(t, out, "S7")
	if c.Kind != store.StepCardLanding || c.Via != store.StepViaLandingOp || c.TaskID != "T7" || len(c.Attempts) != 2 {
		t.Fatalf("card %+v", c)
	}
	a1, a2 := c.Attempts[0], c.Attempts[1]
	if a1.N != 1 || a1.QuestionID != "Q-a" || a1.VoidReason != "expired" || !a1.OpenedAt.Equal(stepAt(11)) || !a1.ClosedAt.Equal(stepAt(15)) || a1.Result != nil {
		t.Fatalf("first attempt %+v", a1)
	}
	if a2.N != 2 || a2.VoidReason != "" || a2.Result == nil || a2.Result.Option != "b" || a2.Result.Choice != "do_not_merge" || !a2.ClosedAt.Equal(stepAt(25)) {
		t.Fatalf("second attempt %+v %+v", a2, a2.Result)
	}
	if _, err := json.Marshal(out); err != nil {
		t.Fatal(err)
	}
}

// An STG card bound by its question to a step is under that step.
func TestFoldStepCardsSTGBinding(t *testing.T) {
	t.Parallel()
	c := store.CardRecord{Family: "stg", Repo: "bifrost-platform", Kind: store.CardSTGFailed, Batch: "b1", TriggerSeq: 3, N: 1,
		AskStarted: true, AskStartedAt: stepAt(1)}
	out := store.FoldStepCards(store.StepCardsInput{
		Bindings: []store.StepBinding{{PlanNodeID: "S2", Kind: "question", Ref: "Q-s", EventID: 1}},
		Cards:    []store.StepCardRecord{{Card: func() store.CardRecord { c.Bound, c.BoundAt, c.QuestionID = true, stepAt(2), "Q-s"; return c }()}},
	})
	if got := oneStepCard(t, out, "S2"); got.Kind != store.StepCardSTG || got.Via != store.StepViaBinding || !got.Attempts[0].OpenedAt.Equal(stepAt(2)) {
		t.Fatalf("card %+v", got)
	}
}

// A sign-off carries its step.
func TestFoldStepCardsSignoff(t *testing.T) {
	t.Parallel()
	out := store.FoldStepCards(store.StepCardsInput{
		Signoffs: []store.SignoffEvent{{PlanNodeID: "S5", Ref: "accept", By: "owner", At: stepAt(3), EventID: 9}},
	})
	s := oneStepCard(t, out, "S5")
	if s.Kind != store.StepCardSignoff || s.Ref != "accept" || s.Via != store.StepViaSignoff || len(s.Attempts) != 1 ||
		s.Attempts[0].EventID != 9 || s.Attempts[0].Result.AnsweredBy != "owner" {
		t.Fatalf("sign-off %+v", s)
	}
}

// A card with no step is kept under the empty key.
func TestFoldStepCardsUnbound(t *testing.T) {
	t.Parallel()
	out := store.FoldStepCards(store.StepCardsInput{
		Tasks:     map[string]string{"T-off": ""},
		Questions: []store.SeenCopy{{Ref: "Q-lost", Status: "open", TaskID: "T-off", At: stepAt(1), EventID: 1}},
		Cards:     []store.StepCardRecord{{Card: landingCard(1, "")}},
	})
	if len(out[""]) != 2 || len(out) != 1 {
		t.Fatalf("unbound %+v", out)
	}
	for _, c := range out[""] {
		if c.PlanNodeID != "" || c.Via != "" || len(c.Attempts) != 1 {
			t.Fatalf("unbound card %+v", c)
		}
	}
}
