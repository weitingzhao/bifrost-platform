package store_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// S3 acceptance of the landing cards (CUT §11.6 v3 items 2, 6, 9; §11.10):
// the settle table row by row (SettleLandingCard), and the pass's wiring
// (controller.LandingCardsPass): the quota pause, the cooldown from the real
// creation, a hold of a day, and a question store without LookupKey.

// cardEventCount counts the events on a stream.
func cardEventCount(t *testing.T, w *world, stream string) int {
	t.Helper()
	var n int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = $1`, stream).Scan(&n))
	return n
}

// repoNotes counts the notes on the repository landing stream that carry key.
func repoNotes(t *testing.T, w *world, key string) int {
	t.Helper()
	var n int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = $1 AND body_json ? $2`, landingRepoStream, key).Scan(&n))
	return n
}

// cardsByN are the cards of one trigger by their number (the world has one).
func cardsByN(t *testing.T, w *world) map[int]store.CardRecord {
	t.Helper()
	out := map[int]store.CardRecord{}
	for _, c := range cardsNow(t, w) {
		if _, dup := out[c.N]; dup {
			t.Fatalf("two cards numbered %d: %+v", c.N, cardsNow(t, w))
		}
		out[c.N] = c
	}
	return out
}

// okMissingRoot is a root whose push was reported accepted and main does not
// show it: an ok_missing trigger (confirm_main_lost, wait).
func okMissingRoot(t *testing.T, w *world, op string, at time.Time) string {
	t.Helper()
	root := w.ready(t, store.Task{OffPlanReason: "card " + op})
	prodChain(t, w, root, op, at, "main_intent")
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "main_result", "outcome", "ok")))
	controllerNote(t, w, root, map[string]any{"text": "anomaly", "landing_anomaly": map[string]any{"op": op, "root": root, "kind": "ok_missing", "R": commitB, "M0": commitA, "head": commitA}})
	return root
}

// lateLandingRoot is a root whose operation ended as never sent and main now
// has: a late_landing notice (ack, remind_later).
func lateLandingRoot(t *testing.T, w *world, op string, at time.Time) string {
	t.Helper()
	store.FreezeEventClock(at)
	root := w.ready(t, store.Task{OffPlanReason: "card " + op})
	controllerNote(t, w, root, map[string]any{"text": "anomaly", "landing_anomaly": map[string]any{"op": op, "root": root, "kind": "late_landing", "R": commitB, "M0": commitA, "head": commitB}})
	return root
}

// cardCtl is a PROD controller over the world with a clock the test moves;
// each pass freezes the event clock at its time.
type cardCtl struct {
	ctl *controller.Controller
	now time.Time
}

func newCardCtl(w *world, q store.CardQuestions) *cardCtl {
	c := &cardCtl{}
	c.ctl = &controller.Controller{DB: w.rt, Env: "prod", LandingCards: q, Now: func() time.Time { return c.now }}
	return c
}

func (c *cardCtl) cardsPassAt(at time.Time) error {
	c.now = at
	store.FreezeEventClock(at)
	return c.ctl.LandingCardsPass(ctx)
}

// quotaSource wraps the fake question store: while *full, AskRaw takes no
// new card (the store's 429 / 507, as the adapter reports it) and counts the
// refusal; LookupKey and the rest are the fake's.
func quotaSource(f *fakeCards, full *bool, refused *int) store.CardQuestions {
	src := f.source()
	ask := src.AskRaw
	src.AskRaw = func(c context.Context, key string, req map[string]any) (string, time.Time, error) {
		if *full {
			*refused++
			return "", time.Time{}, store.ErrCardQuota
		}
		return ask(c, key, req)
	}
	return src
}

// ---- A. The settle table (CUT §11.6 v3 item 2) ----

