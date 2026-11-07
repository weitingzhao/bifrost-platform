package controller_test

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// An accepted task's landing operation in progress (awaiting_grant,
// branch_pushed) whose change reached main another way (the Owner merged the
// coordinator's batch: a 'landed' note) ends in the next pass from the
// database alone, discarded main_changed, with no Git or CI read for it; the
// same pass's claim records its root's k+1 failed{in_main}. An operation
// whose task has no landed note is resumed as before.

// readGit is the rig's Git recording which land branches were read.
type readGit struct {
	batchGit
	refs *[]string
}

func (g readGit) Open(ctx context.Context, repo string) (*landinggit.Workspace, error) {
	return g.batchGit.Open(ctx, repo)
}
func (g readGit) Ref(ctx context.Context, ref string) (string, error) {
	g.r.mu.Lock()
	*g.refs = append(*g.refs, ref)
	g.r.mu.Unlock()
	return g.batchGit.Ref(ctx, ref)
}

// pushedAlone writes task's operation 1 from its claim to branch_pushed on
// rev, prepared on M0 batchM0.
func (r *batchRig) pushedAlone(t *testing.T, task, rev string) string {
	t.Helper()
	var attempt, a string
	must(t, r.pool.QueryRow(ctx, `SELECT attempt_id, pushed_commit FROM agentrt.attempt WHERE task_id=$1`, task).Scan(&attempt, &a))
	op, ref := "land:"+task+":1", "refs/heads/land/prod/"+task+"/1"
	start := r.now()
	for _, rec := range []map[string]any{
		{"kind": "claimed", "task": task, "attempt": attempt, "A": a, "rule_version": 1},
		{"kind": "prepared", "R": rev, "M0": batchM0, "committer_date": start.UTC().Format(time.RFC3339), "dry_run": false},
		{"kind": "branch_intent", "ref": ref, "R": rev},
		{"kind": "branch_pushed", "ref": ref, "R": rev},
	} {
		r.clock(time.Minute)
		rec["v"], rec["op"] = landing.Version, op
		must(t, store.InTx(ctx, r.pool, func(tx pgx.Tx) error {
			_, err := store.Append(ctx, tx, store.Event{Stream: "landing:bifrost-platform", Type: "note", Actor: "controller", TaskID: task,
				Body: map[string]any{"text": "test: " + rec["kind"].(string), "landing": rec}})
			return err
		}))
	}
	return op
}

// landedNote records, as RecordLandings does, that task's result is on main
// as sha.
func (r *batchRig) landedNote(t *testing.T, task, sha string) {
	t.Helper()
	must(t, store.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		_, err := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-3"), Type: "note", Actor: "controller", MissionID: "M-3", TaskID: task,
			Body: map[string]any{"text": "its accepted result is on main as " + sha[:12], "landed": sha}})
		return err
	}))
}

func resultOf(task string) string {
	sum := sha1.Sum([]byte(task))
	return hex.EncodeToString(sum[:])
}

