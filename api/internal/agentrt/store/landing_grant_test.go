package store_test

import (
	"context"
	"os"
	"slices"
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

// Landing grants (W-31 round 4, core block C-5; DESIGN-coordinator-landing-
// v3-2026-10-29.md §5.5–§5.9, §12, §6 T23–T25, T27–T36, T48–T49). Each
// refusal has its success through the same entry; the refusals change one
// thing of it.

// c5 is a waiting operation of M-3's root, with what its grant needs.
type c5 struct {
	w        *world
	root, op string
	src      store.LandingSource
	gsrc     store.GrantSources
	at       time.Time
	ci       landing.CIRun
	ciReason string
	key      store.GrantCIKey
	patch    store.GrantPatch
	main     string
	signer   signer
}

// c5RuleBody is liveWorld's landing rule (stg_release none).
var c5RuleBody = strings.Replace(landingRuleBody, "stg_release: bifrost-deliver-platform", "stg_release: none", 1)

const c5Core = "api/internal/agentrt/store/c5.go"
const c5Soft = "api/internal/research/c5.go"

// c5Wait: a root prepared and waiting at the final gate (C-4), the review
// with the trust chain's answers, and grants enabled: the Owner's signed
// envelope names the coordinator's external host (§2.1). pinned makes the
// root a pinned one (the fixture sets the column as an operator would not
// be able to: the pin path itself is C-1/C-2's tests').
func c5Wait(t *testing.T, path string, pinned, enabled bool) *c5 {
	t.Helper()
	at := time.Now().UTC().Truncate(time.Second)
	t.Cleanup(store.FreezeEventClock(at))
	c := &c5{at: at, main: commitA, signer: signer{t, rptest.NewKey(t)}}
	c.w = liveWorld(t, "none", at)
	store.FreezeEventClock(at.Add(-40 * time.Minute))
	c.root = c.w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "C5", BaseCommit: commitB})
	c.w.passRound(t, c.root, "h1", "h2", "h3")
	if pinned {
		c5Pin(t, c.w, c.root)
	}
	// Grants are enabled before the claim: before, nothing waits (R-03).
	if enabled {
		c.enable(t, 1)
	}
	c.src = c4Source(path)
	c.src.GrantPatch = func(context.Context, string, string, string) (store.GrantPatch, error) { return c.patch, nil }
	c.src.GrantCI = func(context.Context, store.GrantCIKey, time.Time) (store.GrantCIEvidence, error) {
		ev := store.GrantCIEvidence{Run: c.ci.Run, UID: c.ci.UID, CreatedAt: c.ci.CreatedAt, Status: c.ci.Status, Reason: c.ciReason,
			Candidate: c.ci.PlatformCommit, Siblings: map[string]string{"bifrost-ui": c.ci.UICommit, "bifrost-trade-infra": c.ci.InfraCommit}}
		if ev.Status == "Succeeded" && ev.Reason == "" {
			ev.Pipeline = "bifrost-ci-platform"
		}
		return ev, nil
	}
	store.FreezeEventClock(at.Add(-30 * time.Minute))
	op, d, err := store.ClaimLanding(ctx, c.w.rt, c.src, "prod", c.root, at)
	ok(t, err)
	if !d.Allowed || op == "" {
		t.Fatal("claim", d)
	}
	c.op = op
	prepared := at.Add(-29 * time.Minute)
	c4Prepare(t, c.w, c.root, op, prepared, true)
	// The kick that pins its CI, as the controller makes it (B-01).
	c.key = store.GrantCIKey{Repo: "bifrost-platform", Revision: commitB, Base: commitA,
		Siblings: map[string]string{"bifrost-ui": commitC, "bifrost-trade-infra": commitA}}
	ok(t, store.AppendCIKick(ctx, c.w.rt, op, "ci_kick", 1, c.key, "test: pinned kick"))
	c.ci = landing.CIRun{Run: "green", UID: "uid-green", R: commitB, Status: "Succeeded", CreatedAt: prepared.Add(time.Minute),
		PlatformCommit: commitB, UICommit: commitC, InfraCommit: commitA}
	c.patch = store.GrantPatch{SHA256: strings.Repeat("7", 64), Shown: "diff --git a/x b/x\n", Size: 19, Match: true}
	store.FreezeEventClock(at.Add(-20 * time.Minute))
	_, d, err = store.AuthorizeLandingPush(ctx, c.w.rt, c.src, op, c.observe())
	ok(t, err)
	if d.Reason != "awaiting_grant" {
		t.Fatal("not waiting", d)
	}
	c5Trust(t, c.w, c.root)
	c.gsrc = store.GrantSources{Facts: c.src, Main: func(context.Context) (string, error) { return c.main, nil }}
	store.FreezeEventClock(at)
	return c
}

