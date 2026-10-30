package store_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

const platformRepo = "bifrost-platform"

// The global clock/signing anchor makes callers serial. No fixture bypasses
// schema triggers: the two repositories use the real signed rule and tasks.
func s66World(t *testing.T, liveInfra bool, quota int, clock ...time.Time) (*world, time.Time) {
	t.Helper()
	w := newKernelWorld(t)
	at := time.Now().UTC().Truncate(time.Second)
	var databaseNow time.Time
	ok(t, w.rt.QueryRow(ctx, `SELECT now()`).Scan(&databaseNow))
	enabled := databaseNow.Add(-time.Hour)
	if len(clock) > 0 {
		at = clock[0]
		if earlier := at.Add(-time.Hour); earlier.Before(enabled) {
			enabled = earlier
		}
	}
	// Mission.created_at comes from PostgreSQL now(), not FreezeEventClock.
	// Put the real mode/enabled records before both clocks; a future midnight
	// acceptance must not put the fence after the mission's creation.
	t.Cleanup(store.FreezeEventClock(enabled))
	s := signer{t, rptest.NewKey(t)}
	body := strings.Replace(infraRuleBody, "max_landings_per_day: 5", fmt.Sprintf("max_landings_per_day: %d", quota), 1)
	_, err := s.sign(w, ruleText("landing:dev", 1, "initial", since, until, body))
	ok(t, err)
	for _, repo := range landing.LandingRepoNames() {
		if repo != infraRepo || liveInfra {
			_, err = store.EnsureLandingMode(ctx, w.rt, repo)
			ok(t, err)
		}
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, err := store.ObserveLandingEnabled(ctx, tx, "stg", "dev", repo, enabled)
			return err
		}))
	}
	store.FreezeEventClock(at)
	w.goalMission(t, "M-3")
	return w, at
}

func s66Root(t *testing.T, w *world, mission, repo string) string {
	t.Helper()
	r := w.ready(t, store.Task{MissionID: mission, Repo: repo, BaseCommit: commitB, OffPlanReason: "S6-6 multi-repository fixture"})
	w.passRound(t, r, "h1", "h2", "h3")
	return r
}

func s66Source() store.LandingSource {
	return store.LandingSource{Result: func(_ context.Context, repo, _, _, _, _ string) (bool, []rules.LandingChange, error) {
		c := infraChange()
		if repo == platformRepo {
			c.OldPath = "console/src/components/Pair.tsx"
			c.NewPath = c.OldPath
		}
		return true, []rules.LandingChange{c}, nil
	}, History: func(context.Context, string, string, string, string) (landing.MainHistory, error) {
		return landing.MainHistory{State: landing.Never}, nil
	}}
}

// Serial: event clock and signing anchor. T51: each same-entry control can
// claim; one platform flight never occupies infra's independent flight slot.
func TestS66T51PerRepoInFlightPG(t *testing.T) {
	w, at := s66World(t, true, 5)
	src := s66Source()
	for _, repo := range landing.LandingRepoNames() {
		root := s66Root(t, w, "M-3", repo)
		op, d, err := store.ClaimLanding(ctx, w.rt, src, "prod", root, at)
		ok(t, err)
		if op == "" || !d.Allowed {
			t.Fatal("independent slot control", repo, op, d)
		}
	}
	other := s66Root(t, w, "M-3", infraRepo)
	op, d, err := store.ClaimLanding(ctx, w.rt, src, "prod", other, at)
	ok(t, err)
	if op != "" || d.Reason != "limit" {
		t.Fatal("same repo second flight", op, d)
	}
	if landingState(t, w, infraStream).LatestOperation(other) != "" {
		t.Fatal("limit wrote an operation")
	}
}

