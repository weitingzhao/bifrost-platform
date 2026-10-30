package store_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/questioncards"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/stgrelease"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

func b5hCount(s store.StgState, kind string) int {
	n := 0
	for _, r := range s.Records {
		if r.Record.String("kind") == kind {
			n++
		}
	}
	return n
}
func b5hClosed(s store.StgState) bool { return s.Batches[b5Batch].Phase == "CLOSED" }
func (x *b5hWorld) assertDelivered(t *testing.T, tries int) {
	t.Helper()
	s := x.state(t)
	m := store.StgMemberKey{Root: x.root, Op: x.op, R: x.p}
	if !b5hClosed(s) || s.Members[m].To != "delivered" || b5hCount(s, "started") != tries || b5hCount(s, "bound") != tries || b5hCount(s, "run_evidence") != tries || b5hCount(s, "verify") != tries || b5hCount(s, "member") != 1 || b5hCount(s, "settled") != 1 || x.runs.creates != tries || len(x.runs.uids) != tries || s.Budget[store.StgBudgetKey{R: x.p, Gen: 1}] != int64(tries) {
		t.Fatalf("delivery identity/count mismatch: phase=%s member=%+v creates=%d UIDs=%d budget=%v", s.Batches[b5Batch].Phase, s.Members[m], x.runs.creates, len(x.runs.uids), s.Budget)
	}
	seen := map[string]bool{}
	for n := 1; n <= tries; n++ {
		tr := s.Batches[b5Batch].Tries[int64(n)]
		uid := tr.Bound.Record.String("uid")
		outcome := "failed"
		if n == tries {
			outcome = "verified"
		}
		if uid == "" || seen[uid] || !x.runs.uids[types.UID(uid)] || tr.RunEvidence.Record.String("uid") != uid || tr.Started.Record.Int("intent_seq") != tr.Started.Seq || tr.Verify.Record.String("outcome") != outcome {
			t.Fatal("try identity or terminal classification changed", n, uid, tr.Verify)
		}
		seen[uid] = true
	}
	before := s.LastSeq
	for range 3 {
		ok(t, x.c.ExternalPass(ctx))
		ok(t, x.c.Pass(ctx))
	}
	if x.state(t).LastSeq != before {
		t.Fatal("settled replay appended records")
	}
}

// Serial: real PostgreSQL, signing anchor, local Git PATH and event clock.
func TestB5HCrossPGRestartAndCreateReplies(t *testing.T) {
	for _, cut := range []string{"snapshot", "started", "bound", "archive", "verified", "closed", "409", "lost_reply"} {
		t.Run(cut, func(t *testing.T) {
			x := b5hFixture(t)
			if cut == "snapshot" {
				x.restart(t)
			}
			if cut == "409" {
				x.runs.responseErr = apierrors.NewAlreadyExists(schema.GroupResource{Resource: "pipelineruns"}, "same-name")
			}
			if cut == "lost_reply" || cut == "started" {
				x.runs.responseErr = context.DeadlineExceeded
			}
			if cut == "started" {
				x.runs.readErr = context.DeadlineExceeded
			}
			err := x.c.Pass(ctx)
			if cut == "started" {
				if err == nil {
					t.Fatal("lost create and GET replies were hidden")
				}
				s := x.state(t)
				tr := s.Batches[b5Batch].Tries[1]
				if tr.Started.Seq == 0 || tr.Bound.Seq != 0 || !tr.Occupied() || x.runs.creates != 1 {
					t.Fatal("ambiguous create changed occupancy")
				}
				x.restart(t)
				x.runs.readErr = nil
				ok(t, x.c.Pass(ctx))
			} else {
				ok(t, err)
			}
			if cut == "bound" || cut == "409" || cut == "lost_reply" {
				x.restart(t)
			}
			x.runs.work = b5hWork("d", "f")
			x.terminal(t, "Succeeded")
			ok(t, x.c.Pass(ctx))
			if cut == "archive" {
				x.restart(t)
				x.runs.readErr = context.DeadlineExceeded
			}
			ok(t, x.c.Pass(ctx))
			if cut == "verified" {
				x.restart(t)
			}
			x.until(t, b5hClosed)
			if cut == "closed" {
				x.restart(t)
			}
			x.assertDelivered(t, 1)
		})
	}
}

