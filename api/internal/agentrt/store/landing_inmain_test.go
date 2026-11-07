package store_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// A failed{in_main} root whose change is on main (a landed note on its
// mission stream) is settled by the controller, main_delivered, without a
// card; the Owner's own answer stays the Owner's.

// inMainRoot is an accepted root of M-3 whose landing claim ended
// failed{in_main}, with a landed note naming landed when it is not "".
func inMainRoot(t *testing.T, w *world, at time.Time, landed string) string {
	t.Helper()
	store.FreezeEventClock(at.Add(-40 * time.Minute))
	root := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "in main " + landed, BaseCommit: commitB})
	w.passRound(t, root, "h1", "h2", "h3")
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord("land:"+root+":1", "failed", "why", "in_main")))
	if landed != "" {
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, err := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-3"), Type: "note", Actor: "controller", MissionID: "M-3", TaskID: root,
				Body: map[string]any{"text": "on main", "landed": landed}})
			return err
		}))
	}
	return root
}

func disposals(t *testing.T, w *world, root string) []string {
	t.Helper()
	var whys []string
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT body_json->'landing_disposed'->>'why' FROM agentrt.event WHERE event_stream_id = $1
		  AND body_json->'landing_disposed'->>'root' = $2 ORDER BY seq`, landingRepoStream, root)
		if err != nil {
			return err
		}
		whys, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	}))
	return whys
}

// Serial: FreezeEventClock.
func TestSettleInMain(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := liveWorld(t, "none", at)
	landed := inMainRoot(t, w, at, commitB)
	asked := inMainRoot(t, w, at, "")
	landedOp, askedOp := "land:"+landed+":1", "land:"+asked+":1"

	// Both ask before the pass: the landed root's card is open already.
	f := newFakeCards(func() time.Time { return at })
	cardPass(t, w, f, at)
	open := map[string]string{}
	for _, c := range cardsNow(t, w) {
		if c.Kind == store.CardInMain {
			open[c.Root] = c.QuestionID
		}
	}
	if open[landed] == "" || open[asked] == "" {
		t.Fatalf("in_main cards before the pass: %v", open)
	}

	store.FreezeEventClock(at.Add(time.Minute))
	n, err := store.SettleInMain(ctx, w.rt, "bifrost-platform")
	ok(t, err)
	if n != 1 {
		t.Fatalf("settled %d, want 1", n)
	}
	n, err = store.SettleInMain(ctx, w.rt, "bifrost-platform")
	ok(t, err)
	if n != 0 {
		t.Fatalf("settled again: %d", n)
	}
	if got := disposals(t, w, landed); len(got) != 1 || got[0] != "main_delivered" {
		t.Fatalf("landed root disposals: %v", got)
	}
	if got := disposals(t, w, asked); len(got) != 0 {
		t.Fatalf("a root without a landed note was disposed: %v", got)
	}

	got := triggersAt(t, w, at.Add(2*time.Minute))
	if tr, has := got[landedOp]; has {
		t.Errorf("a settled in-main root still has a trigger: %+v", tr)
	}
	if tr := got[askedOp]; tr.Kind != store.CardInMain {
		t.Errorf("a root without a landed note does not ask: %+v", tr)
	}

	// The card already open is withdrawn; the other stays open.
	cardPass(t, w, f, at.Add(3*time.Minute))
	f.mu.Lock()
	landedStatus, askedStatus := f.byID[open[landed]].q.Status, f.byID[open[asked]].q.Status
	f.mu.Unlock()
	if landedStatus != "withdrawn" || askedStatus != "open" {
		t.Fatalf("after the pass: landed card %s, asked card %s", landedStatus, askedStatus)
	}

	status, err := store.ReadMissionLandingStatus(ctx, w.rt, "M-3")
	ok(t, err)
	classes := map[string]store.LandingRootStatus{}
	for _, r := range status.Roots {
		classes[r.Root] = r
	}
	if c := classes[landed]; c.Class != store.RootDelivered || c.Reason != "in_main" {
		t.Errorf("settled root: %+v", c)
	}
	if c := classes[asked]; c.Class != store.RootUnresolved || c.Reason != "in_main_unverified" {
		t.Errorf("unsettled root: %+v", c)
	}
}

// Serial: FreezeEventClock. main_delivered is the controller's, for a
// failed{in_main} root's landed commit, once; owner_delivered still follows
// the Owner's answer.
func TestLandingDisposedMainDelivered(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := liveWorld(t, "none", at)
	landed := inMainRoot(t, w, at, commitB)
	bare := inMainRoot(t, w, at, "")
	store.FreezeEventClock(at.Add(time.Minute))
	disposal := func(actor, root, why, main string) error {
		d := map[string]any{"v": "1", "root": root, "op": "land:" + root + ":1", "why": why}
		if main != "" {
			d["main"] = main
		}
		if why == "owner_delivered" {
			d["trigger_seq"], d["question_id"] = 1, "q-none"
		}
		return store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, err := store.Append(ctx, tx, store.Event{Stream: landingRepoStream, Type: "note", Actor: actor, TaskID: root,
				Body: map[string]any{"text": "disposed", "landing_disposed": d}})
			return err
		})
	}
	for name, err := range map[string]error{
		"owner_delivered without the Owner's answer": disposal("controller", landed, "owner_delivered", ""),
		"main_delivered without its commit":          disposal("controller", landed, "main_delivered", ""),
		"main_delivered of another commit":           disposal("controller", landed, "main_delivered", commitA),
		"main_delivered by another actor":            disposal("owner", landed, "main_delivered", commitB),
		"main_delivered without a landed note":       disposal("controller", bare, "main_delivered", commitB),
	} {
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	ok(t, disposal("controller", landed, "main_delivered", commitB))
	if err := disposal("controller", landed, "main_delivered", commitB); err == nil {
		t.Error("a second main_delivered was accepted")
	}
	if got := disposals(t, w, landed); len(got) != 1 {
		t.Fatalf("disposals: %v", got)
	}
}
