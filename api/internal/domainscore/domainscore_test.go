package domainscore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testRuler = `{"version":4,"items":[
	{"id":"dependency_direction","label":"Direction","max":25,"by":"machine","measured":true},
	{"id":"contracts","label":"Contracts","max":75,"by":"machine","measured":false}],
	"walls":{"base":{"file":"w_test.go","test":"TestWall"}}}`

func input(t *testing.T, own string, wall bool, pkgs ...Package) Input {
	t.Helper()
	ruler, err := ParseRuler([]byte(testRuler))
	if err != nil {
		t.Fatal(err)
	}
	in := Input{Commit: "c", Ruler: ruler, Packages: pkgs, WallHolds: map[string]bool{"base": wall}}
	if err = json.Unmarshal([]byte(own), &in.Ownership); err != nil {
		t.Fatal(err)
	}
	return in
}

func domain(t *testing.T, a Answer, id string) Domain {
	t.Helper()
	for _, d := range a.Domains {
		if d.Domain == id {
			return d
		}
	}
	t.Fatalf("no domain %s in %+v", id, a.Domains)
	return Domain{}
}

const own = `{"domains":[{"id":"base"},{"id":"plan"},{"id":"owner"},{"id":"tenant"}],
	"default":{"domains":["base"],"status":"default-maintainer"},
	"packages":{
	"api/internal/core":{"domains":["base"],"status":"assigned"},
	"api/internal/plan":{"domains":["plan"],"status":"assigned"},
	"api/internal/owner":{"domains":["owner"],"status":"assigned"},
	"api/internal/tenant":{"domains":["tenant"],"status":"assigned"},
	"api/internal/both":{"domains":["plan","owner"],"status":"mixed"}}}`

// Origin: 2026-11-16, the independent review of the foundation code (shared
// notes brain/foundation-code-quality-review-2026-11-16.md, sections 1 and 9):
// twelve imports from the base up into the domains, no wall, 55% of the lines
// in packages nobody had been assigned, scored 13 of 25. The same shape here:
// the base's imports of the domains that stand on it are its cycle edges, the
// unassigned package takes from its share, and a domain's import of the base
// is the right way and costs it nothing.
func TestTheBaseIsScoredForImportsThatPointUp(t *testing.T) {
	in := input(t, own, false,
		Package{Path: "api/internal/core", Imports: nil, Lines: 45},
		Package{Path: "api/internal/unlisted", Imports: []string{"api/internal/plan", "api/internal/tenant", "api/internal/core"}, Lines: 55},
		Package{Path: "api/internal/plan", Imports: []string{"api/internal/core", "api/internal/both"}, Lines: 10},
		Package{Path: "api/internal/tenant", Imports: []string{"api/internal/unlisted"}, Lines: 10},
		Package{Path: "api/internal/both", Imports: []string{"api/internal/core"}, Lines: 500},
		Package{Path: "api/cmd/root", Imports: []string{"api/internal/plan", "api/internal/unlisted"}, Lines: 900},
	)
	a := Score(in)
	if a.RulerVersion != 4 || a.Commit != "c" || len(a.Domains) != 4 {
		t.Fatalf("answer = %+v", a)
	}
	base := domain(t, a, "base")
	it := base.Items[0]
	// 2 edges: 17 - 1 = 16; no wall; 45 of 100 lines assigned: 4 x 0.45 = 1.8.
	if base.Packages != 2 || base.Lines != 100 || !it.Measured || it.Score != 17.8 || base.Score != 17.8 || base.Max != 25 {
		t.Fatalf("base = %+v", base)
	}
	want := []string{
		"api/internal/unlisted -> api/internal/plan (plan)",
		"api/internal/unlisted -> api/internal/tenant (tenant)",
		"no wall: nothing stops a new import that points the wrong way",
		"55 of 100 production lines are in packages whose row is not 'assigned'",
	}
	if strings.Join(it.Evidence, "\n") != strings.Join(want, "\n") {
		t.Fatalf("evidence = %q", it.Evidence)
	}
	// The domains: an import of the base is the right way and costs nothing
	// (tenant: 17 + 0 + 4). Plan also imports a package two domains share:
	// no cycle, one cross edge, named in its evidence (16 + 0 + 4).
	// Origin of the cross edge: 2026-11-16, ruler version 2 with a wall for
	// every domain gave all nine domains 25 of 25 while sixteen imports ran
	// from one domain into another's package or a shared one.
	if d := domain(t, a, "tenant"); d.Items[0].Score != 21 || d.Items[0].Measures["cycle_edges"] != 0 || d.Items[0].Measures["cross_edges"] != 0 {
		t.Errorf("tenant = %+v", d.Items[0])
	}
	if d := domain(t, a, "plan"); d.Items[0].Score != 20 || d.Items[0].Measures["cross_edges"] != 1 || d.Items[0].Evidence[0] != "api/internal/plan -> api/internal/both (mixed)" {
		t.Errorf("plan = %+v", d.Items[0])
	}
	if base.Items[0].Measures["cross_edges"] != 0 {
		t.Errorf("the base has cross edges: %+v", base.Items[0])
	}
	// A domain with no package is not measured, and the item this version
	// does not compute is unmeasured for everyone.
	if d := domain(t, a, "owner"); d.Items[0].Measured || d.Max != 0 || d.Items[1].Measured {
		t.Errorf("owner = %+v", d)
	}
	// With the wall, four more.
	in.WallHolds["base"] = true
	if got := domain(t, Score(in), "base").Items[0].Score; got != 21.8 {
		t.Errorf("with the wall base = %v, want 21.8", got)
	}
}

