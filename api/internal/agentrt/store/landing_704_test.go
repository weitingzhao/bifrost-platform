package store_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// Count absent streams; lastSeq deliberately requires an existing row.
func landingStreamRows704(t *testing.T, w *world, stream string) int {
	t.Helper()
	var n int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event_stream WHERE event_stream_id=$1`, stream).Scan(&n))
	return n
}
func link704(t *testing.T, w *world, root, dep string) {
	t.Helper()
	w.legacy(t, fmt.Sprintf("INSERT INTO agentrt.task_dependency(task_id,depends_on_task_id) VALUES('%s','%s')", root, dep))
}

// Serial: signing anchor and event clock. All cases enter the actual claim
// transaction; an accepted dependency has not yet taken effect on main.
func TestReview704PendingDependencyDoesNotHideFloorPG(t *testing.T) {
	for _, which := range []string{"path", "size"} {
		for _, bad := range []bool{false, true} {
			for _, effective := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/bad=%t/effective=%t", which, bad, effective), func(t *testing.T) {
					change := infraChange()
					if bad {
						if which == "path" {
							change.OldPath = "agent-config/work/multi-agent/PLAN-x.md"
							change.NewPath = change.OldPath
						} else {
							change.Added = 1501
						}
					}
					w, root, src := infraWorld(t, []rules.LandingChange{change})
					dep := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "pending main", Repo: infraRepo, BaseCommit: commitB})
					w.passRound(t, dep, "h1", "h2", "h3")
					link704(t, w, root, dep)
					history := src.History
					src.History = func(ctx context.Context, repo, task, a, snapshot string) (landing.MainHistory, error) {
						if task == dep && effective {
							return landing.MainHistory{State: landing.Effective, Commit: a}, nil
						}
						return history(ctx, repo, task, a, snapshot)
					}
					before := w.lastSeq(t, infraStream)
					op, d, err := store.ClaimLanding(ctx, w.rt, src, "prod", root, time.Now())
					ok(t, err)
					if !bad && effective {
						if op == "" || !d.Allowed {
							t.Fatal("same-entry successful control", op, d)
						}
						return
					}
					if op != "" {
						t.Fatal("claim unexpectedly succeeded", op, d)
					}
					if !bad {
						if d.Reason != "dependencies" || w.lastSeq(t, infraStream) != before {
							t.Fatal("valid policy should wait", d)
						}
						return
					}
					soft := "size"
					if which == "path" {
						soft = "rule_paths"
					}
					if d.Reason != "floor" || !reflect.DeepEqual(d.Detail["soft"], []string{soft}) || d.Detail["grant_supported"] != false {
						t.Fatal("masked floor", d)
					}
					state := landingState(t, w, infraStream)
					o := state.Operations[state.LatestOperation(root)]
					if o.Phase != "failed" || o.Outcome != "floor" || o.R != "" {
						t.Fatal("not terminal", o)
					}
					var writes int
					ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE body_json->'landing'->>'op'=$1 AND body_json->'landing'->>'kind' IN ('claimed','prepared','branch_intent','awaiting_grant','reserved','main_intent')`, state.LatestOperation(root)).Scan(&writes))
					if writes != 0 {
						t.Fatal("floor produced work", writes)
					}
					quota := landingState(t, w, "landing-quota:dev:"+time.Now().UTC().Format("2006-01-02"))
					if len(quota.Reservations) != 0 {
						t.Fatal("floor reserved quota")
					}
				})
			}
		}
	}
}

// Serial: FreezeEventClock and the signing anchor.
func TestReview704UnsignedDependencyClaimPG(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := liveWorld(t, "none", at)
	dep := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "infra dependency", Repo: infraRepo, BaseCommit: commitB})
	w.passRound(t, dep, "h1", "h2", "h3")
	root := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "platform downstream", BaseCommit: commitB})
	w.passRound(t, root, "h1", "h2", "h3")
	link704(t, w, root, dep)
	src := c4Source("console/src/components/Downstream.tsx")
	reads := 0
	src.History = func(_ context.Context, repo, task, a, snapshot string) (landing.MainHistory, error) {
		if task == dep {
			if repo != infraRepo {
				t.Fatal("dependency read wrong repo", repo)
			}
			reads++
			return landing.MainHistory{State: landing.Effective, Commit: a}, nil
		}
		return landing.MainHistory{State: landing.Never}, nil
	}
	before := w.lastSeq(t, landingRepoStream)
	op, d, err := store.ClaimLanding(ctx, w.rt, src, "prod", root, at)
	ok(t, err)
	if op != "" || d.Reason != "dependencies" || reads != 0 || w.lastSeq(t, landingRepoStream) != before {
		t.Fatal("unsigned dependency read or claimed", op, d, reads)
	}
	_, err = store.ReconcileDenied(ctx, w.rt, infraRepo)
	ok(t, err)
	if landingStreamRows704(t, w, infraStream) != 0 {
		t.Fatal("unsigned infra stream created")
	}
	s := signer{t, rptest.NewKey(t)}
	_, err = s.sign(w, ruleText("landing:dev", 2, "loosen", since, until, infraRuleBody))
	ok(t, err)
	op, d, err = store.ClaimLanding(ctx, w.rt, src, "prod", root, at)
	ok(t, err)
	if op == "" || !d.Allowed || reads != 1 {
		t.Fatal("signed dependency control", op, d, reads)
	}
	if landingStreamRows704(t, w, infraStream) != 0 {
		t.Fatal("dependency evidence created a landing stream")
	}
}
