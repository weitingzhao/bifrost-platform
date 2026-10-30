package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/stgrelease"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
	"k8s.io/apimachinery/pkg/types"
)

// All time comes from b5fWorld's SELECT now(), before creating the mission.
// The fixture uses real Append/Parse/Fold for every release transition.
func b5gFixture(t *testing.T, mode string) (*b5Fixture, store.Appended) {
	t.Helper()
	w, at := b5fWorld(t, "enabled")
	root := s66Landed(t, w, "M-3", "bifrost-platform", at)
	at = store.DecisionAt().Add(time.Minute)
	store.FreezeEventClock(at)
	x := b5Snapshot(t, w, root, "land:"+root+":1", at)
	member := map[string]any{"root": root, "op": x.op, "R": commitB}
	snap := x.read(t).Batches[b5Batch].Tries[1].Snapshot.Record
	if mode == "quarantine" {
		b5gStart(t, x, 1)
		a := b5Append(t, w, b5Record("quarantined", "batch", "try", 1, "why", "unknown_execution", "at", store.DecisionAt().Format(time.RFC3339Nano)))
		return x, a
	}
	var ref store.Appended
	if mode == "exhausted" {
		for n := int64(1); n <= 3; n++ {
			if n > 1 {
				due := x.read(t).Debits[commitB].NotBefore
				b5Append(t, w, b5Record("retry", "batch", "try", n, "not_before", due.Format(time.RFC3339Nano)))
				store.FreezeEventClock(due)
				b5Append(t, w, b5Record("snapshot", "batch", "try", n, "inputs", snap["inputs"], "base", snap["base"], "set", snap["set"], "not_before", due.Format(time.RFC3339Nano)))
			}
			b5gStart(t, x, n)
			b5Append(t, w, b5Record("bound", "batch", "try", n, "uid", fmt.Sprintf("b5g-run-%d", n)))
			store.FreezeEventClock(store.DecisionAt().Add(time.Minute))
			b5Append(t, w, b5Record("run_evidence", "batch", "try", n, "uid", fmt.Sprintf("b5g-run-%d", n), "status", "Failed", "raw", map[string]any{"results": []any{}, "child_refs": []any{}, "conditions": []any{}}, "at", store.DecisionAt().Format(time.RFC3339Nano)))
			ref = b5Append(t, w, b5Record("verify", "batch", "try", n, "outcome", "failed", "why", "run_failed", "evidence", ""))
			b5gEffect(t, x, n)
		}
		b5Append(t, w, b5Record("member", "batch", "root", root, "op", x.op, "R", commitB, "to", "exhausted", "ref", ref.Seq))
		b5Append(t, w, b5Record("settled", "batch", "members", []any{member}, "why", "exhausted"))
	}
	why := map[string]string{"exhausted": "budget_exhausted", "blocked": "ci_red", "floor": "hard_floor"}[mode]
	block := store.StgBlockID(commitB, commitA, why, "b5g")
	a := b5Append(t, w, b5Record("blocked", "block", "block", block, "why", why, "P", commitB, "base", snap["base"], "first_at", store.DecisionAt().Format(time.RFC3339Nano), "evidence_version", "b5g", "detail", map[string]any{"members": []any{member}}))
	if mode == "floor" {
		a = b5Append(t, w, b5Record("withheld", "grant", "grant", "hard-floor", "why", "hard_floor", "trigger_seq", a.Seq, "members", []any{member}))
	}
	return x, a
}

// Only external workload reads and archive cleanup are substituted. The
// production controller constructs the effect and PostgresLedger persists it.
// Other execution methods stay nil: this fixture must never send a release.
type b5gEffectRuns struct {
	stgrelease.Execution
	workloads map[string]any
	reads     int
}

func (r *b5gEffectRuns) ObserveWorkloads(context.Context) (map[string]any, error) {
	r.reads++
	return r.workloads, nil
}
func (*b5gEffectRuns) RemoveArchivedFinalizer(context.Context, delivery.NamedDeliverInput, types.UID) error {
	return nil // The terminal archive was already appended by the fixture.
}

