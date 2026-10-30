package store_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The first transaction of a platform pass settles bounds and returns its
// scan snapshot. Interleave the other controller after that commit, with no
// sleeps, production hook or open transaction/stream lock held by A.
type td462AfterSnapshot struct {
	store.DB
	after func()
}

type td462SnapshotTx struct {
	pgx.Tx
	after func()
}

func (d *td462AfterSnapshot) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	tx, err := d.DB.BeginTx(ctx, opts)
	if err != nil || d.after == nil {
		return tx, err
	}
	after := d.after
	d.after = nil
	return td462SnapshotTx{Tx: tx, after: after}, nil
}

func (tx td462SnapshotTx) Commit(ctx context.Context) error {
	if err := tx.Tx.Commit(ctx); err != nil {
		return err
	}
	tx.after()
	return nil
}

func td462PrepareBusy(t *testing.T, c *c5, root, op string) {
	t.Helper()
	c4Prepare(t, c.w, root, op, c.at, true)
	ok(t, store.AppendCIKick(ctx, c.w.rt, op, "ci_kick", 1, c.key, "TD-462: competing controller"))
}

func td462CountOpens(ctl *controller.Controller, p *livePusher) *int {
	opens := 0
	ctl.OpenLive = func(context.Context, string, string) (controller.LiveWorkspace, error) {
		opens++
		return p, nil
	}
	return &opens
}