// s66Landed is a protocol fixture, not a claim/gate test. T53/54 exercise
// CloseMissions with accepted roots and honest per-repo main evidence.
func s66Landed(t *testing.T, w *world, mission, repo string, at time.Time) string {
	t.Helper()
	root := s66Root(t, w, mission, repo)
	op := "land:" + root + ":1"
	ref := "refs/heads/land/prod/" + root + "/1"
	base := store.DecisionAt()
	for i, r := range []map[string]any{
		newLandingRecord(op, "claimed", "task", root, "attempt", landingAttempt, "A", commitA, "rule_version", 1),
		newLandingRecord(op, "prepared", "R", commitB, "M0", commitA, "committer_date", base.Add(time.Second).Format(time.RFC3339Nano), "dry_run", false),
		newLandingRecord(op, "branch_intent", "ref", ref, "R", commitB),
		newLandingRecord(op, "branch_pushed", "ref", ref, "R", commitB),
		newLandingRecord(op, "ci_seen", "run", "s66-green", "status", "Succeeded", "R", commitB, "created_at", base.Add(3*time.Second).Format(time.RFC3339Nano)),
		newLandingRecord(op, "main_intent", "R", commitB, "M0", commitA),
		newLandingRecord(op, "main_result", "outcome", "ok"),
	} {
		store.FreezeEventClock(base.Add(time.Duration(i) * time.Second))
		ok(t, appendLanding(w, "landing:"+repo, r))
	}
	store.FreezeEventClock(base.Add(10 * time.Second))
	_, err := store.ReconcileMain(ctx, w.rt, repo, evidence(store.MainEvidence{Head: commitB, HasR: true, Validity: "effective"}))
	ok(t, err)
	return root
}

func s66ReadBoth(t *testing.T, w *world, at time.Time) {
	t.Helper()
	store.FreezeEventClock(at)
	for _, repo := range landing.LandingRepoNames() {
		_, err := store.ReconcileMain(ctx, w.rt, repo, evidence(store.MainEvidence{Head: commitB, HasR: true, Validity: "effective"}))
		ok(t, err)
	}
}

// Serial: global clock. T53's positive and negative use the same close
// entry; only the infra root is left unclaimed in the negative.
func TestS66T53CloseNeedsEveryRepositoryPG(t *testing.T) {
	for _, unresolved := range []bool{false, true} {
		t.Run(fmt.Sprint(unresolved), func(t *testing.T) {
			w, at := s66World(t, true, 5)
			s66Landed(t, w, "M-3", platformRepo, at)
			if unresolved {
				s66Root(t, w, "M-3", infraRepo)
			} else {
				s66Landed(t, w, "M-3", infraRepo, at)
			}
			if st := closePass(t, w, at.Add(2*time.Minute)); st != "active" {
				t.Fatal("closed before round", st)
			}
			s66ReadBoth(t, w, at.Add(3*time.Minute))
			want := "done"
			if unresolved {
				want = "active"
			}
			if st := closePass(t, w, at.Add(4*time.Minute)); st != want {
				t.Fatal("cross repo close", st, want)
			}
			view, err := store.ReadMissionLandingStatus(ctx, w.rt, "M-3")
			ok(t, err)
			if len(view.Roots) != 2 || view.Roots[0].Repo != platformRepo || view.Roots[1].Repo != infraRepo {
				t.Fatal("repository projection", view)
			}
			if unresolved {
				if view.Closed != nil {
					t.Fatal("unresolved root closed")
				}
				return
			}
			if view.Closed == nil || view.Closed.Delivered != 2 {
				t.Fatal("close evidence", view.Closed)
			}
			var repos []string
			for _, r := range view.Closed.Roots {
				repos = append(repos, fmt.Sprint(r["repo"]))
			}
			if !slices.Equal(repos, landing.LandingRepoNames()) {
				t.Fatal("close omitted repo", repos)
			}
		})
	}
}

