package store_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// R5:5.6-1 (DESIGN-step-landed-superseded-2026-11-01 §6): a root the Owner
// declined whose same change landed from another root of its step stops
// holding back step_landed, through a controller-only landing_delivered_by
// note the store checks when it is written and step_landed checks again
// when it reads it.

var (
	dbOldA   = strings.Repeat("a1", 20)
	dbNewA   = strings.Repeat("b2", 20)
	dbThirdA = strings.Repeat("c3", 20)
)

// dbClock moves the frozen event clock on ten seconds at a time, so the
// stream's records keep their order in time.
type dbClock struct{ at time.Time }

func (c *dbClock) dbTick() time.Time {
	c.at = c.at.Add(10 * time.Second)
	store.FreezeEventClock(c.at)
	return c.at
}

// dbStep is a second step that, like s2, belongs to no mission, so M-3's
// tasks may sit on it.
const dbStep = "R2:2.1-9"

// dbWorld is a live landing stream with mission M-3 and steps s2 and
// dbStep, the event clock at its start, a day ago.
func dbWorld(t *testing.T) (*world, *dbClock) {
	t.Helper()
	return dbWorldAt(t, time.Now().Add(-24*time.Hour).UTC().Truncate(time.Second))
}

// dbWorldAt is dbWorld whose records are written from 45 minutes before at.
func dbWorldAt(t *testing.T, at time.Time) (*world, *dbClock) {
	t.Helper()
	t.Cleanup(store.FreezeEventClock(at))
	w := liveWorld(t, "none", at)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: dbStep, Parent: item, Kind: "step", Label: "2.1-9", Title: "s9",
			StepKind: "agent", Ordinal: 9, Root: root})
		return err
	}))
	return w, &dbClock{at: at.Add(-45 * time.Minute)}
}

// dbAccepted is an accepted implementation of M-3 on step, in repo.
func dbAccepted(t *testing.T, w *world, c *dbClock, step, repo string) string {
	t.Helper()
	c.dbTick()
	id := w.ready(t, store.Task{MissionID: "M-3", PlanNode: step, Repo: repo, BaseCommit: commitB})
	w.passRound(t, id, "h1", "h2", "h3")
	return id
}

// dbOp runs root's first operation on the platform stream, claimed with
// result a, to end: "conflict" (failed{conflict}), "declined" (then the
// Owner's "Do not merge": failed{declined}), "intent" (branch_intent),
// "pushed" (branch_pushed), "landed" (main_result{ok}, landed) or
// "reverted" (landed, then reverted).
func dbOp(t *testing.T, w *world, c *dbClock, root, a, end string) string {
	t.Helper()
	op := "land:" + root + ":1"
	ref := "refs/heads/land/prod/" + root + "/1"
	put := func(kind string, kv ...any) {
		t.Helper()
		c.dbTick()
		ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, kind, kv...)))
	}
	put("claimed", "task", root, "attempt", landingAttempt, "A", a, "rule_version", 1)
	if end == "conflict" || end == "declined" {
		put("failed", "why", "conflict")
		if end == "declined" {
			put("failed", "why", "declined")
		}
		return op
	}
	prepared := c.dbTick().Add(10 * time.Second)
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "prepared", "R", commitB, "M0", commitA,
		"committer_date", prepared.Format(time.RFC3339Nano), "dry_run", false)))
	put("branch_intent", "ref", ref, "R", commitB)
	if end == "intent" {
		return op
	}
	put("branch_pushed", "ref", ref, "R", commitB)
	if end == "pushed" {
		return op
	}
	seen := c.at.Add(5 * time.Second)
	put("ci_seen", "run", "ci-delivered", "status", "Succeeded", "R", commitB, "created_at", seen.Format(time.RFC3339Nano))
	put("main_intent", "R", commitB, "M0", commitA)
	put("main_result", "outcome", "ok")
	put("landed", "R", commitB)
	if end == "reverted" {
		put("reverted", "R", commitB, "by", "owner")
	}
	return op
}