func b5gEffect(t *testing.T, x *b5Fixture, n int64) {
	t.Helper()
	before := x.read(t)
	rule := inForce(t, x.w, "landing:dev", store.DecisionAt())
	if rule == nil {
		t.Fatal("effect fixture has no current landing rule")
	}
	runs := &b5gEffectRuns{workloads: x.obs.Workloads}
	c := &stgrelease.Controller{DB: x.w.rt, Tenant: "dev", Runs: runs,
		Policy: func() stgrelease.WindowPolicy {
			return stgrelease.WindowPolicy{Environment: "prod", Rule: &rule.Rule}
		}}
	defer func() { ok(t, c.Close()) }()
	ok(t, c.Pass(ctx))
	after := x.read(t)
	effect := after.Batches[b5Batch].Tries[n].Effect
	want, err := json.Marshal(x.obs.Workloads)
	ok(t, err)
	got, err := json.Marshal(effect.Record["workloads"])
	ok(t, err)
	if runs.reads != 1 || after.LastSeq != before.LastSeq+1 || effect.Seq != after.LastSeq || effect.Record.String("class") != "unchanged" || string(got) != string(want) || !effect.Record.Time("at").Equal(store.DecisionAt()) {
		t.Fatal("controller did not persist the observed unchanged effect", effect)
	}
	if !reflect.DeepEqual(before.Budget, after.Budget) || !reflect.DeepEqual(before.Debits, after.Debits) {
		t.Fatal("effect recording changed budget or backoff")
	}
}

func b5gStart(t *testing.T, x *b5Fixture, n int64) {
	t.Helper()
	b5Append(t, x.w, b5Record("started", "batch", "try", n, "intent_seq", 0, "run", fmt.Sprintf("stgrel-1-%d-%s", n, commitB[:8]),
		"labels", map[string]any{"bifrost.io/stg-release": "true", "bifrost.io/stg-release-n": "1", "bifrost.io/stg-release-try": fmt.Sprint(n)},
		"annotations", map[string]any{"bifrost.io/stg-release-batch": b5Batch, "bifrost.io/stg-release-intent": "0"}, "fingerprint", strings.Repeat("0", 64), "spend", []any{},
		"lease", map[string]any{"who": fmt.Sprintf("stgrel-bifrost-platform-1-%d", n), "gen": 1}, "deadline", store.DecisionAt().Add(2*time.Minute).Format(time.RFC3339Nano)))
}

func b5gCard(t *testing.T, x *b5Fixture, f *fakeCards, kind string) store.CardRecord {
	t.Helper()
	cardPass(t, x.w, f, store.DecisionAt())
	for _, c := range cardsNow(t, x.w) {
		if c.Kind == kind && !c.Ended() {
			return c
		}
	}
	t.Fatal("card not asked", kind, cardsNow(t, x.w))
	return store.CardRecord{}
}

