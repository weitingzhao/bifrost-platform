package codeguard

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"
)

// Assembly reaches each domain only through what that domain made public
// (LAW-10: "it reaches each domain through what that domain offers"). A
// domain names its public packages in its row,
// domains[].contracts.public_packages; an import by an assembly package of a
// domain package that is not public is held in
// assembly_reach_<domain>_test.go and only shrinks.
//
// Origin: 2026-11-22. Measured by name, assembly used 1,097 exported names of
// other domains, none registered, and registering names one by one with
// nothing holding them had already been dropped once (2026-11-21: they drift
// at once). Counted by package it is 87 domain packages (foundation, the
// base, not counted). Foundation decided this by first principles (LAW-5):
// the return is efficiency, one line per domain instead of a thousand names.

var assemblyReachHeld = map[string]bool{}

func heldAssemblyReach(edges ...string) bool {
	for _, e := range edges {
		assemblyReachHeld[e] = true
	}
	return true
}

// publicOf reads each domain's public packages from the merged table.
func publicOf(raw []byte) (map[string]map[string]bool, error) {
	var doc struct {
		Domains []struct {
			ID        string
			Contracts struct {
				PublicPackages []string `json:"public_packages"`
			}
		}
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	out := map[string]map[string]bool{}
	for _, d := range doc.Domains {
		out[d.ID] = map[string]bool{}
		for _, p := range d.Contracts.PublicPackages {
			out[d.ID][p] = true
		}
	}
	return out, nil
}

// assemblyReach lists, sorted, "importer -> imported (domain)" for every import
// by an assembly package of a package of another domain (not the base) that
// the domain did not make public.
func assemblyReach(o ownership, public map[string]map[string]bool, imports map[string][]string) []string {
	base := baseOf(o)
	var out []string
	for pkg, deps := range imports {
		if o.domainOf(pkg) != assembly {
			continue
		}
		for _, dep := range deps {
			d := o.domainOf(dep)
			if d == assembly || d == base || d == "mixed" {
				continue
			}
			if !public[d][dep] {
				out = append(out, fmt.Sprintf("%s -> %s (%s)", pkg, dep, d))
			}
		}
	}
	sort.Strings(out)
	return out
}

func assemblyReachErrors(got []string, held map[string]bool) []string {
	var out []string
	now := map[string]bool{}
	for _, e := range got {
		now[e] = true
		if !held[e] {
			out = append(out, e+": assembly imports a package its domain did not make public; reach it through the domain's public package, or have the domain list it under contracts.public_packages (LAW-10)")
		}
	}
	for e := range held {
		if !now[e] {
			out = append(out, e+" is gone or public now: take it off assembly_reach_<domain>_test.go (the list only shrinks)")
		}
	}
	sort.Strings(out)
	return out
}

func TestAssemblyReachesDomainsThroughTheirPublicPackages(t *testing.T) {
	raw := ownershipRaw(t)
	var o ownership
	if err := json.Unmarshal(raw, &o); err != nil {
		t.Fatal(err)
	}
	public, err := publicOf(raw)
	if err != nil {
		t.Fatal(err)
	}
	got := assemblyReach(o, public, moduleImports(t))
	t.Logf("assembly imports of non-public domain packages: %d (held %d)", len(got), len(assemblyReachHeld))
	if p := printAssemblyReach(); p {
		for _, e := range got {
			t.Log(e)
		}
	}
	for _, msg := range assemblyReachErrors(got, assemblyReachHeld) {
		t.Error(msg)
	}
}

// The control: an import of a non-public package is named, of a public one
// is not, of the base is not, and a held edge that became public is named.
func TestAssemblyReachSeesNonPublicImports(t *testing.T) {
	var o ownership
	if err := json.Unmarshal([]byte(`{"default":{"domains":["foundation"]},"packages":{
		"api/internal/wire":{"domains":["assembly"]},
		"api/internal/plan":{"domains":["plan"]},
		"api/internal/plan/api":{"domains":["plan"]}}}`), &o); err != nil {
		t.Fatal(err)
	}
	public := map[string]map[string]bool{"plan": {"api/internal/plan/api": true}}
	got := assemblyReach(o, public, map[string][]string{"api/internal/wire": {"api/internal/plan", "api/internal/plan/api", "api/internal/base"}})
	if len(got) != 1 || got[0] != "api/internal/wire -> api/internal/plan (plan)" {
		t.Fatalf("reach = %q", got)
	}
	errs := assemblyReachErrors(nil, map[string]bool{"api/internal/wire -> api/internal/plan (plan)": true})
	if len(errs) != 1 {
		t.Fatalf("errors = %q", errs)
	}
}

func printAssemblyReach() bool { return os.Getenv("CODEGUARD_PRINT_ASSEMBLY_REACH") != "" }