func deliveredNotes(t *testing.T, w *world) int {
	t.Helper()
	var n int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE body_json ? 'landing_delivered_by'`).Scan(&n))
	return n
}

func deliveredBy(root, op, by, byOp, oldA, newA string) store.DeliveredBy {
	return store.DeliveredBy{Root: root, Op: op, By: by, ByOp: byOp, OldA: oldA, NewA: newA,
		PatchID: strings.Repeat("d", 40), DiffSHA256: strings.Repeat("e", 64)}
}

// Serial: FreezeEventClock. §6 item 1: each check refuses on its own, with
// nothing written; the note that passes them all is written and changes
// nothing the fold says.
func TestDeliveredByAppendChecks(t *testing.T) {
	w, c := dbWorld(t)
	old := dbAccepted(t, w, c, s2, "bifrost-platform")
	cur := dbAccepted(t, w, c, s2, "bifrost-platform")
	pending := dbAccepted(t, w, c, s2, "bifrost-platform")
	reverted := dbAccepted(t, w, c, s2, "bifrost-platform")
	overlap := dbAccepted(t, w, c, s2, "bifrost-platform")
	elsewhere := dbAccepted(t, w, c, dbStep, "bifrost-platform")
	infra := dbAccepted(t, w, c, s2, infraRepo)
	// An implementation that writes no code, given a landed operation: only
	// the role and effect check tells it apart.
	c.dbTick()
	exempt := w.ready(t, store.Task{MissionID: "M-3", PlanNode: s2, Effect: "none", BaseCommit: commitB})
	w.passRound(t, exempt, "h1", "h2", "h3")
	oldOp := dbOp(t, w, c, old, dbOldA, "declined")
	curOp := dbOp(t, w, c, cur, dbNewA, "landed")
	pendingOp := dbOp(t, w, c, pending, dbThirdA, "pushed")
	revertedOp := dbOp(t, w, c, reverted, dbThirdA, "reverted")
	overlapOp := dbOp(t, w, c, overlap, dbThirdA, "conflict")
	elsewhereOp := dbOp(t, w, c, elsewhere, dbThirdA, "landed")
	// An infra task's operation on the platform stream: only the repository
	// check tells it apart.
	infraOp := dbOp(t, w, c, infra, dbThirdA, "landed")
	exemptOp := dbOp(t, w, c, exempt, dbThirdA, "landed")
	good := deliveredBy(old, oldOp, cur, curOp, dbOldA, dbNewA)
	c.dbTick()

	for name, d := range map[string]store.DeliveredBy{
		"root's latest operation is not declined": deliveredBy(overlap, overlapOp, cur, curOp, dbThirdA, dbNewA),
		"op is not root's operation":              deliveredBy(old, curOp, cur, curOp, dbOldA, dbNewA),
		"by_op has not landed":                    deliveredBy(old, oldOp, pending, pendingOp, dbOldA, dbThirdA),
		"by_op was reverted":                      deliveredBy(old, oldOp, reverted, revertedOp, dbOldA, dbThirdA),
		"by_op is another task's":                 deliveredBy(old, oldOp, cur, elsewhereOp, dbOldA, dbThirdA),
		"by is on another step":                   deliveredBy(old, oldOp, elsewhere, elsewhereOp, dbOldA, dbThirdA),
		"by is of another repository":             deliveredBy(old, oldOp, infra, infraOp, dbOldA, dbThirdA),
		"by writes no code":                       deliveredBy(old, oldOp, exempt, exemptOp, dbOldA, dbThirdA),
		"by is root itself":                       deliveredBy(old, oldOp, old, oldOp, dbOldA, dbOldA),
		"old_a is not what root claimed":          deliveredBy(old, oldOp, cur, curOp, dbNewA, dbNewA),
		"new_a is not what by claimed":            deliveredBy(old, oldOp, cur, curOp, dbOldA, dbOldA),
	} {
		if err := store.RecordLandingDeliveredBy(ctx, w.rt, "bifrost-platform", d); !errors.Is(err, store.ErrDeliveredByRefused) {
			t.Errorf("%s: %v, want refused", name, err)
		}
	}
	// Only the controller writes it, on the repository stream, under root.
	appendAs := func(stream, actor, task string, body map[string]any) error {
		return store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, err := store.Append(ctx, tx, store.Event{Stream: stream, Type: "note", Actor: actor, TaskID: task,
				Body: map[string]any{"text": "delivered", "landing_delivered_by": body}})
			return err
		})
	}
	body := map[string]any{"root": good.Root, "op": good.Op, "by": good.By, "by_op": good.ByOp, "old_a": good.OldA, "new_a": good.NewA,
		"patch_id": good.PatchID, "diff_sha256": good.DiffSHA256}
	for name, err := range map[string]error{
		"another actor":      appendAs(landingRepoStream, "owner", old, body),
		"another stream":     appendAs("landing:"+infraRepo, "controller", old, body),
		"under another task": appendAs(landingRepoStream, "controller", cur, body),
	} {
		if !errors.Is(err, store.ErrDeliveredByRefused) {
			t.Errorf("%s: %v, want refused", name, err)
		}
	}
	// Its shape: every field, nothing else.
	extra := map[string]any{"why": "same"}
	for k, v := range body {
		extra[k] = v
	}
	missing := map[string]any{}
	for k, v := range body {
		if k != "diff_sha256" {
			missing[k] = v
		}
	}
	for name, b := range map[string]map[string]any{"a field more": extra, "a field less": missing} {
		if err := appendAs(landingRepoStream, "controller", old, b); err == nil {
			t.Errorf("%s: written", name)
		}
	}
	if n := deliveredNotes(t, w); n != 0 {
		t.Fatalf("%d notes written by refused appends", n)
	}

	ok(t, store.RecordLandingDeliveredBy(ctx, w.rt, "bifrost-platform", good))
	if n := deliveredNotes(t, w); n != 1 {
		t.Fatalf("%d notes, want the one that passed", n)
	}
	// The note is beside the protocol: the root stays failed{declined}.
	if o := landingState(t, w, landingRepoStream).Operations[oldOp]; o.Phase != "failed" || o.Outcome != "declined" {
		t.Fatalf("the declined root's fold moved: %s{%s}", o.Phase, o.Outcome)
	}
}

// Serial: FreezeEventClock. §6 item 4: step_landed lets a declined root
// step aside only for a delivery it reads as landed in the same step, the
// latest note winning, and still needs a root truly landed.
func TestStepLandedReadsDeliveredBy(t *testing.T) {
	w, c := dbWorld(t)
	landed := func(why string, want plan.Tri) {
		t.Helper()
		if ev := stepEvidence(t, w, s2, "accepted", 1); ev.Landed != want {
			t.Fatalf("%s: step_landed %s, want %s", why, ev.Landed, want)
		}
	}
	// Today's shape (5.5-2/3/4): two tasks on one step, the old one stacked
	// on an earlier slice failed conflict and the Owner declined it; the new
	// one, the same change on the new main, landed.
	old := dbAccepted(t, w, c, s2, "bifrost-platform")
	cur := dbAccepted(t, w, c, s2, "bifrost-platform")
	oldOp := dbOp(t, w, c, old, dbOldA, "declined")
	curOp := dbOp(t, w, c, cur, dbNewA, "landed")
	landed("declined, no note", plan.False)

	c.dbTick()
	ok(t, store.RecordLandingDeliveredBy(ctx, w.rt, "bifrost-platform", deliveredBy(old, oldOp, cur, curOp, dbOldA, dbNewA)))
	landed("declined, its change landed from the other task", plan.True)
	if ev := stepEvidence(t, w, s2, "accepted", 1); ev.STG != plan.False {
		t.Fatalf("step_stg_settled reads the landed root only: %s", ev.STG)
	}

	// A third task: the step waits for it, then holds once it landed.
	third := dbAccepted(t, w, c, s2, "bifrost-platform")
	landed("a third task accepted, not landed", plan.False)
	thirdOp := dbOp(t, w, c, third, dbThirdA, "landed")
	landed("the third task landed", plan.True)

	// The task that delivered moves to another step: the note no longer
	// names a root this step landed, so the declined root is a no again.
	moveToStep(t, w, cur, dbStep)
	landed("the delivering task moved off the step", plan.False)
	if ev := stepEvidence(t, w, dbStep, "accepted", 1); ev.Landed != plan.True {
		t.Fatalf("the moved task's own step: %s", ev.Landed)
	}

	// The latest note wins: one naming the third task, landed here.
	c.dbTick()
	ok(t, store.RecordLandingDeliveredBy(ctx, w.rt, "bifrost-platform", deliveredBy(old, oldOp, third, thirdOp, dbOldA, dbThirdA)))
	landed("the latest note names a landed root of the step", plan.True)

	// That delivery is reverted: no root delivered it any more.
	c.dbTick()
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(thirdOp, "reverted", "R", commitB, "by", "owner")))
	landed("the delivering landing reverted", plan.False)
}

// Serial: FreezeEventClock and signing anchor. §6 item 1: a landing grant
// request the Owner rejected declines the root as "Do not merge" does.
func TestDeliveredByAfterRejectedGrant(t *testing.T) {
	w, root, op, _, at := c4Wait(t)
	moveToStep(t, w, root, s2)
	wrote, err := store.RecordLandingGrantRequest(ctx, w.rt, c4Request(op, "rejected", at))
	ok(t, err)
	o := landingState(t, w, landingRepoStream).Operations[op]
	if !wrote || o.Phase != "failed" || o.Outcome != "declined" || o.A == "" {
		t.Fatalf("fixture: %v %s{%s} A %q", wrote, o.Phase, o.Outcome, o.A)
	}
	c := &dbClock{at: at}
	cur := dbAccepted(t, w, c, s2, "bifrost-platform")
	curOp := dbOp(t, w, c, cur, dbNewA, "landed")
	if ev := stepEvidence(t, w, s2, "accepted", 1); ev.Landed != plan.False {
		t.Fatalf("declined, no note: %s", ev.Landed)
	}
	c.dbTick()
	ok(t, store.RecordLandingDeliveredBy(ctx, w.rt, "bifrost-platform", deliveredBy(root, op, cur, curOp, o.A, dbNewA)))
	if ev := stepEvidence(t, w, s2, "accepted", 1); ev.Landed != plan.True {
		t.Fatalf("declined by a rejected grant, delivered: %s", ev.Landed)
	}
}

// deliveredGit is a repository Git whose only reach is the change identity
// the backfill reads: same says whether the two changes are one; err fails
// the read.
type deliveredGit struct {
	calls int
	same  bool
	err   error
}

func (g *deliveredGit) Open(context.Context, string) (*landinggit.Workspace, error) {
	return nil, errors.New("no workspace in this test")
}
func (g *deliveredGit) Ref(context.Context, string) (string, error) {
	return "", errors.New("no ref in this test")
}
func (g *deliveredGit) Delete(context.Context, string, string) error {
	return errors.New("no delete in this test")
}
func (g *deliveredGit) Changes(_ context.Context, a, b string) (landinggit.ChangeIdentity, landinggit.ChangeIdentity, error) {
	g.calls++
	if g.err != nil {
		return landinggit.ChangeIdentity{}, landinggit.ChangeIdentity{}, g.err
	}
	x := landinggit.ChangeIdentity{PatchID: strings.Repeat("1", 40), DiffSHA256: strings.Repeat("2", 64)}
	y := x
	if !g.same {
		y.DiffSHA256 = strings.Repeat("3", 64)
	}
	return x, y, nil
}

// deliveredPass runs the backfill's pass over repo's stream as it stands.
func deliveredPass(t *testing.T, w *world, ctl *controller.Controller, repo string) error {
	t.Helper()
	return ctl.LandingDeliveredByPass(ctx, repo, landingState(t, w, "landing:"+repo))
}

// Serial: FreezeEventClock. §6 item 3: each pass compares at most one pair;
// a pair whose changes differ, or that cannot be compared, is not read
// again, the latter logged once; one whose commits the remote seemed not to
// serve is read again after six hours, not before, logged each time; any
// other failed read writes nothing and is tried again later; the same
// change writes exactly one note per declined root. The landing loop passes over every live repository in
// turn: another repository's pass, with nothing to compare, forgets none of
// this one's.
func TestDeliveredByControllerPass(t *testing.T) {
	w, c := dbWorld(t)
	old2 := dbAccepted(t, w, c, s2, "bifrost-platform")
	old3 := dbAccepted(t, w, c, dbStep, "bifrost-platform")
	cur2 := dbAccepted(t, w, c, s2, "bifrost-platform")
	cur3 := dbAccepted(t, w, c, dbStep, "bifrost-platform")
	dbOp(t, w, c, old2, dbOldA, "declined")
	dbOp(t, w, c, old3, dbThirdA, "declined")
	cur2Op := dbOp(t, w, c, cur2, dbNewA, "landed")
	dbOp(t, w, c, cur3, dbNewA, "landed")
	c.dbTick()
	now := c.at
	var logs []string
	infra := &deliveredGit{}
	controllerWith := func(g *deliveredGit) *controller.Controller {
		return &controller.Controller{DB: w.rt, Env: "prod", Now: func() time.Time { return now },
			Logf:     func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) },
			DrillGit: map[string]controller.DrillGit{"bifrost-platform": g, infraRepo: infra}}
	}
	pass := func(ctl *controller.Controller, g *deliveredGit, calls, notes int, failed bool) {
		t.Helper()
		err := deliveredPass(t, w, ctl, "bifrost-platform")
		if (err != nil) != failed {
			t.Fatalf("pass error %v, want failed %v", err, failed)
		}
		if g.calls != calls || deliveredNotes(t, w) != notes {
			t.Fatalf("%d reads, %d notes; want %d, %d", g.calls, deliveredNotes(t, w), calls, notes)
		}
		// The infra repository's pass comes next, as in the landing loop.
		ok(t, deliveredPass(t, w, ctl, infraRepo))
	}
	differ := &deliveredGit{}
	ctl := controllerWith(differ)
	pass(ctl, differ, 1, 0, false) // one pair, s2's
	pass(ctl, differ, 2, 0, false) // the other, the other step's
	pass(ctl, differ, 2, 0, false) // neither again
	pass(ctl, differ, 2, 0, false)

	stuck := &deliveredGit{err: fmt.Errorf("%w: test", landinggit.ErrNotComparable)}
	ctl = controllerWith(stuck)
	pass(ctl, stuck, 1, 0, false)
	pass(ctl, stuck, 2, 0, false)
	now = now.Add(controller.DeliveredByNotServedRetry + controller.DeliveredByRetry)
	pass(ctl, stuck, 2, 0, false) // never tried again
	if len(logs) != 2 {
		t.Fatalf("pairs that cannot be compared logged %d times, want once each: %q", len(logs), logs)
	}

	logs = nil
	unserved := &deliveredGit{err: landinggit.ErrNotServed}
	ctl = controllerWith(unserved)
	pass(ctl, unserved, 1, 0, false)
	pass(ctl, unserved, 2, 0, false)
	now = now.Add(controller.DeliveredByRetry)
	pass(ctl, unserved, 2, 0, false) // not before six hours
	now = now.Add(controller.DeliveredByNotServedRetry - controller.DeliveredByRetry)
	pass(ctl, unserved, 3, 0, false) // read again
	pass(ctl, unserved, 4, 0, false)
	pass(ctl, unserved, 4, 0, false)
	if len(logs) != 4 {
		t.Fatalf("not served logged %d times, want each time it waits: %q", len(logs), logs)
	}

	failing := &deliveredGit{err: errors.New("gitea did not answer")}
	ctl = controllerWith(failing)
	pass(ctl, failing, 1, 0, true)
	pass(ctl, failing, 2, 0, true)
	pass(ctl, failing, 2, 0, false) // both wait
	now = now.Add(controller.DeliveredByRetry)
	failing.err, failing.same = nil, true
	pass(ctl, failing, 3, 1, false)
	pass(ctl, failing, 4, 2, false)
	pass(ctl, failing, 4, 2, false) // each root has its note: nothing to compare
	if infra.calls != 0 {
		t.Fatalf("the infra repository, with nothing declined, read %d pairs", infra.calls)
	}

	var by, byOp, oldA, newA, patch, digest string
	ok(t, w.rt.QueryRow(ctx, `SELECT n->>'by', n->>'by_op', n->>'old_a', n->>'new_a', n->>'patch_id', n->>'diff_sha256'
	  FROM (SELECT body_json->'landing_delivered_by' n FROM agentrt.event WHERE body_json->'landing_delivered_by'->>'root' = $1) x`, old2).
		Scan(&by, &byOp, &oldA, &newA, &patch, &digest))
	if by != cur2 || byOp != cur2Op || oldA != dbOldA || newA != dbNewA || patch != strings.Repeat("1", 40) || digest != strings.Repeat("2", 64) {
		t.Fatalf("s2's note: %s %s %s %s %s %s", by, byOp, oldA, newA, patch, digest)
	}
	for _, step := range []string{s2, dbStep} {
		if ev := stepEvidence(t, w, step, "accepted", 1); ev.Landed != plan.True {
			t.Fatalf("%s: step_landed %s", step, ev.Landed)
		}
	}
}

// Serial: FreezeEventClock. A root whose note names a sibling that has since
// moved off the step is compared again, with a sibling landed on the step
// now, so step_landed (which reads only a by landed on the step) can hold
// again.
func TestDeliveredByNotedAgainOnceTheDeliveryMoved(t *testing.T) {
	w, c := dbWorld(t)
	old := dbAccepted(t, w, c, s2, "bifrost-platform")
	cur := dbAccepted(t, w, c, s2, "bifrost-platform")
	third := dbAccepted(t, w, c, s2, "bifrost-platform")
	dbOp(t, w, c, old, dbOldA, "declined")
	dbOp(t, w, c, cur, dbNewA, "landed")
	c.dbTick()
	g := &deliveredGit{same: true}
	ctl := &controller.Controller{DB: w.rt, Env: "prod", Now: func() time.Time { return c.at },
		DrillGit: map[string]controller.DrillGit{"bifrost-platform": g}}
	landed := func(why string, want plan.Tri, calls, notes int) {
		t.Helper()
		ok(t, deliveredPass(t, w, ctl, "bifrost-platform"))
		if ev := stepEvidence(t, w, s2, "accepted", 1); ev.Landed != want || g.calls != calls || deliveredNotes(t, w) != notes {
			t.Fatalf("%s: step_landed %s, %d reads, %d notes; want %s, %d, %d", why, ev.Landed, g.calls, deliveredNotes(t, w), want, calls, notes)
		}
	}
	landed("the third task not landed yet: the step waits for it", plan.False, 1, 1)
	thirdOp := dbOp(t, w, c, third, dbThirdA, "landed")
	landed("noted, its delivery still landed on the step", plan.True, 1, 1)
	moveToStep(t, w, cur, dbStep)
	c.dbTick()
	landed("the delivery moved off: compared with the third task", plan.True, 2, 2)
	var by, byOp string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->'landing_delivered_by'->>'by', body_json->'landing_delivered_by'->>'by_op' FROM agentrt.event
	  WHERE body_json ? 'landing_delivered_by' ORDER BY event_id DESC LIMIT 1`).Scan(&by, &byOp))
	if by != third || byOp != thirdOp {
		t.Fatalf("the new note names %s %s, not the third task", by, byOp)
	}
	landed("noted again: nothing to compare", plan.True, 2, 2)
}

