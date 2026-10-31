package landingci

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ktesting "k8s.io/client-go/testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules/s5check"
)

const s5LandingPath = "console/src/lib/patrol/cronSchedule.ts"

var s5LandingNow = time.Date(2026, 10, 30, 13, 0, 0, 0, time.UTC)

// s5LandingFacts are STG dry-run facts for one enrolled S5 modification,
// with the deployment material equal to s5TestContract's pinned material.
// The key is produced by line B's NewS5CIKey, not written by hand.
func s5LandingFacts(t *testing.T) (landing.Facts, S5CIKey) {
	t.Helper()
	k0 := s5TestKey()
	epoch := s5LandingNow.Add(-24 * time.Hour)
	change := rules.LandingChange{Kind: "M", OldPath: s5LandingPath, NewPath: s5LandingPath, OldMode: "100644", NewMode: "100644", Added: 1, Deleted: 1, AddedText: "return value + 1"}
	f := landing.Facts{Environment: k0.Env, Mode: "dry_run", Tenant: k0.Tenant, Repo: k0.Repo, Task: k0.Task, Attempt: k0.Attempt, A: k0.A, Base: k0.Base,
		MissionState: "active", StepState: "active", TaskState: "accepted", SingleCommit: true, DependenciesEffective: true,
		AcceptedAt: epoch.Add(time.Minute), MissionCreated: epoch, EnabledAt: epoch, History: landing.MainHistory{State: landing.Never},
		Changes: []rules.LandingChange{change}, DeployChanges: []rules.LandingChange{change},
		Rule: &rules.Rule{Header: rules.Header{ID: "landing:dev", Version: k0.RuleVersion, Effective: epoch.Add(-12 * time.Hour), Expires: epoch.Add(30 * 24 * time.Hour)}, Kind: rules.KindLanding,
			Landing: &rules.Landing{Tenant: "dev", Repos: []rules.LandingRepo{{Repo: "bifrost-platform", Allow: []string{s5LandingPath}, STGRelease: "bifrost-deliver-platform"}},
				Limits: rules.LandingLimits{MaxFiles: 10, MaxChangedLines: 300, MaxLandingsPerDay: 5, MaxInFlightPerRepo: 1}}}}
	p, err := s5TestContract(t).policy()
	if err != nil {
		t.Fatal(err)
	}
	f.S5Policy = p
	d := strings.Repeat("c", 64)
	stable := strings.Repeat("9", 40)
	f.S5Patch = landing.PatchEvidence{A: f.A, ParentA: f.Base, Base: f.Base, SourcePatchSHA256: d, SourceStablePatchID: stable, DiffAlgorithmVersion: "git-stable-v1",
		OldNewPathsAndModes: []landing.S5PathMode{{OldPath: s5LandingPath, NewPath: s5LandingPath, OldMode: "100644", NewMode: "100644", Kind: "M"}},
		M0:                  k0.M0, R: k0.R, ParentR: k0.M0, ReplayMatch: true, ReplayPatchSHA256: strings.Repeat("b", 64), ReplayStablePatchID: stable, BaselineTree: k0.BaselineTree, ResultTree: k0.ResultTree}
	f.S5Review = landing.ReviewBinding{Repo: f.Repo, Task: f.Task, AcceptedAttempt: f.Attempt, A: f.A, Base: f.Base, AcceptanceVerificationID: "v_accept", ImplementerAttempt: f.Attempt,
		ImplementerExecutor: "exec:codex", ImplementerProvider: "openai", ReviewVerificationID: "v_review", ReviewerTask: "t_review", ReviewerAttempt: "a_review",
		ReviewerExecutor: "exec:claude", ReviewerProvider: "anthropic", Verdict: "pass", ReviewArtifactID: "sha256:" + d, ReviewArtifactSHA256: d, ReviewedPatchSHA256: d, ReviewedStablePatchID: stable}
	// Prepared before the fixture run's creationTimestamp (12:00Z).
	f.Operation = landing.Operation{Task: f.Task, Attempt: f.Attempt, A: f.A, R: k0.R, M0: k0.M0, Phase: "branch_pushed", PreparedAt: time.Date(2026, 10, 30, 11, 0, 0, 0, time.UTC)}
	k, why := landing.NewS5CIKey(f, k0.Op, 1)
	if why != "" {
		t.Fatal(why)
	}
	kd, err := k.Digest()
	if err != nil {
		t.Fatal(err)
	}
	f.Operation.S5 = landing.S5State{Required: true, Key: k, KeyDigest: kd}
	return f, k
}

