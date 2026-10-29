package store_test

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func c4Source(path string) store.LandingSource {
	return store.LandingSource{
		Result: func(context.Context, string, string, string, string, string) (bool, []rules.LandingChange, error) {
			return true, []rules.LandingChange{{OldPath: path, NewPath: path, OldMode: "100644", NewMode: "100644", Added: 1, Deleted: 1, AddedText: "<p>Ready</p>"}}, nil
		},
		History: func(context.Context, string, string, string, string) (landing.MainHistory, error) {
			return landing.MainHistory{State: landing.Never}, nil
		},
	}
}

func c4Ready(t *testing.T, path string) (*world, string, string, store.LandingSource, time.Time) {
	t.Helper()
	at := time.Now().UTC().Truncate(time.Second)
	t.Cleanup(store.FreezeEventClock(at))
	w := liveWorld(t, "none", at)
	store.FreezeEventClock(at.Add(-40 * time.Minute))
	root := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "C4", BaseCommit: commitB})
	w.passRound(t, root, "h1", "h2", "h3")
	src := c4Source(path)
	store.FreezeEventClock(at.Add(-30 * time.Minute))
	op, d, err := store.ClaimLanding(ctx, w.rt, src, "prod", root, at)
	ok(t, err)
	if !d.Allowed || op == "" {
		t.Fatal("preparation claim", d)
	}
	c4Prepare(t, w, root, op, at.Add(-29*time.Minute), true)
	store.FreezeEventClock(at)
	return w, root, op, src, at
}

func c4Prepare(t *testing.T, w *world, root, op string, at time.Time, pushed bool) {
	t.Helper()
	store.FreezeEventClock(at)
	parts := strings.Split(op, ":")
	ref := "refs/heads/land/prod/" + root + "/" + parts[len(parts)-1]
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "prepared", "R", commitB, "M0", commitA, "committer_date", at.Format(time.RFC3339Nano), "dry_run", false)))
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "branch_intent", "ref", ref, "R", commitB)))
	if pushed {
		ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "branch_pushed", "ref", ref, "R", commitB)))
	}
}

func c4Wait(t *testing.T) (*world, string, string, store.LandingSource, time.Time) {
	t.Helper()
	w, root, op, src, at := c4Ready(t, "api/internal/agentrt/store/c4.go")
	auth, d, err := store.AuthorizeLandingPush(ctx, w.rt, src, op, observed(at.Add(-29*time.Minute)))
	ok(t, err)
	if d.Reason != "awaiting_grant" || auth.Op != "" {
		t.Fatal(auth, d)
	}
	return w, root, op, src, at
}

func c4Request(op, status string, at time.Time) store.LandingGrantRequest {
	return store.LandingGrantRequest{Op: op, ApprovalID: "approval_c4", ApprovalNumber: 100, Status: status, ParamsHash: strings.Repeat("a", 64), SeenAt: at}
}

// Serial: the real database fixtures freeze the global event clock. T37's
// C-4 half: a core root prepares/waits, releases its slot and quota, another
// root lands. Grant creation/consumption and PROD acceptance belong to C-5/C-7.
func TestC4T37WaitingReleasesSlotAndNoGrantNeverPushes(t *testing.T) {
	w, root, op, src, at := c4Wait(t)
	s := landingState(t, w, landingRepoStream)
	o := s.Operations[op]
	if o.Active() || o.Terminal() || !slices.Contains(o.Soft, "core") {
		t.Fatal(o)
	}
	quota := landingState(t, w, "landing-quota:dev:"+at.Format("2006-01-02"))
	if len(quota.Reservations) != 0 {
		t.Fatal(quota)
	}
	var before int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id=$1`, landingRepoStream).Scan(&before))
	for i := 0; i < 3; i++ {
		auth, d, err := store.AuthorizeLandingPush(ctx, w.rt, src, op, func(context.Context, landing.Operation) (store.LiveObservation, error) {
			t.Fatal("waiting polled external evidence")
			return store.LiveObservation{}, nil
		})
		ok(t, err)
		if auth.Op != "" || d.Reason != "awaiting_grant" {
			t.Fatal(auth, d)
		}
	}
	if err := appendLanding(w, landingRepoStream, newLandingRecord(op, "main_intent", "R", commitB, "M0", commitA)); err == nil {
		t.Fatal("direct append bypassed missing grant")
	}
	var after int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id=$1`, landingRepoStream).Scan(&after))
	if before != after {
		t.Fatal("waiting spam", before, after)
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		tr, err := store.LandingCardTriggers(ctx, tx, "bifrost-platform", at)
		for _, x := range tr {
			if x.Root == root {
				t.Fatal("waiting created card", x)
			}
		}
		return err
	}))
	if closePass(t, w, at) != "active" {
		t.Fatal("waiting mission closed")
	}
	other := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "C4 other root", BaseCommit: commitB})
	w.passRound(t, other, "h1", "h2", "h3")
	plain := c4Source("console/src/components/A.tsx")
	op2, d, err := store.ClaimLanding(ctx, w.rt, plain, "prod", other, at)
	ok(t, err)
	if !d.Allowed || op2 == "" {
		t.Fatal("wait held slot", d)
	}
	c4Prepare(t, w, other, op2, at, true)
	store.FreezeEventClock(at.Add(2 * time.Minute))
	auth, d, err := store.AuthorizeLandingPush(ctx, w.rt, plain, op2, observed(at))
	ok(t, err)
	if !d.Allowed || auth.Op != op2 {
		t.Fatal("ordinary final gate", d)
	}
	_, err = store.RecordPushResult(ctx, w.rt, auth, "ok")
	ok(t, err)
	effectiveRead(t, w, at.Add(3*time.Minute))
	if p, _ := phaseOf(t, w, op2); p != "landed" {
		t.Fatal(p)
	}
	// Reject only the first root; B4 then closes on the second root's
	// fresh delivery and preserves the rejected approval as disposition.
	store.FreezeEventClock(at.Add(4 * time.Minute))
	_, err = store.RecordLandingGrantRequest(ctx, w.rt, c4Request(op, "rejected", at.Add(4*time.Minute)))
	ok(t, err)
	closePass(t, w, at.Add(5*time.Minute))
	effectiveRead(t, w, at.Add(6*time.Minute))
	if closePass(t, w, at.Add(7*time.Minute)) != "done" {
		t.Fatal("B4 did not close delivered+disposed")
	}
}