func TestLandingOnMainEnds(t *testing.T) {
	r := newBatchRig(t, true)
	waiting := r.accepted(t, "console/src/components/OnMainA.tsx")
	pushed := r.accepted(t, "console/src/components/OnMainB.tsx")
	other := r.accepted(t, "console/src/components/OnMainC.tsx")
	waitingOp := r.waitingAlone(t, waiting)
	pushedRev, otherRev := strings.Repeat("6", 40), strings.Repeat("7", 40)
	pushedOp := r.pushedAlone(t, pushed, pushedRev)
	otherOp := r.pushedAlone(t, other, otherRev)
	r.landedNote(t, waiting, strings.Repeat("8", 40))
	r.landedNote(t, pushed, strings.Repeat("9", 40))

	// What the pass reads: main's history (A of the landed roots is in it),
	// CI by revision, CI kicked, land branches.
	onMain := map[string]bool{resultOf(waiting): true, resultOf(pushed): true}
	r.src.Facts.History = func(_ context.Context, _, _, commit, _ string) (landing.MainHistory, error) {
		if onMain[commit] {
			return landing.MainHistory{State: landing.Effective, Commit: commit}, nil
		}
		return landing.MainHistory{State: landing.Never}, nil
	}
	var ciRevs, refs []string
	ci := r.src.CI
	r.src.CI = func(ctx context.Context, repo, rev string, after time.Time) (landing.CIRun, error) {
		r.mu.Lock()
		ciRevs = append(ciRevs, rev)
		r.mu.Unlock()
		return ci(ctx, repo, rev, after)
	}
	r.ctl.DrillGit = map[string]controller.DrillGit{"bifrost-platform": readGit{batchGit{r}, &refs}}
	var logs []string
	r.ctl.Logf = func(format string, args ...any) {
		r.mu.Lock()
		logs = append(logs, format)
		r.mu.Unlock()
	}

	r.clock(time.Minute)
	r.pass(t)

	s := r.state(t)
	for _, c := range []struct{ root, op, rev string }{{waiting, waitingOp, "5555555555555555555555555555555555555555"}, {pushed, pushedOp, pushedRev}} {
		if o := s.Operations[c.op]; o.Phase != "discarded" || o.Outcome != "main_changed" {
			t.Fatalf("%s: %s %s, want discarded main_changed", c.op, o.Phase, o.Outcome)
		}
		next := s.LatestOperation(c.root)
		if o := s.Operations[next]; next != "land:"+c.root+":2" || o.Phase != "failed" || o.Outcome != "in_main" {
			t.Fatalf("%s: latest %s %s %s, want land:%s:2 failed in_main", c.root, next, o.Phase, o.Outcome, c.root)
		}
		if s.RootPending(c.root) {
			t.Fatalf("%s is still pending", c.root)
		}
		if slices.Contains(ciRevs, c.rev) || slices.Contains(refs, s.Operations[c.op].Ref) {
			t.Fatalf("%s: read CI %v or Git %v", c.op, ciRevs, refs)
		}
		for _, name := range r.ensured {
			if name == landing.CIName(c.op, 1) || name == landing.CIName(c.op, 2) {
				t.Fatalf("%s: CI kicked", c.op)
			}
		}
	}
	// The operation without a landed note is resumed: its CI is read.
	if o := s.Operations[otherOp]; o.Terminal() || !slices.Contains(ciRevs, otherRev) {
		t.Fatalf("%s: %s %s, CI read %v: want resumed", otherOp, o.Phase, o.Outcome, ciRevs)
	}
	ended := 0
	for _, l := range logs {
		if strings.Contains(l, "its task's change is on main as") {
			ended++
		}
	}
	if ended != 2 {
		t.Fatalf("%d ended lines, want 2: %v", ended, logs)
	}
	if len(r.pushes) != 0 {
		t.Fatal("pushed", r.pushes)
	}
}

// A root's eighth operation whose task has a landed note ends failed{bound},
// never a ninth claim.
func TestLandingOnMainEndsBound(t *testing.T) {
	r := newBatchRig(t, true)
	root := r.accepted(t, "console/src/components/OnMainBound.tsx")
	var attempt, a string
	must(t, r.pool.QueryRow(ctx, `SELECT attempt_id, pushed_commit FROM agentrt.attempt WHERE task_id=$1`, root).Scan(&attempt, &a))
	rev := strings.Repeat("6", 40)
	var op string
	for k := 1; k <= 8; k++ {
		op = "land:" + root + ":" + string(rune('0'+k))
		ref := "refs/heads/land/prod/" + root + "/" + string(rune('0'+k))
		recs := []map[string]any{
			{"kind": "claimed", "task": root, "attempt": attempt, "A": a, "rule_version": 1},
			{"kind": "prepared", "R": rev, "M0": batchM0, "committer_date": r.now().UTC().Format(time.RFC3339), "dry_run": false},
			{"kind": "branch_intent", "ref": ref, "R": rev},
			{"kind": "branch_pushed", "ref": ref, "R": rev},
		}
		if k < 8 {
			recs = append(recs, map[string]any{"kind": "discarded", "why": "main_changed"})
		}
		for _, rec := range recs {
			r.clock(time.Second)
			rec["v"], rec["op"] = landing.Version, op
			must(t, store.InTx(ctx, r.pool, func(tx pgx.Tx) error {
				_, err := store.Append(ctx, tx, store.Event{Stream: "landing:bifrost-platform", Type: "note", Actor: "controller", TaskID: root,
					Body: map[string]any{"text": "test: " + rec["kind"].(string), "landing": rec}})
				return err
			}))
		}
	}
	r.landedNote(t, root, strings.Repeat("8", 40))
	r.clock(time.Minute)
	r.pass(t)
	s := r.state(t)
	if o := s.Operations[op]; o.Phase != "failed" || o.Outcome != "bound" {
		t.Fatalf("%s: %s %s, want failed bound", op, o.Phase, o.Outcome)
	}
	if latest := s.LatestOperation(root); latest != op {
		t.Fatalf("latest %s, want %s: a ninth claim", latest, op)
	}
}