// Serial: shared event clock, real transaction commits and Ack failure.
func TestStgCardsPGAnswerBeforeAckAndReplay(t *testing.T) {
	for _, letter := range []string{"a", "b"} {
		t.Run(letter, func(t *testing.T) {
			x, trigger := b5gFixture(t, "exhausted")
			f := newFakeCards(store.DecisionAt)
			c := b5gCard(t, x, f, store.CardSTGFailed)
			if c.Family != "stg" || c.TriggerSeq != trigger.Seq || c.StopGeneration != 0 || c.Root != "" || c.Block == "" || c.Batch != "" {
				t.Fatal(c)
			}
			before := x.read(t)
			f.answer(c.QuestionID, letter, store.DecisionAt())
			f.ackFails = true
			if _, err := store.SettleLandingCard(ctx, x.w.rt, f.source(), c, store.DecisionAt()); err == nil {
				t.Fatal("Ack failure was hidden")
			}
			after := x.read(t)
			if len(after.Grants) != 1 || len(after.Answers) != 1 || after.Blocks[c.Block].IsOpen() || after.Budget[store.StgBudgetKey{R: commitB, Gen: 1}] != 3 || after.Debits[commitB] != before.Debits[commitB] {
				t.Fatal("non-atomic budget decision")
			}
			if letter == "a" && after.Generations[commitB] != 2 || letter == "b" && len(after.Withheld) != 1 {
				t.Fatal("wrong choice", after)
			}
			for range 3 {
				_, err := store.SettleLandingCard(ctx, x.w.rt, f.source(), c, store.DecisionAt())
				if err == nil || x.read(t).LastSeq != after.LastSeq || f.asks != 1 {
					t.Fatal("Ack retry authorized twice", err)
				}
			}
			f.ackFails = false
			state, err := store.SettleLandingCard(ctx, x.w.rt, f.source(), c, store.DecisionAt())
			ok(t, err)
			if state != "acked" || !cardsNow(t, x.w)[c.Prep].Acked || x.read(t).LastSeq != after.LastSeq {
				t.Fatal("Ack retry did not finish", state)
			}
			view, found, err := store.ReadLandingCard(ctx, x.w.rt, c.Repo, c.QuestionID)
			ok(t, err)
			if !found || view.Family != "stg" || view.Block != c.Block || view.Outcome["outcome"] != "applied" || view.NextStep == "" {
				t.Fatal(view)
			}
			delete(f.byID, c.QuestionID)
			kept, found, err := store.ReadLandingCard(ctx, x.w.rt, c.Repo, c.QuestionID)
			ok(t, err)
			if !found || !reflect.DeepEqual(view, kept) {
				t.Fatal("receipt depended on question retention")
			}
		})
	}
}

// Serial: real rule/stream locks; none must fence every card entry.
func TestStgCardsPGNoneDoesNotAskOrWrite(t *testing.T) {
	for _, stage := range []string{"trigger", "prepared", "bound", "answered", "handled"} {
		t.Run(stage, func(t *testing.T) {
			x, _ := b5gFixture(t, "exhausted")
			f := newFakeCards(store.DecisionAt)
			var c store.CardRecord
			if stage == "prepared" {
				_, err := store.PrepareLandingCards(ctx, x.w.rt, "bifrost-platform", store.DecisionAt())
				ok(t, err)
			} else if stage != "trigger" {
				c = b5gCard(t, x, f, store.CardSTGFailed)
			}
			if stage == "answered" || stage == "handled" {
				f.answer(c.QuestionID, "a", store.DecisionAt())
			}
			if stage == "handled" {
				f.ackFails = true
				_, err := store.SettleLandingCard(ctx, x.w.rt, f.source(), c, store.DecisionAt())
				if err == nil {
					t.Fatal("expected Ack failure")
				}
				f.ackFails = false
			}
			x.none(t)
			before, cards, asks := x.read(t), cardsNow(t, x.w), f.asks
			count := x.w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_stream_id IN ('landing:bifrost-platform','landing-cards:bifrost-platform')`)
			ctl := &controller.Controller{DB: x.w.rt, Env: "prod", Now: store.DecisionAt}
			ctl.LandingCards = f.source()
			for range 3 {
				ok(t, ctl.LandingCardsPass(ctx))
			}
			if stage == "answered" {
				outcome, _, err := store.HandleLandingCardAnswer(ctx, x.w.rt, c, f.byID[c.QuestionID].q, store.DecisionAt())
				ok(t, err)
				if outcome != "" {
					t.Fatal("none handled answer", outcome)
				}
			}
			if f.asks != asks || x.read(t).LastSeq != before.LastSeq || !reflect.DeepEqual(cards, cardsNow(t, x.w)) || count != x.w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_stream_id IN ('landing:bifrost-platform','landing-cards:bifrost-platform')`) {
				t.Fatal("none changed cards or ledger")
			}
			for _, q := range f.byID {
				if q.acked {
					t.Fatal("none acknowledged suspended card")
				}
			}
		})
	}
}