// Serial: FreezeEventClock. T40: latest rejection atomically binds the approval;
// expiry/unknown merely observe. Every malformed rejection has a valid control.
func TestC4T40RejectionOwnershipAndObservation(t *testing.T) {
	w, root, op, _, at := c4Wait(t)
	for i, status := range []string{"pending", "expired", "unknown"} {
		r := c4Request(op, status, at)
		r.ApprovalID = fmt.Sprintf("approval_%d", i)
		r.ApprovalNumber = int64(i + 1)
		wrote, err := store.RecordLandingGrantRequest(ctx, w.rt, r)
		ok(t, err)
		if !wrote {
			t.Fatal("not observed")
		}
		wrote, err = store.RecordLandingGrantRequest(ctx, w.rt, r)
		ok(t, err)
		if wrote {
			t.Fatal("duplicate observation")
		}
		if p, _ := phaseOf(t, w, op); p != "awaiting_grant" {
			t.Fatal(status, p)
		}
	}
	if err := appendLanding(w, landingRepoStream, newLandingRecord(op, "failed", "why", "declined")); err == nil {
		t.Fatal("decline without approval")
	}
	wrote, err := store.RecordLandingGrantRequest(ctx, w.rt, c4Request(op, "rejected", at))
	ok(t, err)
	if !wrote {
		t.Fatal("rejection not recorded")
	}
	var failure, request string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->'landing'->>'why',body_json->'landing_grant_request'->>'approval_id' FROM agentrt.event WHERE event_stream_id=$1 ORDER BY seq DESC LIMIT 1`, landingRepoStream).Scan(&failure, &request))
	if failure != "declined" || request != "approval_c4" {
		t.Fatal(failure, request)
	}
	st, err := store.ReadMissionLandingStatus(ctx, w.rt, "M-3")
	ok(t, err)
	if len(st.Roots) != 1 || st.Roots[0].Root != root || st.Roots[0].Class != store.RootDisposed || st.Roots[0].ApprovalID != request {
		t.Fatal(st)
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		tr, err := store.LandingCardTriggers(ctx, tx, "bifrost-platform", at)
		if len(tr) != 0 {
			t.Fatal(tr)
		}
		return err
	}))
}

// Serial: FreezeEventClock; concurrent workers use independent PG connections.
// T41/T42/T43: shared refresh transition, unique k+1, old-decision isolation,
// and branch push response loss recovered by reading the same ref and SHA.
func TestC4T41T42RefreshGenerationsAndLostBranchResponse(t *testing.T) {
	w, root, op, src, at := c4Wait(t)
	old := landingState(t, w, landingRepoStream).Operations[op]
	if _, err := store.RefreshWaitingForTest(ctx, w.rt, root, op, commitA); err == nil {
		t.Fatal("refresh with unchanged main")
	}
	wrote, err := store.RefreshWaitingForTest(ctx, w.rt, root, op, commitB)
	ok(t, err)
	if !wrote {
		t.Fatal("refresh not recorded")
	}
	if _, err := store.RefreshWaitingForTest(ctx, w.rt, root, op, commitB); err == nil {
		t.Fatal("old op refreshed twice")
	}
	conns := []*pgx.Conn{w.db.Connect(t, w.db.Runtime), w.db.Connect(t, w.db.Runtime)}
	var wg sync.WaitGroup
	claims := make(chan string, 2)
	errs := make(chan error, 2)
	for _, conn := range conns {
		wg.Add(1)
		go func(db *pgx.Conn) {
			defer wg.Done()
			next, _, err := store.ClaimLanding(ctx, db, src, "prod", root, at)
			claims <- next
			errs <- err
		}(conn)
	}
	wg.Wait()
	close(claims)
	close(errs)
	for err := range errs {
		ok(t, err)
	}
	var next string
	count := 0
	for id := range claims {
		if id != "" {
			next = id
			count++
		}
	}
	if count != 1 || next != fmt.Sprintf("land:%s:2", root) {
		t.Fatal(count, next)
	}
	c4Prepare(t, w, root, next, at, true)
	// Model a crash just before branch_pushed separately on a third root,
	// since the acknowledged second branch itself must remain immutable.
	newOp := landingState(t, w, landingRepoStream).Operations[next]
	if newOp.Ref == old.Ref || newOp.ClaimNumber != 2 {
		t.Fatal(newOp)
	}
	store.FreezeEventClock(at.Add(time.Minute))
	_, err = store.RecordLandingGrantRequest(ctx, w.rt, c4Request(op, "rejected", at.Add(time.Minute)))
	ok(t, err)
	if phase, _ := phaseOf(t, w, next); phase != "branch_pushed" {
		t.Fatal("old rejection ended new op", phase)
	}
	kept := landingState(t, w, landingRepoStream).Operations[op]
	if kept.Ref != old.Ref || kept.R != old.R || kept.M0 != old.M0 || !kept.CommitterDate.Equal(old.CommitterDate) {
		t.Fatal("rewrote old generation")
	}
	// Existing branch-intent recovery: no replay/Open or new ref on restart.
	other := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "lost branch response", BaseCommit: commitB})
	lost := "land:" + other + ":1"
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(lost, "claimed", "task", other, "attempt", landingAttempt, "A", commitA, "rule_version", 1)))
	c4Prepare(t, w, other, lost, at.Add(2*time.Minute), false)
	remote := &branchObserved{tip: commitB}
	ctl := controller.Controller{DB: w.rt, Env: "prod", Live: true}
	ok(t, ctl.LandingDrillBranch(ctx, remote, lost, at.Add(3*time.Minute)))
	if phase, _ := phaseOf(t, w, lost); phase != "branch_pushed" || remote.opens != 0 {
		t.Fatal(phase, remote.opens)
	}
	before := landingState(t, w, landingRepoStream)
	after := landingState(t, w, landingRepoStream)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("restart fold differs")
	}
}

// Serial: FreezeEventClock. T39/T43/T47: 72h settlement runs with live off;
// repetition adds no event, expiry offers try_again and retry resets E/W only.
func TestC4T39T47ExpirationSettlesWithoutLiveAndRetries(t *testing.T) {
	w, root, op, _, at := c4Wait(t)
	store.FreezeEventClock(at.Add(71 * time.Hour))
	ctl := controller.Controller{DB: w.rt, Env: "prod", Live: false, Now: store.DecisionAt, Reconcile: &controller.LandingReconcile{}}
	ok(t, ctl.LandingReconcilePass(ctx))
	if phase, _ := phaseOf(t, w, op); phase != "awaiting_grant" {
		t.Fatal(phase)
	}
	store.FreezeEventClock(at.Add(72 * time.Hour))
	ok(t, ctl.LandingReconcilePass(ctx))
	if phase, why := phaseOf(t, w, op); phase != "failed" || why != "approval_expired" {
		t.Fatal(phase, why)
	}
	var before int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id=$1`, landingRepoStream).Scan(&before))
	ok(t, ctl.LandingReconcilePass(ctx))
	var after int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id=$1`, landingRepoStream).Scan(&after))
	if before != after {
		t.Fatal("repeated settlement")
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		tr, err := store.LandingCardTriggers(ctx, tx, "bifrost-platform", at.Add(72*time.Hour))
		if len(tr) != 1 || tr[0].Kind != store.CardRootFailed || !slices.Contains(tr[0].Choices, "try_again") {
			t.Fatal(tr)
		}
		return err
	}))
	ok(t, applyRetry(w.rt, root, op, "retry after expired grant wait", at.Add(72*time.Hour)))
	b := landing.RootTiming(landingState(t, w, landingRepoStream), root, at.Add(72*time.Hour))
	if b.Execution != 0 || b.Waiting != 0 || b.Operations != 1 {
		t.Fatal(b)
	}
}

// Serial: FreezeEventClock. T46: same reconciler entry succeeds on an active
// mission and fails stale after only its state changes, with no Git/CI source.
func TestC4T46WaitingMissionStop(t *testing.T) {
	w, _, op, _, at := c4Wait(t)
	ended, err := store.SettleWaitingLandingStale(ctx, w.rt)
	ok(t, err)
	if len(ended) != 0 {
		t.Fatal(ended)
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-3"), Type: "mission_state_changed", Actor: "controller", MissionID: "M-3", Body: map[string]any{"from": "active", "to": "stopped", "reason": "C4 stale test"}})
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE agentrt.mission SET state='stopped',version=version+1 WHERE mission_id='M-3'`)
		return err
	}))
	store.FreezeEventClock(at.Add(time.Minute))
	ended, err = store.SettleWaitingLandingStale(ctx, w.rt)
	ok(t, err)
	if !slices.Equal(ended, []string{op}) {
		t.Fatal(ended)
	}
	if phase, why := phaseOf(t, w, op); phase != "failed" || why != "stale" {
		t.Fatal(phase, why)
	}
}