// The trusted path end to end: B's key → Ensure (fake Kubernetes assigns
// the UID) → B binds the UID → Read → conversion → B's VerifyS5Evidence and
// AuthorizeLanding. Every negative below changes one thing from this.
func TestS5LandingWiringSuccessControl(t *testing.T) {
	f, k := s5LandingFacts(t)
	c := s5TestContract(t)

	// One key, one encoding: B's persisted digest is the transport's.
	name, _ := k.RunName()
	params := s5Params(k)
	if params["keyDigest"] != f.Operation.S5.KeyDigest || s5Hash([]byte(params["key"])) != f.Operation.S5.KeyDigest ||
		name != "s5-"+f.Operation.S5.KeyDigest[:40]+"-g1" {
		t.Fatal("persisted key digest differs from the CI parameters")
	}
	var echoed S5CIKey
	if err := decodeS5([]byte(params["key"]), &echoed); err != nil || echoed != k {
		t.Fatal("key parameter does not round-trip", err)
	}

	client, d := s5TestClient()
	d.PrependReactor("create", "pipelineruns", func(a ktesting.Action) (bool, runtime.Object, error) {
		o := a.(ktesting.CreateAction).GetObject().(*unstructured.Unstructured)
		o.SetUID(types.UID("server-run-uid"))
		if err := d.Tracker().Create(pipelineRunGVR, o, "cicd"); err != nil {
			t.Fatal(err)
		}
		return true, o, nil
	})
	uid, err := client.ensureLandingS5CI(context.Background(), k, c)
	if err != nil || uid != "server-run-uid" {
		t.Fatal("ensure", uid, err)
	}
	created, err := d.Resource(pipelineRunGVR).Namespace("cicd").Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := namedValues(*created, "spec", "params"); got["keyDigest"] != f.Operation.S5.KeyDigest || got["key"] != params["key"] {
		t.Fatal("created run carries another key")
	}
	f.Operation.S5.UID = uid

	_, _, objs := s5Fixture(t, k)
	reader, _ := s5TestClient(objs...)
	ci, err := reader.landingS5CI(context.Background(), k, uid, c)
	if err != nil {
		t.Fatal(err)
	}
	if !ci.IdentityVerified || ci.Status != "Succeeded" || ci.Reason != "" || ci.Evidence == nil || ci.Key != k || ci.UID != uid || ci.Run != name || ci.KeyDigest != f.Operation.S5.KeyDigest {
		t.Fatalf("conversion: %+v", ci)
	}
	if !ci.CreatedAt.Equal(time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)) || !ci.CompletedAt.Equal(time.Date(2026, 10, 30, 12, 1, 0, 0, time.UTC)) {
		t.Fatal("run times", ci.CreatedAt, ci.CompletedAt)
	}
	v, why := landing.VerifyS5Evidence(ci, f, f.S5Policy, f.S5Review)
	if why != "" {
		t.Fatal("verify", why)
	}
	if why, soft := landing.AuthorizeLanding(f, v, s5LandingNow); why != "" || len(soft) != 0 {
		t.Fatal("authorize", why, soft)
	}

	t.Run("zero result authorizes nothing", func(t *testing.T) {
		if _, why := landing.VerifyS5Evidence(landing.S5CIResult{}, f, f.S5Policy, f.S5Review); why == "" {
			t.Fatal("zero CI result verified")
		}
		if why, _ := landing.AuthorizeLanding(f, landing.VerifiedS5{}, s5LandingNow); why == "" {
			t.Fatal("zero VerifiedS5 authorized")
		}
	})
	t.Run("other UID", func(t *testing.T) {
		got, err := reader.landingS5CI(context.Background(), k, "another-uid", c)
		if err != nil || got.IdentityVerified || got.Status == "Succeeded" {
			t.Fatalf("%+v %v", got, err)
		}
		g := f
		g.Operation.S5.UID = "another-uid"
		if _, why := landing.VerifyS5Evidence(got, g, g.S5Policy, g.S5Review); why != "S5_CI_IDENTITY" {
			t.Fatal(why)
		}
	})
	t.Run("missing UID", func(t *testing.T) {
		got, err := reader.landingS5CI(context.Background(), k, "", c)
		if err != nil || got.IdentityVerified || got.Status == "Succeeded" {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("running", func(t *testing.T) {
		_, _, o := s5Fixture(t, k)
		setField(t, o[0], []any{map[string]any{"type": "Succeeded", "status": "Unknown", "reason": "Running"}}, "status", "conditions")
		r, _ := s5TestClient(o...)
		got, err := r.landingS5CI(context.Background(), k, uid, c)
		if err != nil || got.Status != "Running" || got.IdentityVerified || got.Evidence != nil {
			t.Fatalf("%+v %v", got, err)
		}
		if _, why := landing.VerifyS5Evidence(got, f, f.S5Policy, f.S5Review); why != "S5_EVIDENCE_MISSING" {
			t.Fatal(why)
		}
	})
	t.Run("not found", func(t *testing.T) {
		r, _ := s5TestClient()
		got, err := r.landingS5CI(context.Background(), k, uid, c)
		if err != nil || got.Status != "Missing" || got.IdentityVerified {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("failed run", func(t *testing.T) {
		_, _, o := s5Fixture(t, k)
		setField(t, o[0], []any{map[string]any{"type": "Succeeded", "status": "False"}}, "status", "conditions")
		r, _ := s5TestClient(o...)
		got, err := r.landingS5CI(context.Background(), k, uid, c)
		if err != nil || got.Status != "Failed" || got.IdentityVerified {
			t.Fatalf("%+v %v", got, err)
		}
		if _, why := landing.VerifyS5Evidence(got, f, f.S5Policy, f.S5Review); why == "" {
			t.Fatal("failed run verified")
		}
	})
	t.Run("terminal run without results is a failure, not a wait", func(t *testing.T) {
		_, _, o := s5Fixture(t, k)
		setField(t, o[0], []any{}, "status", "results")
		r, _ := s5TestClient(o...)
		got, err := r.landingS5CI(context.Background(), k, uid, c)
		if err != nil || got.Status != "Failed" || got.IdentityVerified {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("forged evidence after digest", func(t *testing.T) {
		ev := S5Evidence{Identity: S5RunIdentity{Run: name, UID: uid, KeyDigest: f.Operation.S5.KeyDigest, Generation: 1}, Status: "Succeeded",
			Content: []byte(s5TestResults(k, uid)["evidence"]), CreatedAt: ci.CreatedAt, CompletedAt: ci.CompletedAt}
		ev.Digest = s5Hash(ev.Content)
		if got := s5LandingResult(k, ev.Identity, ev); !got.IdentityVerified {
			t.Fatal("control conversion")
		}
		forged := ev
		forged.Content = []byte(strings.Replace(string(ev.Content), `"passed":true`, `"passed":true `, 1))
		if got := s5LandingResult(k, ev.Identity, forged); got.IdentityVerified || got.Status == "Succeeded" {
			t.Fatal("content not matching its digest adopted")
		}
		forged = ev
		forged.Reason = "S5_BINDING"
		if got := s5LandingResult(k, ev.Identity, forged); got.IdentityVerified || got.Status == "Succeeded" {
			t.Fatal("Succeeded with a reason adopted")
		}
		forged = ev
		forged.Identity.UID = "x"
		if got := s5LandingResult(k, ev.Identity, forged); got.IdentityVerified {
			t.Fatal("other identity adopted")
		}
		forged = ev
		forged.CompletedAt = time.Time{}
		if got := s5LandingResult(k, ev.Identity, forged); got.IdentityVerified {
			t.Fatal("no completion time adopted")
		}
	})
	t.Run("key material not deployed", func(t *testing.T) {
		other := c
		m := *c.Materials
		m.TestsDigest = strings.Repeat("d", 64)
		other.Materials = &m
		r, _ := s5TestClient()
		if _, err := r.ensureLandingS5CI(context.Background(), k, other); err == nil || !strings.Contains(err.Error(), "S5_CI_IDENTITY") {
			t.Fatal("ensure under other deployment material", err)
		}
		if _, err := reader.landingS5CI(context.Background(), k, uid, other); err == nil {
			t.Fatal("read under other deployment material")
		}
	})
}

// The shipped trust root is UNBUILT: every S5 callback refuses and no run is
// created. Built-but-unpinned and wrong-profile roots refuse too; only a root
// whose profile is the reviewed compiled-in manifest yields a policy.
func TestS5LandingUnbuiltDeploymentRefuses(t *testing.T) {
	if _, err := S5Policy(context.Background()); err == nil || !strings.Contains(err.Error(), "UNBUILT") {
		t.Fatal("UNBUILT policy available", err)
	}
	_, k := s5LandingFacts(t)
	client, d := s5TestClient()
	if uid, err := client.EnsureLandingS5CI(context.Background(), k); err == nil || uid != "" {
		t.Fatal("UNBUILT ensure", uid, err)
	}
	for _, a := range d.Actions() {
		if a.GetVerb() == "create" {
			t.Fatal("UNBUILT deployment created a run")
		}
	}
	if got, err := client.LandingS5CI(context.Background(), k, "server-run-uid"); err == nil || got.IdentityVerified {
		t.Fatal("UNBUILT read", got, err)
	}

	reviewed, err := s5checkEmbeddedDigest()
	if err != nil {
		t.Fatal(err)
	}
	built := s5TestContract(t)
	built.Materials.ProfileDigest = reviewed
	if p, err := built.policy(); err != nil || p.ProfileDigest != reviewed || p.CheckerImageDigest != k.CheckerImageDigest || !p.Valid() {
		t.Fatal("built control", p, err)
	}
	if err := built.deployable(); err != nil {
		t.Fatal("reviewed profile refused", err)
	}
	if err := s5TestContract(t).deployable(); err == nil || !strings.Contains(err.Error(), "reviewed manifest") {
		t.Fatal("unreviewed profile deployable", err)
	}
	unpinned := built
	unpinned.Materials = nil
	if _, err := unpinned.policy(); err == nil {
		t.Fatal("unpinned materials accepted")
	}
	zeroUI := built
	m := *built.Materials
	m.UISHA = strings.Repeat("0", 40)
	zeroUI.Materials = &m
	if _, err := zeroUI.policy(); err == nil {
		t.Fatal("unpinned UI accepted")
	}
	unbuilt := built
	unbuilt.Image = s5ImageRepository + "sha256:" + strings.Repeat("0", 64)
	if _, err := unbuilt.policy(); err == nil {
		t.Fatal("UNBUILT image accepted")
	}
	otherRepo := built
	otherRepo.Image = "registry.example/other@" + k.CheckerImageDigest
	if _, err := otherRepo.policy(); err == nil {
		t.Fatal("foreign image repository accepted")
	}
}

func s5checkEmbeddedDigest() (string, error) { return s5check.EmbeddedPolicyDigest() }

// The shipped trust root carries the infra pins.py materials placeholder in
// exactly the s5Materials shape: every field parses and is all zeros, so the
// UNBUILT root stays closed on both the image and the material checks.
func TestS5ShippedMaterialsPlaceholderShape(t *testing.T) {
	var raw struct {
		Materials map[string]string `json:"materials"`
	}
	if err := json.Unmarshal(s5ContractJSON, &raw); err != nil || len(raw.Materials) != 6 {
		t.Fatalf("materials member: %v %v", raw.Materials, err)
	}
	c, err := loadS5Contract()
	if err != nil || c.Materials == nil {
		t.Fatal("materials not parsed", err)
	}
	m := *c.Materials
	for _, v := range []string{m.ProfileDigest, m.ToolchainDigest, m.CallersDigest, m.TypesDigest, m.TestsDigest} {
		if v != strings.Repeat("0", 64) {
			t.Fatal("shipped material is not the UNBUILT placeholder", m)
		}
	}
	if m.UISHA != strings.Repeat("0", 40) {
		t.Fatal("shipped UI pin is not the UNBUILT placeholder", m)
	}
	built := c
	built.Image = s5ImageRepository + s5TestKey().CheckerImageDigest
	if _, err := built.policy(); err == nil || !strings.Contains(err.Error(), "S5_EVIDENCE_MISSING") {
		t.Fatal("placeholder materials accepted under a built image", err)
	}
}
