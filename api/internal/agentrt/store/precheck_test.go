package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// R5:5.7-9, R5:5.7-10 (store/precheck.go): where the landing runs a CI it is
// an implementation's check. A submitted attempt is accepted on submission,
// with no verify task and no review; a red landing CI sends the task back
// for its next round, told what failed, instead of the Owner's card. Where
// the landing runs none the deterministic check stays and its pass accepts.
// Precheck verify keeps the check and then the review.

// precheckPath is a file the test landing rule allows.
const precheckPath = "console/src/components/Precheck.tsx"

// ciRoot is a ready implementation of M-3 in bifrost-platform on liveWorld:
// a landing rule names the repository and its landing is on.
func ciRoot(t *testing.T) (*world, string, time.Time) {
	t.Helper()
	at := time.Now().UTC().Truncate(time.Second)
	t.Cleanup(store.FreezeEventClock(at))
	w := liveWorld(t, "none", at)
	store.FreezeEventClock(at.Add(-40 * time.Minute))
	return w, w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "precheck", BaseCommit: commitB}), at
}

// checksOf are the verification and review tasks of subject.
func checksOf(t *testing.T, w *world, subject string) []string {
	t.Helper()
	rows, err := w.rt.Query(ctx, `SELECT role FROM agentrt.task WHERE subject_task_id = $1 ORDER BY created_at`, subject)
	ok(t, err)
	roles, err := pgx.CollectRows(rows, pgx.RowTo[string])
	ok(t, err)
	return roles
}

// verificationsOf are subject's verifications, oldest first, as
// method/verdict[/accepts].
func verificationsOf(t *testing.T, w *world, subject string) []string {
	t.Helper()
	rows, err := w.rt.Query(ctx, `SELECT method || '/' || verdict || CASE WHEN accepts_attempt THEN '/accepts' ELSE '' END
	  FROM agentrt.verification WHERE task_id = $1 ORDER BY created_at, verification_id`, subject)
	ok(t, err)
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	ok(t, err)
	return out
}

// acceptOnSubmission runs root's round and checks it with the default
// precheck.
func acceptOnSubmission(t *testing.T, w *world, root string) {
	t.Helper()
	if c := w.run(t, "h1", cursor, "done"); c.TaskID != root {
		t.Fatalf("claimed %s, not %s", c.TaskID, root)
	}
	id, accepted, err := store.StartVerificationWith(ctx, w.rt, root, store.ConcludeOptions{})
	ok(t, err)
	if id != "" || !accepted {
		t.Fatalf("submission: check %q, accepted %v", id, accepted)
	}
}

// Serial: FreezeEventClock. The defaults: the controller's acceptance pass
// accepts a platform attempt on submission, with no verify task and no
// review, a landing_ci verification accepting it; and its landing is
// prepared.
func TestPrecheckCIAcceptsOnSubmission(t *testing.T) {
	w, root, at := ciRoot(t)
	if c := w.run(t, "h1", cursor, "done"); c.TaskID != root {
		t.Fatal("claimed", c.TaskID)
	}
	moved, err := (&controller.Controller{DB: w.rt}).Accept(ctx)
	ok(t, err)
	if moved != 1 || w.state(t, "task", root) != "accepted" {
		t.Fatalf("moved %d, state %s", moved, w.state(t, "task", root))
	}
	if checks := checksOf(t, w, root); len(checks) != 0 {
		t.Fatalf("checks proposed: %v", checks)
	}
	if v := verificationsOf(t, w, root); !slices.Equal(v, []string{"landing_ci/pass/accepts"}) {
		t.Fatalf("verifications: %v", v)
	}
	store.FreezeEventClock(at.Add(-30 * time.Minute))
	op, d, err := store.ClaimLanding(ctx, w.rt, c4Source(precheckPath), "prod", root, at)
	ok(t, err)
	if !d.Allowed || op == "" {
		t.Fatal("the landing is not prepared:", d)
	}
}

