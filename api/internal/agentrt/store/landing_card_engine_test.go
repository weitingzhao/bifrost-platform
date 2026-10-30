package store_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// S3 B3 engine (CUT §11.6 v3): prepare, ask and bind, settle, against a fake
// question store with the real store's key rule (a key finds only an open
// or answered-and-unacknowledged question).

type fakeQuestion struct {
	q       store.Question
	key     string
	acked   bool
	created time.Time
}

type fakeCards struct {
	mu        sync.Mutex
	byID      map[string]*fakeQuestion
	asks      int
	loseReply bool // AskRaw makes the question, then fails
	ackFails  bool
	now       func() time.Time
}

func newFakeCards(now func() time.Time) *fakeCards {
	return &fakeCards{byID: map[string]*fakeQuestion{}, now: now}
}

func (f *fakeCards) protected(key string) *fakeQuestion {
	for _, q := range f.byID {
		if q.key == key && (q.q.Status == "open" || q.q.Status == store.QuestionAnswered && !q.acked) {
			return q
		}
	}
	return nil
}

func (f *fakeCards) source() store.CardQuestions {
	return store.CardQuestions{
		AskRaw: func(_ context.Context, key string, _ map[string]any) (string, time.Time, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if p := f.protected(key); p != nil {
				return p.q.ID, p.created, nil
			}
			f.asks++
			id := fmt.Sprintf("q%d", f.asks)
			f.byID[id] = &fakeQuestion{q: store.Question{ID: id, Status: "open", Options: 2}, key: key, created: f.now()}
			if f.loseReply {
				return "", time.Time{}, errors.New("reply lost")
			}
			return id, f.now(), nil
		},
		LookupKey: func(_ context.Context, key string, _ map[string]any) (store.Question, time.Time, bool, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if p := f.protected(key); p != nil {
				return p.q, p.created, true, nil
			}
			return store.Question{}, time.Time{}, false, nil
		},
		Lookup: func(_ context.Context, id string) (store.Question, bool, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			p, ok := f.byID[id]
			if !ok {
				return store.Question{}, false, nil
			}
			return p.q, true, nil
		},
		Withdraw: func(_ context.Context, id, _ string) (string, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			p, ok := f.byID[id]
			if !ok {
				return "gone", nil
			}
			if p.q.Status == "open" {
				p.q.Status = "withdrawn"
			}
			return p.q.Status, nil
		},
		Ack: func(_ context.Context, id string) (bool, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.ackFails {
				return false, errors.New("ack failed")
			}
			p, ok := f.byID[id]
			if !ok {
				return false, nil
			}
			p.acked = true
			return true, nil
		},
	}
}

func (f *fakeCards) answer(id, letter string, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.byID[id]
	p.q.Status, p.q.Option, p.q.Channel, p.q.AnsweredBy, p.q.AnsweredAt = store.QuestionAnswered, letter, "console", "owner", at.UTC().Format(time.RFC3339Nano)
	p.q.OwnerAnswer = true
}

// cardPass is one pass of the engine: settle, ask, prepare, then ask again.
func cardPass(t *testing.T, w *world, f *fakeCards, now time.Time) {
	t.Helper()
	store.FreezeEventClock(now)
	src := f.source()
	settle := func() {
		_, bound, err := store.LandingCardsToSettle(ctx, w.rt, "bifrost-platform")
		ok(t, err)
		for _, c := range bound {
			_, _ = store.SettleLandingCard(ctx, w.rt, src, c, now)
		}
	}
	askAll := func() {
		ask, _, err := store.LandingCardsToSettle(ctx, w.rt, "bifrost-platform")
		ok(t, err)
		for _, c := range ask {
			_, _ = store.AskLandingCard(ctx, w.rt, src, "bifrost-platform", c.Prep, now, true)
		}
	}
	settle()
	askAll()
	_, err := store.PrepareLandingCards(ctx, w.rt, "bifrost-platform", now)
	ok(t, err)
	askAll()
}

func cardsNow(t *testing.T, w *world) map[string]store.CardRecord {
	t.Helper()
	var cards map[string]store.CardRecord
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var err error
		cards, err = store.LandingCards(ctx, tx, store.CardsStream("bifrost-platform"))
		return err
	}))
	return cards
}

