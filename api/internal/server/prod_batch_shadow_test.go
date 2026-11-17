//bifrost:owner delivery

package server

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/headrelease"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
)

// Origin of every test in this file. 2026-11-16: 19 PROD releases of the
// platform in one evening, each by a seat holding the window ~129 s; the
// coordinator asked for a shadow of "the platform releases PROD in batches"
// before anyone turns it on. Each test says below what it guards.

func shadowRows(t *testing.T) (prod, source headrelease.Deliverable) {
	t.Helper()
	table, _, err := headrelease.LoadTable()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range table.Deliverables {
		switch d.Name {
		case prodShadowRow:
			prod = d
		case prodShadowSource:
			source = d
		}
	}
	if prod.Pipeline != platformProd || source.Pipeline != platformStg {
		t.Fatalf("the shipped table's rows: prod %q source %q", prod.Pipeline, source.Pipeline)
	}
	return prod, source
}

// shadowWorld is a moment at which everything holds, as reads.
type shadowWorld struct {
	now      time.Time
	records  map[string]releases.Record // "lane/env"
	errs     map[string]error
	asked    []map[string]any
	decision releasepolicy.Decision
	cards    []approvals.Approval
	holder   string
	ci       string
}

func (w *shadowWorld) reads() prodShadowReads {
	return prodShadowReads{
		now: func() time.Time { return w.now },
		rule: func(pipeline string) (releases.Rule, bool) {
			switch pipeline {
			case platformStg:
				return releases.Rule{Lane: "platform", Env: "stg", Deploys: true}, true
			case platformProd:
				return releases.Rule{Lane: "platform", Env: "prod", Deploys: true}, true
			}
			return releases.Rule{}, false
		},
		current: func(_ context.Context, lane, env string) (releases.Record, error) {
			return w.records[lane+"/"+env], w.errs[lane+"/"+env]
		},
		ci: func(_ context.Context, repo, commit string) (string, string, error) {
			return w.ci, "ci-platform-" + commit[:4], nil
		},
		policy: func(_ context.Context, params map[string]any) releasepolicy.Decision {
			w.asked = append(w.asked, params)
			return w.decision
		},
		cards: func() ([]approvals.Approval, error) { return w.cards, nil },
		window: func(context.Context) (string, string, bool, error) {
			return w.holder, "bifrost-platform", w.holder != "", nil
		},
		frozen: func(context.Context) (bool, string) { return false, "" },
	}
}

func newShadowWorld() (*shadowWorld, string, string, string) {
	now := time.Date(2026, 11, 16, 23, 45, 0, 0, time.UTC)
	p, c, ui := rptest.SHA("prod-plat"), rptest.SHA("stg-plat"), rptest.SHA("stg-ui")
	return &shadowWorld{
		now: now,
		records: map[string]releases.Record{
			"platform/stg": {Run: "bifrost-deliver-platform-c", CompletedAt: now.Add(-11 * time.Minute),
				Repos: map[string]releases.RepoBuild{"bifrost-platform": {SHA: c}, "bifrost-ui": {SHA: ui}, "bifrost-trade-infra": {SHA: rptest.SHA("infra")}}},
			"platform/prod": {Run: "bifrost-deliver-platform-prod-p", CompletedAt: now.Add(-25 * time.Minute),
				Repos: map[string]releases.RepoBuild{"bifrost-platform": {SHA: p}, "bifrost-ui": {SHA: rptest.SHA("prod-ui")}}},
		},
		errs:     map[string]error{},
		decision: releasepolicy.Decision{PolicyID: "rp-1", Reasons: []string{"release window: no release window is open; hold one first: release.sh hold --what <repo>"}},
		ci:       "Succeeded",
	}, p, c, ui
}