// Serial: PG + fake clock. These reach actual second Creates, not just retry
// records; every try is authorized and debited by the final gate again.
func TestB5HCrossPGPartialRolloutCreates(t *testing.T) {
	for _, effect := range []string{"unchanged", "partial", "all", "third_party"} {
		t.Run(effect, func(t *testing.T) {
			x := b5hFixture(t)
			ok(t, x.c.Pass(ctx))
			x.terminal(t, "Failed")
			switch effect {
			case "partial":
				x.runs.work["platform-api"].(map[string]any)["digest"] = b5hWork("d", "f")["platform-api"].(map[string]any)["digest"]
			case "all":
				x.runs.work = b5hWork("d", "f")
			case "third_party":
				x.runs.work = b5hWork("9", "9")
			}
			x.until(t, func(s store.StgState) bool { return b5hClosed(s) || s.Batches[b5Batch].Tries[1].Retry.Seq > 0 })
			s := x.state(t)
			tr := s.Batches[b5Batch].Tries[1]
			want := "this_try"
			if effect == "unchanged" {
				want = "unchanged"
			}
			if effect == "third_party" {
				want = "other"
			}
			if tr.Effect.Record.String("class") != want {
				t.Fatal("effect classification", tr.Effect)
			}
			if effect == "third_party" {
				if !b5hClosed(s) || len(s.Blocks) != 1 || b5hCount(s, "started") != 1 || x.runs.creates != 1 {
					t.Fatal("third party update retried")
				}
				return
			}
			due := tr.Retry.Record.Time("not_before")
			if !due.Equal(tr.RunEvidence.Record.Time("at").Add(10 * time.Minute)) {
				t.Fatal("backoff was not terminal-based")
			}
			x.advance(t, due.Sub(store.DecisionAt())-time.Second)
			x.restart(t)
			ok(t, x.c.Pass(ctx))
			if x.runs.creates != 1 || x.state(t).Batches[b5Batch].Try != 1 {
				t.Fatal("early retry")
			}
			x.advance(t, time.Second)
			ok(t, x.c.Pass(ctx))
			ok(t, x.c.Pass(ctx))
			if x.runs.creates != 2 || len(x.runs.uids) != 2 {
				t.Fatal("retry did not create exactly two UIDs")
			}
			x.runs.work = b5hWork("d", "f")
			x.terminal(t, "Succeeded")
			x.until(t, b5hClosed)
			x.assertDelivered(t, 2)
		})
	}
}

// Serial: PG and policy interleavings; explicit none freezes the whole loop.
func TestB5HCrossPGUnknownLeaseAndNone(t *testing.T) {
	for _, mode := range []string{"read_timeout", "live_off", "none", "expired_before_post"} {
		t.Run(mode, func(t *testing.T) {
			x := b5hFixture(t)
			if mode == "expired_before_post" {
				x.runs.beforeSend = func() {
					store.FreezeEventClock(store.DecisionAt().Add(3 * time.Minute))
					x.clock.SetTime(store.DecisionAt())
				}
				if err := x.c.Pass(ctx); err == nil {
					t.Fatal("expired send accepted")
				}
				x.runs.beforeSend = nil
				x.restart(t)
				ok(t, x.c.Pass(ctx))
				tr := x.state(t).Batches[b5Batch].Tries[1]
				if !tr.IsQuarantined() || !tr.Occupied() || tr.Unsent.Seq != 0 || x.runs.creates != 0 || x.state(t).Budget[store.StgBudgetKey{R: x.p, Gen: 1}] != 1 {
					t.Fatal("expired unbound 404 re-sent or refunded")
				}
				return
			}
			ok(t, x.c.Pass(ctx))
			before := x.state(t)
			p := *x.policy.Load()
			if mode == "read_timeout" {
				x.runs.readErr = context.DeadlineExceeded
			}
			if mode == "live_off" {
				p.Live = false
				x.policy.Store(&p)
			}
			if mode == "none" {
				(&b5Fixture{w: x.w}).none(t)
				rule, err := store.StgControlRule(ctx, x.w.rt, "dev")
				ok(t, err)
				p.Rule = rule
				x.policy.Store(&p)
			}
			for range 3 {
				err := x.c.Pass(ctx)
				if mode == "read_timeout" {
					if err == nil {
						t.Fatal("read timeout hidden")
					}
				} else {
					ok(t, err)
				}
			}
			s := x.state(t)
			tr := s.Batches[b5Batch].Tries[1]
			if s.LastSeq != before.LastSeq || !tr.Occupied() || tr.Verify.Seq != 0 || b5hCount(s, "started") != 1 || x.runs.creates != 1 {
				t.Fatal("unknown/running/none changed execution")
			}
		})
	}
}