// budgetGit records when the landing pass reached a pending operation's Git
// (Ref, with the deadline it was given) and the backfill's (Changes, which
// takes all the time it is given).
type budgetGit struct {
	refAt, refDeadline, changesAt, changesDeadline time.Time
}

func (g *budgetGit) Open(context.Context, string) (*landinggit.Workspace, error) {
	return nil, errors.New("no workspace in this test")
}
func (g *budgetGit) Ref(ctx context.Context, _ string) (string, error) {
	g.refAt = time.Now()
	g.refDeadline, _ = ctx.Deadline()
	return "", errors.New("no ref in this test")
}
func (g *budgetGit) Delete(context.Context, string, string) error { return nil }
func (g *budgetGit) Changes(ctx context.Context, _, _ string) (landinggit.ChangeIdentity, landinggit.ChangeIdentity, error) {
	g.changesAt = time.Now()
	g.changesDeadline, _ = ctx.Deadline()
	<-ctx.Done()
	return landinggit.ChangeIdentity{}, landinggit.ChangeIdentity{}, ctx.Err()
}

// Serial: FreezeEventClock. §6 item 3: the backfill runs outside the landing
// pass's work budget. With a pair to compare whose read takes all it is
// given, the pending operation is still visited first and with its whole
// share; the backfill gets what the work left, short of the cleanup's
// reservation, and nothing at all when that is under DeliveredByMinShare.
func TestDeliveredByRunsAfterTheWork(t *testing.T) {
	w, c := dbWorldAt(t, time.Now().UTC().Truncate(time.Second))
	old := dbAccepted(t, w, c, s2, "bifrost-platform")
	cur := dbAccepted(t, w, c, s2, "bifrost-platform")
	pending := dbAccepted(t, w, c, s2, "bifrost-platform")
	dbOp(t, w, c, old, dbOldA, "declined")
	dbOp(t, w, c, cur, dbNewA, "landed")
	dbOp(t, w, c, pending, dbThirdA, "intent")
	c.dbTick()
	// run is one landing pass of length d: the pending operation's share and
	// the backfill's read, as they happened.
	run := func(d time.Duration) (g *budgetGit, start, end time.Time) {
		t.Helper()
		g = &budgetGit{}
		ctl := &controller.Controller{DB: w.rt, Env: "prod", Live: true, Now: time.Now, Drill: &controller.DrillSources{},
			DrillGit: map[string]controller.DrillGit{"bifrost-platform": g}}
		pass, cancel := context.WithTimeout(ctx, d)
		defer cancel()
		start = time.Now()
		end, _ = pass.Deadline()
		_ = ctl.LandingDrillPass(pass)
		if g.refAt.IsZero() {
			t.Fatalf("%s pass: the pending operation was not visited", d)
		}
		return g, start, end
	}

	// 4 s: 2 s reserved for the cleanup, 2 s of work, under the minimum
	// share left after it: no backfill this pass.
	if g, _, _ := run(4 * time.Second); !g.changesAt.IsZero() {
		t.Fatal("a backfill started on less than DeliveredByMinShare")
	}

	// 12 s: 6 s for the cleanup, 6 s of work, the visits' half (3 s) the one
	// pending operation's; the backfill gets the rest of the work's 6 s.
	g, start, end := run(12 * time.Second)
	switch {
	case g.changesAt.IsZero():
		t.Fatal("the backfill did not run after the work")
	case !g.refAt.Before(g.changesAt):
		t.Fatalf("the backfill ran before the work: ref %s, changes %s", g.refAt.Sub(start), g.changesAt.Sub(start))
	case g.refDeadline.Sub(start) < 2500*time.Millisecond:
		t.Fatalf("the pending operation's share ends %s after the pass began, want about 3s", g.refDeadline.Sub(start))
	case g.changesDeadline.After(end.Add(-6*time.Second + 50*time.Millisecond)): // the clock moves between reads
		t.Fatalf("the backfill's read ends %s before the pass does, inside the cleanup's 6s", end.Sub(g.changesDeadline))
	}
}