// Serial: FreezeEventClock. T39: two generations contribute to the same
// retry epoch; main movement does not reset the 72-hour waiting allowance.
func TestC4T39WaitingAccumulatesAcrossGenerations(t *testing.T) {
	w, root, op, src, at := c4Wait(t)
	store.FreezeEventClock(at.Add(40 * time.Hour))
	_, err := store.RefreshWaitingForTest(ctx, w.rt, root, op, commitB)
	ok(t, err)
	next, d, err := store.ClaimLanding(ctx, w.rt, src, "prod", root, at.Add(40*time.Hour))
	ok(t, err)
	if next == "" || !d.Allowed {
		t.Fatal(d)
	}
	c4Prepare(t, w, root, next, at.Add(40*time.Hour), true)
	store.FreezeEventClock(at.Add(40*time.Hour + 2*time.Minute))
	_, d, err = store.AuthorizeLandingPush(ctx, w.rt, src, next, observed(at.Add(40*time.Hour)))
	ok(t, err)
	if d.Reason != "awaiting_grant" {
		t.Fatal(d)
	}
	now := at.Add(73*time.Hour + 2*time.Minute)
	store.FreezeEventClock(now)
	s, err := store.SettleLandingDrillBounds(ctx, w.rt, now)
	ok(t, err)
	if o := s.Operations[next]; o.Phase != "failed" || o.Outcome != "approval_expired" {
		t.Fatal(o)
	}
	if b := landing.RootTiming(s, root, now); b.Waiting != 73*time.Hour || b.Execution >= time.Hour || b.Operations != 2 {
		t.Fatal(b)
	}
}