// Origin: see the top of the file. Guards where each fact comes from: the
// candidate is what the STG record says STG runs (not main's head), soaked
// counts from that delivery's end, the policy is asked the request a seat
// would send for it (the candidate as revision, the ui commit of the same STG
// delivery beside it, and no infraRevision: the PROD pipeline takes none),
// and a policy whose only reason is the window the shadow does not hold is
// "covers". All green gathers to "would release C from P".
func TestProdShadowGathersTheCandidateFromWhatStgRuns(t *testing.T) {
	prod, source := shadowRows(t)
	w, p, c, ui := newShadowWorld()
	f := w.reads().gather(context.Background(), prod, source, headrelease.DefaultProdSoak, headrelease.DefaultProdGap)
	d := headrelease.DecideProd(f)
	if !d.WouldRelease || d.From != p || d.To != c {
		t.Fatalf("decision %+v from facts %+v", d, f)
	}
	if len(w.asked) != 1 {
		t.Fatalf("the policy was asked %d times", len(w.asked))
	}
	asked := w.asked[0]
	beside, _ := asked["params"].(map[string]string)
	if asked["name"] != platformProd || asked["revision"] != c || len(beside) != 1 || beside["uiRevision"] != ui {
		t.Fatalf("asked the policy %v", asked)
	}
	if f.Stg.Since != w.now.Add(-11*time.Minute) || f.Prod.Since != w.now.Add(-25*time.Minute) || f.CIRun == "" {
		t.Fatalf("facts %+v", f)
	}
}

// Origin: see the top of the file. Guards each read's failure and each
// "not now" as the condition it belongs to: an unsettled STG asks nothing
// about a candidate (there is none); an unsettled PROD is a wait; a record
// read that fails otherwise is unknown; the policy's own reasons survive
// with the window's set aside; a held window, an open card and a time-out
// of the policy's evaluation each land on their own condition.
func TestProdShadowGatherNamesWhatStandsInTheWay(t *testing.T) {
	prod, source := shadowRows(t)
	gather := func(edit func(w *shadowWorld), ctx context.Context) (headrelease.ProdDecision, *shadowWorld) {
		w, _, _, _ := newShadowWorld()
		edit(w)
		return headrelease.DecideProd(w.reads().gather(ctx, prod, source, headrelease.DefaultProdSoak, headrelease.DefaultProdGap)), w
	}
	bg := context.Background()

	d, w := gather(func(w *shadowWorld) {
		w.errs["platform/stg"] = fmt.Errorf("%w: run x may have changed platform/stg", releases.ErrUnsettled)
	}, bg)
	if d.WouldRelease || strings.Join(d.Failing, ",") != headrelease.CondStgSettled || len(d.Unknown) != 0 || len(w.asked) != 0 || d.To != "" {
		t.Fatalf("unsettled stg: %+v, policy asked %d", d, len(w.asked))
	}
	d, _ = gather(func(w *shadowWorld) { w.errs["platform/prod"] = releases.ErrNoDeployment }, bg)
	if d.WouldRelease || d.Verdict != "wait: prod is not settled" {
		t.Fatalf("no prod record: %+v", d)
	}
	d, _ = gather(func(w *shadowWorld) { w.errs["platform/prod"] = errors.New("connection refused") }, bg)
	if d.WouldRelease || strings.Join(d.Unknown, ",") != headrelease.CondProdSettled {
		t.Fatalf("prod records unreadable: %+v", d)
	}
	d, _ = gather(func(w *shadowWorld) {
		w.decision.Reasons = append(w.decision.Reasons, "bifrost-platform: CI did not succeed for aaaaaaaa")
	}, bg)
	if d.WouldRelease || strings.Join(d.Failing, ",") != headrelease.CondPolicy || d.Reasons[headrelease.CondPolicy] != "bifrost-platform: CI did not succeed for aaaaaaaa" {
		t.Fatalf("policy reason: %+v", d)
	}
	d, _ = gather(func(w *shadowWorld) {
		w.holder = "ci-w1"
		w.cards = []approvals.Approval{
			{Number: 524, Action: "start_pipeline_run", Status: approvals.StatusPending, Params: map[string]any{"name": platformProd}},
			{Number: 525, Action: "start_pipeline_run", Status: approvals.StatusApproved, Params: map[string]any{"name": platformProd}},
			{Number: 526, Action: "start_pipeline_run", Status: approvals.StatusPending, Params: map[string]any{"name": "bifrost-deliver-prod"}}, // Trade's
			{Number: 527, Action: "start_pipeline_run", Status: approvals.StatusExecuted, Params: map[string]any{"name": platformProd}},
			{Number: 528, Action: "apply_manifest", Status: approvals.StatusPending, Params: map[string]any{"name": platformProd}},
		}
	}, bg)
	if d.WouldRelease || strings.Join(d.Failing, ",") != headrelease.CondNoOpenCard+","+headrelease.CondWindowFree ||
		d.Reasons[headrelease.CondNoOpenCard] != "#524 pending, #525 approved" || !strings.Contains(d.Reasons[headrelease.CondWindowFree], "ci-w1") {
		t.Fatalf("card and window: %+v", d)
	}
	done, cancel := context.WithCancel(bg)
	cancel()
	d, _ = gather(func(*shadowWorld) {}, done)
	if d.WouldRelease || !strings.Contains(strings.Join(d.Unknown, ","), headrelease.CondPolicy) {
		t.Fatalf("an evaluation that ran out of time: %+v", d)
	}
	// No engine, no ledger, no records: unknown, never a release.
	d = headrelease.DecideProd(prodShadowReads{now: time.Now}.gather(bg, prod, source, 0, 0))
	if d.WouldRelease || len(d.Unknown) == 0 {
		t.Fatalf("nothing wired: %+v", d)
	}
}

