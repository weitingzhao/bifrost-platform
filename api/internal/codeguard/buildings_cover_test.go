package codeguard

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// A domain that declares its buildings puts every package it owns in one, so
// the City draws only the buildings the domain wrote (laws 75, 79).
//
// Origin: 2026-11-22, the coordinator asked whether the foundation district
// in the City really is the 9 buildings Foundation declared that day. It was
// not: api/cmd/bifrost-bus, api/cmd/platform-api and api/cmd/operator-plane
// were in no declared building, so the catalog grouped them by directory into
// a tenth, undeclared "cmd" building. They were placed the same day (bus,
// and a declared "entry" building for the two process entry points).

type buildingRow struct {
	ID        string
	Buildings []struct {
		ID    string
		Paths []string
	}
}

// outsideBuildings names, sorted, the packages of domain d that no declared
// building of d holds (a path prefix, as the catalog reads it).
func outsideBuildings(o ownership, d buildingRow, pkgs []string) []string {
	var out []string
	for _, p := range pkgs {
		if o.domainOf(p) != d.ID {
			continue
		}
		in := false
		for _, b := range d.Buildings {
			for _, q := range b.Paths {
				q = strings.Trim(q, "/")
				if p == q || strings.HasPrefix(p, q+"/") {
					in = true
				}
			}
		}
		if !in {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

func TestADomainsBuildingsHoldAllItsPackages(t *testing.T) {
	o := loadOwnership(t)
	var doc struct{ Domains []buildingRow }
	if err := json.Unmarshal(ownershipRaw(t), &doc); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "list", "-e", "-f", "{{.ImportPath}}", "./...")
	cmd.Dir = filepath.Join("..", "..")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	var pkgs []string
	for _, line := range strings.Fields(string(out)) {
		pkgs = append(pkgs, "api/"+strings.TrimPrefix(strings.TrimPrefix(line, module), "api/"))
	}
	for _, d := range doc.Domains {
		if len(d.Buildings) == 0 {
			continue
		}
		for _, p := range outsideBuildings(o, d, pkgs) {
			t.Errorf("%s is %s's but in none of the buildings %s declared in config/domain-ownership.json: add its path to one, or declare a new one (at most 16, law 79)", p, d.ID, d.ID)
		}
	}
}

// The control: a package of the domain outside every building is named; one
// under a building's path, and another domain's package, are not.
func TestOutsideBuildingsNamesAStray(t *testing.T) {
	o := ownership{Packages: map[string]struct{ Domains []string }{
		"api/internal/bus/x": {Domains: []string{"foundation"}},
		"api/cmd/stray":      {Domains: []string{"foundation"}},
		"api/internal/plan":  {Domains: []string{"plan"}},
	}}
	d := buildingRow{ID: "foundation"}
	d.Buildings = append(d.Buildings, struct {
		ID    string
		Paths []string
	}{"bus", []string{"api/internal/bus"}})
	got := outsideBuildings(o, d, []string{"api/internal/bus/x", "api/cmd/stray", "api/internal/plan"})
	if strings.Join(got, ",") != "api/cmd/stray" {
		t.Fatalf("outside = %q, want [api/cmd/stray]", got)
	}
}
