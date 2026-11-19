//bifrost:owner delivery

package server

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/headrelease"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
)

// The names the tests pin by their text: the switch's variable and the start
// of the engine's window reason (headrelease/prod_reads.go).
const (
	prodShadowEnv      = "PROD_HEAD_RELEASE"
	policyWindowReason = "release window: "
)

func shadowRowsOf(t *testing.T) (prod, source headrelease.Deliverable) {
	t.Helper()
	table, _, err := headrelease.LoadTable()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range table.Deliverables {
		switch d.Name {
		case headrelease.ProdRowName:
			prod = d
		case headrelease.ProdSourceName:
			source = d
		}
	}
	return prod, source
}

// Origin of every test in this file. 2026-11-16: 19 PROD releases of the
// platform in one evening, each by a seat holding the window ~129 s; the
// coordinator asked for a shadow of "the platform releases PROD in batches"
// before anyone turns it on. Each test says below what it guards.

// Origin: see the top of the file; the brief's "the real check includes
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
		f := headrelease.ProdReads{Now: time.Now,
			Rule: func(string) (releases.Rule, bool) {
				return releases.Rule{Lane: "platform", Env: "stg", Deploys: true}, true
			},
			Current: func(context.Context, string, string) (releases.Record, error) { return record, nil },
			Policy:  func(context.Context, map[string]any) releasepolicy.Decision { return d },
		}.Gather(context.Background(), prod, source, 0, 0)
		return f.PolicyCovered, f.PolicyReasons
	}
	h, plat, ui := newPinnedHarness(t)
	params := map[string]any{"name": platformProd, "revision": plat, "who": headrelease.ProdShadowPrincipal, "params": map[string]string{"uiRevision": ui}}
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
	// The shadow's eyes moved to headrelease (decoupling item 5, A3): the same
	// scan holds them.
	eyes, eyeLiterals := sourceNames(t, "../headrelease/prod_reads.go")
	for _, name := range forbidden {
		if idents[name] > 0 {
			t.Errorf("prod_batch_shadow.go names %s: the shadow reads and records, nothing else", name)
		}
		if eyes[name] > 0 {
			t.Errorf("headrelease/prod_reads.go names %s: the shadow reads and records, nothing else", name)
		}
		for text := range literals {
			if strings.Contains(text, name) && text != prodShadowAsksAbout {
				t.Errorf("prod_batch_shadow.go has the string %q", text)
			}
		}
		for text := range eyeLiterals {
			if strings.Contains(text, name) {
				t.Errorf("headrelease/prod_reads.go has the string %q", text)
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
		if headrelease.ProdShadowOn() || (&Server{}).prodShadowOf(table) != nil {
			t.Errorf("%s=%q built a shadow", prodShadowEnv, value)
		}
	}
	t.Setenv(prodShadowEnv, "shadow")
	if !headrelease.ProdShadowOn() {
		t.Error("positive control: shadow is not on")
	}
}
