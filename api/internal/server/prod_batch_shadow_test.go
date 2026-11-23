//bifrost:owner delivery

package server

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// The names the tests pin by their text: the switch's variable, the start of
// the engine's window reason (headrelease/prod_reads.go) and the two rows of
// deliverables.yaml the shadow reads.
const (
	prodShadowEnv = "PROD_HEAD_RELEASE"
)

// Origin of every test in this file. 2026-11-16: 19 PROD releases of the
// platform in one evening, each by a seat holding the window ~129 s; the
// coordinator asked for a shadow of "the platform releases PROD in batches"
// before anyone turns it on. Each test says below what it guards.

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
	// So did the switch, the rows, the pace and the record (decoupling item
	// 5): the same scan holds them too.
	built, builtLiterals := sourceNames(t, "../headrelease/prod_shadow_build.go")
	for _, name := range forbidden {
		if idents[name] > 0 {
			t.Errorf("prod_batch_shadow.go names %s: the shadow reads and records, nothing else", name)
		}
		if eyes[name] > 0 {
			t.Errorf("headrelease/prod_reads.go names %s: the shadow reads and records, nothing else", name)
		}
		if built[name] > 0 {
			t.Errorf("headrelease/prod_shadow_build.go names %s: the shadow reads and records, nothing else", name)
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
		for text := range builtLiterals {
			if strings.Contains(text, name) {
				t.Errorf("headrelease/prod_shadow_build.go has the string %q", text)
			}
		}
	}
	// Positive control of the added file: the scan does read it.
	if built["ProdShadowLoop"] == 0 || builtLiterals["prod-shadow"] != 1 {
		t.Errorf("the scan of headrelease/prod_shadow_build.go found neither the loop nor its site")
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