// Serial: event clock. T54: changing only infra's live state selects its
// legacy hold. An unclaimed root inside the first-claim window still holds a
// mixed mission, even when the platform root is ready to close.
func TestS66T54PartialLiveKeepsLegacyHoldPG(t *testing.T) {
	for _, live := range []bool{true, false} {
		t.Run(fmt.Sprint(live), func(t *testing.T) {
			w, at := s66World(t, live, 5)
			s66Landed(t, w, "M-3", platformRepo, at)
			s66Root(t, w, "M-3", infraRepo)
			closePass(t, w, at.Add(2*time.Minute))
			s66ReadBoth(t, w, at.Add(3*time.Minute))
			want := "active"
			if st := closePass(t, w, at.Add(4*time.Minute)); st != want {
				t.Fatal("legacy hold lost", st, want)
			}
		})
	}
}

type s66GateResult struct {
	auth     store.Authorization
	decision landing.Decision
	err      error
}

// Serial: event clock and signing anchor. Card identity, persisted floor
// detail and the mission read all stay on infra; lookup by question must not
// silently search platform alone.
func TestS66InfraCardAndMissionProjectionPG(t *testing.T) {
	bad := infraChange()
	bad.Added = 1501
	w, root, src := infraWorld(t, []rules.LandingChange{bad})
	at := time.Now().UTC().Truncate(time.Second)
	t.Cleanup(store.FreezeEventClock(at))
	_, d, err := store.ClaimLanding(ctx, w.rt, src, "prod", root, at)
	ok(t, err)
	if d.Reason != "floor" {
		t.Fatal(d)
	}
	prep, err := store.PrepareLandingCards(ctx, w.rt, infraRepo, at)
	ok(t, err)
	if len(prep) != 1 {
		t.Fatal("no infra card", prep)
	}
	fake := newFakeCards(func() time.Time { return at })
	id, err := store.AskLandingCard(ctx, w.rt, fake.source(), infraRepo, prep[0], at, true)
	ok(t, err)
	v, found, err := store.FindLandingCard(ctx, w.rt, id)
	ok(t, err)
	if !found || v.Repo != infraRepo || v.Root != root {
		t.Fatal("card repository lost", v, found)
	}
	_, found, err = store.ReadLandingCard(ctx, w.rt, platformRepo, id)
	ok(t, err)
	if found {
		t.Fatal("infra card leaked into platform")
	}
	st, err := store.ReadMissionLandingStatus(ctx, w.rt, "M-3")
	ok(t, err)
	if len(st.Roots) != 1 || st.Roots[0].Repo != infraRepo || st.Roots[0].QuestionID != id || st.Roots[0].Waiting != nil || st.Roots[0].Floor["grant_supported"] != false {
		t.Fatal("infra status lost floor or card", st)
	}
}