// Serial: PG; ExternalPass must not settle an archived but unverified try.
func TestB5HCrossPGExternalWrongInputOrder(t *testing.T) {
	for _, externalFirst := range []bool{true, false} {
		t.Run(fmt.Sprint(externalFirst), func(t *testing.T) {
			x := b5hFixture(t)
			ok(t, x.c.Pass(ctx))
			x.terminal(t, "Succeeded")
			uid := x.state(t).Batches[b5Batch].Tries[1].Bound.Record.String("uid")
			for _, o := range x.runs.observations {
				if string(o.Identity.UID) == uid {
					for _, raw := range o.Raw["child_refs"].([]any) {
						r := raw.(map[string]any)
						if r["pipelineTaskName"] == "clone-ui" {
							r["task_run"].(map[string]any)["status"].(map[string]any)["results"] = []any{map[string]any{"name": "commit", "value": commitB}}
						}
					}
				}
			}
			external := b5hObservation(t, delivery.NamedDeliverInput{Batch: 901, Try: 1, IntentSeq: 1, P: x.p, U: commitA, Infra: commitA}, "Succeeded", "external-uid", "d", "f")
			x.runs.observations[external.Identity.UID] = external
			x.runs.external = []*unstructured.Unstructured{external.Run}
			x.runs.work = b5hWork("d", "f")
			if externalFirst {
				ok(t, x.c.ExternalPass(ctx))
				if !x.state(t).Batches[b5Batch].Tries[1].Occupied() {
					t.Fatal("external cover freed running slot")
				}
			}
			ok(t, x.c.Pass(ctx))
			if !externalFirst {
				ok(t, x.c.ExternalPass(ctx))
			}
			x.restart(t)
			x.until(t, b5hClosed)
			s := x.state(t)
			tr := s.Batches[b5Batch].Tries[1]
			m := store.StgMemberKey{Root: x.root, Op: x.op, R: x.p}
			if tr.Verify.Record.String("why") != "wrong_input" || len(s.Blocks) != 1 || len(s.Covers) != 1 || b5hCount(s, "settled") != 1 || b5hCount(s, "member") != 1 || s.Members[m].To != "covered" || s.Budget[store.StgBudgetKey{R: x.p, Gen: 1}] != 1 || tr.Bound.Record.String("uid") != uid || x.runs.creates != 1 || len(x.runs.uids) != 1 {
				t.Fatal("external coverage erased verification or block", s.Blocks, tr.Verify)
			}
			for _, block := range s.Blocks {
				if block.Opened.Record.String("why") != "wrong_input" || !block.IsOpen() {
					t.Fatal(block)
				}
			}
			before := s.LastSeq
			for range 3 {
				ok(t, x.c.ExternalPass(ctx))
				ok(t, x.c.Pass(ctx))
			}
			if x.state(t).LastSeq != before {
				t.Fatal("replay duplicated settlement")
			}
		})
	}
}

