package controller_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// R5:5.7 (PROD 2026-11-06): land:t_01M6RR249A4QXSD28Z6H1CHCR7:1, prepared on
// M0 5661d6c4, reached awaiting_grant; another landing then moved main to
// 632b874f, and the operation stayed waiting for a grant the final gate could
// only throw away. Main moved past an awaiting_grant operation's M0 ends it as
// the drill's replay does (discarded main_changed), with or without a grant;
// main still M0 or unreadable leaves it waiting.

// awaitingGrantRig is a rig whose core root waits in awaiting_grant on M0
// batchM0, leading a batch of three, or alone (no batch's member); lead is
// its operation.
func awaitingGrantRig(t *testing.T, batch bool) (r *batchRig, lead string, ops []string) {
	t.Helper()
	r = newBatchRig(t, true)
	root := r.accepted(t, "api/internal/agentrt/store/batch_grant.go")
	if !batch {
		// The rig prepares a lone root in a Git workspace it does not fake:
		// its records are written as the drill writes them.
		lead = r.waitingAlone(t, root)
		ops = []string{lead}
		r.status = "Succeeded"
	} else {
		b := r.formed(t, []string{root, r.accepted(t, "console/src/components/GrantA.tsx"), r.accepted(t, "console/src/components/GrantC.tsx")})
		lead, ops = b.Lead, b.Members
		r.clock(time.Minute)
		r.status = "Succeeded"
		r.pass(t)
	}
	// The pass after: the rule recheck answers awaiting_grant for the rule
	// in force, and is not run again for it (as on PROD before main moved).
	r.clock(time.Minute)
	r.pass(t)
	if o := r.state(t).Operations[lead]; o.Phase != "awaiting_grant" || o.M0 != batchM0 {
		t.Fatalf("lead %s: %s on %s, want awaiting_grant on M0", lead, o.Phase, o.M0)
	}
	return r, lead, ops
}

// waitingAlone writes task's operation 1 from its claim to awaiting_grant.
func (r *batchRig) waitingAlone(t *testing.T, task string) string {
	t.Helper()
	var attempt, a string
	must(t, r.pool.QueryRow(ctx, `SELECT attempt_id, pushed_commit FROM agentrt.attempt WHERE task_id=$1`, task).Scan(&attempt, &a))
	op, ref, rev := "land:"+task+":1", "refs/heads/land/prod/"+task+"/1", "5555555555555555555555555555555555555555"
	// One record a minute: prepared at +2m, its CI run created at +4m30s,
	// seen at +5m.
	start := r.now()
	for _, rec := range []map[string]any{
		{"kind": "claimed", "task": task, "attempt": attempt, "A": a, "rule_version": 1},
		{"kind": "prepared", "R": rev, "M0": batchM0, "committer_date": start.UTC().Format(time.RFC3339), "dry_run": false},
		{"kind": "branch_intent", "ref": ref, "R": rev},
		{"kind": "branch_pushed", "ref": ref, "R": rev},
		{"kind": "ci_seen", "run": landing.CIName(op, 1), "status": "Succeeded", "R": rev, "created_at": start.Add(4*time.Minute + 30*time.Second).UTC().Format(time.RFC3339)},
		{"kind": "awaiting_grant", "soft": []string{"core"}, "files": 1, "lines": 2, "material": "sha256:" + strings.Repeat("a", 64)},
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

// grantOn stands a usable grant on op for the rest of the test.
func grantOn(t *testing.T, op string) {
	t.Helper()
	t.Cleanup(controller.SetUsableLandingGrantForTest(func(_ context.Context, _ store.DB, id string) (string, error) {
		if id == op {
			return "grant_1", nil
		}
		return "", nil
	}))
}

func TestLandingAwaitingGrantReplaysWhenMainMoved(t *testing.T) {
	moved := "4444444444444444444444444444444444444444"
	for _, c := range []struct {
		name         string
		batch, grant bool
	}{{"alone", false, false}, {"alone, granted", false, true}, {"batch", true, false}, {"batch, granted", true, true}} {
		t.Run(c.name, func(t *testing.T) {
			r, lead, ops := awaitingGrantRig(t, c.batch)
			if c.grant {
				grantOn(t, lead)
			}
			r.src.Main = func(context.Context, string) (string, error) { return moved, nil }
			r.clock(time.Minute)
			_ = r.ctl.LandingDrillPass(ctx) // the k+1 the discard opens is not this test's
			s := r.state(t)
			for _, op := range ops {
				if o := s.Operations[op]; o.Phase != "discarded" || o.Outcome != "main_changed" {
					t.Fatalf("%s: %s %s, want discarded main_changed", op, o.Phase, o.Outcome)
				}
			}
			if len(r.pushes) != 0 {
				t.Fatal("pushed on a moved main", r.pushes)
			}
		})
	}
}

func TestLandingAwaitingGrantStaysOnM0OrUnreadableMain(t *testing.T) {
	for _, c := range []struct {
		name string
		main func(context.Context, string) (string, error)
	}{
		{"main is M0", func(context.Context, string) (string, error) { return batchM0, nil }},
		{"main unreadable", func(context.Context, string) (string, error) { return "", errors.New("gitea down") }},
		{"main empty", func(context.Context, string) (string, error) { return "", nil }},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, lead, _ := awaitingGrantRig(t, false)
			r.src.Main = c.main
			r.clock(time.Minute)
			_ = r.ctl.LandingDrillPass(ctx) // an unreadable main fails the rule recheck, as before
			if o := r.state(t).Operations[lead]; o.Phase != "awaiting_grant" {
				t.Fatalf("%s: %s %s, want awaiting_grant", lead, o.Phase, o.Outcome)
			}
			if len(r.pushes) != 0 {
				t.Fatal("pushed without a grant", r.pushes)
			}
		})
	}
}