// Serial: controller quota pause shares the PG-backed durable preparation.
func TestStgCardsPGQuotaRetainsTriggerAndPreparation(t *testing.T) {
	x, trigger := b5gFixture(t, "exhausted")
	f := newFakeCards(store.DecisionAt)
	src := f.source()
	allowed, calls := false, 0
	ask := src.AskRaw
	src.AskRaw = func(ctx context.Context, key string, request map[string]any) (string, time.Time, error) {
		calls++
		if !allowed {
			return "", time.Time{}, store.ErrCardQuota
		}
		return ask(ctx, key, request)
	}
	ctl := &controller.Controller{DB: x.w.rt, Env: "prod", Now: store.DecisionAt}
	ctl.LandingCards = src
	if err := ctl.LandingCardsPass(ctx); !errors.Is(err, store.ErrCardQuota) {
		t.Fatal(err)
	}
	asked, _, err := store.LandingCardsToSettle(ctx, x.w.rt, "bifrost-platform")
	ok(t, err)
	if len(asked) != 1 || !asked[0].AskStarted || asked[0].TriggerSeq != trigger.Seq || calls != 1 {
		t.Fatal(asked, calls)
	}
	for range 3 {
		ok(t, ctl.LandingCardsPass(ctx))
	}
	if calls != 1 || len(cardsNow(t, x.w)) != 1 {
		t.Fatal("quota was hammered or trigger duplicated")
	}
	// A restarted controller recovers the same prep/key once quota clears.
	allowed = true
	ctl = &controller.Controller{DB: x.w.rt, Env: "prod", Now: store.DecisionAt}
	ctl.LandingCards = src
	ok(t, ctl.LandingCardsPass(ctx))
	c := cardsNow(t, x.w)[asked[0].Prep]
	if !c.Bound || c.Key != asked[0].Key || c.TriggerSeq != trigger.Seq || len(cardsNow(t, x.w)) != 1 || f.asks != 1 {
		t.Fatal("quota lost or duplicated trigger", c)
	}
}

// Serial: card lifecycle and 24-hour reminders use a database-anchored clock.
func TestStgCardsPGNoticesRemindersExpiryAndResolution(t *testing.T) {
	for _, mode := range []string{"blocked", "floor", "quarantine"} {
		t.Run(mode, func(t *testing.T) {
			x, _ := b5gFixture(t, mode)
			f := newFakeCards(store.DecisionAt)
			kind := store.CardSTGBlocked
			if mode == "floor" {
				kind = store.CardSTGIneligible
			}
			c := b5gCard(t, x, f, kind)
			before := x.read(t)
			at := store.DecisionAt()
			f.answer(c.QuestionID, "b", at)
			_, err := store.SettleLandingCard(ctx, x.w.rt, f.source(), c, at)
			ok(t, err)
			if len(x.read(t).Grants) != len(before.Grants) || !reflect.DeepEqual(x.read(t).Blocks, before.Blocks) {
				t.Fatal("reminder changed business state")
			}
			store.FreezeEventClock(at.Add(23 * time.Hour))
			_, err = store.PrepareLandingCards(ctx, x.w.rt, c.Repo, store.DecisionAt())
			ok(t, err)
			if len(cardsAtKind(t, x.w, kind)) != 1 {
				t.Fatal("reminded before 24h")
			}
			store.FreezeEventClock(at.Add(24 * time.Hour))
			second := b5gCard(t, x, f, kind)
			if second.N != 2 || second.TriggerSeq != c.TriggerSeq {
				t.Fatal("reminder changed interval", second)
			}
			f.byID[second.QuestionID].q.Status = "expired"
			_, err = store.SettleLandingCard(ctx, x.w.rt, f.source(), second, store.DecisionAt())
			ok(t, err)
			store.FreezeEventClock(at.Add(30 * time.Hour))
			third := b5gCard(t, x, f, kind)
			if third.N != 3 {
				t.Fatal("expired card not replaced")
			}
			letter := "a"
			if mode == "quarantine" {
				letter = "c"
			}
			f.answer(third.QuestionID, letter, store.DecisionAt())
			_, err = store.SettleLandingCard(ctx, x.w.rt, f.source(), third, store.DecisionAt())
			ok(t, err)
			final := x.read(t)
			if mode == "quarantine" {
				if final.Batches[b5Batch].Tries[1].Occupied() || final.Budget[store.StgBudgetKey{R: commitB, Gen: 1}] != 1 {
					t.Fatal("quarantine did not close with budget kept")
				}
			} else if !reflect.DeepEqual(final.Blocks, before.Blocks) || !reflect.DeepEqual(final.Withheld, before.Withheld) {
				t.Fatal("ack bypassed block/floor")
			}
		})
	}
}

