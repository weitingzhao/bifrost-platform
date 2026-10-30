package store_test

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/questioncards"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
)

// Serial: FreezeEventClock and signing key; full adapter -> PG answer -> Ack.
func TestStgCardsPGOwnerAdapter(t *testing.T) {
	t.Setenv("APPROVAL_NOTIFY_URL", "")
	t.Setenv("OPS_VIEWER_ENV", "prod")
	for _, tc := range []struct{ mode, letter, outcome, why string }{
		{"exhausted", "a", "applied", "release_granted"}, {"exhausted", "b", "applied", "release_withheld"},
		{"blocked", "a", "acknowledged", "ack"}, {"blocked", "b", "held", "remind_later"},
		{"floor", "a", "acknowledged", "ack"}, {"floor", "b", "held", "remind_later"},
		{"quarantine", "a", "acknowledged", "ack"}, {"quarantine", "b", "held", "remind_later"}, {"quarantine", "c", "applied", "quarantine_closed"},
	} {
		t.Run(tc.mode+"/"+tc.letter, func(t *testing.T) {
			ctx := context.Background()
			x, _ := b5gFixture(t, tc.mode)
			svc := questions.New(filepath.Join(t.TempDir(), "questions"), nil)
			svc.SetClock(store.DecisionAt)
			q := questioncards.New(svc)
			preps, err := store.PrepareLandingCards(ctx, x.w.rt, "bifrost-platform", store.DecisionAt())
			ok(t, err)
			var card store.CardRecord
			for _, prep := range preps {
				_, err = store.AskLandingCard(ctx, x.w.rt, q, "bifrost-platform", prep, store.DecisionAt(), true)
				ok(t, err)
			}
			_, bound, err := store.LandingCardsToSettle(ctx, x.w.rt, "bifrost-platform")
			ok(t, err)
			for _, c := range bound {
				if tc.mode != "floor" || c.Kind == store.CardSTGIneligible {
					card = c
					break
				}
			}
			if card.QuestionID == "" {
				t.Fatal("no card", bound)
			}
			raw, _ := json.Marshal(map[string]string{"option": tc.letter, "channel": "console"})
			res := svc.Answer(ctx, "owner-console", "admin", "admin", card.QuestionID, raw)
			if res.Code != http.StatusOK {
				t.Fatal(res)
			}
			fact, found, err := q.Lookup(ctx, card.QuestionID)
			ok(t, err)
			if !found || !fact.OwnerAnswer || fact.AnsweredBy != "admin" {
				t.Fatal(fact)
			}
			// Binding refusal must happen before any outcome or business edge.
			bad := card
			bad.QuestionID = "another-question"
			before := x.read(t).LastSeq
			if _, _, err = store.HandleLandingCardAnswer(ctx, x.w.rt, bad, fact, store.DecisionAt()); err == nil || x.read(t).LastSeq != before {
				t.Fatal("forged binding wrote", err)
			}
			status, err := store.SettleLandingCard(ctx, x.w.rt, q, card, store.DecisionAt())
			ok(t, err)
			if status != "acked" {
				t.Fatal(status)
			}
			view, found, err := store.ReadLandingCard(ctx, x.w.rt, card.Repo, card.QuestionID)
			ok(t, err)
			if !found || view.Outcome["outcome"] != tc.outcome || view.Outcome["why"] != tc.why || view.Outcome["by"] != "admin" || view.Outcome["owner_answer"] != true {
				t.Fatal(view)
			}
			state := x.read(t)
			principal, found, err := svc.Lookup(card.QuestionID)
			ok(t, err)
			if !found || principal.Answer == nil || principal.Answer.By != "admin" || principal.Answer.Approval || principal.Answer.Read == nil {
				t.Fatal("answer/approval/Ack contract", principal)
			}
			status, err = store.SettleLandingCard(ctx, x.w.rt, q, card, store.DecisionAt())
			ok(t, err)
			if status != "acked" || x.read(t).LastSeq != state.LastSeq {
				t.Fatal("replay reapplied business")
			}
			if tc.mode == "exhausted" && (len(state.Grants) != 1 || state.Budget[store.StgBudgetKey{R: commitB, Gen: 1}] != 3) {
				t.Fatal("budget history changed")
			}
			if tc.why == "quarantine_closed" && state.Batches["stg:bifrost-platform:1"].Tries[1].Occupied() {
				t.Fatal("quarantine not closed")
			}
		})
	}
}