// Serial: real question adapter, PG, rule clock and local Git. The same
// controller-produced budget trigger survives quota, lost Ask and failed Ack.
func TestB5HCrossPGBudgetCardFaults(t *testing.T) {
	t.Setenv("APPROVAL_NOTIFY_URL", "")
	t.Setenv("OPS_VIEWER_ENV", "prod")
	x := b5hFixture(t)
	for n := int64(1); n <= 3; n++ {
		if n > 1 {
			due := x.state(t).Debits[x.p].NotBefore
			x.advance(t, due.Sub(store.DecisionAt()))
			ok(t, x.c.Pass(ctx))
		}
		ok(t, x.c.Pass(ctx))
		x.terminal(t, "Failed")
		x.until(t, func(s store.StgState) bool { return b5hClosed(s) || s.Batches[b5Batch].Tries[n].Retry.Seq > 0 })
	}
	s := x.state(t)
	if !b5hClosed(s) || len(s.Blocks) != 1 || b5hCount(s, "started") != 3 || x.runs.creates != 3 {
		t.Fatal("budget did not exhaust through controller")
	}
	var trigger store.StgBlock
	for _, b := range s.Blocks {
		trigger = b
	}
	svc := questions.New(filepath.Join(t.TempDir(), "questions"), nil)
	svc.SetClock(store.DecisionAt)
	source := questioncards.New(svc)
	// Fill the real requester's open quota, then remove those test questions.
	fillers := b5hFillQuota(t, svc)
	ctl := &controller.Controller{DB: x.w.rt, Env: "prod", Now: store.DecisionAt, LandingCards: source}
	if err := ctl.LandingCardsPass(ctx); !errors.Is(err, store.ErrCardQuota) {
		t.Fatal("expected actual question quota", err)
	}
	preps, _, err := store.LandingCardsToSettle(ctx, x.w.rt, platformRepo)
	ok(t, err)
	if len(preps) != 1 || preps[0].TriggerSeq != trigger.Opened.Seq || !preps[0].AskStarted {
		t.Fatal("quota lost preparation", preps)
	}
	b5hReleaseQuota(t, svc, fillers)
	ask := source.AskRaw
	source.AskRaw = func(ctx context.Context, key string, req map[string]any) (string, time.Time, error) {
		_, _, err := ask(ctx, key, req)
		if err != nil {
			return "", time.Time{}, err
		}
		return "", time.Time{}, context.DeadlineExceeded
	}
	_, err = store.AskLandingCard(ctx, x.w.rt, source, platformRepo, preps[0].Prep, store.DecisionAt(), true)
	if err == nil {
		t.Fatal("lost Ask reply hidden")
	}
	source.AskRaw = ask
	id, err := store.AskLandingCard(ctx, x.w.rt, source, platformRepo, preps[0].Prep, store.DecisionAt(), true)
	ok(t, err)
	_, bound, err := store.LandingCardsToSettle(ctx, x.w.rt, platformRepo)
	ok(t, err)
	if len(bound) != 1 || bound[0].QuestionID != id || bound[0].Key != preps[0].Key {
		t.Fatal("lost reply changed question identity", bound)
	}
	card := bound[0]
	res := svc.Answer(ctx, "console-session", "renamed-admin", "admin", id, []byte(`{"option":"a","channel":"console"}`))
	if res.Code != 200 {
		t.Fatal(res)
	}
	ack := source.Ack
	source.Ack = func(context.Context, string) (bool, error) { return false, context.DeadlineExceeded }
	_, err = store.SettleLandingCard(ctx, x.w.rt, source, card, store.DecisionAt())
	if err == nil {
		t.Fatal("Ack loss hidden")
	}
	after := x.state(t)
	if len(after.Grants) != 1 || len(after.Answers) != 1 || after.Generations[x.p] != 2 || after.Blocks[card.Block].IsOpen() || !reflect.DeepEqual(after.Debits, s.Debits) || after.Budget[store.StgBudgetKey{R: x.p, Gen: 1}] != 3 {
		t.Fatal("grant changed history or identity")
	}
	for range 3 {
		_, err = store.SettleLandingCard(ctx, x.w.rt, source, card, store.DecisionAt())
		if err == nil || x.state(t).LastSeq != after.LastSeq {
			t.Fatal("Ack replay granted twice")
		}
	}
	source.Ack = ack
	status, err := store.SettleLandingCard(ctx, x.w.rt, source, card, store.DecisionAt())
	ok(t, err)
	q, found, err := svc.Lookup(id)
	ok(t, err)
	if status != "acked" || !found || q.Answer == nil || q.Answer.By != "renamed-admin" || q.Answer.Approval || x.runs.creates != 3 {
		t.Fatal("answer identity/approval/send boundary", q, status)
	}
}

// Serial: actual independent PG connections, bounded concurrent controllers.
func TestB5HCrossPGTwoObserversOneArchive(t *testing.T) {
	x := b5hFixture(t)
	ok(t, x.c.Pass(ctx))
	x.runs.work = b5hWork("d", "f")
	x.terminal(t, "Succeeded")
	ok(t, x.c.Close())
	p := *x.policy.Load()
	p.Live = false
	x.policy.Store(&p)
	other := x.controller(x.w.db.Connect(t, x.w.db.Runtime))
	defer other.Close()
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	done := make(chan error, 2)
	for _, c := range []*stgrelease.Controller{x.c, other} {
		safego.Go("b5h-two-observers", func() {
			select {
			case <-start:
				done <- c.Pass(bounded)
			case <-bounded.Done():
				done <- bounded.Err()
			}
		})
	}
	close(start)
	for range 2 {
		select {
		case err := <-done:
			ok(t, err)
		case <-bounded.Done():
			t.Fatal("observers deadlocked")
		}
	}
	s := x.state(t)
	if b5hCount(s, "run_evidence") != 1 || b5hCount(s, "bound") != 1 || b5hCount(s, "started") != 1 || x.runs.creates != 1 {
		t.Fatal("competing observers duplicated archive/UID")
	}
	x.until(t, b5hClosed)
	x.assertDelivered(t, 1)
}