// Serial: FreezeEventClock. A core-path attempt accepted on submission gets
// its core landing grant material with no problem: no review, so the card's
// review is empty and has no trust answers; the acceptance is the landing
// CI's. The grant is written from it.
func TestPrecheckCICoreGrantMaterial(t *testing.T) {
	c := c5WaitAccepted(t, c5Core, false, true, func(w *world, root string) { acceptOnSubmission(t, w, root) })
	m, err := store.ReadLandingGrantMaterial(ctx, c.w.rt, c.gsrc, c.op, true)
	ok(t, err)
	if len(m.Problems) != 0 || m.Action != store.ActionCoreLanding || len(m.Candidate.HardCorePaths) == 0 {
		t.Fatalf("material: %s %v", m.Action, m.Problems)
	}
	if m.Candidate.Review != (store.GrantReview{}) || len(m.Candidate.Acceptance) != 1 || m.Candidate.Acceptance[0].Result != "pass" {
		t.Fatalf("checks on the card: %+v %+v", m.Candidate.Review, m.Candidate.Acceptance)
	}
	b, err := json.Marshal(m.Candidate)
	ok(t, err)
	if strings.Contains(string(b), "trust") {
		t.Fatalf("the card still names the trust answers: %s", b)
	}
	if g := c.grant(t, store.ActionCoreLanding); g.Task != c.root {
		t.Fatal("grant", g)
	}
}

// ciRed is a root accepted on submission whose landing operation is
// prepared and its branch pushed, with max rounds as given (0: the
// default).
func ciRed(t *testing.T, maxRounds int) (*world, string, string, time.Time) {
	t.Helper()
	w, root, at := ciRoot(t)
	if maxRounds > 0 {
		w.legacy(t, fmt.Sprintf(`UPDATE agentrt.task SET max_rounds = %d WHERE task_id = '%s'`, maxRounds, root))
	}
	acceptOnSubmission(t, w, root)
	store.FreezeEventClock(at.Add(-30 * time.Minute))
	op, d, err := store.ClaimLanding(ctx, w.rt, c4Source(precheckPath), "prod", root, at)
	ok(t, err)
	if !d.Allowed || op == "" {
		t.Fatal("claim", d)
	}
	c4Prepare(t, w, root, op, at.Add(-29*time.Minute), true)
	store.FreezeEventClock(at.Add(-20 * time.Minute))
	return w, root, op, at
}

var redCI = landing.CIFailure{Pipeline: "bifrost-ci-platform", Run: "landing-ci-0123456789abcdef01234567-2",
	TaskRuns: []landing.CIFailedTask{{Name: "landing-ci-0123456789abcdef01234567-2-go-test", PipelineTask: "go-test", Step: "test",
		Message: `"step-test" exited with code 1`, LogTail: "--- FAIL: TestPrecheck (0.01s)\nFAIL\tgithub.com/x/store\t0.1s"}}}

// rootFailed says whether root has a root_failed card trigger now.
func rootFailed(t *testing.T, w *world, root string, at time.Time) []string {
	t.Helper()
	var choices []string
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		triggers, err := store.LandingCardTriggers(ctx, tx, "bifrost-platform", at)
		for _, tr := range triggers {
			if tr.Root == root && tr.Kind == store.CardRootFailed {
				choices = tr.Choices
			}
		}
		return err
	}))
	return choices
}