func cardFailedRoot(t *testing.T, w *world, op, why string, at time.Time) string {
	t.Helper()
	root := w.ready(t, store.Task{OffPlanReason: "card " + op})
	prodChain(t, w, root, op, at, "branch_pushed")
	store.FreezeEventClock(at.Add(10 * time.Minute))
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "failed", "why", why)))
	return root
}

// Serial: FreezeEventClock. A failed root: a card, answered try again; the
// retry is recorded once, the card acknowledged, and no new card follows.
func TestLandingCardAnsweredAndCarriedOut(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().Add(-3 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	cardFailedRoot(t, w, "land:c1:1", "stale", at)
	f := newFakeCards(func() time.Time { return at.Add(time.Hour) })
	cardPass(t, w, f, at.Add(time.Hour))
	cards := cardsNow(t, w)
	var c store.CardRecord
	for _, x := range cards {
		c = x
	}
	if len(cards) != 1 || !c.Bound || c.QuestionID != "q1" || c.Options["a"] != "try_again" || f.asks != 1 {
		t.Fatalf("%+v asks=%d", cards, f.asks)
	}
	f.answer("q1", "a", at.Add(90*time.Minute))
	cardPass(t, w, f, at.Add(2*time.Hour))
	c = cardsNow(t, w)[c.Prep]
	if !c.Acked || !f.byID["q1"].acked {
		t.Fatalf("not acknowledged: %+v", c)
	}
	if o := landingState(t, w, landingRepoStream).Operations["land:c1:1"]; !o.Retried {
		t.Fatal("the retry was not carried out")
	}
	cardPass(t, w, f, at.Add(9*time.Hour))
	if len(cardsNow(t, w)) != 1 || f.asks != 1 {
		t.Fatal("a card after the answer was carried out")
	}
}

// Serial: FreezeEventClock. The store made the card but the reply was lost:
// the next pass finds it by its key and binds it; nothing is asked twice.
// An ask started more than seven days ago that finds nothing is closed
// unconfirmed, never asked again.
func TestLandingCardAskRecovery(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().Add(-10 * 24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	cardFailedRoot(t, w, "land:c2:1", "conflict", at)
	f := newFakeCards(func() time.Time { return at.Add(time.Hour) })
	f.loseReply = true
	cardPass(t, w, f, at.Add(time.Hour))
	var c store.CardRecord
	for _, x := range cardsNow(t, w) {
		c = x
	}
	if c.Bound || !c.AskStarted || f.asks != 1 {
		t.Fatalf("%+v asks=%d", c, f.asks)
	}
	f.loseReply = false
	cardPass(t, w, f, at.Add(2*time.Hour))
	if c = cardsNow(t, w)[c.Prep]; !c.Bound || c.QuestionID != "q1" || f.asks != 1 {
		t.Fatalf("not recovered: %+v asks=%d", c, f.asks)
	}

	// Past the window, nothing found: unconfirmed, no ask.
	w2 := newKernelWorld(t)
	cardFailedRoot(t, w2, "land:c3:1", "conflict", at)
	f2 := newFakeCards(func() time.Time { return at.Add(time.Hour) })
	f2.loseReply = true
	cardPass(t, w2, f2, at.Add(time.Hour))
	delete(f2.byID, "q1") // trimmed
	f2.loseReply = false
	cardPass(t, w2, f2, at.Add(time.Hour+store.CardRecoverWindow))
	for _, x := range cardsNow(t, w2) {
		if x.N == 1 && (!x.Closed || x.ClosedWhy != "unconfirmed") {
			t.Fatalf("%+v", x)
		}
	}
}

// Serial: FreezeEventClock. ok_missing answered wait: held 24 hours, then a
// second card; when the push lands, the open card is withdrawn as resolved.
func TestLandingCardHeldThenResolved(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().Add(-3 * 24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	root := w.ready(t, store.Task{OffPlanReason: "card ok missing"})
	prodChain(t, w, root, "land:c4:1", at, "main_intent")
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord("land:c4:1", "main_result", "outcome", "ok")))
	controllerNote(t, w, root, map[string]any{"text": "anomaly", "landing_anomaly": map[string]any{"op": "land:c4:1", "root": root, "kind": "ok_missing", "R": commitB, "M0": commitA, "head": commitA}})
	f := newFakeCards(func() time.Time { return at.Add(time.Hour) })
	cardPass(t, w, f, at.Add(time.Hour))
	f.answer("q1", "b", at.Add(2*time.Hour)) // wait
	cardPass(t, w, f, at.Add(3*time.Hour))
	cardPass(t, w, f, at.Add(20*time.Hour))
	if f.asks != 1 {
		t.Fatal("asked again while held", f.asks)
	}
	if v, _, err := store.ReadLandingCard(ctx, w.rt, "bifrost-platform", "q1"); err != nil || v.Outcome["outcome"] != "held" || v.Outcome["remind_at"] == nil {
		t.Fatalf("held view: %+v %v", v, err)
	}
	cardPass(t, w, f, at.Add(27*time.Hour))
	if f.asks != 2 {
		t.Fatal("not asked again after the hold", f.asks)
	}
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord("land:c4:1", "landed", "R", commitB)))
	cardPass(t, w, f, at.Add(28*time.Hour))
	if f.byID["q2"].q.Status != "withdrawn" {
		t.Fatal("the open card was not withdrawn")
	}
	for _, c := range cardsNow(t, w) {
		if c.N == 2 && (!c.Closed || c.ClosedWhy != "resolved") {
			t.Fatalf("%+v", c)
		}
	}
}

