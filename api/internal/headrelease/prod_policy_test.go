package headrelease

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
)


// shadowRowsOf reads the two rows of deliverables.yaml the shadow reads.
func shadowRowsOf(t *testing.T) (prod, source Deliverable) {
	t.Helper()
	table, _, err := LoadTable()
	if err != nil {
		t.Fatal(err)
	}
	p, src := ProdRows(table)
	if p == nil || src == nil {
		t.Fatal("deliverables.yaml lacks the shadow's rows")
	}
	return *p, *src
}

// pinnedEngine is the signed policy engine with the platform PROD release
// pinned to STG's commits, as server's newPinnedHarness built it.
func pinnedEngine(t *testing.T) (*releasepolicy.Engine, *rptest.Facts, string, string) {
	t.Helper()
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	key, cms := rptest.NewKey(t), rptest.NewConfigMaps()
	var doc map[string]any
	if err := json.Unmarshal([]byte(rptest.PolicyText("rp-pins", time.Date(2026, 10, 8, 11, 0, 0, 0, time.UTC), 90, "bifrost-deliver-platform-prod")), &doc); err != nil {
		t.Fatal(err)
	}
	doc["ci_repos"] = []string{"bifrost-platform"}
	doc["pinned"] = map[string]any{"bifrost-deliver-platform-prod": map[string]any{
		"from":   "bifrost-deliver-platform",
		"params": map[string]string{"revision": "bifrost-platform", "uiRevision": "bifrost-ui"},
	}}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw) + "\n"
	cms.Set(releasepolicy.PolicyConfigMap, map[string]string{"policy.yaml": text, "policy.sig": key.Sign(t, []byte(text), releasepolicy.NamespacePolicy)})
	cms.Set(releasepolicy.FreezeConfigMap, map[string]string{"frozen": "false"})
	plat, ui := rptest.SHA("plat"), rptest.SHA("ui")
	facts := &rptest.Facts{
		Records: map[string]map[string]string{
			"bifrost-deliver-platform":      {"bifrost-platform": plat, "bifrost-ui": ui},
			"bifrost-deliver-platform-prod": {"bifrost-platform": rptest.SHA("old-plat"), "bifrost-ui": rptest.SHA("old-ui")},
		},
		Green: map[string]bool{"bifrost-platform@" + plat: true},
		Files: map[string][]string{"bifrost-platform": {"docs/readme.md"}, "bifrost-ui": {"src/Button.tsx"}},
	}
	eng := releasepolicy.New(releasepolicy.Deps{
		ConfigMaps: cms, Writer: rptest.Writer{CMs: cms}, Git: facts, CI: facts, Window: facts, Deployed: facts,
		Anchor: key.Fingerprint, Now: func() time.Time { return now },
	})
	return eng, facts, plat, ui
}

// Origin: 2026-11-16, the PROD batch shadow (in server/prod_batch_shadow_test.go
// until 2026-11-23; it tests this package's Gather against the real engine,
// so it moved here with the shadow's building); the brief's "the real check includes
// window_held_by_requester; the shadow must evaluate every clause except the
// window". Guards, against the real engine, that the one reason set aside is
// the window's: with the window not held the engine refuses a release it
// otherwise covers, and policySansWindow reads that as covered; a red CI
// beside it is kept as a reason. If the engine rewords its window reason
// this fails, instead of the shadow saying "policy does not cover" all night.
func TestProdShadowPolicySetsOnlyTheWindowAside(t *testing.T) {
	// policySansWindow is read through the shadow's own gather: the decision
	// the policy returns comes out as PolicyCovered and PolicyReasons.
	prod, source := shadowRowsOf(t)
	record := releases.Record{Repos: map[string]releases.RepoBuild{"bifrost-platform": {SHA: strings.Repeat("a", 40)}}}
	policySansWindow := func(d releasepolicy.Decision) (bool, []string) {
		f := ProdReads{Now: time.Now,
			Rule: func(string) (releases.Rule, bool) {
				return releases.Rule{Lane: "platform", Env: "stg", Deploys: true}, true
			},
			Current: func(context.Context, string, string) (releases.Record, error) { return record, nil },
			Policy:  func(context.Context, map[string]any) releasepolicy.Decision { return d },
		}.Gather(context.Background(), prod, source, 0, 0)
		return f.PolicyCovered, f.PolicyReasons
	}
	eng, facts, plat, ui := pinnedEngine(t)
	params := map[string]any{"name": "bifrost-deliver-platform-prod", "revision": plat, "who": ProdShadowPrincipal, "params": map[string]string{"uiRevision": ui}}
	ctx := context.Background()

	if d := eng.Evaluate(ctx, "start_pipeline_run", params); !d.Auto {
		t.Fatalf("positive control: with the window held the engine covers it; reasons %v", d.Reasons)
	}
	facts.Window = "no release window is open; hold one first: release.sh hold --what <repo>"
	d := eng.Evaluate(ctx, "start_pipeline_run", params)
	if d.Auto || len(d.Reasons) != 1 || !strings.HasPrefix(d.Reasons[0], policyWindowReason) {
		t.Fatalf("the engine without the window: auto=%v reasons=%v", d.Auto, d.Reasons)
	}
	if covered, reasons := policySansWindow(d); !covered || len(reasons) != 0 {
		t.Fatalf("the window alone: covered=%v reasons=%v", covered, reasons)
	}
	facts.Green = map[string]bool{}
	d = eng.Evaluate(ctx, "start_pipeline_run", params)
	if covered, reasons := policySansWindow(d); covered || len(reasons) != 1 || strings.HasPrefix(reasons[0], policyWindowReason) {
		t.Fatalf("a red CI beside the window: covered=%v reasons=%v (engine said %v)", covered, reasons, d.Reasons)
	}
	if covered, _ := policySansWindow(releasepolicy.Decision{}); covered {
		t.Fatal("a decision with no reasons that is not Auto read as covered")
	}
}