// Serial: FreezeEventClock, local Git PATH. Today's real shape end to end
// with real Git (§5, §6 item 6): the old result stacked on an earlier slice
// that never landed, the new one on the new main where that slice landed as
// another commit; the change itself byte for byte the same. The controller
// reads the two commits alone and writes the note; a hand-in whose change
// was edited (one space) gets none.
func TestDeliveredByRealShapeWithGit(t *testing.T) {
	w, c := dbWorld(t)
	f := localGit704(t)
	src, remote := f.sources[platformRepo], f.remotes[platformRepo]
	commit := func(parent string, files map[string]string, msg string) string {
		t.Helper()
		f.run(src, "checkout", "--detach", parent)
		for name, content := range files {
			ok(t, os.MkdirAll(filepath.Dir(filepath.Join(src, name)), 0700))
			ok(t, os.WriteFile(filepath.Join(src, name), []byte(content), 0644))
			f.run(src, "add", "--", name)
		}
		f.run(src, "commit", "-m", msg)
		sha := f.run(src, "rev-parse", "HEAD")
		f.run(src, "push", remote, sha+":refs/heads/agent/"+strings.ReplaceAll(msg, " ", "-"))
		return sha
	}
	work := "console/src/pages/Work.tsx"
	main := commit(f.bases[platformRepo], map[string]string{work: "export const tabs = 4\n"}, "main")
	slice := commit(main, map[string]string{"console/src/pages/Summary.tsx": "summary\n"}, "earlier slice")
	oldA := commit(slice, map[string]string{work: "export const tabs = 4\nexport const summary = true\n"}, "old hand-in")
	sliceLanded := commit(main, map[string]string{"console/src/pages/Summary.tsx": "summary\n"}, "earlier slice landed")
	newA := commit(sliceLanded, map[string]string{work: "export const tabs = 4\nexport const summary = true\n"}, "new hand-in")
	editedA := commit(sliceLanded, map[string]string{work: "export const tabs = 4\nexport const summary =  true\n"}, "edited hand-in")

	old := dbAccepted(t, w, c, s2, platformRepo)
	cur := dbAccepted(t, w, c, s2, platformRepo)
	old3 := dbAccepted(t, w, c, dbStep, platformRepo)
	edited := dbAccepted(t, w, c, dbStep, platformRepo)
	oldOp := dbOp(t, w, c, old, oldA, "declined")
	dbOp(t, w, c, cur, newA, "landed")
	dbOp(t, w, c, old3, oldA, "declined")
	dbOp(t, w, c, edited, editedA, "landed")
	c.dbTick()

	ctl := &controller.Controller{DB: w.rt, Env: "prod", Now: func() time.Time { return c.at }, DrillGit: f.adapters()}
	for range 3 {
		ok(t, deliveredPass(t, w, ctl, platformRepo))
	}
	if n := deliveredNotes(t, w); n != 1 {
		t.Fatalf("%d notes, want s2's only", n)
	}
	var root string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->'landing_delivered_by'->>'root' FROM agentrt.event WHERE body_json ? 'landing_delivered_by'`).Scan(&root))
	if root != old {
		t.Fatalf("the note is for %s, not the declined root of s2", root)
	}
	if ev := stepEvidence(t, w, s2, "accepted", 1); ev.Landed != plan.True {
		t.Fatalf("s2, the same change landed: %s", ev.Landed)
	}
	if ev := stepEvidence(t, w, dbStep, "accepted", 1); ev.Landed != plan.False {
		t.Fatalf("the other step, an edited change landed: %s", ev.Landed)
	}
	if o := landingState(t, w, landingRepoStream).Operations[oldOp]; o.Phase != "failed" || o.Outcome != "declined" {
		t.Fatalf("the declined root's fold moved: %s{%s}", o.Phase, o.Outcome)
	}
}