// Serial: FreezeEventClock. A stop's card answered resume writes the resume
// of that generation once; an old stop's card answered after a newer stop
// is moot and resumes nothing.
func TestLandingCardRepoResume(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().Add(-3 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	root := w.ready(t, store.Task{OffPlanReason: "card stop"})
	stop := func(gen int) {
		controllerNote(t, w, root, map[string]any{"text": "stop", "landing_stop": map[string]any{"repo": "bifrost-platform", "op": "land:x:1", "root": root, "generation": gen}})
	}
	resumes := func() int {
		var n int
		ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = $1 AND body_json ? 'landing_resume'`, landingRepoStream).Scan(&n))
		return n
	}
	stop(1)
	f := newFakeCards(func() time.Time { return at.Add(time.Hour) })
	cardPass(t, w, f, at.Add(time.Hour))
	f.answer("q1", "a", at.Add(90*time.Minute))
	cardPass(t, w, f, at.Add(2*time.Hour))
	if resumes() != 1 {
		t.Fatal("resume not written once", resumes())
	}
	v, found, err := store.ReadLandingCard(ctx, w.rt, "bifrost-platform", "q1")
	ok(t, err)
	if !found || v.Kind != store.CardRepoStopped || v.Outcome["outcome"] != "applied" || v.Outcome["why"] != "repo_resumed" || !v.Acked || v.NextStep == "" {
		t.Fatalf("the card's view: %+v", v)
	}
	if _, found, err := store.ReadLandingCard(ctx, w.rt, "bifrost-platform", "q-none"); found || err != nil {
		t.Fatal("a question no card was bound to", found, err)
	}
	cardPass(t, w, f, at.Add(3*time.Hour))
	if resumes() != 1 || f.asks != 1 {
		t.Fatal("asked or resumed again")
	}
	// Stop 2, its card; then stop 3 before the answer: the answer is moot.
	stop(2)
	cardPass(t, w, f, at.Add(4*time.Hour))
	stop(3)
	f.answer("q2", "a", at.Add(4*time.Hour+time.Minute))
	cardPass(t, w, f, at.Add(5*time.Hour))
	if resumes() != 1 {
		t.Fatal("an old stop's answer resumed a newer stop")
	}
}

// Serial: FreezeEventClock. A bound card the store no longer has, never
// handled: closed lost. An Ack that fails is retried; the answer is handled
// once.
func TestLandingCardLostAndAckRetried(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().Add(-3 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	cardFailedRoot(t, w, "land:c6:1", "conflict", at)
	f := newFakeCards(func() time.Time { return at.Add(time.Hour) })
	cardPass(t, w, f, at.Add(time.Hour))
	delete(f.byID, "q1")
	cardPass(t, w, f, at.Add(2*time.Hour))
	for _, c := range cardsNow(t, w) {
		if c.QuestionID == "q1" && (!c.Closed || c.ClosedWhy != "lost") {
			t.Fatalf("%+v", c)
		}
	}

	w2 := newKernelWorld(t)
	cardFailedRoot(t, w2, "land:c7:1", "stale", at)
	f2 := newFakeCards(func() time.Time { return at.Add(time.Hour) })
	cardPass(t, w2, f2, at.Add(time.Hour))
	f2.answer("q1", "b", at.Add(90*time.Minute)) // do not merge
	f2.ackFails = true
	cardPass(t, w2, f2, at.Add(2*time.Hour))
	if o := landingState(t, w2, landingRepoStream).Operations["land:c7:1"]; o.Outcome != "declined" {
		t.Fatal("not declined", o.Outcome)
	}
	f2.ackFails = false
	cardPass(t, w2, f2, at.Add(3*time.Hour))
	var answers int
	ok(t, w2.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = $1 AND body_json ? 'landing_answer'`, landingRepoStream).Scan(&answers))
	if answers != 1 || !f2.byID["q1"].acked {
		t.Fatal("handled", answers, "acked", f2.byID["q1"].acked)
	}
}

