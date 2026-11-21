package headrelease

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
)

const (
	platformProd = "bifrost-deliver-platform-prod"
	platformStg  = "bifrost-deliver-platform"
)

// Origin of every test in this file. 2026-11-16: 19 PROD releases of the
// platform in one evening, each by a seat holding the window ~129 s; the
// coordinator asked for a shadow of "the platform releases PROD in batches"
// before anyone turns it on. Each test says below what it guards.

func shadowRows(t *testing.T) (prod, source Deliverable) {
	t.Helper()
	table, _, err := LoadTable()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range table.Deliverables {
		switch d.Name {
		case ProdRowName:
			prod = d
		case ProdSourceName:
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
	cards    []string
	holder   string
	ci       string
}

func (w *shadowWorld) reads() ProdReads {
	return ProdReads{
		Now: func() time.Time { return w.now },
		Rule: func(pipeline string) (releases.Rule, bool) {
			switch pipeline {
			case platformStg:
				return releases.Rule{Lane: "platform", Env: "stg", Deploys: true}, true
			case platformProd:
				return releases.Rule{Lane: "platform", Env: "prod", Deploys: true}, true
			}
			return releases.Rule{}, false
		},
		Current: func(_ context.Context, lane, env string) (releases.Record, error) {
			return w.records[lane+"/"+env], w.errs[lane+"/"+env]
		},
		CI: func(_ context.Context, repo, commit string) (string, string, error) {
			return w.ci, "ci-platform-" + commit[:4], nil
		},
		Policy: func(_ context.Context, params map[string]any) releasepolicy.Decision {
			w.asked = append(w.asked, params)
			return w.decision
		},
		Cards: func(string) ([]string, error) { return w.cards, nil },
		Window: func(context.Context) (string, string, bool, error) {
			return w.holder, "bifrost-platform", w.holder != "", nil
		},
		Frozen: func(context.Context) (bool, string) { return false, "" },
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
	f := w.reads().Gather(context.Background(), prod, source, DefaultProdSoak, DefaultProdGap)
	d := DecideProd(f)
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
	gather := func(edit func(w *shadowWorld), ctx context.Context) (ProdDecision, *shadowWorld) {
		w, _, _, _ := newShadowWorld()
		edit(w)
		return DecideProd(w.reads().Gather(ctx, prod, source, DefaultProdSoak, DefaultProdGap)), w
	}
	bg := context.Background()

	d, w := gather(func(w *shadowWorld) {
		w.errs["platform/stg"] = fmt.Errorf("%w: run x may have changed platform/stg", releases.ErrUnsettled)
	}, bg)
	if d.WouldRelease || strings.Join(d.Failing, ",") != CondStgSettled || len(d.Unknown) != 0 || len(w.asked) != 0 || d.To != "" {
		t.Fatalf("unsettled stg: %+v, policy asked %d", d, len(w.asked))
	}
	d, _ = gather(func(w *shadowWorld) { w.errs["platform/prod"] = releases.ErrNoDeployment }, bg)
	if d.WouldRelease || d.Verdict != "wait: prod is not settled" {
		t.Fatalf("no prod record: %+v", d)
	}
	d, _ = gather(func(w *shadowWorld) { w.errs["platform/prod"] = errors.New("connection refused") }, bg)
	if d.WouldRelease || strings.Join(d.Unknown, ",") != CondProdSettled {
		t.Fatalf("prod records unreadable: %+v", d)
	}
	d, _ = gather(func(w *shadowWorld) {
		w.decision.Reasons = append(w.decision.Reasons, "bifrost-platform: CI did not succeed for aaaaaaaa")
	}, bg)
	if d.WouldRelease || strings.Join(d.Failing, ",") != CondPolicy || d.Reasons[CondPolicy] != "bifrost-platform: CI did not succeed for aaaaaaaa" {
		t.Fatalf("policy reason: %+v", d)
	}
	d, _ = gather(func(w *shadowWorld) {
		w.holder = "ci-w1"
		// The open ones among #524 pending, #525 approved, #526 pending (Trade's),
		// #527 executed and #528 (another action): approvalreadings.OpenReleaseCards.
		w.cards = []string{"#524 pending", "#525 approved"}
	}, bg)
	if d.WouldRelease || strings.Join(d.Failing, ",") != CondNoOpenCard+","+CondWindowFree ||
		d.Reasons[CondNoOpenCard] != "#524 pending, #525 approved" || !strings.Contains(d.Reasons[CondWindowFree], "ci-w1") {
		t.Fatalf("card and window: %+v", d)
	}
	done, cancel := context.WithCancel(bg)
	cancel()
	d, _ = gather(func(*shadowWorld) {}, done)
	if d.WouldRelease || !strings.Contains(strings.Join(d.Unknown, ","), CondPolicy) {
		t.Fatalf("an evaluation that ran out of time: %+v", d)
	}
	// No engine, no ledger, no records: unknown, never a release.
	d = DecideProd(ProdReads{Now: time.Now}.Gather(bg, prod, source, 0, 0))
	if d.WouldRelease || len(d.Unknown) == 0 {
		t.Fatalf("nothing wired: %+v", d)
	}
}

// Origin: 2026-11-16 night. The coordinator asked for a night of shadow
// records; the switch is an environment variable of PROD's workers, and the
// PROD manifest is not an agent's to change while the Owner sleeps. So where
// head-release runs the shadow is on unless it is switched off, and where it
// does not run (STG's platform, a local process) nothing is judged.
// 2026-11-21 (CI line): unset where head-release runs is now on (it
// releases); shadow and off still switch it to recording or to nothing.
func TestProdShadowIsOnWhereHeadReleaseRunsUnlessSwitchedOff(t *testing.T) {
	for _, c := range []struct{ head, value, want string }{
		{"stg", "", ProdOn},
		{"stg", "off", ProdOff},
		{"stg", "on", ProdOn},
		{"stg", "shadow", ProdShadow},
		{"stg", "true", ProdOff},
		{"", "", ProdOff},
		{"", "shadow", ProdShadow},
	} {
		t.Setenv("HEAD_RELEASE", c.head)
		t.Setenv(prodShadowEnv, c.value)
		if got := ProdShadowMode(); got != c.want {
			t.Errorf("HEAD_RELEASE=%q %s=%q: %s, want %s", c.head, prodShadowEnv, c.value, got, c.want)
		}
	}
}
