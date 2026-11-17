package codeguard

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// module is the import path prefix of this module's packages; the ownership
// table names them from the repository root ("api/internal/x").
const module = "github.com/weitingzhao/bifrost-platform/"

// ownership is config/domain-ownership.json as far as the wall reads it.
type ownership struct {
	Default  struct{ Domains []string }
	Packages map[string]struct{ Domains []string }
}

// domainOf is the one domain a package belongs to: its row's, the default's
// for a package with no row, and "mixed" for a row that names several.
func (o ownership) domainOf(pkg string) string {
	domains := o.Default.Domains
	if row, ok := o.Packages[pkg]; ok && len(row.Domains) > 0 {
		domains = row.Domains
	}
	if len(domains) != 1 {
		return "mixed"
	}
	return domains[0]
}

// upward lists, sorted, every direct import by a foundation package of a
// package of this module that is not foundation's, as "importer -> imported
// (domain)". The assembly roots under api/cmd wire everything and are not
// foundation's inside.
func upward(o ownership, imports map[string][]string) []string {
	var out []string
	for pkg, deps := range imports {
		if strings.HasPrefix(pkg, "api/cmd/") || o.domainOf(pkg) != "foundation" {
			continue
		}
		for _, dep := range deps {
			if d := o.domainOf(dep); d != "foundation" {
				out = append(out, pkg+" -> "+dep+" ("+d+")")
			}
		}
	}
	sort.Strings(out)
	return out
}

// foundationUpward is the baseline: the imports that point the wrong way on
// 2026-11-16. It only shrinks. Foundation is what every other domain stands
// on, so a foundation package that imports a domain's package makes the two
// one piece: the domain can no longer change without the base changing.
//
// Origin: 2026-11-16, an independent review of the foundation code (shared
// notes brain/foundation-code-quality-review-2026-11-16.md) counted these and
// found that nothing stopped a new one; five of them had been made that same
// week, when tenant logic was extracted and the package left behind imported
// the new one.
var foundationUpward = []string{
	"api/internal/cluster -> api/internal/placement (compute)",
	"api/internal/cluster -> api/internal/tenant/appreadings (tenant)",
	"api/internal/operatorplane -> api/internal/agentbridge (compute)",
	"api/internal/operatorplane -> api/internal/agentdeploy (compute)",
	"api/internal/operatorplane -> api/internal/launchd (compute)",
	"api/internal/satellite -> api/internal/tenant/ingestview (tenant)",
	"api/internal/selfhealth -> api/internal/gitops (delivery)",
	"api/internal/workactions -> api/internal/approvalnotify (owner)",
}

func TestFoundationDoesNotImportTheDomainsAboveIt(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "config", "domain-ownership.json"))
	if err != nil {
		t.Fatal(err)
	}
	var o ownership
	if err = json.Unmarshal(raw, &o); err != nil {
		t.Fatal(err)
	}
	if o.domainOf("api/internal/agentrt/eventlog") != "foundation" || len(o.Packages) < 50 {
		t.Fatalf("the ownership table did not load as expected: %d rows", len(o.Packages))
	}
	cmd := exec.Command("go", "list", "-f", `{{.ImportPath}} {{join .Imports ","}}`, "./...")
	cmd.Dir = filepath.Join("..", "..")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	imports := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		pkg, deps, _ := strings.Cut(line, " ")
		pkg = strings.TrimPrefix(pkg, module)
		for _, dep := range strings.Split(deps, ",") {
			if strings.HasPrefix(dep, module) {
				imports[pkg] = append(imports[pkg], strings.TrimPrefix(dep, module))
			}
		}
	}
	if len(imports) < 50 {
		t.Fatalf("go list named %d packages with imports of this module", len(imports))
	}
	got := upward(o, imports)
	known := map[string]bool{}
	for _, edge := range foundationUpward {
		known[edge] = true
	}
	seen := map[string]bool{}
	for _, edge := range got {
		seen[edge] = true
		if !known[edge] {
			t.Errorf("a foundation package imports a domain above it: %s — depend on an interface foundation declares, or move the package to the domain it serves (config/domain-ownership.json)", edge)
		}
	}
	for _, edge := range foundationUpward {
		if !seen[edge] {
			t.Errorf("%s is gone — take it off foundationUpward (the list only shrinks)", edge)
		}
	}
}

// The positive control of the wall above: on a made-up module the check
// names the one import that points up, and nothing else (an import of a
// foundation package, an importer that is not foundation's, an assembly root,
// a package with no row, a row of several domains).
func TestFoundationWallSeesAnUpwardImport(t *testing.T) {
	var o ownership
	if err := json.Unmarshal([]byte(`{"default":{"domains":["foundation"]},"packages":{
		"api/internal/base":{"domains":["foundation"]},
		"api/internal/plan":{"domains":["plan"]},
		"api/internal/both":{"domains":["plan","owner"]}}}`), &o); err != nil {
		t.Fatal(err)
	}
	got := upward(o, map[string][]string{
		"api/internal/base":     {"api/internal/unlisted", "api/internal/plan"},
		"api/internal/unlisted": {"api/internal/base", "api/internal/both"},
		"api/internal/plan":     {"api/internal/base", "api/internal/both"},
		"api/cmd/root":          {"api/internal/plan"},
	})
	want := []string{"api/internal/base -> api/internal/plan (plan)", "api/internal/unlisted -> api/internal/both (mixed)"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("upward = %q, want %q", got, want)
	}
}