func cardsAtKind(t *testing.T, w *world, kind string) []store.CardRecord {
	t.Helper()
	var out []store.CardRecord
	for _, c := range cardsNow(t, w) {
		if kind == "" || c.Kind == kind {
			out = append(out, c)
		}
	}
	return out
}

// Serial: distinct PostgreSQL connections serialize competing answers.
func TestStgCardsPGConcurrentAnswerAndMootAfterCover(t *testing.T) {
	for _, covered := range []bool{false, true} {
		t.Run(fmt.Sprint(covered), func(t *testing.T) {
			x, _ := b5gFixture(t, "exhausted")
			f := newFakeCards(store.DecisionAt)
			c := b5gCard(t, x, f, store.CardSTGFailed)
			if covered {
				at := store.DecisionAt().Add(time.Minute)
				b5fEvidence(t, x.w, at, "cluster/cicd/covered")
				b5fCover(t, x.w, store.StgMemberKey{Root: x.root, Op: x.op, R: commitB}, "cluster/cicd/covered", at.Add(time.Minute))
			}
			f.answer(c.QuestionID, "a", store.DecisionAt())
			q := f.byID[c.QuestionID].q
			type result struct {
				outcome string
				err     error
			}
			results := make(chan result, 2)
			runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			for _, db := range []store.DB{x.w.rt, x.w.db.Connect(t, x.w.db.Runtime)} {
				safego.Go("stg-card-answer-test", func() {
					out, _, err := store.HandleLandingCardAnswer(runCtx, db, c, q, store.DecisionAt())
					results <- result{out, err}
				})
			}
			want := "applied"
			if covered {
				want = "moot"
			}
			for range 2 {
				select {
				case r := <-results:
					ok(t, r.err)
					if r.outcome != want {
						t.Fatal(r)
					}
				case <-runCtx.Done():
					t.Fatal("answer locks did not finish")
				}
			}
			s := x.read(t)
			if len(s.Answers) != 1 || covered && len(s.Grants) != 0 || !covered && len(s.Grants) != 1 {
				t.Fatal("answer was not unique or revived covered member")
			}
		})
	}
}

