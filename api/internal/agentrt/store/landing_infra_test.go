package store_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

const infraRepo = "bifrost-trade-infra"
const infraStream = "landing:" + infraRepo
const infraPath = "agent-config/work/multi-agent/REPORT-a.md"
const infraRuleBody = `tenant: dev
repos:
  - repo: bifrost-platform
    allow: ["console/src/components/**/*.tsx"]
    deny: []
    stg_release: none
  - repo: bifrost-trade-infra
    allow: ["agent-config/work/multi-agent/CUT-*.md", "agent-config/work/multi-agent/DESIGN-*.md", "agent-config/work/multi-agent/REPORT-*.md", "agent-config/work/multi-agent/REVIEW-*.md", "agent-config/work/multi-agent/LESSONS-*.md", "agent-config/work/multi-agent/reviews/*.md"]
    deny: []
    stg_release: none
    limits: {max_files: 5, max_changed_lines: 1500, max_in_flight: 1}
limits: {max_files: 10, max_changed_lines: 300, max_landings_per_day: 5, max_in_flight_per_repo: 1}
`

func infraWorld(t *testing.T, changes []rules.LandingChange) (*world, string, store.LandingSource) {
	t.Helper()
	w := newKernelWorld(t)
	s := signer{t, rptest.NewKey(t)}
	_, err := s.sign(w, ruleText("landing:dev", 1, "initial", since, until, infraRuleBody))
	ok(t, err)
	enabled := time.Now().Add(-time.Hour)
	restore := store.FreezeEventClock(enabled)
	_, err = store.EnsureLandingMode(ctx, w.rt, infraRepo)
	ok(t, err)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.ObserveLandingEnabled(ctx, tx, "prod", "dev", infraRepo, enabled)
		return err
	}))
	restore()
	w.goalMission(t, "M-3")
	root := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "infra landing fixture", Repo: infraRepo, BaseCommit: commitB})
	w.passRound(t, root, "h1", "h2", "h3")
	src := store.LandingSource{Result: func(_ context.Context, repo, task, attempt, base, a string) (bool, []rules.LandingChange, error) {
		if repo != infraRepo || task != root || base != commitB || a != commitA {
			t.Fatalf("wrong repository evidence %s %s %s %s", repo, task, base, a)
		}
		return true, changes, nil
	}, History: func(_ context.Context, repo, task, a, snapshot string) (landing.MainHistory, error) {
		if repo != infraRepo {
			t.Fatal("wrong history repo", repo)
		}
		return landing.MainHistory{State: landing.Never}, nil
	}}
	return w, root, src
}

func infraChange() rules.LandingChange {
	return rules.LandingChange{OldPath: infraPath, NewPath: infraPath, OldMode: "100644", NewMode: "100644", Added: 20}
}