// enable signs the dev envelope with the coordinator's external host.
func (c *c5) enable(t *testing.T, version int) {
	t.Helper()
	enableGrants(t, c.w, c.signer, version, true)
}

// enableGrants signs version of the dev envelope, naming the coordinator's
// external host (grants enabled) or not (a tightening that stops them).
func enableGrants(t *testing.T, w *world, s signer, version int, hosts bool) {
	t.Helper()
	if version == 1 {
		registerSeat(t, w)
		ok(t, store.RegisterHost(ctx, w.rt, "owner", externalHost, store.HostExternal))
	}
	direction := map[bool]string{true: "loosen", false: "tighten"}[hosts]
	if version == 1 {
		direction = rules.Initial
	}
	body := strings.Replace(pinEnvelope(version, direction, hosts),
		"hosts: [h1, coordinator-mbp]", "hosts: [h1, h2, h3, h4, h5, h6, h7, h8, coordinator-mbp]", 1)
	ok(t, firstErr(s.sign(w, body)))
}

func (c *c5) observe() func(context.Context, landing.Operation) (store.LiveObservation, error) {
	return func(context.Context, landing.Operation) (store.LiveObservation, error) {
		return store.LiveObservation{Main: c.main, CI: c.ci}, nil
	}
}

func c5Pin(t *testing.T, w *world, task string) {
	t.Helper()
	var db string
	ok(t, w.rt.QueryRow(ctx, `SELECT current_database()`).Scan(&db))
	admin, err := pgx.Connect(ctx, os.Getenv("AGENTRT_TEST_PG")+" dbname="+db)
	ok(t, err)
	defer admin.Close(ctx)
	_, err = admin.Exec(ctx, `SET session_replication_role = replica`)
	ok(t, err)
	_, err = admin.Exec(ctx, `UPDATE agentrt.task SET executor_pin = 'external:coordinator' WHERE task_id = $1`, task)
	ok(t, err)
}