// Serial: FreezeEventClock. T44: the eighth operation may reach main; only
// the need to open a ninth ends the root bound. Same stream and gate each time.
func TestC4T44EighthPushAndNinthRefusal(t *testing.T) {
	for _, ninth := range []bool{false, true} {
		t.Run(fmt.Sprint(ninth), func(t *testing.T) {
			w, root, op, src, at := c4Ready(t, "console/src/components/A.tsx")
			for k := 1; k < 8; k++ {
				ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "discarded", "why", "main_changed")))
				var d landing.Decision
				var err error
				op, d, err = store.ClaimLanding(ctx, w.rt, src, "prod", root, at)
				ok(t, err)
				if !d.Allowed {
					t.Fatal(k, d)
				}
				c4Prepare(t, w, root, op, at, true)
			}
			store.FreezeEventClock(at.Add(2 * time.Minute))
			if !ninth {
				auth, d, err := store.AuthorizeLandingPush(ctx, w.rt, src, op, observed(at))
				ok(t, err)
				if !d.Allowed || auth.Op != op {
					t.Fatal("eighth denied", d)
				}
				store.FreezeEventClock(at.Add(100 * time.Hour))
				_, err = store.SettleLandingDrillBounds(ctx, w.rt, at.Add(100*time.Hour))
				ok(t, err)
				if phase, _ := phaseOf(t, w, op); phase != "main_intent" {
					t.Fatal("timer terminated at_main", phase)
				}
			} else {
				ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "discarded", "why", "main_changed")))
				next, d, err := store.ClaimLanding(ctx, w.rt, src, "prod", root, at)
				ok(t, err)
				if next != "" || d.Reason != "bound" {
					t.Fatal(next, d)
				}
				s := landingState(t, w, landingRepoStream)
				last := s.Operations[s.LatestOperation(root)]
				if last.Phase != "failed" || last.Outcome != "bound" {
					t.Fatal(last)
				}
			}
		})
	}
}
