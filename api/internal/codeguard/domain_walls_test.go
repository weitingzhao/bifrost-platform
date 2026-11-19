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

// across lists, sorted, every direct import by a package of one domain (not
// the base, not an assembly root under api/cmd) of a package that is neither
// its own domain's nor the base's: another domain's, or one several domains
// share. base is the default domain, which every domain stands on.
func across(o ownership, imports map[string][]string) []string {
	base := "mixed"
	if len(o.Default.Domains) == 1 {
		base = o.Default.Domains[0]
	}
	var out []string
	for pkg, deps := range imports {
		from := o.domainOf(pkg)
		if strings.HasPrefix(pkg, "api/cmd/") || from == base || from == "mixed" {
			continue
		}
		for _, dep := range deps {
			if to := o.domainOf(dep); to != from && to != base {
				out = append(out, pkg+" -> "+dep+" ("+from+" -> "+to+")")
			}
		}
	}
	sort.Strings(out)
	return out
}

// domainsAcross is the baseline: the imports from one domain into another, or
// into a package several domains share, on 2026-11-16. It only shrinks. A
// domain reaches another through what that one offers for it (a contract:
// an interface the importer declares, a registered event, a read the owner
// publishes); an import of its package makes the two change together, and
// nothing in the code said which imports were meant.
//
// Origin: 2026-11-16. The independent review of the foundation code (shared
// notes brain/foundation-code-quality-review-2026-11-16.md) found the domain
// boundaries enforced by nothing; the foundation wall of that evening closed
// the base. The eight domains above it scored 21 of 25 on dependency
// direction for the one reason that nothing held their imports of each other
// (config/quality-ruler.json), and the coordinator ruled the wall be extended
// to every domain.
var domainsAcross = []string{
	"api/internal/approvals -> api/internal/threadinbox (owner -> coordination)",
	"api/internal/lineage -> api/internal/agentrt/landinggit (lineage -> verification)",
	"api/internal/questions -> api/internal/threadinbox (owner -> coordination)",
	"api/internal/releasepolicy -> api/internal/actions (delivery -> owner)",
}

func moduleImports(t *testing.T) map[string][]string {
	t.Helper()
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
	return imports
}

func TestADomainImportsOnlyItselfAndTheBase(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "config", "domain-ownership.json"))
	if err != nil {
		t.Fatal(err)
	}
	var o ownership
	if err = json.Unmarshal(raw, &o); err != nil {
		t.Fatal(err)
	}
	if o.domainOf("api/internal/agentrt/eventlog") != "foundation" || o.domainOf("api/internal/ciscore") != "verification" || len(o.Packages) < 50 {
		t.Fatalf("the ownership table did not load as expected: %d rows", len(o.Packages))
	}
	got := across(o, moduleImports(t))
	known := map[string]bool{}
	for _, edge := range domainsAcross {
		known[edge] = true
	}
	seen := map[string]bool{}
	for _, edge := range got {
		seen[edge] = true
		if !known[edge] {
			t.Errorf("a domain imports another domain's package: %s — reach it through what its owner offers (an interface you declare, a registered event, a read it publishes), or, if the package is yours, say so in config/domain-ownership.json with the reason", edge)
		}
	}
	for _, edge := range domainsAcross {
		if !seen[edge] {
			t.Errorf("%s is gone — take it off domainsAcross (the list only shrinks)", edge)
		}
	}
}

// The positive control of the wall above: on a made-up module the check names
// the import of another domain and the import of a shared package, and
// nothing else (the base's own imports, an import of the base, an import
// inside the domain, an assembly root, a shared package's imports).
func TestDomainWallSeesAnImportAcross(t *testing.T) {
	var o ownership
	if err := json.Unmarshal([]byte(`{"default":{"domains":["foundation"]},"packages":{
		"api/internal/base":{"domains":["foundation"]},
		"api/internal/plan":{"domains":["plan"]},
		"api/internal/plan/view":{"domains":["plan"]},
		"api/internal/owner":{"domains":["owner"]},
		"api/internal/both":{"domains":["plan","owner"]}}}`), &o); err != nil {
		t.Fatal(err)
	}
	got := across(o, map[string][]string{
		"api/internal/base":      {"api/internal/plan"},
		"api/internal/unlisted":  {"api/internal/owner"},
		"api/internal/plan":      {"api/internal/base", "api/internal/plan/view", "api/internal/unlisted", "api/internal/owner", "api/internal/both"},
		"api/internal/plan/view": {"api/internal/base"},
		"api/internal/both":      {"api/internal/owner", "api/internal/plan"},
		"api/cmd/root":           {"api/internal/plan", "api/internal/owner"},
	})
	want := []string{"api/internal/plan -> api/internal/both (plan -> mixed)", "api/internal/plan -> api/internal/owner (plan -> owner)"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("across = %q, want %q", got, want)
	}
}