// Serial: FreezeEventClock. Row «bound, not handled»: the store says the
// question expired, or was withdrawn, unanswered: the card is closed for that
// reason; the trigger still applies, so after its cooldown (six hours from
// the card's creation) card n+1 is prepared and asked.
func TestS3CardSettleExpiredOrWithdrawnAsksAgain(t *testing.T) {
	at := time.Now().Add(-2 * 24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	for _, status := range []string{"expired", "withdrawn"} {
		t.Run(status, func(t *testing.T) {
			w := newKernelWorld(t)
			cardFailedRoot(t, w, "land:acc1"+status[:1]+":1", "stale", at)
			f := newFakeCards(func() time.Time { return at.Add(time.Hour) })
			cardPass(t, w, f, at.Add(time.Hour))
			if c := cardsByN(t, w)[1]; !c.Bound || c.QuestionID != "q1" {
				t.Fatalf("not bound: %+v", c)
			}
			f.byID["q1"].q.Status = status
			cardPass(t, w, f, at.Add(2*time.Hour))
			cards := cardsByN(t, w)
			if c := cards[1]; !c.Closed || c.ClosedWhy != status || c.Acked {
				t.Fatalf("card 1 not closed %s: %+v", status, c)
			}
			if len(cards) != 1 || f.asks != 1 {
				t.Fatalf("a card inside the cooldown: %d cards, %d asks", len(cards), f.asks)
			}
			if repoNotes(t, w, "landing_answer")+repoNotes(t, w, "landing_card_outcome") != 0 {
				t.Fatal("an unanswered card was handled")
			}
			f.now = func() time.Time { return at.Add(8 * time.Hour) }
			cardPass(t, w, f, at.Add(8*time.Hour))
			cards = cardsByN(t, w)
			if c := cards[2]; len(cards) != 2 || !c.Bound || c.QuestionID != "q2" || c.Key == cards[1].Key || f.asks != 2 {
				t.Fatalf("no card n+1: %+v asks=%d", cards, f.asks)
			}
		})
	}
}

// Serial: FreezeEventClock. Row «handled, not acknowledged»: the answer was
// carried out, the Ack failed, then the store lost the question (404): the
// card is closed gone_after_handled; the answer is not carried out again and
// no Ack is forged.
func TestS3CardSettleGoneAfterHandled(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := newKernelWorld(t)
	cardFailedRoot(t, w, "land:acc2:1", "stale", at)
	f := newFakeCards(func() time.Time { return at.Add(time.Hour) })
	cardPass(t, w, f, at.Add(time.Hour))
	f.answer("q1", "b", at.Add(90*time.Minute)) // do not merge
	f.ackFails = true
	cardPass(t, w, f, at.Add(2*time.Hour))
	if n := repoNotes(t, w, "landing_answer"); n != 1 {
		t.Fatal("not handled once", n)
	}
	if c := cardsByN(t, w)[1]; c.Ended() {
		t.Fatalf("ended though the Ack failed: %+v", c)
	}
	f.ackFails = false
	delete(f.byID, "q1")
	_, bound, err := store.LandingCardsToSettle(ctx, w.rt, "bifrost-platform")
	ok(t, err)
	if len(bound) != 1 {
		t.Fatal(bound)
	}
	store.FreezeEventClock(at.Add(3 * time.Hour))
	did, err := store.SettleLandingCard(ctx, w.rt, f.source(), bound[0], at.Add(3*time.Hour))
	ok(t, err)
	if did != "gone_after_handled" {
		t.Fatal(did)
	}
	cardPass(t, w, f, at.Add(4*time.Hour))
	c := cardsByN(t, w)[1]
	if !c.Closed || c.ClosedWhy != "gone_after_handled" || c.Acked {
		t.Fatalf("%+v", c)
	}
	if n := repoNotes(t, w, "landing_answer") + repoNotes(t, w, "landing_card_outcome"); n != 1 {
		t.Fatal("handled again", n)
	}
	if o := landingState(t, w, landingRepoStream).Operations["land:acc2:1"]; o.Outcome != "declined" {
		t.Fatal("the decision was not kept", o.Outcome)
	}
	if len(cardsNow(t, w)) != 1 || f.asks != 1 {
		t.Fatal("a card after the decision", f.asks)
	}
}

// Serial: FreezeEventClock. Row «to withdraw»: an open card whose trigger no
// longer applies is withdrawn and closed (the code's why is resolved). When
// the Withdraw finds the question answered meanwhile, nothing is written on
// that step; the next pass reads the answer and handles it as moot
// (trigger_gone, the §8 choice not carried out), then acknowledges it: the
// answer is not lost.
func TestS3CardSettleWithdrawnOrAnsweredMeanwhile(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()

	t.Run("withdrawn", func(t *testing.T) {
		w := newKernelWorld(t)
		okMissingRoot(t, w, "land:acc3a:1", at)
		f := newFakeCards(func() time.Time { return at.Add(time.Hour) })
		cardPass(t, w, f, at.Add(time.Hour))
		store.FreezeEventClock(at.Add(90 * time.Minute))
		ok(t, appendLanding(w, landingRepoStream, newLandingRecord("land:acc3a:1", "landed", "R", commitB)))
		cardPass(t, w, f, at.Add(2*time.Hour))
		if f.byID["q1"].q.Status != "withdrawn" {
			t.Fatal("not withdrawn", f.byID["q1"].q.Status)
		}
		if c := cardsByN(t, w)[1]; !c.Closed || c.ClosedWhy != "resolved" {
			t.Fatalf("%+v", c)
		}
	})

	t.Run("answered meanwhile", func(t *testing.T) {
		w := newKernelWorld(t)
		okMissingRoot(t, w, "land:acc3b:1", at)
		f := newFakeCards(func() time.Time { return at.Add(time.Hour) })
		cardPass(t, w, f, at.Add(time.Hour))
		store.FreezeEventClock(at.Add(90 * time.Minute))
		ok(t, appendLanding(w, landingRepoStream, newLandingRecord("land:acc3b:1", "landed", "R", commitB)))
		// The Owner answers confirm_main_lost just before the Withdraw lands.
		src := f.source()
		withdraw := src.Withdraw
		src.Withdraw = func(c context.Context, id, reason string) (string, error) {
			f.answer(id, "a", at.Add(110*time.Minute))
			return withdraw(c, id, reason)
		}
		_, bound, err := store.LandingCardsToSettle(ctx, w.rt, "bifrost-platform")
		ok(t, err)
		if len(bound) != 1 {
			t.Fatal(bound)
		}
		cardsBefore, repoBefore := cardEventCount(t, w, store.CardsStream("bifrost-platform")), cardEventCount(t, w, landingRepoStream)
		store.FreezeEventClock(at.Add(2 * time.Hour))
		did, err := store.SettleLandingCard(ctx, w.rt, src, bound[0], at.Add(2*time.Hour))
		ok(t, err)
		if did != "" || f.byID["q1"].q.Status != store.QuestionAnswered {
			t.Fatalf("withdraw step: %q, %s", did, f.byID["q1"].q.Status)
		}
		if cardEventCount(t, w, store.CardsStream("bifrost-platform")) != cardsBefore || cardEventCount(t, w, landingRepoStream) != repoBefore {
			t.Fatal("the withdraw step that found an answer wrote something")
		}
		cardPass(t, w, f, at.Add(3*time.Hour))
		v, found, err := store.ReadLandingCard(ctx, w.rt, "bifrost-platform", "q1")
		ok(t, err)
		if !found || v.Outcome["outcome"] != "moot" || v.Outcome["why"] != "trigger_gone" || v.Outcome["choice"] != "confirm_main_lost" || !v.Acked {
			t.Fatalf("the answer: %+v", v)
		}
		if repoNotes(t, w, "landing_answer") != 0 {
			t.Fatal("a §8 choice was carried out after its trigger went")
		}
		if p, _ := phaseOf(t, w, "land:acc3b:1"); p != "landed" {
			t.Fatal("the landing changed", p)
		}
		if c := cardsByN(t, w)[1]; !c.Acked || !f.byID["q1"].acked {
			t.Fatalf("not acknowledged: %+v", c)
		}
	})
}

// Serial: FreezeEventClock. A read of the question store that fails writes
// nothing: not on the card stream, not on the repository stream; an open
// card and an answered one alike.
func TestS3CardSettleFailedReadWritesNothing(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := newKernelWorld(t)
	cardFailedRoot(t, w, "land:acc4:1", "stale", at)
	f := newFakeCards(func() time.Time { return at.Add(time.Hour) })
	cardPass(t, w, f, at.Add(time.Hour))
	src := f.source()
	src.Lookup = func(context.Context, string) (store.Question, bool, error) {
		return store.Question{}, false, errors.New("question store unreachable")
	}
	settle := func(now time.Time) {
		t.Helper()
		_, bound, err := store.LandingCardsToSettle(ctx, w.rt, "bifrost-platform")
		ok(t, err)
		if len(bound) != 1 {
			t.Fatal(bound)
		}
		cardsBefore, repoBefore := cardEventCount(t, w, store.CardsStream("bifrost-platform")), cardEventCount(t, w, landingRepoStream)
		store.FreezeEventClock(now)
		did, err := store.SettleLandingCard(ctx, w.rt, src, bound[0], now)
		if err == nil || did != "" {
			t.Fatalf("a failed read settled: %q %v", did, err)
		}
		if cardEventCount(t, w, store.CardsStream("bifrost-platform")) != cardsBefore || cardEventCount(t, w, landingRepoStream) != repoBefore {
			t.Fatal("a failed read wrote something")
		}
	}
	settle(at.Add(2 * time.Hour)) // open
	f.answer("q1", "a", at.Add(150*time.Minute))
	settle(at.Add(3 * time.Hour)) // answered
	if c := cardsByN(t, w)[1]; c.Ended() || !c.Bound {
		t.Fatalf("%+v", c)
	}
	// The read works again: the answer is handled.
	cardPass(t, w, f, at.Add(4*time.Hour))
	if o := landingState(t, w, landingRepoStream).Operations["land:acc4:1"]; !o.Retried {
		t.Fatal("the answer was lost")
	}
}

// Serial: FreezeEventClock. Q1 handled (held), its Ack failing; after the
// hold the same root gets card Q2, bound; Q1 is still acknowledged on a
// later pass (landing_card_acked names Q1), and Q2 stays open.
func TestS3CardSettleOldAckAfterNewCard(t *testing.T) {
	at := time.Now().Add(-3 * 24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := newKernelWorld(t)
	okMissingRoot(t, w, "land:acc5:1", at)
	f := newFakeCards(func() time.Time { return at.Add(time.Hour) })
	cardPass(t, w, f, at.Add(time.Hour))
	f.answer("q1", "b", at.Add(2*time.Hour)) // wait
	f.ackFails = true
	cardPass(t, w, f, at.Add(3*time.Hour))
	v, _, err := store.ReadLandingCard(ctx, w.rt, "bifrost-platform", "q1")
	ok(t, err)
	if v.Outcome["outcome"] != "held" || v.Acked {
		t.Fatalf("q1: %+v", v)
	}
	f.now = func() time.Time { return at.Add(27 * time.Hour) }
	cardPass(t, w, f, at.Add(27*time.Hour))
	cards := cardsByN(t, w)
	if c := cards[2]; !c.Bound || c.QuestionID != "q2" || f.asks != 2 {
		t.Fatalf("no second card: %+v asks=%d", cards, f.asks)
	}
	if c := cards[1]; c.Ended() || f.byID["q1"].acked {
		t.Fatalf("q1 acknowledged while the Ack failed: %+v", c)
	}
	f.ackFails = false
	cardPass(t, w, f, at.Add(28*time.Hour))
	cards = cardsByN(t, w)
	if !cards[1].Acked || !f.byID["q1"].acked {
		t.Fatalf("q1 not acknowledged after q2: %+v", cards[1])
	}
	if c := cards[2]; c.Ended() || f.byID["q2"].q.Status != "open" {
		t.Fatalf("q2: %+v", c)
	}
	var acked []string
	rows, err := w.rt.Query(ctx, `SELECT body_json->'landing_card_acked'->>'question_id' FROM agentrt.event WHERE event_stream_id = $1 AND body_json ? 'landing_card_acked' ORDER BY seq`, store.CardsStream("bifrost-platform"))
	ok(t, err)
	for rows.Next() {
		var q string
		ok(t, rows.Scan(&q))
		acked = append(acked, q)
	}
	rows.Close()
	ok(t, rows.Err())
	if !slices.Equal(acked, []string{"q1"}) {
		t.Fatal("acknowledgements", acked)
	}
	if n := repoNotes(t, w, "landing_card_outcome"); n != 1 {
		t.Fatal("q1 handled again", n)
	}
}

// ---- B. LandingCardsPass (CUT §11.6 v3 items 6, 9; §11.10) ----

// Serial: FreezeEventClock. The first card asked this pass meets a full
// quota: the card after it, whose question the store already has, is still
// taken up by its key and bound; nothing new is asked or prepared this pass,
// nor within 30 minutes (the quota itself free again); after 30 minutes
// asking resumes.
func TestS3CardPassQuotaPausesNewCards(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := newKernelWorld(t)
	cardFailedRoot(t, w, "land:acc6a:1", "stale", at)
	cardFailedRoot(t, w, "land:acc6b:1", "stale", at.Add(5*time.Minute))
	f := newFakeCards(func() time.Time { return at.Add(time.Hour) })
	store.FreezeEventClock(at.Add(time.Hour))
	prepared, err := store.PrepareLandingCards(ctx, w.rt, "bifrost-platform", at.Add(time.Hour))
	ok(t, err)
	if len(prepared) != 2 {
		t.Fatal(prepared)
	}
	slices.Sort(prepared) // the pass asks in this order
	first, second := prepared[0], prepared[1]
	// The second card's ask reached the store; its reply was lost.
	f.loseReply = true
	if _, err := store.AskLandingCard(ctx, w.rt, f.source(), "bifrost-platform", second, at.Add(time.Hour), true); err == nil {
		t.Fatal("a lost reply bound")
	}
	f.loseReply = false
	// A third trigger after the preparation: a new card for the pass to make.
	cardFailedRoot(t, w, "land:acc6c:1", "stale", at.Add(time.Hour))

	full, refused := true, 0
	cc := newCardCtl(w, quotaSource(f, &full, &refused))
	t0 := at.Add(2 * time.Hour)
	err = cc.cardsPassAt(t0)
	if !errors.Is(err, store.ErrCardQuota) {
		t.Fatal("the quota was not reported", err)
	}
	cards := cardsNow(t, w)
	if len(cards) != 2 || refused != 1 || f.asks != 1 {
		t.Fatalf("asked or prepared past the quota: %d cards, %d refused, %d asks", len(cards), refused, f.asks)
	}
	if c := cards[first]; c.Bound || !c.AskStarted {
		t.Fatalf("first: %+v", c)
	}
	if c := cards[second]; !c.Bound || c.QuestionID != "q1" {
		t.Fatalf("the card the store has was not taken up: %+v", c)
	}

	full = false
	ok(t, cc.cardsPassAt(t0.Add(10*time.Minute)))
	ok(t, cc.cardsPassAt(t0.Add(29*time.Minute)))
	cards = cardsNow(t, w)
	if len(cards) != 2 || refused != 1 || f.asks != 1 || cards[first].Bound {
		t.Fatalf("asked within the pause: %d cards, %d refused, %d asks", len(cards), refused, f.asks)
	}

	f.now = func() time.Time { return t0.Add(31 * time.Minute) }
	ok(t, cc.cardsPassAt(t0.Add(31*time.Minute)))
	cards = cardsNow(t, w)
	if len(cards) != 3 || f.asks != 3 {
		t.Fatalf("not resumed after the pause: %d cards, %d asks", len(cards), f.asks)
	}
	for _, c := range cards {
		if !c.Bound || c.Ended() {
			t.Fatalf("%+v", c)
		}
	}
}

// Serial: FreezeEventClock. The cooldown counts from the card's real
// creation in the store, not its preparation: prepared at T0, created at
// T0+5h, expired at T0+6h; at T0+7h (six hours after the preparation) no new
// card; at T0+11h (six after the creation) card 2.
func TestS3CardPassCooldownFromRealCreation(t *testing.T) {
	at := time.Now().Add(-2 * 24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := newKernelWorld(t)
	cardFailedRoot(t, w, "land:acc7a:1", "stale", at)
	t0 := at.Add(time.Hour)
	store.FreezeEventClock(t0)
	prepared, err := store.PrepareLandingCards(ctx, w.rt, "bifrost-platform", t0)
	ok(t, err)
	if len(prepared) != 1 {
		t.Fatal(prepared)
	}
	f := newFakeCards(func() time.Time { return t0.Add(5 * time.Hour) })
	cc := newCardCtl(w, f.source())
	ok(t, cc.cardsPassAt(t0.Add(5*time.Hour)))
	c := cardsByN(t, w)[1]
	if !c.Bound || !c.CreatedAt.Equal(t0.Add(5*time.Hour)) || !c.PreparedAt.Equal(t0) {
		t.Fatalf("card 1: %+v", c)
	}
	f.byID["q1"].q.Status = "expired"
	ok(t, cc.cardsPassAt(t0.Add(6*time.Hour)))
	if c := cardsByN(t, w)[1]; !c.Closed || c.ClosedWhy != "expired" {
		t.Fatalf("%+v", c)
	}
	ok(t, cc.cardsPassAt(t0.Add(7*time.Hour)))
	ok(t, cc.cardsPassAt(t0.Add(11*time.Hour-time.Minute)))
	if n := len(cardsNow(t, w)); n != 1 || f.asks != 1 {
		t.Fatalf("a card inside the cooldown from its creation: %d cards, %d asks", n, f.asks)
	}
	f.now = func() time.Time { return t0.Add(11 * time.Hour) }
	ok(t, cc.cardsPassAt(t0.Add(11*time.Hour)))
	if c := cardsByN(t, w)[2]; !c.Bound || f.asks != 2 {
		t.Fatalf("no card after the cooldown: %+v asks=%d", c, f.asks)
	}
}

// Serial: FreezeEventClock. A card held (wait on ok_missing, remind_later on
// late_landing) is acknowledged; the same trigger has no new card for 24
// hours from the answer (CardHoldFor), then one.
func TestS3CardPassHeldWaitsADay(t *testing.T) {
	at := time.Now().Add(-3 * 24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	for _, tc := range []struct {
		name, kind, why string
		make            func(*testing.T, *world, string, time.Time) string
	}{
		{"wait", store.CardOkMissing, "wait", okMissingRoot},
		{"remind_later", store.CardLateLanding, "remind_later", lateLandingRoot},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newKernelWorld(t)
			tc.make(t, w, "land:acc7"+tc.name[:1]+":1", at)
			f := newFakeCards(func() time.Time { return at.Add(time.Hour) })
			cc := newCardCtl(w, f.source())
			ok(t, cc.cardsPassAt(at.Add(time.Hour)))
			if c := cardsByN(t, w)[1]; c.Kind != tc.kind || c.Options["b"] != tc.why {
				t.Fatalf("card 1: %+v", c)
			}
			answered := at.Add(2 * time.Hour)
			f.answer("q1", "b", answered)
			ok(t, cc.cardsPassAt(at.Add(3*time.Hour)))
			v, _, err := store.ReadLandingCard(ctx, w.rt, "bifrost-platform", "q1")
			ok(t, err)
			if v.Outcome["outcome"] != "held" || v.Outcome["why"] != tc.why || v.Outcome["remind_at"] != answered.Add(store.CardHoldFor).UTC().Format(time.RFC3339Nano) || !v.Acked {
				t.Fatalf("held: %+v", v)
			}
			ok(t, cc.cardsPassAt(at.Add(9*time.Hour))) // past the six-hour cooldown
			ok(t, cc.cardsPassAt(answered.Add(store.CardHoldFor-time.Minute)))
			if n := len(cardsNow(t, w)); n != 1 || f.asks != 1 {
				t.Fatalf("asked again while held: %d cards, %d asks", n, f.asks)
			}
			f.now = func() time.Time { return answered.Add(store.CardHoldFor) }
			ok(t, cc.cardsPassAt(answered.Add(store.CardHoldFor)))
			if c := cardsByN(t, w)[2]; !c.Bound || c.QuestionID != "q2" || f.asks != 2 {
				t.Fatalf("not asked after the hold: %+v asks=%d", c, f.asks)
			}
		})
	}
}

// Serial: FreezeEventClock. CardQuestions without LookupKey: the pass still
// carries a bound card to its end (answer handled, acknowledged), returns no
// error, and neither asks nor prepares a card (code as it is: it returns
// before the ask step; LandingCardsComplete says the store is not complete).
func TestS3CardPassWithoutLookupKey(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := newKernelWorld(t)
	cardFailedRoot(t, w, "land:acc8a:1", "stale", at)
	f := newFakeCards(func() time.Time { return at.Add(time.Hour) })
	full := f.source()
	ok(t, newCardCtl(w, full).cardsPassAt(at.Add(time.Hour)))
	if c := cardsByN(t, w)[1]; !c.Bound || c.QuestionID != "q1" {
		t.Fatalf("%+v", c)
	}
	f.answer("q1", "b", at.Add(90*time.Minute)) // do not merge
	// A second root, its trigger new.
	cardFailedRoot(t, w, "land:acc8b:1", "stale", at.Add(time.Hour))
	noKey := full
	noKey.LookupKey = nil
	if controller.LandingCardsComplete(noKey) {
		t.Fatal("complete without LookupKey")
	}
	cc := newCardCtl(w, noKey)
	ok(t, cc.cardsPassAt(at.Add(2*time.Hour)))
	ok(t, cc.cardsPassAt(at.Add(9*time.Hour)))
	cards := cardsNow(t, w)
	if len(cards) != 1 || f.asks != 1 {
		t.Fatalf("asked or prepared without LookupKey: %d cards, %d asks", len(cards), f.asks)
	}
	if c := cardsByN(t, w)[1]; !c.Acked || !f.byID["q1"].acked {
		t.Fatalf("the bound card was not carried along: %+v", c)
	}
	if o := landingState(t, w, landingRepoStream).Operations["land:acc8a:1"]; o.Outcome != "declined" {
		t.Fatal("not handled", o.Outcome)
	}
}

// Serial: FreezeEventClock. Review 675 R-04: with LookupKey alone (no Ask,
// Lookup or Ack, as on a restart that wired only it), the pass takes up a card
// the question store already has by its key, and asks nothing new.
func TestS3CardPassLookupKeyAlone(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := newKernelWorld(t)
	cardFailedRoot(t, w, "land:acc9a:1", "stale", at)
	f := newFakeCards(func() time.Time { return at.Add(time.Hour) })
	store.FreezeEventClock(at.Add(time.Hour))
	prepared, err := store.PrepareLandingCards(ctx, w.rt, "bifrost-platform", at.Add(time.Hour))
	ok(t, err)
	if len(prepared) != 1 {
		t.Fatal(prepared)
	}
	// The ask reaches the store, its reply is lost: nothing bound.
	f.loseReply = true
	if _, err := store.AskLandingCard(ctx, w.rt, f.source(), "bifrost-platform", prepared[0], at.Add(time.Hour), true); err == nil {
		t.Fatal("a lost reply bound")
	}
	src := f.source()
	only := store.CardQuestions{LookupKey: src.LookupKey}
	ok(t, newCardCtl(w, only).cardsPassAt(at.Add(time.Hour+time.Minute)))
	if c := cardsByN(t, w)[1]; !c.Bound || c.QuestionID != "q1" || f.asks != 1 {
		t.Fatalf("not taken up by its key alone: %+v, %d asks", c, f.asks)
	}
}
