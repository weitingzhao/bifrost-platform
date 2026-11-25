package codeguard

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The ratchets of LAW-9 (the Owner in the Foundation thread, 2026-11-22:
// decoupling that is cleared but not held is temporary). Each holds one end
// point of the decoupling plan; a baseline only shrinks, and loosening one is
// the Owner's (LAW-9).

func loadOwnership(t *testing.T) ownership {
	t.Helper()
	raw := ownershipRaw(t)
	var o ownership
	if err := json.Unmarshal(raw, &o); err != nil {
		t.Fatal(err)
	}
	return o
}

// unlisted names, sorted, the packages that have no row in the ownership
// table.
func unlisted(o ownership, pkgs []string) []string {
	var out []string
	for _, p := range pkgs {
		if _, ok := o.Packages[p]; !ok {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// Every package has a row: a new package says which domain it is and why
// when it lands.
//
// Origin: 2026-11-15, the overlap ruling made a package with no row
// foundation's by default ("default-maintainer"), so a package nobody placed
// sat in the base unseen; on 2026-11-21 nine such packages had to be judged
// one by one (the decoupling ledger, "B 类 9 个"). Since 2026-11-21 every
// package has a row (195); this keeps it so.
func TestEveryPackageHasARow(t *testing.T) {
	o := loadOwnership(t)
	cmd := exec.Command("go", "list", "-e", "-f", "{{.ImportPath}}", "./...")
	cmd.Dir = filepath.Join("..", "..")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	var pkgs []string
	for _, line := range strings.Fields(string(out)) {
		pkgs = append(pkgs, strings.TrimPrefix(line, module))
	}
	if len(pkgs) < 100 {
		t.Fatalf("go list named %d packages", len(pkgs))
	}
	for _, p := range unlisted(o, pkgs) {
		t.Errorf("%s has no row in config/domain-ownership.json: add one that names its one domain and the reason (LAW-10 一). The map line (0.2) owns that table: ask it which domain, or read the domains through GET /api/v1/catalog/domain", p)
	}
}

// The control: a package without a row is named, one with a row is not.
func TestUnlistedNamesAPackageWithoutARow(t *testing.T) {
	var o ownership
	if err := json.Unmarshal([]byte(`{"default":{"domains":["foundation"]},"packages":{"api/internal/a":{"domains":["plan"]}}}`), &o); err != nil {
		t.Fatal(err)
	}
	got := unlisted(o, []string{"api/internal/a", "api/internal/b"})
	if strings.Join(got, ",") != "api/internal/b" {
		t.Fatalf("unlisted = %q", got)
	}
}

// assemblyFuncs is the baseline: the top-level functions and methods in the
// production files of each assembly package. It only shrinks. Assembly wires
// the domains (registers routes, binds ports, starts and stops); a handler
// or a rule belongs in the package of the domain it serves, and assembly
// registers it there. 34 of server's functions on 2026-11-22 were still
// handlers of other domains (the decoupling plan, step 2).
//
// Origin: LAW-9, 2026-11-22. Code has flowed into the assembly root before:
// on 2026-11-20 agentrt/agentrtapi folded into server with its handlers, the
// day after 2026-11-18 had moved the question bridge (about 330 lines) out
// of it. Nothing held the size of the root, so a domain's code could land
// there and the walls would not see it: assembly may import every domain.
var assemblyFuncs = map[string]int{
	"api/internal/agentrt/integration": 0,
	"api/internal/agentrt/planwiring":  3,
	"api/internal/server":              274,
}

// funcsIn counts the top-level func declarations in a directory's non-test
// Go files.
func funcsIn(dir string) (int, error) {
	n := 0
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return 0, err
	}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, parser.SkipObjectResolution)
		if err != nil {
			return 0, err
		}
		for _, d := range file.Decls {
			if _, ok := d.(*ast.FuncDecl); ok {
				n++
			}
		}
	}
	return n, nil
}

// assemblyErrors compares the counts with the baseline.
func assemblyErrors(asm []string, count map[string]int, baseline map[string]int) []string {
	var out []string
	for _, p := range asm {
		held, ok := baseline[p]
		n := count[p]
		switch {
		case !ok:
			out = append(out, fmt.Sprintf("%s is a new assembly package (%d functions): add it to assemblyFuncs with its count", p, n))
		case n > held:
			out = append(out, fmt.Sprintf("%s has %d functions and the baseline is %d: assembly only wires (routes, ports, start and stop); put the handler or rule in the package of the domain it serves and register it here (LAW-9; the baseline only shrinks, raising it is the Owner's, LAW-9)", p, n, held))
		case n < held:
			out = append(out, fmt.Sprintf("%s has %d functions, fewer than the baseline %d: lower assemblyFuncs to %d (the list only shrinks)", p, n, held, n))
		}
	}
	for p := range baseline {
		found := false
		for _, a := range asm {
			found = found || a == p
		}
		if !found {
			out = append(out, fmt.Sprintf("%s is no longer an assembly package: take it off assemblyFuncs", p))
		}
	}
	sort.Strings(out)
	return out
}

func TestAssemblyOnlyShrinks(t *testing.T) {
	o := loadOwnership(t)
	var asm []string
	count := map[string]int{}
	for p := range o.Packages {
		if o.domainOf(p) != assembly {
			continue
		}
		asm = append(asm, p)
		n, err := funcsIn(filepath.Join("..", "..", "..", filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		count[p] = n
	}
	sort.Strings(asm)
	if len(asm) == 0 {
		t.Fatal("the ownership table has no assembly package")
	}
	t.Logf("assembly functions: %v", count)
	for _, msg := range assemblyErrors(asm, count, assemblyFuncs) {
		t.Error(msg)
	}
}

// The control: a count over the baseline is named, one under it asks to
// lower it, an equal one is quiet.
func TestAssemblyErrorsNameAGrowth(t *testing.T) {
	base := map[string]int{"api/internal/wire": 3, "api/internal/root": 5}
	got := assemblyErrors([]string{"api/internal/root", "api/internal/wire"}, map[string]int{"api/internal/wire": 4, "api/internal/root": 5}, base)
	if len(got) != 1 || !strings.Contains(got[0], "api/internal/wire has 4 functions and the baseline is 3") {
		t.Fatalf("growth: %q", got)
	}
	got = assemblyErrors([]string{"api/internal/root", "api/internal/wire"}, map[string]int{"api/internal/wire": 2, "api/internal/root": 5}, base)
	if len(got) != 1 || !strings.Contains(got[0], "lower assemblyFuncs to 2") {
		t.Fatalf("shrink: %q", got)
	}
	if got = assemblyErrors([]string{"api/internal/root", "api/internal/wire"}, map[string]int{"api/internal/wire": 3, "api/internal/root": 5}, base); len(got) != 0 {
		t.Fatalf("equal: %q", got)
	}
}