// Serial: FreezeEventClock and the signing test anchor. T1–T12 through the
// real claim/append transaction. Each negative has a successful same-entry
// control; no branch, intent or quota can be produced by a refusal.
func TestS6InfraClaimLayersPG(t *testing.T) {
	for _, tc := range []struct {
		name, layer string
		changes     func() []rules.LandingChange
	}{
		{"T1", "hard", func() []rules.LandingChange {
			c := infraChange()
			c.OldPath = "k8s/overlays/prod/kustomization.yaml"
			c.NewPath = c.OldPath
			return []rules.LandingChange{c}
		}},
		{"T2", "hard", func() []rules.LandingChange {
			return []rules.LandingChange{{OldPath: "k8s/base-platform/manifest.yaml", OldMode: "100644", Deleted: 20}}
		}},
		{"T3", "hard", func() []rules.LandingChange {
			return []rules.LandingChange{{OldPath: infraPath, OldMode: "100644", Deleted: 20}, {NewPath: "agent-config/scripts/x.md", NewMode: "100644", Added: 20}}
		}},
		{"T4-skill", "hard", func() []rules.LandingChange {
			return []rules.LandingChange{{NewPath: "agent-config/work/multi-agent/reviews/SKILL.md", NewMode: "100644", Added: 20}}
		}},
		{"T4-runbook", "hard", func() []rules.LandingChange {
			return []rules.LandingChange{{NewPath: "agent-config/work/multi-agent/DESIGN-RUNBOOK-x.md", NewMode: "100644", Added: 20}}
		}},
		{"T5", "structural", func() []rules.LandingChange {
			c := infraChange()
			c.NewMode = "100755"
			return []rules.LandingChange{c}
		}},
		{"T6", "structural", func() []rules.LandingChange {
			c := infraChange()
			c.NewMode = "120000"
			return []rules.LandingChange{c}
		}},
		{"T7", "structural", func() []rules.LandingChange { c := infraChange(); c.Binary = true; return []rules.LandingChange{c} }},
		{"T8", "change_kind", func() []rules.LandingChange {
			return []rules.LandingChange{{OldPath: infraPath, OldMode: "100644", Deleted: 20}}
		}},
		{"T9", "change_kind", func() []rules.LandingChange {
			return []rules.LandingChange{{OldPath: infraPath, OldMode: "100644", Deleted: 20}, {NewPath: "agent-config/work/multi-agent/REPORT-b.md", NewMode: "100644", Added: 20}}
		}},
		{"T10", "soft", func() []rules.LandingChange {
			return []rules.LandingChange{{NewPath: "agent-config/work/multi-agent/PLAN-x.md", NewMode: "100644", Added: 20}}
		}},
		{"T11", "soft", func() []rules.LandingChange {
			var out []rules.LandingChange
			for i := 0; i < 6; i++ {
				out = append(out, rules.LandingChange{NewPath: fmt.Sprintf("agent-config/work/multi-agent/REPORT-%d.md", i), NewMode: "100644", Added: 1})
			}
			return out
		}},
		{"T12", "soft", func() []rules.LandingChange { c := infraChange(); c.Added = 1501; return []rules.LandingChange{c} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, negative := range []bool{false, true} {
				changes := infraControl(tc.name)
				if negative {
					changes = tc.changes()
				}
				w, task, src := infraWorld(t, changes)
				op, d, err := store.ClaimLanding(ctx, w.rt, src, "prod", task, time.Now())
				ok(t, err)
				if !negative {
					if op == "" || !d.Allowed {
						t.Fatal("control", op, d)
					}
					continue
				}
				if op != "" || d.Reason != "floor" || d.Detail[tc.layer] == nil {
					t.Fatalf("wrong refusal %+v %s", d, op)
				}
				state := landingState(t, w, infraStream)
				o := state.Operations[state.LatestOperation(task)]
				if o.Phase != "failed" || o.Outcome != "floor" || o.R != "" || len(state.Reservations) != 0 {
					t.Fatal("not born terminal", o)
				}
				var count int
				ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE task_id=$1 AND body_json->'landing'->>'kind' IN ('awaiting_grant','prepared','reserved','main_intent')`, task).Scan(&count))
				if count != 0 {
					t.Fatal("refusal prepared or reserved", count)
				}
				var detail map[string]any
				ok(t, w.rt.QueryRow(ctx, `SELECT body_json->'landing_floor' FROM agentrt.event WHERE event_stream_id=$1 AND task_id=$2 AND body_json ? 'landing_floor'`, infraStream, task).Scan(&detail))
				if detail[tc.layer] == nil {
					t.Fatal("missing audit layer", detail)
				}
			}
		})
	}
}

// Serial: FreezeEventClock. T63: closing the repository disables writes but
// its committed main intent still reaches landed on the read-only loop.
func TestS6InfraClosedRepositoryReconcilesPG(t *testing.T) {
	w, task, src := infraWorld(t, []rules.LandingChange{infraChange()})
	op, d, err := store.ClaimLanding(ctx, w.rt, src, "prod", task, time.Now())
	ok(t, err)
	if !d.Allowed {
		t.Fatal(d)
	}
	at := time.Now().UTC().Truncate(time.Second)
	restore := store.FreezeEventClock(at)
	defer restore()
	ref := "refs/heads/land/prod/" + task + "/1"
	for _, r := range []map[string]any{newLandingRecord(op, "prepared", "R", commitB, "M0", commitA, "committer_date", at.Format(time.RFC3339Nano), "dry_run", false), newLandingRecord(op, "branch_intent", "ref", ref, "R", commitB), newLandingRecord(op, "branch_pushed", "ref", ref, "R", commitB)} {
		ok(t, appendLanding(w, infraStream, r))
	}
	store.FreezeEventClock(at.Add(2 * time.Minute))
	auth, d, err := store.AuthorizeLandingPush(ctx, w.rt, src, op, observed(at), infraRepo)
	ok(t, err)
	if !d.Allowed || auth.Repo != infraRepo {
		t.Fatal(auth, d)
	}
	// Revocation stops new work but cannot orphan a committed main intent.
	_, err = store.TightenRule(ctx, w.rt, wr("thread:s1"), "landing:dev", []byte(ruleText("landing:dev", 2, "tighten", since, until, strings.Replace(landingRuleBody, "stg_release: bifrost-deliver-platform", "stg_release: none", 1))))
	ok(t, err)
	repos, err := store.ExistingLandingRepos(ctx, w.rt)
	ok(t, err)
	reads := 0
	ctl := controller.Controller{DB: w.rt, Env: "prod", Live: false, LiveRepos: []string{}, Reconcile: &controller.LandingReconcile{Repos: repos, MainEvidence: func(_ context.Context, repo, root, a, r string) (store.MainEvidence, error) {
		reads++
		if repo != infraRepo || root != task {
			t.Fatal(repo, root)
		}
		return store.MainEvidence{Head: r, HasR: true, Validity: "effective"}, nil
	}}}
	ok(t, ctl.LandingDrillPass(ctx))
	ok(t, ctl.LandingReconcilePass(ctx))
	if reads != 1 || landingState(t, w, infraStream).Operations[op].Phase != "landed" {
		t.Fatal("closed infra not reconciled", reads)
	}
	if len(landingState(t, w, landingRepoStream).Operations) != 0 {
		t.Fatal("platform stream changed")
	}
}

// Serial: signing key and event clock. T62/T72: v1 does not enable infra;
// signing the new rule enables its independent limits without widening v1.
func TestS6SignedRuleRequiredBeforeInfraModePG(t *testing.T) {
	w := newKernelWorld(t)
	s := signer{t, rptest.NewKey(t)}
	_, err := s.sign(w, ruleText("landing:dev", 1, "initial", since, until, landingRuleBody))
	ok(t, err)
	mode, err := store.EnsureLandingMode(ctx, w.rt, infraRepo)
	ok(t, err)
	if !mode.IsZero() || landingStreamRows704(t, w, infraStream) != 0 {
		t.Fatal("v1 activated infra")
	}
	git := &startupDrillGit{}
	ctl := controller.Controller{DB: w.rt, Env: "prod", Live: true, LiveRepos: []string{infraRepo}, DrillGit: map[string]controller.DrillGit{infraRepo: git}, Drill: &controller.DrillSources{}}
	ok(t, ctl.LandingDrillPass(ctx))
	if git.opens.Load() != 0 || landingStreamRows704(t, w, infraStream) != 0 {
		t.Fatal("v1 started infra")
	}
	w.goalMission(t, "M-3")
	task := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "v1 cannot claim infra", Repo: infraRepo, BaseCommit: commitB})
	w.passRound(t, task, "h1", "h2", "h3")
	op, d, err := store.ClaimLanding(ctx, w.rt, store.LandingSource{}, "prod", task, time.Now())
	ok(t, err)
	// The rule is checked before a claim can create the infra stream.
	if op != "" || d.Reason != "rule" || landingStreamRows704(t, w, infraStream) != 0 {
		t.Fatal("v1 claimed or read external evidence", op, d)
	}
	_, err = s.sign(w, ruleText("landing:dev", 2, "loosen", since, until, infraRuleBody))
	ok(t, err)
	mode, err = store.EnsureLandingMode(ctx, w.rt, infraRepo)
	ok(t, err)
	if mode.IsZero() || w.lastSeq(t, infraStream) == 0 {
		t.Fatal("signed infra did not enable")
	}
	body := strings.Replace(infraRuleBody, "max_changed_lines: 1500", "max_changed_lines: 1000", 1)
	_, err = store.TightenRule(ctx, w.rt, wr("thread:s1"), "landing:dev", []byte(ruleText("landing:dev", 3, "tighten", since, until, body)))
	ok(t, err)
}

func infraControl(name string) []rules.LandingChange {
	c := infraChange()
	switch name {
	case "T3", "T9":
		c.OldPath = ""
		c.OldMode = ""
		c.NewPath = "agent-config/work/multi-agent/REPORT-b.md"
	case "T4-skill":
		c.OldPath = ""
		c.OldMode = ""
		c.NewPath = "agent-config/work/multi-agent/reviews/r1.md"
	case "T4-runbook", "T10":
		c.OldPath = ""
		c.OldMode = ""
		c.NewPath = "agent-config/work/multi-agent/DESIGN-x.md"
	case "T11":
		var out []rules.LandingChange
		for i := 0; i < 5; i++ {
			out = append(out, rules.LandingChange{NewPath: fmt.Sprintf("agent-config/work/multi-agent/REPORT-%d.md", i), NewMode: "100644", Added: 1})
		}
		return out
	case "T12":
		c.Added = 1500
	}
	return []rules.LandingChange{c}
}

// Serial: FreezeEventClock. T61: candidates, facts, operation streams and
// CI adapters remain separate even when both tasks accepted commitA and
// both prepared commitB. No CI backend selection is added here (S6-5).
func TestS6SameSHAIsolatedCandidatesAndCIPG(t *testing.T) {
	w, infraTask, infraSrc := infraWorld(t, []rules.LandingChange{infraChange()})
	at := time.Now().UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at.Add(-time.Hour))()
	_, err := store.EnsureLandingMode(ctx, w.rt, "bifrost-platform")
	ok(t, err)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.ObserveLandingEnabled(ctx, tx, "prod", "dev", "bifrost-platform", at)
		return err
	}))
	store.FreezeEventClock(at)
	platformTask := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "same SHA control", BaseCommit: commitB})
	w.passRound(t, platformTask, "h1", "h2", "h3")
	platformSrc := c4Source("console/src/components/Pair.tsx")
	src := store.LandingSource{Result: func(ctx context.Context, repo, task, attempt, base, a string) (bool, []rules.LandingChange, error) {
		if repo == infraRepo {
			return infraSrc.Result(ctx, repo, task, attempt, base, a)
		}
		if repo != "bifrost-platform" || task != platformTask || a != commitA {
			t.Fatal("crossed fact source", repo, task, a)
		}
		return platformSrc.Result(ctx, repo, task, attempt, base, a)
	}, History: func(ctx context.Context, repo, task, a, snapshot string) (landing.MainHistory, error) {
		if repo == infraRepo {
			return infraSrc.History(ctx, repo, task, a, snapshot)
		}
		return platformSrc.History(ctx, repo, task, a, snapshot)
	}}
	ops := map[string]string{}
	for repo, task := range map[string]string{infraRepo: infraTask, "bifrost-platform": platformTask} {
		candidates, err := store.LandingCandidates(ctx, w.rt, at.Add(-time.Hour), repo)
		ok(t, err)
		if len(candidates) != 1 || candidates[0] != task {
			t.Fatal("crossed candidates", repo, candidates)
		}
		op, d, err := store.ClaimLanding(ctx, w.rt, src, "prod", task, at)
		ok(t, err)
		if op == "" || !d.Allowed {
			t.Fatal("claim", repo, d)
		}
		ops[repo] = op
		prepareS6(t, w, repo, task, op, at)
	}
	store.FreezeEventClock(at.Add(15 * time.Minute))
	for repo, op := range ops {
		var ciReads, kicks int
		check := func(got string) {
			if got != repo {
				t.Fatal("crossed CI source", got, repo)
			}
		}
		sources := controller.DrillSources{Facts: src, Main: func(_ context.Context, got string) (string, error) { check(got); return commitA, nil },
			CI: func(_ context.Context, got, r string, _ time.Time) (landing.CIRun, error) {
				check(got)
				ciReads++
				if r != commitB {
					t.Fatal(r)
				}
				return landing.CIRun{R: r, Status: "Missing"}, nil
			},
			WindowBlocked: func(_ context.Context, got string) (bool, error) { check(got); return false, nil },
			EnsureCI:      func(_ context.Context, got, name, r string) error { check(got); kicks++; return nil }}
		ctl := controller.Controller{DB: w.rt, Env: "prod", Live: true, LiveRepos: []string{repo}}
		a, err := ctl.LandingDrillCI(ctx, sources, op, at, repo)
		ok(t, err)
		if a.Kind != "ci_kick" || ciReads != 1 || kicks != 1 {
			t.Fatal("CI dispatch", repo, a, ciReads, kicks)
		}
	}
}

func prepareS6(t *testing.T, w *world, repo, task, op string, at time.Time) {
	t.Helper()
	ref := "refs/heads/land/prod/" + task + "/1"
	for _, r := range []map[string]any{newLandingRecord(op, "prepared", "R", commitB, "M0", commitA, "committer_date", at.Format(time.RFC3339Nano), "dry_run", false), newLandingRecord(op, "branch_intent", "ref", ref, "R", commitB), newLandingRecord(op, "branch_pushed", "ref", ref, "R", commitB)} {
		ok(t, appendLanding(w, "landing:"+repo, r))
	}
}

// Serial: event clock and signing anchor. T13 proves the real gate's
// platform wait and the infra soft failure, without an infra grant read.
func TestS6SoftFailureNeverWaitsForInfraGrantPG(t *testing.T) {
	c := c5Wait(t, c5Soft, true, true)
	if c.phase(t) != "awaiting_grant" {
		t.Fatal("platform control did not wait")
	}
	bad := infraChange()
	bad.OldPath = "agent-config/work/multi-agent/PLAN-x.md"
	bad.NewPath = bad.OldPath
	w, task, src := infraWorld(t, []rules.LandingChange{bad})
	src.GrantPatch = func(context.Context, string, string, string) (store.GrantPatch, error) {
		t.Fatal("infra read grant patch")
		return store.GrantPatch{}, nil
	}
	src.GrantCI = func(context.Context, store.GrantCIKey, time.Time) (store.GrantCIEvidence, error) {
		t.Fatal("infra read grant CI")
		return store.GrantCIEvidence{}, nil
	}
	op, d, err := store.ClaimLanding(ctx, w.rt, src, "prod", task, time.Now())
	ok(t, err)
	if op != "" || d.Reason != "floor" {
		t.Fatal(op, d)
	}
	s := landingState(t, w, infraStream)
	if s.Operations[s.LatestOperation(task)].Phase != "failed" {
		t.Fatal("infra did not terminate")
	}
}

// Serial: event clock and signing anchor. T15 refuses an unknown repo at
// binding, before evidence, while the identical accepted platform succeeds.
func TestS6UnknownRepoRefusedBeforeEvidencePG(t *testing.T) {
	w, task, src := goalDrill(t, "M-3")
	if op, d, err := store.ClaimLandingDrill(ctx, w.rt, src, task, time.Now()); err != nil || op == "" || !d.Allowed {
		t.Fatal(op, d, err)
	}
	other := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "unknown landing repo", Repo: "bifrost-research", BaseCommit: commitB})
	w.passRound(t, other, "h1", "h2", "h3")
	op, d, err := store.ClaimLandingDrill(ctx, w.rt, store.LandingSource{}, other, time.Now())
	ok(t, err)
	if op != "" || d.Reason != "repo" {
		t.Fatal(op, d)
	}
	// An unknown repository has no landing stream at all: none is created.
	var streams int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event_stream WHERE event_stream_id = 'landing:bifrost-research'`).Scan(&streams))
	if streams != 0 {
		t.Fatal("unknown stream written")
	}
}

// Serial: FreezeEventClock. T63 includes a recovered unknown push and a
// successful push later missing from main, settled by the Owner's answer.
func TestS6ClosedInfraUnknownAndMainLostPG(t *testing.T) {
	for _, outcome := range []string{"unknown", "ok"} {
		t.Run(outcome, func(t *testing.T) {
			w, task, src := infraWorld(t, []rules.LandingChange{infraChange()})
			op, d, err := store.ClaimLanding(ctx, w.rt, src, "prod", task, time.Now())
			ok(t, err)
			if !d.Allowed {
				t.Fatal(d)
			}
			at := time.Now().UTC().Truncate(time.Second)
			defer store.FreezeEventClock(at)()
			prepareS6(t, w, infraRepo, task, op, at)
			store.FreezeEventClock(at.Add(2 * time.Minute))
			auth, d, err := store.AuthorizeLandingPush(ctx, w.rt, src, op, observed(at), infraRepo)
			ok(t, err)
			if !d.Allowed {
				t.Fatal(d)
			}
			_, err = store.RecordPushResult(ctx, w.rt, auth, outcome)
			ok(t, err)
			store.FreezeEventClock(at.Add(time.Hour))
			ctl := controller.Controller{DB: w.rt, Env: "prod", LiveRepos: []string{}, Reconcile: &controller.LandingReconcile{Repos: []string{infraRepo}, MainEvidence: func(_ context.Context, repo, root, a, r string) (store.MainEvidence, error) {
				if repo != infraRepo || root != task {
					t.Fatal(repo, root)
				}
				if outcome == "unknown" {
					return store.MainEvidence{Head: r, HasR: true, Validity: "effective"}, nil
				}
				return store.MainEvidence{Head: commitA}, nil
			}}}
			ok(t, ctl.LandingDrillPass(ctx))
			ok(t, ctl.LandingReconcilePass(ctx))
			want := "landed"
			if outcome == "ok" {
				res, err := store.HandleLandingAnswer(ctx, w.rt, answerCard("q-infra-lost", "confirm_main_lost", at.Add(time.Hour)), task, op, at.Add(time.Hour))
				ok(t, err)
				if res.Outcome != store.LandingAnswerApplied || res.Why != "main_lost" {
					t.Fatal(res)
				}
				want = "failed"
			}
			if o := landingState(t, w, infraStream).Operations[op]; o.Phase != want {
				t.Fatal(o)
			}
		})
	}
}
