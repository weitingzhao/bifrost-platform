package testscope

import (
	"os/exec"
	"strings"
	"testing"
)

// 2026-11-19, LANE-ci-w1: the decoupling acceptance reported b6c6abc2's
// incremental run as missing internal/delivery and internal/promote. The run
// was right (its base was e1f0dcec and nothing it changed reaches those two);
// the acceptance diffed from the last commit with a land record, d04fc8d0, and
// so counted e96b7a14's change to internal/delivery against b6c6abc2. This
// case checks the selector on the real tree against an independent answer:
// every package with tests whose dependencies, its tests' included, reach the
// changed package must be selected. It names no package, so it holds as the
// graph changes. Positive control: building the graph without test variants
// (load's go list without -test) leaves out the packages that reach delivery
// only through a test import, and fails this case.
func TestRealGraphSelectsEveryPackageThatReachesTheChange(t *testing.T) {
	if testing.Short() {
		t.Skip("loads the whole module's graph")
	}
	const mod = "github.com/weitingzhao/bifrost-platform/api/"
	for _, changed := range []string{"internal/delivery", "internal/agentrt/eventlog", "internal/releases"} {
		t.Run(changed, func(t *testing.T) {
			out, err := exec.Command("go", "list", "-test", "-f",
				"{{.ImportPath}}|{{if or .TestGoFiles .XTestGoFiles}}t{{end}}|{{join .Deps \" \"}}", "../../...").Output()
			if err != nil {
				t.Fatal(err)
			}
			hasTests, reaches := map[string]bool{}, map[string]bool{}
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				f := strings.SplitN(line, "|", 3)
				p := plain(f[0])
				if strings.HasSuffix(p, ".test") {
					continue
				}
				if f[1] == "t" {
					hasTests[p] = true
				}
				if p == mod+changed {
					reaches[p] = true
				}
				for _, d := range strings.Fields(f[2]) {
					if d == mod+changed {
						reaches[p] = true
					}
				}
			}
			// A deleted file resolves to the package of its directory (TestDependentsAndGuards).
			r := SelectChanges("../..", strings.NewReader("D\tapi/"+changed+"/gone.go"))
			if r.Scope != "incremental" {
				t.Fatalf("%+v", r)
			}
			got := map[string]bool{}
			for _, p := range strings.Fields(r.Packages) {
				got[p] = true
			}
			n := 0
			for p := range reaches {
				if !hasTests[p] {
					continue
				}
				n++
				if !got[p] {
					t.Errorf("%s reaches %s but was not selected", p, changed)
				}
			}
			if n == 0 {
				t.Fatalf("no tested package reaches %s: the case checks nothing", changed)
			}
		})
	}
}