// Serial: FreezeEventClock. A red landing CI with rounds left: the operation
// fails ci_red, and the task goes back to ready in round 2 with no
// root_failed card; its next claim names the landing CI's failure as the
// failed check, with the failed TaskRun and its log. The next attempt is
// accepted on submission, which retries the landing: the next operation
// claims it.
func TestPrecheckCIRedLandingIsRework(t *testing.T) {
	w, root, op, at := ciRed(t, 0)
	reworked, err := store.RecordLandingCIRed(ctx, w.rt, "bifrost-platform", op, "test: failed", redCI, store.PrecheckCI)
	ok(t, err)
	if !reworked || w.state(t, "task", root) != "ready" {
		t.Fatalf("reworked %v, state %s", reworked, w.state(t, "task", root))
	}
	if p, why := phaseOf(t, w, op); p != "failed" || why != "ci_red" {
		t.Fatal("operation", p, why)
	}
	var round int
	ok(t, w.rt.QueryRow(ctx, `SELECT round_no FROM agentrt.task WHERE task_id = $1`, root).Scan(&round))
	if round != 2 {
		t.Fatal("round", round)
	}
	if v := verificationsOf(t, w, root); !slices.Equal(v, []string{"landing_ci/pass/accepts", "landing_ci/fail"}) {
		t.Fatalf("verifications: %v", v)
	}
	if choices := rootFailed(t, w, root, at); choices != nil {
		t.Fatal("a root_failed card for a rework:", choices)
	}
	store.FreezeEventClock(at.Add(-15 * time.Minute))
	c := w.claim(t, "h2", store.LeaseTTL, cursor)
	if c == nil || c.TaskID != root || len(c.Failed) != 1 || c.Failed[0].Method != store.MethodLandingCI || c.Failed[0].AttemptNo != 1 {
		t.Fatalf("round 2's claim: %+v", c)
	}
	var report []byte
	ok(t, w.rt.QueryRow(ctx, `SELECT body FROM agentrt.artifact_blob WHERE artifact_id = $1`, c.Failed[0].Evidence).Scan(&report))
	var doc struct {
		CI landing.CIFailure `json:"landing_ci"`
	}
	ok(t, json.Unmarshal(report, &doc))
	if doc.CI.Run != redCI.Run || len(doc.CI.TaskRuns) != 1 || doc.CI.TaskRuns[0] != redCI.TaskRuns[0] {
		t.Fatalf("the failure told: %s", report)
	}
	// Round 2 hands in and is accepted on submission; the landing retries.
	f := fence(c, "h2")
	ok(t, store.Start(ctx, w.rt, f, "", ""))
	exit := 0
	_, err = store.Computed(ctx, w.rt, f, &exit, "exit", "done")
	ok(t, err)
	calls := 0
	_, err = store.Persist(ctx, w.rt, f, store.Result{Report: w.artifact(t, "report", "round 2"), Ref: c.PushedRef, Commit: commitA},
		heads(map[string]string{c.PushedRef: commitA}, &calls))
	ok(t, err)
	_, err = store.Submit(ctx, w.rt, f)
	ok(t, err)
	_, accepted, err := store.StartVerificationWith(ctx, w.rt, root, store.ConcludeOptions{})
	ok(t, err)
	if !accepted || w.state(t, "task", root) != "accepted" {
		t.Fatal("round 2 not accepted on submission")
	}
	if o := landingState(t, w, landingRepoStream).Operations[op]; !o.Retried {
		t.Fatal("the red operation was not retried")
	}
	store.FreezeEventClock(at.Add(-10 * time.Minute))
	next, d, err := store.ClaimLanding(ctx, w.rt, c4Source(precheckPath), "prod", root, at)
	ok(t, err)
	if !d.Allowed || next == "" || next == op {
		t.Fatal("the next operation", next, d)
	}
	if o := landingState(t, w, landingRepoStream).Operations[next]; o.Attempt != c.AttemptID {
		t.Fatalf("the next operation lands attempt %s, not %s", o.Attempt, c.AttemptID)
	}
}

// Serial: FreezeEventClock. Precheck verify is today's: a platform attempt
// whose landing runs a CI is still checked, then reviewed, and only the
// review accepts; a red landing CI is the Owner's card.
func TestPrecheckVerifyChecksThenReviews(t *testing.T) {
	w, root, at := ciRoot(t)
	w.passRound(t, root, "h1", "h2", "h3")
	if checks := checksOf(t, w, root); !slices.Equal(checks, []string{"verify", "review"}) {
		t.Fatal("checks", checks)
	}
	if v := verificationsOf(t, w, root); !slices.Equal(v, []string{"deterministic/pass", "review/pass/accepts"}) {
		t.Fatalf("verifications: %v", v)
	}
	store.FreezeEventClock(at.Add(-30 * time.Minute))
	op, d, err := store.ClaimLanding(ctx, w.rt, c4Source(precheckPath), "prod", root, at)
	ok(t, err)
	if !d.Allowed {
		t.Fatal("claim", d)
	}
	c4Prepare(t, w, root, op, at.Add(-29*time.Minute), true)
	store.FreezeEventClock(at.Add(-20 * time.Minute))
	reworked, err := store.RecordLandingCIRed(ctx, w.rt, "bifrost-platform", op, "test: failed", redCI, store.PrecheckVerify)
	ok(t, err)
	if reworked || w.state(t, "task", root) != "accepted" || rootFailed(t, w, root, at) == nil {
		t.Fatal("a red CI under precheck verify is not the card")
	}
}