// Serial: signing, clock, hooks and the local Git fixture. T50: both
// repositories individually pass the gate and push their own bare remote;
// concurrently, the shared last slot admits only the first intent. These
// are real transactions on two connections, with PostgreSQL proving the
// second waits on the quota lock. No production remote is used.
func TestS66T50SharedQuotaDualRemotePG(t *testing.T) {
	for _, repos := range [][]string{{platformRepo}, {infraRepo}, landing.LandingRepoNames()} {
		t.Run(strings.Join(repos, "+"), func(t *testing.T) {
			w, at := s66World(t, true, 1)
			git := localGit704(t)
			gits := git.adapters()
			src := controller.MultiRepoGitFacts(gits)
			ops := map[string]string{}
			for _, repo := range repos {
				root := w.ready(t, store.Task{MissionID: "M-3", Repo: repo, BaseCommit: git.bases[repo], OffPlanReason: "T50 real Git"})
				passReal704(t, w, root, git.result(repo, root, false))
				op, d, err := store.ClaimLanding(ctx, w.rt, src, "prod", root, at)
				ok(t, err)
				if !d.Allowed {
					t.Fatal("claim control", repo, d)
				}
				ops[repo] = op
				ctl := controller.Controller{DB: w.rt, Env: "prod", Live: true, LiveRepos: repos, DrillGit: gits, Drill: &controller.DrillSources{Facts: src}}
				ctl.Drill.InfraPlatformHead = func(ctx context.Context) (string, error) { return gits[platformRepo].Ref(ctx, "refs/heads/main") }
				ok(t, ctl.LandingDrillBranch(ctx, gits[repo], op, at, repo))
			}
			store.FreezeEventClock(at.Add(2 * time.Minute))
			gate := func(db store.DB, repo string) s66GateResult {
				a, d, err := store.AuthorizeLandingPush(ctx, db, src, ops[repo], func(_ context.Context, o landing.Operation) (store.LiveObservation, error) {
					return store.LiveObservation{Main: o.M0, CI: landing.CIRun{Run: "s66-green", R: o.R, Status: "Succeeded", CreatedAt: o.PreparedAt.Add(time.Second)}}, nil
				}, repo)
				return s66GateResult{a, d, err}
			}
			results := map[string]s66GateResult{}
			if len(repos) == 1 {
				results[repos[0]] = gate(w.rt, repos[0])
			} else {
				held, release := make(chan struct{}), make(chan struct{})
				var once sync.Once
				unblock := func() { once.Do(func() { close(release) }) }
				defer unblock()
				defer store.SetHook(func(point, op string) {
					if point == "landing gate: before main_intent" && op == ops[platformRepo] {
						close(held)
						<-release
					}
				})()
				a, b := make(chan s66GateResult, 1), make(chan s66GateResult, 1)
				dbA, dbB := w.db.Connect(t, w.db.Runtime), w.db.Connect(t, w.db.Runtime)
				safego.Go("s66.quota.first", func() { a <- gate(dbA, platformRepo) })
				select {
				case <-held:
				case r := <-a:
					t.Fatal("first gate never held", r)
				case <-time.After(10 * time.Second):
					t.Fatal("first gate timeout")
				}
				safego.Go("s66.quota.second", func() { b <- gate(dbB, infraRepo) })
				// The first transaction has not committed; a repository-local
				// quota would allow the second to finish and this assertion fails.
				w.waitFor(t, "agentrt.event_stream", time.Now().Add(10*time.Second))
				unblock()
				results[platformRepo], results[infraRepo] = <-a, <-b
			}
			pushes := map[string]int{}
			for _, repo := range repos {
				r := results[repo]
				ok(t, r.err)
				if !r.decision.Allowed {
					if len(repos) != 2 || repo != infraRepo || r.decision.Reason != "limit" || r.auth.Op != "" {
						t.Fatal("unexpected refusal", repo, r)
					}
					continue
				}
				g, err := landinggit.LanderFromEnv("prod", repo)
				ok(t, err)
				ws, err := g.Open(ctx, r.auth.R)
				ok(t, err)
				out := ws.PushMain(ctx, r.auth.R, r.auth.M0, r.auth.Deadline)
				ws.Close()
				pushes[repo]++
				if out != landinggit.PushOK {
					t.Fatal("push failed", repo, out)
				}
				_, err = store.RecordPushResult(ctx, w.rt, r.auth, "ok")
				ok(t, err)
			}
			q := landingState(t, w, "landing-quota:dev:"+at.Add(2*time.Minute).Format("2006-01-02"))
			if len(q.Reservations) != 1 {
				t.Fatal("quota is not shared", q.Reservations)
			}
			winner := repos[0]
			for _, repo := range landing.LandingRepoNames() {
				want := git.bases[repo]
				if repo == winner {
					want = results[repo].auth.R
				}
				if head := git.run(git.remotes[repo], "rev-parse", "main"); head != want {
					t.Fatal("wrong remote changed", repo, head, want)
				}
				wantPush := 0
				if repo == winner {
					wantPush = 1
				}
				if pushes[repo] != wantPush {
					t.Fatal("push count", repo, pushes)
				}
			}
			if len(repos) == 2 && landingState(t, w, infraStream).Operations[ops[infraRepo]].Phase != "branch_pushed" {
				t.Fatal("loser wrote intent")
			}
		})
	}
}
