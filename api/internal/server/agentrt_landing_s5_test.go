package server

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

func s5ServerKey() landing.S5CIKey {
	h := strings.Repeat("a", 64)
	return landing.S5CIKey{Schema: 2, Env: "prod", Tenant: "dev", Repo: "bifrost-platform", Task: "t_01ARZ3NDEKTSV4RRFFQ69G5FAV", Attempt: "a_01ARZ3NDEKTSV4RRFFQ69G5FAV", Op: "land:t_01ARZ3NDEKTSV4RRFFQ69G5FAV:1",
		Base: strings.Repeat("1", 40), A: strings.Repeat("2", 40), M0: strings.Repeat("3", 40), R: strings.Repeat("4", 40), BaselineTree: strings.Repeat("5", 40), ResultTree: strings.Repeat("6", 40),
		UISHA: strings.Repeat("7", 40), RuleVersion: 3, RuleDigest: h, ProfileDigest: h, CheckerImageDigest: "sha256:" + h, ToolchainDigest: h, CallersDigest: h, TypesDigest: h, TestsDigest: h,
		ReviewBindingDigest: h, PatchEvidenceDigest: h, Generation: 1}
}

// The live wiring attaches all three S5 callbacks next to the unchanged
// legacy CI callbacks. With the shipped UNBUILT trust root every S5 callback
// refuses before touching Kubernetes.
func TestLandingS5WiredAndUnbuiltRefuses(t *testing.T) {
	t.Setenv("AGENTRT_LANDING_LIVE_REPOS", "")
	_ = os.Unsetenv("AGENTRT_LANDING_LIVE_REPOS")
	t.Setenv("AGENTRT_LANDING_LIVE", "on")
	t.Setenv("AGENTRT_LANDER_USERNAME_FILE", "/not-read/username")
	t.Setenv("AGENTRT_LANDER_PASSWORD_FILE", "/not-read/password")
	s := &Server{cluster: cluster.NewHandler(&config.Config{}, nil)}
	c := &controller.Controller{Env: "prod", DB: &startup704DB{}, LandingCards: completeCardQuestions()}
	s.wireLandingDrill(c)
	if c.Drill == nil {
		t.Fatal("drill not wired")
	}
	f := c.Drill.Facts
	if f.S5Policy == nil || f.S5CI == nil || f.S5Patch == nil || c.Drill.EnsureS5CI == nil {
		t.Fatal("S5 callbacks not wired")
	}
	if f.Result == nil || f.History == nil || c.Drill.CI == nil || c.Drill.EnsureCI == nil || f.GrantCI == nil || c.Drill.EnsurePinnedCI == nil {
		t.Fatal("legacy callbacks lost")
	}
	if p, err := f.S5Policy(context.Background()); err == nil || !strings.Contains(err.Error(), "UNBUILT") || p != (landing.S5Policy{}) {
		t.Fatal("UNBUILT trust root yielded a policy", p, err)
	}
	k := s5ServerKey()
	if uid, err := c.Drill.EnsureS5CI(context.Background(), k); err == nil || uid != "" || !strings.Contains(err.Error(), "UNBUILT") {
		t.Fatal("UNBUILT ensure", uid, err)
	}
	if ci, err := f.S5CI(context.Background(), k, "server-run-uid"); err == nil || ci.IdentityVerified || ci.Status == "Succeeded" {
		t.Fatal("UNBUILT read", ci, err)
	}
}

// landingRepoFacts keeps the S5 readers server-owned: policy and CI pass
// through untouched (nil stays nil), and S5Patch gets the same per-repository
// rule check as every other Git read.
func TestLandingRepoFactsS5PassThrough(t *testing.T) {
	db := &startup704DB{signed: true}
	c := &controller.Controller{DB: db}
	policies, cis, patches := 0, 0, 0
	src := store.LandingSource{
		S5Policy: func(context.Context) (landing.S5Policy, error) {
			policies++
			return landing.S5Policy{}, errors.New("stub")
		},
		S5CI: func(_ context.Context, k landing.S5CIKey, uid string) (landing.S5CIResult, error) {
			cis++
			return landing.S5CIResult{Key: k, UID: uid}, nil
		},
		S5Patch: func(context.Context, string, string, string, string, string, string) (landing.PatchEvidence, error) {
			patches++
			return landing.PatchEvidence{}, nil
		},
	}
	guarded := (&Server{}).landingRepoFacts(c, src)
	if _, err := guarded.S5Policy(context.Background()); err == nil || policies != 1 {
		t.Fatal("policy pass-through", policies)
	}
	if got, err := guarded.S5CI(context.Background(), s5ServerKey(), "uid"); err != nil || got.UID != "uid" || cis != 1 {
		t.Fatal("CI pass-through", cis, err)
	}
	if _, err := guarded.S5Patch(context.Background(), "bifrost-platform", "t", "b", "a", "", ""); err != nil || patches != 1 {
		t.Fatal("platform patch read", err, patches)
	}
	db.signed = false
	if _, err := guarded.S5Patch(context.Background(), "bifrost-trade-infra", "t", "b", "a", "", ""); err == nil || patches != 1 {
		t.Fatal("unsigned repository patch read", err, patches)
	}
	empty := (&Server{}).landingRepoFacts(c, store.LandingSource{})
	if empty.S5Policy != nil || empty.S5CI != nil || empty.S5Patch != nil {
		t.Fatal("nil S5 reader replaced by a non-nil one")
	}
}