// Serial: a block's automatic resolution withdraws the same durable card.
func TestStgCardsPGResolvedAndBindingRefusals(t *testing.T) {
	x, trigger := b5gFixture(t, "blocked")
	f := newFakeCards(store.DecisionAt)
	c := b5gCard(t, x, f, store.CardSTGBlocked)
	f.answer(c.QuestionID, "a", store.DecisionAt())
	q := f.byID[c.QuestionID].q
	for _, change := range []func(*store.CardRecord){func(c *store.CardRecord) { c.Block = strings.Repeat("f", 16) }, func(c *store.CardRecord) { c.QuestionID = "other" }, func(c *store.CardRecord) { c.Options = map[string]string{"a": "accept_unreconciled"} }} {
		bad := c
		change(&bad)
		if _, _, err := store.HandleLandingCardAnswer(ctx, x.w.rt, bad, q, store.DecisionAt()); err == nil {
			t.Fatal("forged binding accepted")
		}
	}
	outcome, _, err := store.HandleLandingCardAnswer(ctx, x.w.rt, c, q, store.DecisionAt())
	ok(t, err)
	if outcome != "acknowledged" || !x.read(t).Blocks[c.Block].IsOpen() {
		t.Fatal("same-entry valid ack failed")
	}
	// Separate fixture keeps the question open while the new CI unblocks it.
	x, trigger = b5gFixture(t, "blocked")
	f = newFakeCards(store.DecisionAt)
	c = b5gCard(t, x, f, store.CardSTGBlocked)
	b5gResolveCI(t, x, c, trigger)
	state, err := store.SettleLandingCard(ctx, x.w.rt, f.source(), c, store.DecisionAt())
	ok(t, err)
	if state != "resolved" || cardsNow(t, x.w)[c.Prep].ClosedWhy != "resolved" || f.byID[c.QuestionID].q.Status != "withdrawn" {
		t.Fatal("resolved block left live card", state)
	}
	// Verify the chain still belongs to the controller after all lifecycle writes.
	ok(t, store.InTx(ctx, x.w.rt, func(tx pgx.Tx) error { _, err := store.LandingCards(ctx, tx, store.CardsStream(c.Repo)); return err }))
}

func b5gResolveCI(t *testing.T, x *b5Fixture, c store.CardRecord, trigger store.Appended) {
	t.Helper()
	store.FreezeEventClock(store.DecisionAt().Add(time.Minute))
	ci := x.obs.CI.Accepted
	ci["read_at"], ci["created_at"], ci["uid"] = store.DecisionAt().Format(time.RFC3339Nano), store.DecisionAt().Format(time.RFC3339Nano), "new-ci"
	p := b5Append(t, x.w, b5Record("ci_observed", "block", "block", c.Block, "interval_seq", trigger.Seq, "P", commitB, "ci", ci, "status", "Succeeded"))
	b5Append(t, x.w, b5Record("unblocked", "block", "block", c.Block, "why", "inputs_changed", "ref", p.Seq))
}

// Serial: a lost Ask reply can leave a protected question after its trigger
// resolves. Recovery must bind it before withdrawing; never orphan it.
func TestStgCardsPGRecoverLostAskAfterResolution(t *testing.T) {
	x, trigger := b5gFixture(t, "blocked")
	f := newFakeCards(store.DecisionAt)
	f.loseReply = true
	preps, err := store.PrepareLandingCards(ctx, x.w.rt, "bifrost-platform", store.DecisionAt())
	ok(t, err)
	if len(preps) != 1 {
		t.Fatal(preps)
	}
	if _, err := store.AskLandingCard(ctx, x.w.rt, f.source(), "bifrost-platform", preps[0], store.DecisionAt(), true); err == nil {
		t.Fatal("lost reply was hidden")
	}
	c := cardsNow(t, x.w)[preps[0]]
	if c.Bound || !c.AskStarted || f.asks != 1 {
		t.Fatal(c)
	}
	b5gResolveCI(t, x, c, trigger)
	f.loseReply = false
	id, err := store.AskLandingCard(ctx, x.w.rt, f.source(), c.Repo, c.Prep, store.DecisionAt(), true)
	ok(t, err)
	if id != "q1" || f.asks != 1 {
		t.Fatal("lost question was orphaned or replaced", id, f.asks)
	}
	c = cardsNow(t, x.w)[c.Prep]
	state, err := store.SettleLandingCard(ctx, x.w.rt, f.source(), c, store.DecisionAt())
	ok(t, err)
	if state != "resolved" || f.byID[id].q.Status != "withdrawn" {
		t.Fatal("recovered question remained open", state)
	}
}