// A repository whose landing runs no CI keeps the deterministic check, and
// with the default precheck its pass accepts the attempt: no review.
func TestPrecheckCINoLandingCIIsChecked(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	for _, repo := range []string{"bifrost-platform", "bifrost-ui"} {
		// bifrost-platform with no landing rule in force; bifrost-ui, which
		// the landing does not know.
		root := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "no landing CI", Repo: repo})
		if c := w.run(t, "h1", cursor, "done"); c.TaskID != root {
			t.Fatal("claimed", c.TaskID)
		}
		v, accepted, err := store.StartVerificationWith(ctx, w.rt, root, store.ConcludeOptions{})
		ok(t, err)
		if v == "" || accepted || w.state(t, "task", root) != "verifying" {
			t.Fatalf("%s: check %q, accepted %v", repo, v, accepted)
		}
		w.run(t, "h2", deterministic, "done")
		did, err := store.ConcludeWith(ctx, w.rt, v, store.ConcludeOptions{})
		ok(t, err)
		if did != "accepted" || w.state(t, "task", root) != "accepted" {
			t.Fatalf("%s: the pass did %q", repo, did)
		}
		if checks := checksOf(t, w, root); !slices.Equal(checks, []string{"verify"}) {
			t.Fatalf("%s: checks %v", repo, checks)
		}
		if got := verificationsOf(t, w, root); !slices.Equal(got, []string{"deterministic/pass/accepts"}) {
			t.Fatalf("%s: verifications %v", repo, got)
		}
	}
}

func TestParsePrecheck(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]store.Precheck{"": store.PrecheckCI, "ci": store.PrecheckCI, " verify ": store.PrecheckVerify} {
		if got, err := store.ParsePrecheck(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	if got, err := store.ParsePrecheck("review"); err == nil || got != store.PrecheckCI {
		t.Errorf("review: %q %v", got, err)
	}
}

// Serial: FreezeEventClock. The drill's own CI step: the rerun of a root
// accepted on submission is red while main is green, so the operation ends
// failed{ci_red}; the controller reads what the run failed on and the root
// goes back for its next round.
func TestPrecheckCIRedFromTheDrill(t *testing.T) {
	w, root, op, at := ciRed(t, 0)
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "ci_kick", "try", 1)))
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "ci_rerun", "try", 2)))
	store.FreezeEventClock(at.Add(-18 * time.Minute))
	red := landing.CIRun{Run: landing.CIName(op, 2), R: commitB, Status: "Failed", CreatedAt: at.Add(-19 * time.Minute)}
	read := ""
	drill := controller.DrillSources{Facts: c4Source(precheckPath),
		Main:          func(context.Context, string) (string, error) { return commitA, nil },
		WindowBlocked: func(context.Context, string) (bool, error) { return false, nil },
		CI: func(_ context.Context, _ string, revision string, _ time.Time) (landing.CIRun, error) {
			if revision == commitB {
				return red, nil
			}
			return landing.CIRun{Run: "main", R: commitA, Status: "Succeeded", CreatedAt: at.Add(-time.Hour)}, nil
		},
		EnsureCI: func(context.Context, string, string, string) error { return nil },
		CIFailure: func(_ context.Context, run string) (landing.CIFailure, error) {
			read = run
			return redCI, nil
		}}
	ctl := controller.Controller{DB: w.rt, Env: "prod", Live: true, DrillGit: map[string]controller.DrillGit{"bifrost-platform": &branchObserved{}}}
	a, err := ctl.LandingDrillCI(ctx, drill, op, at.Add(-18*time.Minute))
	ok(t, err)
	if a.Kind != "failed" || a.Why != "ci_red" || read != red.Run {
		t.Fatalf("action %+v, failure read of %q", a, read)
	}
	if p, why := phaseOf(t, w, op); p != "failed" || why != "ci_red" || w.state(t, "task", root) != "ready" {
		t.Fatal("after the red CI:", p, why, w.state(t, "task", root))
	}
	if choices := rootFailed(t, w, root, at); choices != nil {
		t.Fatal("a root_failed card for a rework:", choices)
	}
}
