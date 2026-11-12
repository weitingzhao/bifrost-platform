package store_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// R5:5.5-18 (DESIGN-r5-5.5-18-declined-rework-2026-11-03): a root the Owner
// declined whose step has reworked work landed, every change different,
// gets a rework_landed card; only his count_replaced on the Console lets
// step_landed excuse it, and only while the siblings it names stay landed
// on the step.

var (
	rwA1 = strings.Repeat("d4", 20)
	rwA2 = strings.Repeat("e5", 20)
	rwA3 = strings.Repeat("f6", 20)
)

func rwNotes(t *testing.T, w *world, key string) int {
	t.Helper()
	var n int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'note' AND body_json ? $1`, key).Scan(&n))
	return n
}

// sameForGit says two changes are the same only for the A pairs in same.
type sameForGit struct {
	deliveredGit
	same map[[2]string]bool
}

func (g *sameForGit) Changes(_ context.Context, a, b string) (landinggit.ChangeIdentity, landinggit.ChangeIdentity, error) {
	g.calls++
	x := landinggit.ChangeIdentity{PatchID: strings.Repeat("1", 40), DiffSHA256: strings.Repeat("2", 64)}
	y := x
	if !g.same[[2]string{a, b}] {
		y.DiffSHA256 = strings.Repeat("3", 64)
	}
	return x, y, nil
}

// rwWorld is the anonymous one-declined, three-landed shape on s2: the root
// declined, three reworked siblings landed, each change different.
type rwWorld struct {
	w                     *world
	c                     *dbClock
	root, b, cc, d        string
	rootOp, bOp, cOp, dOp string
	now                   time.Time
	ctl                   *controller.Controller
	git                   controller.DrillGit
}

func newRWWorld(t *testing.T, g controller.DrillGit) *rwWorld {
	t.Helper()
	w, c := dbWorld(t)
	r := &rwWorld{w: w, c: c, git: g}
	r.root = dbAccepted(t, w, c, s2, "bifrost-platform")
	r.b = dbAccepted(t, w, c, s2, "bifrost-platform")
	r.cc = dbAccepted(t, w, c, s2, "bifrost-platform")
	r.d = dbAccepted(t, w, c, s2, "bifrost-platform")
	r.rootOp = dbOp(t, w, c, r.root, dbOldA, "declined")
	r.bOp = dbOp(t, w, c, r.b, rwA1, "landed")
	r.cOp = dbOp(t, w, c, r.cc, rwA2, "landed")
	r.dOp = dbOp(t, w, c, r.d, rwA3, "landed")
	r.now = c.dbTick()
	r.ctl = r.rwCtl()
	return r
}

func (r *rwWorld) rwCtl() *controller.Controller {
	return &controller.Controller{DB: r.w.rt, Env: "prod", Now: func() time.Time { return r.now },
		Logf: func(string, ...any) {}, DrillGit: map[string]controller.DrillGit{"bifrost-platform": r.git}}
}

// pass runs one backfill pass, the clock on.
func (r *rwWorld) rwPass(t *testing.T) {
	t.Helper()
	r.now = r.c.dbTick()
	ok(t, deliveredPass(t, r.w, r.ctl, "bifrost-platform"))
}

func (r *rwWorld) rwLanded(t *testing.T, why string, want plan.Tri) {
	t.Helper()
	if ev := stepEvidence(t, r.w, s2, "accepted", 1); ev.Landed != want {
		t.Fatalf("%s: step_landed %s, want %s", why, ev.Landed, want)
	}
}

// onlyCard is the one card of the kind, failing on none or more.
func onlyCard(t *testing.T, w *world, kind string) store.CardRecord {
	t.Helper()
	var got []store.CardRecord
	for _, c := range cardsNow(t, w) {
		if c.Kind == kind {
			got = append(got, c)
		}
	}
	if len(got) != 1 {
		t.Fatalf("%d %s cards, want 1: %+v", len(got), kind, got)
	}
	return got[0]
}

func letterOf(t *testing.T, c store.CardRecord, choice string) string {
	t.Helper()
	for l, ch := range c.Options {
		if ch == choice {
			return l
		}
	}
	t.Fatalf("card %s offers no %s: %v", c.Prep, choice, c.Options)
	return ""
}

// Serial: FreezeEventClock. The whole way: three passes compare the three
// pairs, one each, and the third records the set once; a restart compares
// again and records nothing new. One card asks; count_replaced excuses the
// root and the step moves done through the controller's evaluation; nothing
// asks again.
func TestReworkLandedCardCountsTheRootDelivered(t *testing.T) {
	differ := &deliveredGit{}
	r := newRWWorld(t, differ)
	r.w.rule(t, s2, `{"step_landed":{}}`)
	r.rwLanded(t, "declined, reworked siblings landed, nothing recorded", plan.False)

	for i, want := range []int{0, 0, 1, 1} {
		r.rwPass(t)
		if n := rwNotes(t, r.w, "landing_rework_seen"); n != want {
			t.Fatalf("pass %d: %d rework records, want %d (%d reads)", i+1, n, want, differ.calls)
		}
	}
	if differ.calls != 3 || rwNotes(t, r.w, "landing_delivered_by") != 0 {
		t.Fatalf("%d reads, %d delivered_by; want 3, 0", differ.calls, rwNotes(t, r.w, "landing_delivered_by"))
	}
	r.ctl = r.rwCtl() // a restart forgets the comparisons
	for range 4 {
		r.rwPass(t)
	}
	if n := rwNotes(t, r.w, "landing_rework_seen"); n != 1 || differ.calls != 6 {
		t.Fatalf("after a restart: %d records, %d reads; want 1, 6", n, differ.calls)
	}
	r.rwLanded(t, "recorded, not answered", plan.False)
	if moved := r.w.evaluate(t, s2, &deployments{}); moved != "" {
		t.Fatalf("the step moved before the answer: %q", moved)
	}

	f := newFakeCards(func() time.Time { return r.now })
	cardPass(t, r.w, f, r.now)
	card := onlyCard(t, r.w, store.CardReworkLanded)
	if card.Root != r.root || card.Op != r.rootOp || card.QuestionID == "" || f.asks != 1 {
		t.Fatalf("card %+v, %d asks", card, f.asks)
	}
	if card.Options[letterOf(t, card, "still_owed")] != "still_owed" || card.Options[letterOf(t, card, "wait")] != "wait" {
		t.Fatalf("options %v", card.Options)
	}
	body := fmt.Sprint(card.Request["body"])
	for _, id := range []string{r.b, r.cc, r.d} {
		if !strings.Contains(body, id) {
			t.Fatalf("the card does not name %s: %s", id, body)
		}
	}

	f.answer(card.QuestionID, letterOf(t, card, "count_replaced"), r.now.Add(time.Minute))
	cardPass(t, r.w, f, r.now.Add(2*time.Minute))
	if n := rwNotes(t, r.w, "landing_replaced"); n != 1 {
		t.Fatalf("%d replaced records, want 1", n)
	}
	r.rwLanded(t, "counted delivered by the three", plan.True)
	if moved := r.w.evaluate(t, s2, &deployments{}); moved != "done" || r.w.stepState(t, s2) != "done" {
		t.Fatalf("the step did not move done: %q", moved)
	}
	// The root is still failed{declined} for every other reader.
	if o := landingState(t, r.w, landingRepoStream).Operations[r.rootOp]; o.Phase != "failed" || o.Outcome != "declined" {
		t.Fatalf("the declined root's fold moved: %s{%s}", o.Phase, o.Outcome)
	}
	r.rwPass(t)
	cardPass(t, r.w, f, r.now.Add(3*time.Minute))
	if f.asks != 1 || rwNotes(t, r.w, "landing_rework_seen") != 1 {
		t.Fatalf("asked again after the excuse: %d asks, %d records", f.asks, rwNotes(t, r.w, "landing_rework_seen"))
	}
}

// Serial: FreezeEventClock. still_owed records no excuse: the root holds the
// step and the same set is not asked again. A sibling that moves off the
// step changes the set: a new record and a new card, which the Owner may
// answer differently.
func TestReworkLandedStillOwedAndANewSet(t *testing.T) {
	r := newRWWorld(t, &deliveredGit{})
	for range 3 {
		r.rwPass(t)
	}
	f := newFakeCards(func() time.Time { return r.now })
	cardPass(t, r.w, f, r.now)
	card := onlyCard(t, r.w, store.CardReworkLanded)
	f.answer(card.QuestionID, letterOf(t, card, "still_owed"), r.now.Add(time.Minute))
	cardPass(t, r.w, f, r.now.Add(2*time.Minute))
	if n := rwNotes(t, r.w, "landing_replaced"); n != 0 {
		t.Fatalf("still owed wrote %d replaced records", n)
	}
	r.rwLanded(t, "still owed", plan.False)
	cardPass(t, r.w, f, r.now.Add(laterPass))
	if f.asks != 1 {
		t.Fatalf("the same set asked again: %d asks", f.asks)
	}

	first, _, err := store.RecordReworkSeen(ctx, r.w.rt, "bifrost-platform", r.root, r.rootOp, dbOldA, []store.ReworkSibling{
		{By: r.b, ByOp: r.bOp, NewA: rwA1, Why: "different"}, {By: r.cc, ByOp: r.cOp, NewA: rwA2, Why: "different"}, {By: r.d, ByOp: r.dOp, NewA: rwA3, Why: "different"}})
	ok(t, err)
	moveToStep(t, r.w, r.cc, dbStep)
	// The old set is found as it was recorded: a history, not what applies.
	again, st, err := store.RecordReworkSeen(ctx, r.w.rt, "bifrost-platform", r.root, r.rootOp, dbOldA, []store.ReworkSibling{
		{By: r.b, ByOp: r.bOp, NewA: rwA1, Why: "different"}, {By: r.cc, ByOp: r.cOp, NewA: rwA2, Why: "different"}, {By: r.d, ByOp: r.dOp, NewA: rwA3, Why: "different"}})
	ok(t, err)
	if st != "found" || again != first || first == 0 {
		t.Fatalf("the old set after it changed: %d %q, want found %d", again, st, first)
	}
	r.rwPass(t) // the pairs are settled: the new set {b, d} is recorded at once
	if n := rwNotes(t, r.w, "landing_rework_seen"); n != 2 {
		t.Fatalf("%d rework records after the set changed, want 2", n)
	}
	cardPass(t, r.w, f, r.now.Add(laterPass))
	if f.asks != 2 {
		t.Fatalf("the new set was not asked: %d asks", f.asks)
	}
	var next store.CardRecord
	for _, c := range cardsNow(t, r.w) {
		if c.Kind == store.CardReworkLanded && c.QuestionID != card.QuestionID {
			next = c
		}
	}
	body := fmt.Sprint(next.Request["body"])
	if strings.Contains(body, r.cc) || !strings.Contains(body, r.b) || !strings.Contains(body, r.d) {
		t.Fatalf("the new card names the old set: %s", body)
	}
	f.answer(next.QuestionID, letterOf(t, next, "count_replaced"), r.now.Add(laterPass+time.Minute))
	cardPass(t, r.w, f, r.now.Add(laterPass+2*time.Minute))
	r.rwLanded(t, "counted delivered by the two still on the step", plan.True)

	// One of the two named moves off as well: the excuse no longer holds.
	moveToStep(t, r.w, r.d, dbStep)
	r.rwLanded(t, "a named sibling moved off the step", plan.False)
}

// laterPass is a pass well after the one before (it was the engine's
// six-hour cooldown between two cards and a minute, until cleanup wave 1).
const laterPass = 6*time.Hour + time.Minute

// Serial: FreezeEventClock. One sibling the same change, one different: the
// same one writes landing_delivered_by, the root is excused that way, and
// no rework record or card follows.
func TestReworkSameChangeKeepsItsOwnPath(t *testing.T) {
	w, c := dbWorld(t)
	root := dbAccepted(t, w, c, s2, "bifrost-platform")
	same := dbAccepted(t, w, c, s2, "bifrost-platform")
	other := dbAccepted(t, w, c, s2, "bifrost-platform")
	dbOp(t, w, c, root, dbOldA, "declined")
	dbOp(t, w, c, same, rwA1, "landed")
	dbOp(t, w, c, other, rwA2, "landed")
	g := &sameForGit{same: map[[2]string]bool{{dbOldA, rwA1}: true}}
	now := c.dbTick()
	ctl := &controller.Controller{DB: w.rt, Env: "prod", Now: func() time.Time { return now }, Logf: func(string, ...any) {},
		DrillGit: map[string]controller.DrillGit{"bifrost-platform": g}}
	for range 4 {
		now = c.dbTick()
		ok(t, deliveredPass(t, w, ctl, "bifrost-platform"))
	}
	if rwNotes(t, w, "landing_delivered_by") != 1 || rwNotes(t, w, "landing_rework_seen") != 0 {
		t.Fatalf("%d delivered_by, %d rework; want 1, 0", rwNotes(t, w, "landing_delivered_by"), rwNotes(t, w, "landing_rework_seen"))
	}
	if ev := stepEvidence(t, w, s2, "accepted", 1); ev.Landed != plan.True {
		t.Fatalf("same change landed: %s", ev.Landed)
	}
	f := newFakeCards(func() time.Time { return now })
	cardPass(t, w, f, now)
	if f.asks != 0 {
		t.Fatalf("a card for a root its same change excused: %d asks", f.asks)
	}
}

// Serial: FreezeEventClock. NotServed is a wait, not a result: no record.
func TestReworkNotServedRecordsNothing(t *testing.T) {
	r := newRWWorld(t, &deliveredGit{err: landinggit.ErrNotServed})
	for range 4 {
		r.rwPass(t)
	}
	if n := rwNotes(t, r.w, "landing_rework_seen"); n != 0 {
		t.Fatalf("%d rework records from reads the remote did not serve", n)
	}
}

// Serial: FreezeEventClock. §2.4: a step already done is not reopened, so
// nothing about its declined root is recorded or asked.
func TestReworkClosedStepRecordsAndAsksNothing(t *testing.T) {
	r := newRWWorld(t, &deliveredGit{})
	r.w.rule(t, s2, `{"step_tasks":{"state":"accepted"}}`)
	if moved := r.w.evaluate(t, s2, &deployments{}); moved != "done" {
		t.Fatalf("fixture: the step did not move done: %q", moved)
	}
	for range 4 {
		r.rwPass(t)
	}
	if n := rwNotes(t, r.w, "landing_rework_seen"); n != 0 {
		t.Fatalf("%d rework records for a done step", n)
	}
	f := newFakeCards(func() time.Time { return r.now })
	cardPass(t, r.w, f, r.now)
	if f.asks != 0 {
		t.Fatalf("a card for a done step: %d asks", f.asks)
	}
}

// Serial: FreezeEventClock. A card prepared while the step was open whose
// step moves done before it is asked: closed resolved, never asked.
func TestReworkPreparedThenStepDone(t *testing.T) {
	r := newRWWorld(t, &deliveredGit{})
	for range 3 {
		r.rwPass(t)
	}
	prepared, err := store.PrepareLandingCards(ctx, r.w.rt, "bifrost-platform", r.now)
	ok(t, err)
	if len(prepared) != 1 {
		t.Fatalf("%d prepared cards, want 1", len(prepared))
	}
	r.w.rule(t, s2, `{"step_tasks":{"state":"accepted"}}`)
	if moved := r.w.evaluate(t, s2, &deployments{}); moved != "done" {
		t.Fatalf("fixture: the step did not move done: %q", moved)
	}
	f := newFakeCards(func() time.Time { return r.now })
	cardPass(t, r.w, f, r.now)
	card := onlyCard(t, r.w, store.CardReworkLanded)
	if f.asks != 0 || !card.Closed || card.Bound {
		t.Fatalf("asked %d, card %+v; want closed, never asked", f.asks, card)
	}
}