// When the wired policy is unavailable the store leaves Facts.S5Policy zero.
// Then an enrolled S5 candidate is refused, while a lane S5 does not touch
// keeps exactly the legacy decision.
func TestLandingS5UnbuiltKeepsLegacyLanes(t *testing.T) {
	policy, err := wireLandingS5(store.LandingSource{}, nil).S5Policy(context.Background())
	if err == nil {
		t.Fatal("UNBUILT policy available")
	}
	now := time.Date(2026, 10, 30, 13, 0, 0, 0, time.UTC)
	epoch := now.Add(-24 * time.Hour)
	facts := func(path string, allow ...string) landing.Facts {
		ch := rules.LandingChange{Kind: "M", OldPath: path, NewPath: path, OldMode: "100644", NewMode: "100644", Added: 1, Deleted: 1, AddedText: "x"}
		return landing.Facts{Environment: "prod", Mode: "live", Tenant: "dev", Repo: "bifrost-platform", Task: "t_01ARZ3NDEKTSV4RRFFQ69G5FAV", Attempt: "a_01ARZ3NDEKTSV4RRFFQ69G5FAV",
			A: strings.Repeat("2", 40), Base: strings.Repeat("1", 40), MissionState: "active", StepState: "active", TaskState: "accepted", SingleCommit: true, DependenciesEffective: true,
			AcceptedAt: epoch.Add(time.Minute), MissionCreated: epoch, EnabledAt: epoch, History: landing.MainHistory{State: landing.Never}, S5Policy: policy,
			Changes: []rules.LandingChange{ch}, DeployChanges: []rules.LandingChange{ch},
			Rule: &rules.Rule{Header: rules.Header{ID: "landing:dev", Version: 3, Effective: epoch.Add(-12 * time.Hour), Expires: epoch.Add(30 * 24 * time.Hour)}, Kind: rules.KindLanding,
				Landing: &rules.Landing{Tenant: "dev", Repos: []rules.LandingRepo{{Repo: "bifrost-platform", Allow: allow, STGRelease: "bifrost-deliver-platform"}},
					Limits: rules.LandingLimits{MaxFiles: 10, MaxChangedLines: 300, MaxLandingsPerDay: 5, MaxInFlightPerRepo: 1}}}}
	}
	s5 := rules.S5Paths()[0]
	css := "console/src/styles/app.css"
	// Legacy lane, legacy rule: control is allowed and S5 never engages.
	legacy := facts(css, "console/src/styles/**")
	if d := landing.PreparationDecision(legacy, now); !d.Allowed || !reflect.DeepEqual(d, landing.EligibleToClaim(legacy, now)) {
		t.Fatal("legacy lane changed", d)
	}
	// Same lane under a rule that also enrolls an S5 path: unchanged.
	mixed := facts(css, "console/src/styles/**", s5)
	if d := landing.PreparationDecision(mixed, now); !d.Allowed || !reflect.DeepEqual(d, landing.EligibleToClaim(mixed, now)) || landing.S5AutomaticRoute(mixed) {
		t.Fatal("unrelated lane depends on checker availability", d)
	}
	// The enrolled candidate itself: refused, never prepared without material.
	candidate := facts(s5, s5)
	if !landing.S5AutomaticRoute(candidate) {
		t.Fatal("candidate not routed to S5")
	}
	if d := landing.PreparationDecision(candidate, now); d.Allowed || d.Reason != "S5_EVIDENCE_MISSING" {
		t.Fatal("UNBUILT S5 candidate prepared", d)
	}
	if why, _ := landing.AuthorizeLanding(candidate, landing.VerifiedS5{}, now); why == "" {
		t.Fatal("UNBUILT S5 candidate authorized")
	}
}

// B4: rule signing is wired to the same embedded trust root, which is
// UNBUILT in this tree, so a rule enrolling S5 paths cannot be signed yet.
func TestLandingS5SigningOrderUsesEmbeddedTrustRoot(t *testing.T) {
	if err := landingS5Deployable(context.Background()); err == nil {
		t.Fatal("UNBUILT trust root reported deployable")
	}
	saved := store.S5SigningReady
	t.Cleanup(func() { store.S5SigningReady = saved })
	s := &Server{}
	s.wireAgentRuntime(config.RoleAPI)
	if store.S5SigningReady == nil || store.S5SigningReady(context.Background()) == nil {
		t.Fatal("signing order not wired to the trust root")
	}
}
