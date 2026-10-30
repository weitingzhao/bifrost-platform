package store_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
)

type s65Workspace struct {
	controller.LiveWorkspace
	pushes *int
}

func (w s65Workspace) PushMain(ctx context.Context, r, m0 string, until time.Time) landinggit.PushOutcome {
	*w.pushes++
	return w.LiveWorkspace.PushMain(ctx, r, m0, until)
}

// Serial: signing anchor, event clock and local Git PATH. Each refusal shares
// the successful control's claim/preparation/controller/gate entry points.
// PostgreSQL now(), not the wall clock, determines every assertion's UTC day.
func TestS65InfraPreparedCIAndFinalGatePG(t *testing.T) {
	for _, mode := range []string{"green", "quota rollover", "no status", "failed", "wrong result", "push", "inline", "red at final gate"} {
		t.Run(mode, func(t *testing.T) {
			limit := 5
			if mode == "quota rollover" {
				limit = 1
			}
			w, _ := s66World(t, true, limit)
			var databaseNow time.Time
			ok(t, w.rt.QueryRow(ctx, `SELECT now()`).Scan(&databaseNow))
			at := databaseNow.UTC().Truncate(time.Second).Add(time.Minute)
			if mode == "quota rollover" {
				midnight := at.Truncate(24 * time.Hour).Add(24 * time.Hour)
				if midnight.Add(-20 * time.Minute).Before(at) {
					midnight = midnight.Add(24 * time.Hour)
				}
				at = midnight.Add(-20 * time.Minute)
			}
			store.FreezeEventClock(at)
			git := localGit704(t)
			gits := git.adapters()
			src := controller.MultiRepoGitFacts(gits)
			root := w.ready(t, store.Task{MissionID: "M-3", Repo: infraRepo, BaseCommit: git.bases[infraRepo], OffPlanReason: "S6-5 CI integration"})
			passReal704(t, w, root, git.result(infraRepo, root, false))
			op, d, err := store.ClaimLanding(ctx, w.rt, src, "prod", root, at)
			ok(t, err)
			if !d.Allowed {
				t.Fatal("claim control", d)
			}
			gvr := schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "pipelineruns"}
			fake := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{gvr: "PipelineRunList"})
			client := &landingci.Client{Dynamic: func() (dynamic.Interface, error) { return fake, nil }}
			platformHead := git.bases[platformRepo]
			headReads, ciReads, pushes := 0, 0, 0
			keyOf := func(o landing.Operation) landingci.Key {
				return landingci.Key{Repo: infraRepo, Revision: o.R, Base: o.M0, Siblings: map[string]string{platformRepo: o.PlatformRevision}, Generation: o.Generation}
			}
			sources := controller.DrillSources{Facts: src,
				Main: func(ctx context.Context, repo string) (string, error) { return gits[repo].Ref(ctx, "refs/heads/main") },
				CI: func(context.Context, string, string, time.Time) (landing.CIRun, error) {
					t.Fatal("infra used legacy CI")
					return landing.CIRun{}, nil
				},
				EnsureCI:          func(context.Context, string, string, string) error { t.Fatal("infra used legacy creator"); return nil },
				WindowBlocked:     func(context.Context, string) (bool, error) { return false, nil },
				InfraPlatformHead: func(context.Context) (string, error) { headReads++; return platformHead, nil },
			}
			sources.EnsureInfraCI = func(ctx context.Context, o landing.Operation) error {
				return client.Ensure(ctx, landing.InfraCIName(o.R, o.M0, o.PlatformRevision, o.Generation), keyOf(o))
			}
			var later *unstructured.Unstructured
			sources.InfraCI = func(ctx context.Context, o landing.Operation) (landing.CIRun, error) {
				ciReads++
				if mode == "red at final gate" && ciReads == 2 {
					ok(t, unstructured.SetNestedSlice(later.Object, []any{map[string]any{"type": "Succeeded", "status": "False"}}, "status", "conditions"))
					_, err := fake.Resource(gvr).Namespace("cicd").Update(ctx, later, metav1.UpdateOptions{})
					ok(t, err)
				}
				got, err := client.Read(ctx, keyOf(o), o.PreparedAt)
				return got.CIRun, err
			}
			ctl := controller.Controller{DB: w.rt, Env: "prod", Live: true, LiveRepos: []string{infraRepo}, DrillGit: gits, Drill: &sources,
				OpenLive: func(ctx context.Context, repo, r string) (controller.LiveWorkspace, error) {
					ws, err := gits[repo].Open(ctx, r)
					if err != nil {
						return nil, err
					}
					return s65Workspace{ws, &pushes}, nil
				},
			}
			ok(t, ctl.LandingDrillBranch(ctx, gits[infraRepo], op, at, infraRepo))
			prepared := landingState(t, w, infraStream).Operations[op]
			if prepared.PlatformRevision != platformHead || prepared.Generation != 1 || headReads != 1 {
				t.Fatal("preparation not pinned", prepared, headReads)
			}
			platformHead = commitA // a restart cannot re-read and replace the sibling
			restarted := controller.Controller{DB: w.rt, Env: "prod", Live: true, LiveRepos: []string{infraRepo}, DrillGit: gits, Drill: &sources, OpenLive: ctl.OpenLive}
			ok(t, restarted.LandingDrillBranch(ctx, gits[infraRepo], op, at, infraRepo))
			if headReads != 1 {
				t.Fatal("restart repinned sibling")
			}
			kickAt := at.Add(11 * time.Minute)
			if mode == "quota rollover" {
				kickAt = s65QuotaRecovery751(t, w, &restarted, sources, op, prepared, at, fake, gvr)
			}
			store.FreezeEventClock(kickAt)
			action, err := restarted.LandingDrillCI(ctx, sources, op, at, infraRepo)
			ok(t, err)
			if action.Kind != "ci_kick" {
				t.Fatal("missing CI did not kick", action)
			}
			if mode == "quota rollover" {
				for range 3 {
					resume := controller.Controller{DB: w.rt, Env: "prod", Live: true, LiveRepos: []string{infraRepo}}
					action, err := resume.LandingDrillCI(ctx, sources, op, at, infraRepo)
					ok(t, err)
					if action.Kind != "wait" {
						t.Fatal("repeated recovery kicked again", action)
					}
				}
				runs, err := fake.Resource(gvr).Namespace("cicd").List(ctx, metav1.ListOptions{})
				ok(t, err)
				var kicks int
				ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id=$1
                  AND body_json->'landing'->>'op'=$2 AND body_json->'landing'->>'kind'='ci_kick'`, infraStream, op).Scan(&kicks))
				if len(runs.Items) != 1 || kicks != 1 {
					t.Fatal("recovery duplicated CI or intent", len(runs.Items), kicks)
				}
				s65SamePreparation751(t, prepared, landingState(t, w, infraStream).Operations[op])
			}
			name := landing.InfraCIName(prepared.R, prepared.M0, prepared.PlatformRevision, 1)
			run, err := fake.Resource(gvr).Namespace("cicd").Get(ctx, name, metav1.GetOptions{})
			ok(t, err)
			run.SetUID("s65-ci-uid")
			run.SetCreationTimestamp(metav1.NewTime(kickAt.Add(time.Second)))
			results := map[string]string{"infra-commit": prepared.R, "base-commit": prepared.M0, "platform-commit": prepared.PlatformRevision, "tools-from": prepared.M0, "doc-policy": "pass", "purpose": "landing"}
			values := []any{}
			for k, v := range results {
				values = append(values, map[string]any{"name": k, "value": v})
			}
			ok(t, unstructured.SetNestedSlice(run.Object, values, "status", "results"))
			ok(t, unstructured.SetNestedSlice(run.Object, []any{map[string]any{"type": "Succeeded", "status": "True"}}, "status", "conditions"))
			_, err = fake.Resource(gvr).Namespace("cicd").Update(ctx, run, metav1.UpdateOptions{})
			ok(t, err)
			later = run.DeepCopy()
			later.SetName("later")
			later.SetUID("s65-later-uid")
			later.SetCreationTimestamp(metav1.NewTime(kickAt.Add(10 * time.Second)))
			// The push/inline cases have no matching landing run at all.
			if mode == "push" || mode == "inline" {
				ok(t, fake.Tracker().Delete(gvr, "cicd", name))
			}
			switch mode {
			case "no status":
				delete(later.Object, "status")
			case "failed":
				ok(t, unstructured.SetNestedSlice(later.Object, []any{map[string]any{"type": "Succeeded", "status": "False"}}, "status", "conditions"))
			case "wrong result":
				for _, raw := range values {
					m := raw.(map[string]any)
					if m["name"] == "infra-commit" {
						m["value"] = commitA
					}
				}
				ok(t, unstructured.SetNestedSlice(later.Object, values, "status", "results"))
			case "push":
				params, _, _ := unstructured.NestedSlice(later.Object, "spec", "params")
				for _, raw := range params {
					p := raw.(map[string]any)
					if p["name"] == "purpose" {
						p["value"] = "push"
					}
				}
				ok(t, unstructured.SetNestedSlice(later.Object, params, "spec", "params"))
			case "inline":
				ok(t, unstructured.SetNestedMap(later.Object, map[string]any{}, "spec", "pipelineSpec"))
			}
			if mode != "quota rollover" {
				_, err = fake.Resource(gvr).Namespace("cicd").Create(ctx, later, metav1.CreateOptions{})
				ok(t, err)
			}
			ciReads = 0
			final := kickAt.Add(time.Minute)
			store.FreezeEventClock(final)
			action, err = restarted.LandingDrillCI(ctx, sources, op, at, infraRepo)
			ok(t, err)
			state := landingState(t, w, infraStream).Operations[op]
			quota := landingState(t, w, "landing-quota:dev:"+final.Format("2006-01-02"))
			if mode == "green" || mode == "quota rollover" {
				if action.Kind != "ready" || state.Phase != "main_result" || pushes != 1 || quota.Reservations[root] != op || ciReads != 2 {
					t.Fatal("green control", action, state.Phase, pushes, quota, ciReads)
				}
				wantUID := "s65-later-uid"
				if mode == "quota rollover" {
					wantUID = "s65-ci-uid"
				}
				if state.CI.String("uid") != wantUID || !reflect.DeepEqual(state.CI["results"], stringsToAny65(results)) {
					t.Fatal("CI evidence lost", state.CI)
				}
				if git.run(git.remotes[infraRepo], "rev-parse", "main") != prepared.R {
					t.Fatal("main did not land")
				}
			} else {
				if pushes != 0 || len(quota.Reservations) != 0 || state.Phase == "main_intent" || state.Phase == "main_result" {
					t.Fatal("bad CI authorized push", mode, action, state.Phase, pushes, quota)
				}
				if mode == "failed" || mode == "wrong result" {
					if action.Kind != "failed" || action.Why != "ci_red" {
						t.Fatal("wrong refusal", action)
					}
				} else if mode == "red at final gate" {
					if action.Kind != "ready" || ciReads != 2 {
						t.Fatal("did not reach final gate", action, ciReads)
					}
				} else if action.Kind != "wait" {
					t.Fatal("unknown evidence advanced", action)
				}
				if git.run(git.remotes[infraRepo], "rev-parse", "main") != prepared.M0 {
					t.Fatal("refusal changed main")
				}
			}
			if git.run(git.remotes[platformRepo], "rev-parse", "main") != git.bases[platformRepo] {
				t.Fatal("infra changed platform")
			}
		})
	}
}
func stringsToAny65(values map[string]string) map[string]any {
	out := map[string]any{}
	for k, v := range values {
		out[k] = v
	}
	return out
}