// c5Trust records a passing review of the accepted attempt whose report
// answers the trust chain's four questions (store.TrustReviewText).
func c5Trust(t *testing.T, w *world, task string) {
	t.Helper()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var attempt, reviewer string
		if err := tx.QueryRow(ctx, `SELECT attempt_id, verifier_attempt_id FROM agentrt.verification WHERE task_id=$1 AND method='review' AND verdict='pass'
		  ORDER BY created_at DESC LIMIT 1`, task).Scan(&attempt, &reviewer); err != nil {
			return err
		}
		body := c5TrustAnswers
		evidence, err := store.PutArtifact(ctx, tx, "controller", "review", "text/markdown", []byte(body+task))
		if err != nil {
			return err
		}
		if _, err = store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: the trust chain review"}}); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO agentrt.verification (verification_id, task_id, attempt_id, method, verifier_attempt_id, verdict, evidence_artifact_id)
		  VALUES ($1, $2, $3, 'review', $4, 'pass', $5)`, store.NewVerificationID(), task, attempt, reviewer, evidence)
		return err
	}))
}

// c5TrustAnswers is a review report that answers the trust chain in the
// shape store.TrustReviewText asks for.
const c5TrustAnswers = "Correct and complete.\n\n## Trust chain answers\n" +
	"- T1: No, the approval roles and tiers are untouched by this diff.\n" +
	"- T2: No, the material and its digests are computed as before.\n" +
	"- T3: No, no token or credential is read or kept differently.\n" +
	"- T4: No, the landing gate's checks and their order are unchanged.\n"

var c5Approvals = 0

// approval is a verified approval record for action, approved ago.
func (c *c5) approval(action string, ago time.Duration) store.GrantApproval {
	c5Approvals++
	return store.GrantApproval{ID: "appr_c5_" + strings.Repeat("x", c5Approvals), Number: int64(100 + c5Approvals), Action: action,
		ParamsHash: strings.Repeat("ab", 32), ApprovedAt: c.at.Add(-ago)}
}

// params are a request's parameters for the material as it is now.
func (c *c5) params(t *testing.T) store.GrantParams {
	t.Helper()
	m, err := store.ReadLandingGrantMaterial(ctx, c.w.rt, c.gsrc, c.op, true)
	ok(t, err)
	return store.GrantParams{Env: "prod", Repo: "bifrost-platform", Task: c.root, Op: c.op, R: commitB, MaterialDigest: m.MaterialDigest, STGRelease: true}
}

func (c *c5) grant(t *testing.T, action string) store.LandingGrant {
	t.Helper()
	g, err := store.WriteLandingGrant(ctx, c.w.rt, c.gsrc, c.approval(action, time.Minute), c.params(t))
	ok(t, err)
	return g
}

func (c *c5) gate(t *testing.T) (store.Authorization, landing.Decision) {
	t.Helper()
	auth, d, err := store.AuthorizeLandingPush(ctx, c.w.rt, c.src, c.op, c.observe())
	ok(t, err)
	return auth, d
}

func (c *c5) phase(t *testing.T) string {
	t.Helper()
	p, _ := phaseOf(t, c.w, c.op)
	return p
}

func (c *c5) reservations(t *testing.T) int {
	t.Helper()
	return len(landingState(t, c.w, "landing-quota:dev:"+c.at.Format("2006-01-02")).Reservations)
}

func refusedWith(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), code) {
		t.Fatalf("want refusal %q, got %v", code, err)
	}
}

// Serial: FreezeEventClock. T27's success and its consumption (§5.6, §5.1
// step 5): the material is clean, the executor's grant carries every
// field, the final gate pushes it once with landing_grant_use in the
// main_intent, the quota counts it, and the grant cannot be used again.
func TestC5GrantWrittenAndConsumed(t *testing.T) {
	c := c5Wait(t, c5Core, false, true)
	m, err := store.ReadLandingGrantMaterial(ctx, c.w.rt, c.gsrc, c.op, true)
	ok(t, err)
	if len(m.Problems) != 0 || m.Action != store.ActionCoreLanding || !slices.Equal(m.SoftAtRequest, []string{"core", "rule_paths"}) ||
		len(m.Candidate.HardCorePaths) != 1 || m.Candidate.HardCorePaths[0].Entry != "api/internal/agentrt/store/" || !m.Candidate.TrustReview ||
		m.Rule.ID != "landing:dev" || m.Rule.SHA256 == "" || m.FirstLine == "" || m.STGNotice == "" {
		t.Fatalf("material: %+v", m)
	}
	g := c.grant(t, store.ActionCoreLanding)
	if g.GrantID == "" || g.Op != c.op || g.R != commitB || g.M0 != commitA || !slices.Equal(g.Waives, m.SoftAtRequest) || g.CandidateDigest != m.CandidateDigest ||
		g.MaterialDigest != m.MaterialDigest || g.CI.Run != "green" || g.CI.UID != "uid-green" || g.Purpose != "landing_main" || !g.STGRelease || g.Home != "prod" {
		t.Fatalf("grant: %+v", g)
	}
	if st, err := store.ReadLandingWait(ctx, c.w.rt, c.op); err != nil || st.Shown != "authorized" || len(st.Grants) != 1 || !st.Grants[0].Valid {
		t.Fatalf("status: %+v %v", st, err)
	}
	usable, err := store.HasUsableLandingGrant(ctx, c.w.rt, c.op)
	ok(t, err)
	if !usable {
		t.Fatal("grant not usable")
	}
	auth, d := c.gate(t)
	if !d.Allowed || auth.R != commitB || auth.M0 != commitA {
		t.Fatalf("gate: %+v %+v", d, auth)
	}
	var use string
	ok(t, c.w.rt.QueryRow(ctx, `SELECT body_json->'landing_grant_use'->>'grant_id' FROM agentrt.event WHERE event_stream_id=$1 AND body_json->'landing'->>'kind'='main_intent'`,
		landingRepoStream).Scan(&use))
	if use != g.GrantID || c.phase(t) != "main_intent" || c.reservations(t) != 1 {
		t.Fatal("consumption", use, c.phase(t), c.reservations(t))
	}
	usable, err = store.HasUsableLandingGrant(ctx, c.w.rt, c.op)
	ok(t, err)
	if usable {
		t.Fatal("consumed grant still usable")
	}
	if _, d = c.gate(t); d.Allowed {
		t.Fatal("authorized twice")
	}
	if st, _ := store.ReadLandingWait(ctx, c.w.rt, c.op); st.Shown != "handed_to_push" || !st.Grants[0].Consumed {
		t.Fatalf("after: %+v", st)
	}
}

// Serial: FreezeEventClock. T27's refusals: each changes one thing of the
// success above (which runs last, on the same world), writes no grant, and
// the operation keeps waiting; main advanced writes discarded{main_changed}.
func TestC5ExecutorRefusals(t *testing.T) {
	c := c5Wait(t, c5Core, false, true)
	grants := func() int {
		return c.w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_stream_id=$1 AND body_json ? 'landing_grant'`, landingRepoStream)
	}
	try := func(code string, a store.GrantApproval, p store.GrantParams) {
		t.Helper()
		_, err := store.WriteLandingGrant(ctx, c.w.rt, c.gsrc, a, p)
		refusedWith(t, err, code)
		if grants() != 0 || c.phase(t) != "awaiting_grant" {
			t.Fatalf("%s: wrote %d grants, phase %s", code, grants(), c.phase(t))
		}
	}
	good := c.params(t)
	// The guardrail action for a core change (the categories never meet).
	try("action_mismatch", c.approval(store.ActionGuardrailLanding, time.Minute), good)
	// Another R.
	p := good
	p.R = commitC
	try("r_mismatch", c.approval(store.ActionCoreLanding, time.Minute), p)
	// Material the Owner did not see (another stg_release changes it too).
	p = good
	p.MaterialDigest = "sha256:" + strings.Repeat("0", 64)
	try("material_mismatch", c.approval(store.ActionCoreLanding, time.Minute), p)
	p = good
	p.STGRelease = false
	try("material_mismatch", c.approval(store.ActionCoreLanding, time.Minute), p)
	// An approval older than the grant's lifetime.
	try("approval_too_old", c.approval(store.ActionCoreLanding, 25*time.Hour), good)
	// CI moved to another run (still green): the material differs.
	ci := c.ci
	c.ci.Run, c.ci.UID = "green-2", "uid-green-2"
	try("material_mismatch", c.approval(store.ActionCoreLanding, time.Minute), good)
	c.ci = ci
	// No Git reader in this process.
	saved := c.gsrc
	c.gsrc.Facts.GrantPatch = nil
	_, err := store.WriteLandingGrant(ctx, c.w.rt, c.gsrc, c.approval(store.ActionCoreLanding, time.Minute), good)
	if err == nil || grants() != 0 {
		t.Fatal("wrote without its evidence")
	}
	c.gsrc = saved
	// The rule changed since the request: its identity is in the material.
	tighter := strings.Replace(c5RuleBody, "max_landings_per_day: 5", "max_landings_per_day: 4", 1)
	ok(t, firstErr(c.signer.sign(c.w, ruleText("landing:dev", 2, "tighten", since, until, tighter))))
	try("material_mismatch", c.approval(store.ActionCoreLanding, time.Minute), good)
	// The success, on the request made under the rule now in force.
	good = c.params(t)
	if _, err := store.WriteLandingGrant(ctx, c.w.rt, c.gsrc, c.approval(store.ActionCoreLanding, time.Minute), good); err != nil {
		t.Fatal(err)
	}
	if grants() != 1 {
		t.Fatal("no grant")
	}
}