// Serial: FreezeEventClock. Review 670 B-02: a card answered try again after
// its mission became no_merge is moot under the same locks; nothing is
// retried. Review 670 B-03: the seven days are decided on the clock after
// the lookup: a lookup that crosses them closes the card unconfirmed.
func TestLandingCardDecidesOnTheStateAtTheAnswer(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().Add(-10 * 24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	root := cardFailedRoot(t, w, "land:c8:1", "stale", at)
	f := newFakeCards(func() time.Time { return at.Add(time.Hour) })
	cardPass(t, w, f, at.Add(time.Hour))
	var mission string
	ok(t, w.rt.QueryRow(ctx, `SELECT mission_id FROM agentrt.task WHERE task_id = $1`, root).Scan(&mission))
	tamperAdmin(t, w, `UPDATE agentrt.mission SET scope_json = jsonb_set(COALESCE(scope_json,'{}'), '{no_merge}', '"owner"') WHERE mission_id = $1`, mission)
	f.answer("q1", "a", at.Add(90*time.Minute)) // try again
	cardPass(t, w, f, at.Add(2*time.Hour))
	if o := landingState(t, w, landingRepoStream).Operations["land:c8:1"]; o.Retried {
		t.Fatal("retried after the mission became no_merge")
	}
	v, _, err := store.ReadLandingCard(ctx, w.rt, "bifrost-platform", "q1")
	ok(t, err)
	if v.Outcome["outcome"] != "moot" || v.Outcome["why"] != "trigger_gone" {
		t.Fatalf("%+v", v.Outcome)
	}

	// The lookup crosses the window.
	w2 := newKernelWorld(t)
	cardFailedRoot(t, w2, "land:c9:1", "conflict", at)
	f2 := newFakeCards(func() time.Time { return at.Add(time.Hour) })
	f2.loseReply = true
	cardPass(t, w2, f2, at.Add(time.Hour))
	delete(f2.byID, "q1")
	f2.loseReply = false
	src := f2.source()
	look := src.LookupKey
	src.LookupKey = func(c context.Context, key string, req map[string]any) (store.Question, time.Time, bool, error) {
		store.FreezeEventClock(at.Add(time.Hour + store.CardRecoverWindow + time.Second))
		return look(c, key, req)
	}
	store.FreezeEventClock(at.Add(time.Hour + store.CardRecoverWindow - time.Second))
	ask, _, err := store.LandingCardsToSettle(ctx, w2.rt, "bifrost-platform")
	ok(t, err)
	for _, c := range ask {
		_, err := store.AskLandingCard(ctx, w2.rt, src, "bifrost-platform", c.Prep, at.Add(time.Hour+store.CardRecoverWindow-time.Second), true)
		ok(t, err)
	}
	if f2.asks != 1 {
		t.Fatal("asked again across the window", f2.asks)
	}
	for _, c := range cardsNow(t, w2) {
		if !c.Closed || c.ClosedWhy != "unconfirmed" {
			t.Fatalf("%+v", c)
		}
	}
}