func td462GrantUses(t *testing.T, c *c5, grant string) int {
	t.Helper()
	var uses int
	ok(t, c.w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id=$1
	  AND body_json->'landing_grant_use'->>'grant_id'=$2`, landingRepoStream, grant).Scan(&uses))
	return uses
}

// Serial: FreezeEventClock. Review 787 B1: X's whole active lifetime lies
// between A's snapshots. B claims X before W gets a grant, then B's final
// gate moves X to awaiting_grant before A scans again.
func TestTD462ReleaseBetweenControllerSnapshotsPG(t *testing.T) {
	c := c5Wait(t, c5Core, false, true)
	root := td462Root(t, c)
	pA, pB := &livePusher{outcome: landinggit.PushOK}, &livePusher{outcome: landinggit.PushOK}
	a, b := td462Controller(c, pA), td462Controller(c, pB)
	opens := td462CountOpens(a, pA)
	var busy string
	var grant store.LandingGrant
	a.DB = &td462AfterSnapshot{DB: c.w.rt, after: func() {
		// B scans and claims the new root. The test Git adapter stops at
		// branch preparation; complete its branch fixture below.
		err := b.LandingDrillPass(ctx)
		if err == nil || !strings.Contains(err.Error(), "must reconcile before replay") {
			t.Fatal("B did not reach branch preparation", err)
		}
		busy = landingState(t, c.w, landingRepoStream).LatestOperation(root)
		if busy == "" {
			t.Fatal("B did not claim X")
		}
		td462PrepareBusy(t, c, root, busy)
		grant = c.grant(t, store.ActionCoreLanding)
	}}
	ok(t, a.LandingDrillPass(ctx))
	if busy == "" || *opens != 1 || len(pA.pushes) != 0 || c.reservations(t) != 0 {
		t.Fatal("A did not hit the unseen in-flight blocker", busy, *opens, pA.pushes)
	}
	store.FreezeEventClock(c.at.Add(2 * time.Second))
	b.Drill.CI = func(_ context.Context, _, r string, _ time.Time) (landing.CIRun, error) {
		return landing.CIRun{Run: landing.CIName(busy, 1), R: r, Status: "Succeeded", CreatedAt: c.at.Add(time.Second)}, nil
	}
	action, err := b.LandingDrillCI(ctx, *b.Drill, busy, store.DecisionAt())
	ok(t, err)
	if action.Kind != "ready" || landingState(t, c.w, landingRepoStream).Operations[busy].Phase != "awaiting_grant" {
		t.Fatal("B did not release X at the final gate", action)
	}
	store.FreezeEventClock(c.at.Add(15 * time.Second))
	ok(t, a.LandingDrillPass(ctx))
	if *opens < 2 || len(pA.pushes) != 1 || len(pB.pushes) != 0 || c.phase(t) != "main_result" || c.reservations(t) != 1 || td462GrantUses(t, c, grant.GrantID) != 1 {
		t.Fatal("A did not push W exactly once on the next scan", *opens, pA.pushes, pB.pushes)
	}
	store.FreezeEventClock(c.at.Add(30 * time.Second))
	ok(t, a.LandingDrillPass(ctx))
	if len(pA.pushes) != 1 || len(pB.pushes) != 0 || td462GrantUses(t, c, grant.GrantID) != 1 {
		t.Fatal("repeated scan pushed or consumed twice")
	}
}

// Serial: FreezeEventClock. Review 787 B1: the snapshot sees X, but the
// gate is refused by Y or by quota. X's disappearance must not wake W.
func TestTD462NoWakeForReplacementOrQuotaPG(t *testing.T) {
	for _, reason := range []string{"replacement", "quota"} {
		t.Run(reason, func(t *testing.T) {
			c := c5Wait(t, c5Core, false, true)
			xRoot, yRoot := td462Root(t, c), td462Root(t, c)
			x, d, err := store.ClaimLanding(ctx, c.w.rt, c.src, "prod", xRoot, c.at)
			ok(t, err)
			if x == "" || !d.Allowed {
				t.Fatal(d)
			}
			p := &livePusher{outcome: landinggit.PushOK}
			ctl := td462Controller(c, p)
			opens := td462CountOpens(ctl, p)
			ctl.Drill.CI = func(_ context.Context, _, r string, after time.Time) (landing.CIRun, error) {
				if after.Equal(c.at) {
					return landing.CIRun{Run: "busy", R: r, Status: "Running", CreatedAt: c.at}, nil
				}
				return c.ci, nil
			}
			ctl.DB = &td462AfterSnapshot{DB: c.w.rt, after: func() {
				if reason == "replacement" {
					ok(t, appendLanding(c.w, landingRepoStream, newLandingRecord(x, "failed", "why", "stale")))
					y, d, err := store.ClaimLanding(ctx, c.w.rt, c.src, "prod", yRoot, c.at)
					ok(t, err)
					if y == "" || !d.Allowed {
						t.Fatal("replacement claim", d)
					}
					td462PrepareBusy(t, c, yRoot, y)
				}
				c.grant(t, store.ActionCoreLanding)
				if reason == "quota" {
					// Reserve while X is live and prepared, then fail it: its
					// slot is freed, but the day's one-landing quota stays used.
					c4Prepare(t, c.w, xRoot, x, c.at, true)
					body := strings.Replace(c5RuleBody, "max_landings_per_day: 5", "max_landings_per_day: 1", 1)
					ok(t, firstErr(c.signer.sign(c.w, ruleText("landing:dev", 2, "tighten", since, until, body))))
					ok(t, appendLanding(c.w, "landing-quota:dev:"+c.at.Format("2006-01-02"), newLandingRecord(x, "reserved", "root", xRoot)))
					ok(t, appendLanding(c.w, landingRepoStream, newLandingRecord(x, "failed", "why", "stale")))
				}
			}}
			ok(t, ctl.LandingDrillPass(ctx))
			if *opens != 1 || len(p.pushes) != 0 {
				t.Fatal("first refusal", *opens, p.pushes)
			}
			for scan := 1; scan <= 3; scan++ {
				store.FreezeEventClock(c.at.Add(time.Duration(scan) * 15 * time.Second))
				ok(t, ctl.LandingDrillPass(ctx))
				if *opens != 1 || len(p.pushes) != 0 {
					t.Fatal("unrelated exit woke a still-blocked gate", scan, *opens, p.pushes)
				}
			}
		})
	}
}

// Serial: FreezeEventClock. Review 787 B2: closed-window scans only read
// the window. When it opens, the full gate still catches a reclose/revoke
// between the lightweight probe and authorization.
func TestTD462WindowProbeAndFinalGatePG(t *testing.T) {
	for _, race := range []string{"open", "reclosed", "revoked"} {
		t.Run(race, func(t *testing.T) {
			c := c5Wait(t, c5Core, false, true)
			grant := c.grant(t, store.ActionCoreLanding)
			p := &livePusher{outcome: landinggit.PushOK}
			ctl := td462Controller(c, p)
			opens := td462CountOpens(ctl, p)
			blocked, windowReads, evidenceReads := true, 0, 0
			result, history, main, ci, grantCI, patch := ctl.Drill.Facts.Result, ctl.Drill.Facts.History, ctl.Drill.Main, ctl.Drill.CI, ctl.Drill.Facts.GrantCI, ctl.Drill.Facts.GrantPatch
			ctl.Drill.Facts.Result = func(ctx context.Context, repo, task, attempt, base, a string) (bool, []rules.LandingChange, error) {
				evidenceReads++
				return result(ctx, repo, task, attempt, base, a)
			}
			ctl.Drill.Facts.History = func(ctx context.Context, repo, task, a, snapshot string) (landing.MainHistory, error) {
				evidenceReads++
				return history(ctx, repo, task, a, snapshot)
			}
			ctl.Drill.Main = func(ctx context.Context, repo string) (string, error) { evidenceReads++; return main(ctx, repo) }
			ctl.Drill.CI = func(ctx context.Context, repo, r string, after time.Time) (landing.CIRun, error) {
				evidenceReads++
				return ci(ctx, repo, r, after)
			}
			ctl.Drill.Facts.GrantCI = func(ctx context.Context, key store.GrantCIKey, after time.Time) (store.GrantCIEvidence, error) {
				evidenceReads++
				return grantCI(ctx, key, after)
			}
			ctl.Drill.Facts.GrantPatch = func(ctx context.Context, a, m0, r string) (store.GrantPatch, error) {
				evidenceReads++
				return patch(ctx, a, m0, r)
			}
			ctl.Drill.WindowBlocked = func(context.Context, string) (bool, error) { windowReads++; return blocked, nil }
			ok(t, ctl.LandingDrillPass(ctx))
			initialReads := evidenceReads
			if *opens != 1 || windowReads != 1 || initialReads == 0 {
				t.Fatal("first window refusal not reached", *opens, windowReads, initialReads)
			}
			for scan := 1; scan <= 45; scan++ {
				store.FreezeEventClock(c.at.Add(time.Duration(scan) * 15 * time.Second))
				ok(t, ctl.LandingDrillPass(ctx))
				if *opens != 1 || evidenceReads != initialReads || windowReads != scan+1 || len(p.pushes) != 0 {
					t.Fatal("closed window repeated full work", scan, *opens, evidenceReads, windowReads)
				}
			}
			blocked = false
			ctl.OpenLive = func(context.Context, string, string) (controller.LiveWorkspace, error) {
				*opens++
				if race == "reclosed" {
					blocked = true
				} else if race == "revoked" {
					_, err := store.RevokeLandingGrant(ctx, c.w.rt, grant.GrantID)
					ok(t, err)
				}
				return p, nil
			}
			store.FreezeEventClock(c.at.Add(46 * 15 * time.Second))
			ok(t, ctl.LandingDrillPass(ctx))
			want := 0
			if race == "open" {
				want = 1
			}
			if *opens != 2 || len(p.pushes) != want || c.reservations(t) != want || td462GrantUses(t, c, grant.GrantID) != want {
				t.Fatal("probe bypassed final gate or failed to wake", race, *opens, p.pushes)
			}
		})
	}
}