// Origin: see the top of the file; the brief's "the real check includes
// window_held_by_requester; the shadow must evaluate every clause except the
// window". Guards, against the real engine, that the one reason set aside is
// the window's: with the window not held the engine refuses a release it
// otherwise covers, and policySansWindow reads that as covered; a red CI
// beside it is kept as a reason. If the engine rewords its window reason
// this fails, instead of the shadow saying "policy does not cover" all night.
func TestProdShadowPolicySetsOnlyTheWindowAside(t *testing.T) {
	h, plat, ui := newPinnedHarness(t)
	params := map[string]any{"name": platformProd, "revision": plat, "who": prodShadowPrincipal, "params": map[string]string{"uiRevision": ui}}
	ctx := context.Background()

	if d := h.srv.releasePolicy.Evaluate(ctx, prodShadowAsksAbout, params); !d.Auto {
		t.Fatalf("positive control: with the window held the engine covers it; reasons %v", d.Reasons)
	}
	h.facts.Window = "no release window is open; hold one first: release.sh hold --what <repo>"
	d := h.srv.releasePolicy.Evaluate(ctx, prodShadowAsksAbout, params)
	if d.Auto || len(d.Reasons) != 1 || !strings.HasPrefix(d.Reasons[0], policyWindowReason) {
		t.Fatalf("the engine without the window: auto=%v reasons=%v", d.Auto, d.Reasons)
	}
	if covered, reasons := policySansWindow(d); !covered || len(reasons) != 0 {
		t.Fatalf("the window alone: covered=%v reasons=%v", covered, reasons)
	}
	h.facts.Green = map[string]bool{}
	d = h.srv.releasePolicy.Evaluate(ctx, prodShadowAsksAbout, params)
	if covered, reasons := policySansWindow(d); covered || len(reasons) != 1 || strings.HasPrefix(reasons[0], policyWindowReason) {
		t.Fatalf("a red CI beside the window: covered=%v reasons=%v (engine said %v)", covered, reasons, d.Reasons)
	}
	if covered, _ := policySansWindow(releasepolicy.Decision{}); covered {
		t.Fatal("a decision with no reasons that is not Auto read as covered")
	}
}