// Serial: FreezeEventClock. Main moved: the execution fails and the waiting
// operation is discarded{main_changed} (§5.4, §5.6 item 4); the old ref is
// not touched and the next claim makes k+1. Its control writes the grant.
func TestC5ExecutorMainAdvanced(t *testing.T) {
	c := c5Wait(t, c5Core, false, true)
	p := c.params(t)
	c.main = commitC
	_, err := store.WriteLandingGrant(ctx, c.w.rt, c.gsrc, c.approval(store.ActionCoreLanding, time.Minute), p)
	refusedWith(t, err, "main_changed")
	if ph, why := phaseOf(t, c.w, c.op); ph != "discarded" || why != "main_changed" {
		t.Fatal(ph, why)
	}
	// Control: the same request with main at M0 on a fresh world.
	d := c5Wait(t, c5Core, false, true)
	if _, err := store.WriteLandingGrant(ctx, d.w.rt, d.gsrc, d.approval(store.ActionCoreLanding, time.Minute), d.params(t)); err != nil {
		t.Fatal(err)
	}
}

// Serial: FreezeEventClock. Review 694 R-03: before the Owner signs the
// external host entry nothing waits for a grant. A core root is refused at
// the claim as floor, as before C-4/C-5: no operation, no preparation, no CI
// kick, no wait. The control signs the entry and the next root of the same
// change is claimed. (A grant's consumption once the entry is removed:
// TestC5GrantStopsWhenTheEntryGoes.)
func TestC5NothingChangesBeforeTheEntry(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := liveWorld(t, "none", at)
	store.FreezeEventClock(at.Add(-40 * time.Minute))
	root := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "694 R-03", BaseCommit: commitB})
	w.passRound(t, root, "h1", "h2", "h3")
	src := c4Source(c5Core)
	store.FreezeEventClock(at.Add(-30 * time.Minute))
	// The controller's pass on PROD, live on, the v1 rule: no CI, no Git.
	kicks, opens := 0, 0
	sources := controller.DrillSources{Facts: src,
		Main:          func(context.Context) (string, error) { return commitA, nil },
		WindowBlocked: func(context.Context) (bool, error) { return false, nil },
		CI: func(context.Context, string, time.Time) (landing.CIRun, error) {
			return landing.CIRun{Status: "Missing"}, nil
		},
		EnsureCI:       func(context.Context, string, string) error { kicks++; return nil },
		EnsurePinnedCI: func(context.Context, string, store.GrantCIKey) error { kicks++; return nil },
	}
	ctl := controller.Controller{DB: w.rt, Env: "prod", Live: true, DrillGit: &branchObserved{}, Drill: &sources,
		OpenLive: func(context.Context, string) (controller.LiveWorkspace, error) { opens++; return &livePusher{}, nil }}
	ok(t, ctl.LandingDrillPass(ctx))
	if kicks != 0 || opens != 0 {
		t.Fatal("the pass kicked CI or opened a workspace", kicks, opens)
	}
	op, d, err := store.ClaimLanding(ctx, w.rt, src, "prod", root, at)
	ok(t, err)
	if op != "" || d.Reason != "floor" && d.Reason != "terminal" {
		t.Fatalf("a core root before the entry: %q %+v", op, d)
	}
	records := w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_stream_id=$1 AND body_json->'landing'->>'kind' IN ('claimed','prepared','ci_kick','awaiting_grant')`, landingRepoStream)
	if records != 0 {
		t.Fatal("prepared before the entry", records)
	}
	store.FreezeEventClock(at.Add(-25 * time.Minute))
	control := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "694 R-03 control", BaseCommit: commitB})
	w.passRound(t, control, "h4", "h5", "h6")
	enableGrants(t, w, signer{t, rptest.NewKey(t)}, 1, true)
	op, d, err = store.ClaimLanding(ctx, w.rt, src, "prod", control, at)
	ok(t, err)
	if op == "" || !d.Allowed {
		t.Fatalf("the control after the entry: %+v", d)
	}
}

// Serial: FreezeEventClock. The entry removed (a tightening, no signature
// needed by design) stops a grant already written: the gate refuses it,
// nothing is reserved or pushed, the controller no longer sees it usable.
// Signed again, the same grant is consumed.
func TestC5GrantStopsWhenTheEntryGoes(t *testing.T) {
	c := c5Wait(t, c5Core, false, true)
	c.grant(t, store.ActionCoreLanding)
	enableGrants(t, c.w, c.signer, 2, false)
	// Without the entry the change is floor again (R-03), grant or not.
	if _, d := c.gate(t); d.Allowed || d.Reason != "floor" || c.phase(t) != "awaiting_grant" || c.reservations(t) != 0 {
		t.Fatalf("consumed without the entry: %+v", d)
	}
	if g, err := store.UsableLandingGrant(ctx, c.w.rt, c.op); err != nil || g != "" {
		t.Fatal("usable without the entry", g, err)
	}
	enableGrants(t, c.w, c.signer, 3, true)
	if _, d := c.gate(t); !d.Allowed {
		t.Fatalf("with the entry: %+v", d)
	}
}

// Serial: FreezeEventClock. T23 and review 694 R-05: a rule signed after the
// grant makes size fail anew: grant_insufficient, nothing reserved or
// pushed, the operation waiting. The grant is spent on the record
// (landing_grant_refused): the status shows grant_refused with the reason,
// not "authorized", and W runs again from the refusal. A new request under
// the new rule is granted and pushes.
func TestC5T23TighterRuleNeedsANewGrant(t *testing.T) {
	c := c5Wait(t, c5Core, false, true)
	c.grant(t, store.ActionCoreLanding)
	tighter := strings.Replace(c5RuleBody, "max_changed_lines: 300", "max_changed_lines: 1", 1)
	ok(t, firstErr(c.signer.sign(c.w, ruleText("landing:dev", 2, "tighten", since, until, tighter))))
	_, d := c.gate(t)
	if d.Allowed || d.Reason != "grant_insufficient" || c.phase(t) != "awaiting_grant" || c.reservations(t) != 0 {
		t.Fatalf("%+v %s %d", d, c.phase(t), c.reservations(t))
	}
	st, err := store.ReadLandingWait(ctx, c.w.rt, c.op)
	ok(t, err)
	if st.Shown != "grant_refused" || st.GrantRefused != "grant_insufficient" || len(st.Grants) != 1 || st.Grants[0].Valid || !st.Grants[0].Refused {
		t.Fatalf("status after the refusal: %+v", st)
	}
	before := st.WaitingSeconds
	store.FreezeEventClock(c.at.Add(time.Hour))
	if st, _ = store.ReadLandingWait(ctx, c.w.rt, c.op); st.WaitingSeconds != before+3600 {
		t.Fatal("W did not run after the refusal", before, st.WaitingSeconds)
	}
	if g, err := store.UsableLandingGrant(ctx, c.w.rt, c.op); err != nil || g != "" {
		t.Fatal("a refused grant is still usable", g, err)
	}
	g := c.grant(t, store.ActionCoreLanding)
	if !slices.Contains(g.Waives, "size") {
		t.Fatal("the new grant does not waive size", g.Waives)
	}
	if _, d = c.gate(t); !d.Allowed {
		t.Fatalf("the new grant: %+v", d)
	}
}

// Serial: FreezeEventClock. T24 and the guardrail action: a pinned root's
// soft failures (rule_paths), granted; a rule signed since allows its path,
// so nothing fails now: it pushes all the same with landing_grant_use. The
// same root without its pin cannot be granted (not_pinned): only a pinned
// root waits on soft failures alone (§3.4).
func TestC5T24LooserRuleStillConsumes(t *testing.T) {
	const page = "console/src/pages/C5Page.tsx"
	c := c5Wait(t, page, true, true)
	m, err := store.ReadLandingGrantMaterial(ctx, c.w.rt, c.gsrc, c.op, true)
	ok(t, err)
	if m.Action != store.ActionGuardrailLanding || !slices.Equal(m.SoftAtRequest, []string{"rule_paths"}) || len(m.Problems) != 0 {
		t.Fatalf("%+v %v", m.SoftAtRequest, m.Problems)
	}
	g := c.grant(t, store.ActionGuardrailLanding)
	looser := strings.Replace(c5RuleBody, `allow: ["console/src/components/**/*.tsx"]`, `allow: ["console/src/components/**/*.tsx", "console/src/pages/*.tsx"]`, 1)
	ok(t, firstErr(c.signer.sign(c.w, ruleText("landing:dev", 2, "loosen", since, until, looser))))
	if _, d := c.gate(t); !d.Allowed {
		t.Fatalf("%+v", d)
	}
	var use, soft string
	ok(t, c.w.rt.QueryRow(ctx, `SELECT body_json->'landing_grant_use'->>'grant_id', body_json->'landing_grant_use'->>'soft_now' FROM agentrt.event
	  WHERE event_stream_id=$1 AND body_json->'landing'->>'kind'='main_intent'`, landingRepoStream).Scan(&use, &soft))
	if use != g.GrantID || soft != "[]" {
		t.Fatal(use, soft)
	}
	// Unpinned, the same change is not grantable (refused at preparation).
	d := c5WaitUnpinnedSoft(t, page)
	if d != "rule" {
		t.Fatal("unpinned soft change prepared:", d)
	}
}

// c5WaitUnpinnedSoft: an unpinned root of soft failures alone does not
// prepare (C-4's MayAwaitGrant): there is no wait to grant.
func c5WaitUnpinnedSoft(t *testing.T, path string) string {
	t.Helper()
	at := time.Now().UTC().Truncate(time.Second)
	t.Cleanup(store.FreezeEventClock(at))
	w := liveWorld(t, "none", at)
	store.FreezeEventClock(at.Add(-40 * time.Minute))
	root := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "C5 unpinned", BaseCommit: commitB})
	w.passRound(t, root, "h1", "h2", "h3")
	store.FreezeEventClock(at.Add(-30 * time.Minute))
	op, d, err := store.ClaimLanding(ctx, w.rt, c4Source(path), "prod", root, at)
	ok(t, err)
	if op != "" {
		return "claimed"
	}
	return d.Reason
}

// Serial: FreezeEventClock. T25: with a real grant, every unwaivable check
// still refuses: the repository stopped, the day's quota full, the mission
// stopped, the repository taken out of the rule, the rule expired. Zero
// pushes; the control is TestC5GrantWrittenAndConsumed.
func TestC5T25GrantNeverWaivesTheUnwaivable(t *testing.T) {
	cases := map[string]func(t *testing.T, c *c5){
		"stopped": func(t *testing.T, c *c5) {
			ok(t, store.InTx(ctx, c.w.rt, func(tx pgx.Tx) error {
				_, err := store.Append(ctx, tx, store.Event{Stream: landingRepoStream, Type: "note", Actor: "controller", TaskID: c.root,
					Body: map[string]any{"text": "stop", "landing_stop": map[string]any{"repo": "bifrost-platform", "op": c.op, "root": c.root, "generation": 1}}})
				return err
			}))
		},
		"quota": func(t *testing.T, c *c5) {
			fuller := strings.Replace(c5RuleBody, "max_landings_per_day: 5", "max_landings_per_day: 0", 1)
			ok(t, firstErr(c.signer.sign(c.w, ruleText("landing:dev", 2, "tighten", since, until, fuller))))
		},
		"repo out of the rule": func(t *testing.T, c *c5) {
			none := "tenant: dev\nrepos: []\nlimits: {max_files: 10, max_changed_lines: 300, max_landings_per_day: 5, max_in_flight_per_repo: 1}\n"
			ok(t, firstErr(c.signer.sign(c.w, ruleText("landing:dev", 2, "tighten", since, until, none))))
		},
		"rule missing": func(t *testing.T, c *c5) {
			// Before the rule took effect: no rule in force at the decision.
			store.FreezeEventClock(time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC))
		},
		"mission": func(t *testing.T, c *c5) {
			ok(t, store.InTx(ctx, c.w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-3", "stopped", "test") }))
		},
		"rule expired": func(t *testing.T, c *c5) {
			short := ruleText("landing:dev", 2, "tighten", since, c.at.Add(time.Minute).Format(time.RFC3339), c5RuleBody)
			ok(t, firstErr(c.signer.sign(c.w, short)))
			store.FreezeEventClock(c.at.Add(2 * time.Minute))
		},
	}
	want := map[string]string{"stopped": "stopped", "quota": "limit", "mission": "stale", "rule expired": "rule", "repo out of the rule": "rule", "rule missing": "rule"}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			c := c5Wait(t, c5Core, false, true)
			c.grant(t, store.ActionCoreLanding)
			change(t, c)
			auth, d := c.gate(t)
			if d.Allowed || auth.Op != "" || c.phase(t) == "main_intent" || d.Reason != want[name] {
				t.Fatalf("%s: %+v, want refusal %s", name, d, want[name])
			}
		})
	}
}

// Serial: FreezeEventClock. T28: the same approval executed again answers
// the grant it wrote, one record, its expiry unchanged; other parameters
// under the same approval are a conflict. T29: a second approval while the
// first grant is valid is refused; after the Owner revokes it, it succeeds.
func TestC5T28T29OneGrantPerApprovalAndOp(t *testing.T) {
	c := c5Wait(t, c5Core, false, true)
	a := c.approval(store.ActionCoreLanding, time.Minute)
	p := c.params(t)
	first, err := store.WriteLandingGrant(ctx, c.w.rt, c.gsrc, a, p)
	ok(t, err)
	store.FreezeEventClock(c.at.Add(time.Hour))
	again, err := store.WriteLandingGrant(ctx, c.w.rt, c.gsrc, a, p)
	ok(t, err)
	if again.ExpiresAt != first.ExpiresAt || again.GrantedAt != first.GrantedAt {
		t.Fatal("re-execution rewrote the grant", first, again)
	}
	q := p
	q.STGRelease = false
	_, err = store.WriteLandingGrant(ctx, c.w.rt, c.gsrc, a, q)
	refusedWith(t, err, "grant_conflict")
	_, err = store.WriteLandingGrant(ctx, c.w.rt, c.gsrc, c.approval(store.ActionCoreLanding, time.Minute), p)
	refusedWith(t, err, "grant_exists")
	if n := c.w.count(t, `SELECT count(*) FROM agentrt.event WHERE body_json ? 'landing_grant'`); n != 1 {
		t.Fatal(n)
	}
	r, err := store.RevokeLandingGrant(ctx, c.w.rt, first.GrantID)
	ok(t, err)
	if !r.Revoked || r.Already {
		t.Fatal(r)
	}
	if _, err = store.WriteLandingGrant(ctx, c.w.rt, c.gsrc, c.approval(store.ActionCoreLanding, time.Minute), p); err != nil {
		t.Fatal("after revocation:", err)
	}
}

// Serial: FreezeEventClock. T30: the append checks are the last line: only
// the controller writes a grant, closed and complete; only the Owner revokes;
// landing_grant_use rides only on the main_intent of its operation; a
// waiting operation's main_intent without it is refused. Control: the
// executor's own write (c.grant) and the gate's consumption succeed.
func TestC5T30AppendGuards(t *testing.T) {
	c := c5Wait(t, c5Core, false, true)
	m, err := store.ReadLandingGrantMaterial(ctx, c.w.rt, c.gsrc, c.op, true)
	ok(t, err)
	base := store.LandingGrant{V: "1", GrantID: "lg_t30", ApprovalID: "t30", ApprovalNumber: 3, Action: store.ActionCoreLanding,
		ParamsHash: strings.Repeat("ab", 32), Home: "prod", Env: "prod", Tenant: "dev", Repo: "bifrost-platform", Task: c.root, Attempt: m.Candidate.Attempt,
		Op: c.op, A: m.Candidate.A, Base: m.Candidate.Base, R: commitB, M0: commitA, CommitterDate: m.Candidate.CommitterDate,
		CandidateDigest: m.CandidateDigest, MaterialDigest: m.MaterialDigest, Waives: m.SoftAtRequest, SoftAtRequest: m.SoftAtRequest,
		Files: 1, Lines: 2, Rule: m.Rule, ChecksVersion: m.ChecksVersion,
		CI:      store.GrantCI{Pipeline: "bifrost-ci-platform", Run: "green", UID: "uid-green", CreatedAt: m.Candidate.CI.CreatedAt, Status: "Succeeded", SHA: commitB},
		Purpose: "landing_main", STGRelease: true, ApprovedAt: c.at.Add(-time.Minute).Format(time.RFC3339Nano), GrantedAt: c.at.Format(time.RFC3339Nano),
		ExpiresAt: c.at.Add(-time.Minute + store.LandingGrantTTL).Format(time.RFC3339Nano)}
	put := func(actor string, body map[string]any) error {
		return store.InTx(ctx, c.w.rt, func(tx pgx.Tx) error {
			_, err := store.Append(ctx, tx, store.Event{Stream: landingRepoStream, Type: "note", Actor: actor, TaskID: c.root, Body: body})
			return err
		})
	}
	grantBody := func(edit func(*store.LandingGrant)) map[string]any {
		g := base
		if edit != nil {
			edit(&g)
		}
		return map[string]any{"text": "t30", "landing_grant": g.Body()}
	}
	for name, attempt := range map[string]error{
		"operator":            put("operator", grantBody(nil)),
		"agentd":              put("agentd:h1", grantBody(nil)),
		"owner":               put("owner", grantBody(nil)),
		"guardrail for core":  put("controller", grantBody(func(g *store.LandingGrant) { g.Action = store.ActionGuardrailLanding })),
		"waives more":         put("controller", grantBody(func(g *store.LandingGrant) { g.Waives = []string{"core", "rule_paths", "size"} })),
		"expires late":        put("controller", grantBody(func(g *store.LandingGrant) { g.ExpiresAt = c.at.Add(48 * time.Hour).Format(time.RFC3339Nano) })),
		"another R":           put("controller", grantBody(func(g *store.LandingGrant) { g.R, g.CI.SHA = commitC, commitC })),
		"STG home":            put("controller", grantBody(func(g *store.LandingGrant) { g.Home = "stg" })),
		"missing field":       put("controller", map[string]any{"text": "t30", "landing_grant": dropKey(grantBody(nil)["landing_grant"].(map[string]any), "purpose")}),
		"extra field":         put("controller", map[string]any{"text": "t30", "landing_grant": withKey(grantBody(nil)["landing_grant"].(map[string]any), "extra", "x")}),
		"with a landing rec.": put("controller", withKey(grantBody(nil), "landing", newLandingRecord(c.op, "ci_seen", "run", "green", "status", "Succeeded", "R", commitB, "created_at", c.ci.CreatedAt.Format(time.RFC3339Nano)))),
		"main_intent bare":    put("controller", map[string]any{"text": "t30", "landing": newLandingRecord(c.op, "main_intent", "R", commitB, "M0", commitA)}),
	} {
		if attempt == nil {
			t.Errorf("%s: appended", name)
		}
	}
	// Control: the controller's complete grant is accepted.
	ok(t, put("controller", grantBody(nil)))
	// A revocation by anyone but the Owner is refused; the Owner's is not.
	revoke := map[string]any{"text": "r", "landing_grant_revoked": map[string]any{"v": "1", "grant_id": "lg_t30", "by": "owner", "at": c.at.Format(time.RFC3339Nano)}}
	if put("controller", revoke) == nil || put("operator", revoke) == nil {
		t.Fatal("revoked by someone else")
	}
	ok(t, put("owner", revoke))
	if put("owner", revoke) == nil {
		t.Fatal("revoked twice")
	}
}

func dropKey(m map[string]any, k string) map[string]any {
	out := map[string]any{}
	for key, v := range m {
		if key != k {
			out[key] = v
		}
	}
	return out
}

func withKey(m map[string]any, k string, v any) map[string]any {
	out := map[string]any{k: v}
	for key, x := range m {
		if key != k {
			out[key] = x
		}
	}
	return out
}