// Between two domains neither of which is the base, a dependency both ways
// counts against both; one way counts against neither. The floor is 11.
func TestAMutualDependencyCountsAgainstBoth(t *testing.T) {
	oneWay := input(t, own, false,
		Package{Path: "api/internal/plan", Imports: []string{"api/internal/owner"}, Lines: 1},
		Package{Path: "api/internal/owner", Lines: 1})
	if d := domain(t, Score(oneWay), "plan"); d.Items[0].Measures["cycle_edges"] != 0 || d.Items[0].Measures["cross_edges"] != 1 {
		t.Fatalf("one way: plan = %+v", d.Items[0])
	}
	both := input(t, own, false,
		Package{Path: "api/internal/plan", Imports: []string{"api/internal/owner"}, Lines: 1},
		Package{Path: "api/internal/owner", Imports: []string{"api/internal/plan"}, Lines: 1})
	a := Score(both)
	for _, id := range []string{"plan", "owner"} {
		if d := domain(t, a, id); d.Items[0].Measures["cycle_edges"] != 1 || d.Items[0].Score != 20 {
			t.Errorf("both ways: %s = %+v", id, d.Items[0])
		}
	}
	// Twenty edges would be 17 - 10; the direction part stops at 11.
	rows := []string{}
	pkgs := []Package{{Path: "api/internal/owner", Imports: []string{"api/internal/plan/a"}, Lines: 1}}
	for _, n := range strings.Fields("a b c d e f g h i j k l m n o p q r s t") {
		rows = append(rows, `"api/internal/plan/`+n+`":{"domains":["plan"],"status":"assigned"}`)
		pkgs = append(pkgs, Package{Path: "api/internal/plan/" + n, Imports: []string{"api/internal/owner"}, Lines: 1})
	}
	o := strings.Replace(own, `"packages":{`, `"packages":{`+strings.Join(rows, ",")+`,`, 1)
	if d := domain(t, Score(input(t, o, false, pkgs...)), "plan"); d.Items[0].Measures["cycle_edges"] != 20 || d.Items[0].Score != 15 {
		t.Errorf("twenty edges: plan = %+v", d.Items[0])
	}
}

// Origin: 2026-11-16, version 1 of the ruler on main: lineage imports
// verification and nothing of verification imports lineage, yet lineage was
// charged two cycle edges, because verification imports the base, the base
// imports owner, owner imports a shared package and the shared package imports
// lineage. A way back through the base or a shared package is no cycle.
func TestAWayBackThroughTheBaseOrASharedPackageIsNoCycle(t *testing.T) {
	in := input(t, own, false,
		Package{Path: "api/internal/plan", Imports: []string{"api/internal/tenant"}, Lines: 1},
		Package{Path: "api/internal/tenant", Imports: []string{"api/internal/core", "api/internal/both"}, Lines: 1},
		Package{Path: "api/internal/core", Imports: []string{"api/internal/owner"}, Lines: 1},
		Package{Path: "api/internal/owner", Imports: []string{"api/internal/both", "api/internal/core"}, Lines: 1},
		Package{Path: "api/internal/both", Imports: []string{"api/internal/plan"}, Lines: 1})
	a := Score(in)
	if d := domain(t, a, "plan"); d.Items[0].Measures["cycle_edges"] != 0 {
		t.Errorf("plan = %+v", d.Items[0])
	}
	// The base's own import of owner is still one: owner stands on the base.
	if d := domain(t, a, "base"); d.Items[0].Measures["cycle_edges"] != 1 {
		t.Errorf("base = %+v", d.Items[0])
	}
}

func TestARulerOfAnotherVersionIsRefused(t *testing.T) {
	if _, err := ParseRuler([]byte(strings.Replace(testRuler, `"version":4`, `"version":5`, 1))); err == nil || !strings.Contains(err.Error(), "version 5") {
		t.Fatalf("err = %v", err)
	}
	if _, err := ParseRuler([]byte(strings.Replace(testRuler, `"max":75`, `"max":70`, 1))); err == nil || !strings.Contains(err.Error(), "not 100") {
		t.Fatalf("err = %v", err)
	}
}

// The ruler in the repository is the version this code computes, its items
// add up to 100, the two items this version measures are marked so and no
// other (contracts for 5 of its 20 points), and every wall it names is a test
// that is in the code.
func TestTheRepositoryRulerIsWhatThisCodeComputes(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	raw, err := os.ReadFile(filepath.Join(root, "config", "quality-ruler.json"))
	if err != nil {
		t.Fatal(err)
	}
	ruler, err := ParseRuler(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range ruler.Items {
		if it.Measured != (it.ID == "dependency_direction" || it.ID == "contracts") {
			t.Errorf("the ruler says %s measured=%v; this code measures dependency_direction and contracts only", it.ID, it.Measured)
		}
		if want := map[string]float64{"dependency_direction": 25, "contracts": GoSurfaceMax}[it.ID]; it.MeasuredMax != want {
			t.Errorf("the ruler says %v points of %s are measured; this code measures %v", it.MeasuredMax, it.ID, want)
		}
	}
	if len(ruler.Walls) == 0 {
		t.Fatal("the ruler names no wall")
	}
	for domain, wall := range ruler.Walls {
		src, err := os.ReadFile(filepath.Join(root, wall.File))
		if err != nil || !strings.Contains(string(src), "func "+wall.Test+"(t *testing.T)") {
			t.Errorf("the wall of %s, %s in %s, is not in the code (%v)", domain, wall.Test, wall.File, err)
		}
	}
}