// Origin: see the top of the file; and the design's "不起运行、不占窗口、不建卡".
// Guards, by the source, that the shadow's wiring cannot act: this file
// names nothing that starts a run, takes or writes the window, creates or
// decides an approval, writes a ConfigMap or sends a request with a body.
// "start_pipeline_run" appears once, as the action the policy is asked about
// and cards are filtered by. Positive control: the same scan of
// head_release.go, which does start deliveries, finds StartDeliveryIfFree.
func TestProdShadowWiringCannotAct(t *testing.T) {
	forbidden := []string{"StartDeliveryIfFree", "StartPipelineRun", "start", "Start", "Step", "headWorld", "headWorldOf",
		"release-window", "HoldReleaseWindow", "PutReleaseWindow", "WriteConfigMap", "Writer",
		"HandleCreate", "HandleApprove", "HandleResolve", "Create", "SetAutoApprover", "autoApprove", "Decide",
		"MethodPost", "MethodPut", "MethodDelete", "MethodPatch", "NewRequest", "NewRequestWithContext", "exec", "kubernetes", "dynamic"}
	idents, literals := sourceNames(t, "prod_batch_shadow.go")
	for _, name := range forbidden {
		if idents[name] > 0 {
			t.Errorf("prod_batch_shadow.go names %s: the shadow reads and records, nothing else", name)
		}
		for text := range literals {
			if strings.Contains(text, name) && text != prodShadowAsksAbout {
				t.Errorf("prod_batch_shadow.go has the string %q", text)
			}
		}
	}
	if literals[prodShadowAsksAbout] != 1 || idents["prodShadowAsksAbout"] != 3 {
		t.Errorf("start_pipeline_run is written %d times and its constant named %d times; want once, and its definition plus two reads (the policy's question, the cards' filter)",
			literals[prodShadowAsksAbout], idents["prodShadowAsksAbout"])
	}
	if control, _ := sourceNames(t, "head_release.go"); control["StartDeliveryIfFree"] == 0 || control["start"] == 0 {
		t.Errorf("positive control: the scan of head_release.go did not find its start")
	}
}

// sourceNames counts the identifiers and the string literals of a file of
// this package. Comments are not read.
func sourceNames(t *testing.T, file string) (idents, literals map[string]int) {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	idents, literals = map[string]int{}, map[string]int{}
	ast.Inspect(parsed, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Ident:
			idents[x.Name]++
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				text, _ := strconv.Unquote(x.Value)
				literals[text]++
			}
		}
		return true
	})
	return idents, literals
}

// Origin: see the top of the file. Guards the switch where it is read: unset
// and every value but "shadow" build no shadow at all (so nothing is read or
// recorded), "on" included. Positive control: "shadow" is on.
func TestProdShadowSwitchDefaultsOff(t *testing.T) {
	table, _, err := headrelease.LoadTable()
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", "off", "on", "prod", "1"} {
		t.Setenv(prodShadowEnv, value)
		if prodShadowOn() || (&Server{}).prodShadowOf(table) != nil {
			t.Errorf("%s=%q built a shadow", prodShadowEnv, value)
		}
	}
	t.Setenv(prodShadowEnv, "shadow")
	if !prodShadowOn() {
		t.Error("positive control: shadow is not on")
	}
}

// Origin: 2026-11-16 night. The coordinator asked for a night of shadow
// records; the switch is an environment variable of PROD's workers, and the
// PROD manifest is not an agent's to change while the Owner sleeps. So where
// head-release runs the shadow is on unless it is switched off, and where it
// does not run (STG's platform, a local process) nothing is judged. "on" is
// still nothing: that code is not written.
func TestProdShadowIsOnWhereHeadReleaseRunsUnlessSwitchedOff(t *testing.T) {
	for _, c := range []struct{ head, value, want string }{
		{"stg", "", headrelease.ProdShadow},
		{"stg", "off", headrelease.ProdOff},
		{"stg", "on", headrelease.ProdOff},
		{"stg", "shadow", headrelease.ProdShadow},
		{"", "", headrelease.ProdOff},
		{"", "shadow", headrelease.ProdShadow},
	} {
		t.Setenv("HEAD_RELEASE", c.head)
		t.Setenv(prodShadowEnv, c.value)
		if got := prodShadowMode(); got != c.want {
			t.Errorf("HEAD_RELEASE=%q %s=%q: %s, want %s", c.head, prodShadowEnv, c.value, got, c.want)
		}
	}
}
