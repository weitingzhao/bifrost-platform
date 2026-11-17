package domainscore

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const testModule = "example.test/"

// source is one package of a small module held in memory: its path from the
// repository root and its files by name.
type source struct {
	path  string
	files map[string]string
}

type fromMemory map[string]*types.Package

func (m fromMemory) Import(path string) (*types.Package, error) {
	if p, ok := m[path]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("no package %s in the test module", path)
}

// counted type-checks the sources in the order given (a package after the
// ones it imports), test files included the way a test build has them, and
// returns the input Score would get: the packages with their production
// imports and the counts of Surface.
func counted(t *testing.T, ownership string, sources ...source) Input {
	t.Helper()
	in, _ := surfaced(t, ownership, sources...)
	return in
}

// surfaced is counted, with the Surface it counted with.
func surfaced(t *testing.T, ownership string, sources ...source) (Input, *Surface) {
	t.Helper()
	ruler, err := ParseRuler([]byte(strings.Replace(testRuler, `"max":75,"by":"machine","measured":false`, `"max":75,"by":"machine","measured":true,"measured_max":5`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	in := Input{Commit: "c", Ruler: ruler, WallHolds: map[string]bool{}}
	if err = json.Unmarshal([]byte(ownership), &in.Ownership); err != nil {
		t.Fatal(err)
	}
	fset, checked := token.NewFileSet(), fromMemory{}
	infos := map[string]*types.Info{}
	for _, s := range sources {
		pkg := Package{Path: s.path, Lines: 1}
		var files []*ast.File
		names := make([]string, 0, len(s.files))
		for name := range s.files {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			f, err := parser.ParseFile(fset, s.path+"/"+name, s.files[name], parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, f)
			for _, imp := range f.Imports {
				if dep, _ := strconv.Unquote(imp.Path.Value); !strings.HasSuffix(name, "_test.go") {
					pkg.Imports = append(pkg.Imports, strings.TrimPrefix(dep, testModule))
				}
			}
		}
		info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
		p, err := (&types.Config{Importer: checked}).Check(testModule+s.path, fset, files, info)
		if err != nil {
			t.Fatalf("%s: %v", s.path, err)
		}
		checked[testModule+s.path], infos[s.path] = p, info
		in.Packages = append(in.Packages, pkg)
	}
	surface := NewSurface(NewGraph(in.Ownership, in.Packages), testModule)
	for _, s := range sources {
		surface.Add(s.path, fset, infos[s.path])
	}
	in.Contracts = surface.Uses()
	return in, surface
}

// The provider every case uses: a package of the base with one interface,
// one concrete type with a field and a method, and one function.
var core = source{"api/internal/core", map[string]string{"core.go": `package core
type Reader interface{ Read() string }
type Store struct{ Name string }
func (s *Store) Get() string { return s.Name }
func Open() *Store { return &Store{} }
func Unused() {}
type hidden struct{}
func (hidden) Exported() {}
`}}

func user(path, name, body string) source {
	return source{path, map[string]string{name: "package p\nimport \"" + testModule + "api/internal/core\"\n" + body}}
}

func item(t *testing.T, in Input, id string) Item {
	t.Helper()
	return domain(t, Score(in), id).Items[1]
}

// Origin: 2026-11-16: the independent review counted, for foundation, 6 calls
// through an interface against 412 on concrete types and 1,177 field accesses
// from other domains; the coordinator made CI the referee that measures this
// for every domain the same way.
//
// Each case guards one line of what is counted and for whom; "want" is the
// base's interface calls, concrete calls, function calls, field accesses.
func TestUsesByOtherDomainsAreCountedInTheirKind(t *testing.T) {
	cases := []struct {
		name string
		// guards says what would go wrong unseen without the case.
		guards string
		users  []source
		want   [4]float64
	}{
		{"an interface call", "a call through an interface the provider declares is the contract the item rewards; counted as concrete it would cost the provider",
			[]source{user("api/internal/plan", "p.go", `func f(r core.Reader) string { return r.Read() }`)}, [4]float64{1, 0, 0, 0}},
		{"a concrete call", "a call of a method of the provider's struct is what the review's 412 counted",
			[]source{user("api/internal/plan", "p.go", `func f(s *core.Store) string { g := s.Get; return s.Get() + g() }`)}, [4]float64{0, 2, 0, 0}},
		{"a field access", "a read, a write and a key of a struct literal are each one access, as in the review's 1,177 (241 of them were store filling eventlog.Event)",
			[]source{user("api/internal/plan", "p.go", `func f(s *core.Store) core.Store { s.Name = s.Name + "x"; return core.Store{Name: "y"} }`)}, [4]float64{0, 0, 0, 3}},
		{"a function call", "a package-level function is reported apart: the review's concrete calls were methods, and counted with them a function contract (eventlog.Append) would cost its provider",
			[]source{user("api/internal/plan", "p.go", `var s = core.Open()`)}, [4]float64{0, 0, 1, 0}},
		{"an interface the user declares", "a call through the user's own interface that the provider's type satisfies is not a use of the provider's surface at all",
			[]source{user("api/internal/plan", "p.go", `type getter interface{ Get() string }
func f(g getter) string { return g.Get() }
var _ getter = (*core.Store)(nil)`)}, [4]float64{0, 0, 0, 0}},
		{"the same domain", "a package of the base using another package of the base is inside the domain: counted, every domain's own plumbing would read as other domains holding on to it",
			[]source{user("api/internal/unlisted", "p.go", `func f(s *core.Store, r core.Reader) string { return s.Get() + r.Read() + s.Name + core.Open().Name }`)}, [4]float64{0, 0, 0, 0}},
		{"a test file", "tests may reach into anything; the count is of production code only",
			[]source{{"api/internal/plan", map[string]string{"p.go": "package p\n", "p_test.go": "package p\nimport \"" + testModule + "api/internal/core\"\nfunc f(s *core.Store, r core.Reader) string { return s.Get() + r.Read() + s.Name + core.Open().Name }"}}}, [4]float64{0, 0, 0, 0}},
		{"a shared package", "a package several domains share (mixed) is not the provider's own: its uses count for the provider, as the review counted store's 241 accesses of eventlog",
			[]source{user("api/internal/both", "p.go", `func f(s *core.Store, r core.Reader) string { return s.Get() + r.Read() + s.Name + core.Open().Name }`)}, [4]float64{1, 1, 1, 2}},
		{"an assembly root", "api/cmd wires every domain and is nobody's user, as in the dependency graph",
			[]source{user("api/cmd/root", "p.go", `func f(s *core.Store, r core.Reader) string { return s.Get() + r.Read() + s.Name + core.Open().Name }`)}, [4]float64{0, 0, 0, 0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := item(t, counted(t, own, append([]source{core}, c.users...)...), "base").Measures
			got := [4]float64{m["interface_calls"], m["concrete_calls"], m["function_calls"], m["field_accesses"]}
			if got != c.want {
				t.Errorf("interface, concrete, function, field = %v, want %v (guards: %s)", got, c.want, c.guards)
			}
		})
	}
}

// Origin: 2026-11-17, the decoupling line's contract check: a name one
// domain's production code takes from another must be registered as that
// domain's contract or held in a baseline, so the check needs the names and
// who takes them, counted the way the ruler counts. Guards: one entry per
// name and user package (the same name used twice by one package is one);
// a type named in a signature is a use of it; a method is keyed by its
// type; the user's own domain, test files and the
// assembly roots are not users; the list is sorted, so a baseline diff is
// stable. Positive control: without the users map in Add the list is empty
// and the first comparison fails.
func TestUseListNamesEachNameAndWhoTakesIt(t *testing.T) {
	_, s := surfaced(t, own, core,
		user("api/internal/plan", "p.go", `func f(s *core.Store, r core.Reader) string { return s.Get() + s.Get() + r.Read() + s.Name + core.Open().Name }`),
		user("api/internal/both", "p.go", `var o = core.Open()`),
		user("api/internal/unlisted", "p.go", `var o = core.Open()`),
		user("api/cmd/root", "p.go", `var o = core.Open()`),
		source{"api/internal/plan2", map[string]string{"p.go": "package p\n", "p_test.go": "package p\nimport \"" + testModule + "api/internal/core\"\nvar o = core.Open()\n"}},
	)
	got := s.UseList()
	want := []Use{
		{Key: "api/internal/core.Open", Domain: "base", User: "api/internal/both"},
		{Key: "api/internal/core.Open", Domain: "base", User: "api/internal/plan"},
		{Key: "api/internal/core.Reader", Domain: "base", User: "api/internal/plan"},
		{Key: "api/internal/core.Reader.Read", Domain: "base", User: "api/internal/plan"},
		{Key: "api/internal/core.Store", Domain: "base", User: "api/internal/plan"},
		{Key: "api/internal/core.Store.Get", Domain: "base", User: "api/internal/plan"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("UseList =\n%v\nwant\n%v", got, want)
	}
}

// Origin: the same review and ruling as above; its row for contracts took 5
// of 20 off the base for a Go surface "almost nothing goes through an
// interface" of, and the ruler's version 4 computes those 5 points and no
// other. Guards: the score is 5 x the interface share out of 5 (not out of
// the item's 20), the domain's total adds 5 to its max, the evidence names
// the type held and by whom, and the exported names are counted once each.
func TestTheGoSurfaceIsFivePointsByTheInterfaceShare(t *testing.T) {
	in := counted(t, own, core,
		user("api/internal/plan", "p.go", `func f(s *core.Store, r core.Reader) string { return s.Get() + s.Get() + s.Get() + r.Read() + s.Name }`),
		user("api/internal/tenant", "p.go", `var s = core.Open()`))
	base := domain(t, Score(in), "base")
	it := base.Items[1]
	// 1 interface call of 4 method calls: 5 x 0.25 = 1.3 (one decimal).
	if !it.Measured || it.Max != 75 || it.MeasuredMax != 5 || it.Score != 1.3 || it.Measures["interface_share"] != 0.25 || it.Measures["users"] != 2 {
		t.Fatalf("contracts of the base = %+v", it)
	}
	// Reader, Reader.Read, Store, Store.Get, Open, Unused: 6 exported names
	// (the method of the unexported type is not surface); others name all
	// but Unused.
	if it.Measures["exported_total"] != 6 || it.Measures["exported_used_by_others"] != 5 {
		t.Errorf("exported = %v, used by others = %v", it.Measures["exported_total"], it.Measures["exported_used_by_others"])
	}
	if want := "api/internal/core.Store ← api/internal/plan: 4 (method calls 3, field accesses 1)"; it.Evidence[0] != want {
		t.Errorf("evidence = %q, want first %q", it.Evidence, want)
	}
	if last := it.Evidence[len(it.Evidence)-1]; !strings.HasPrefix(last, "5 of the item's 75 points are measured; the other 70 ") {
		t.Errorf("the evidence does not say what is unmeasured: %q", last)
	}
	// Direction 17 + assigned share 4 = 21 of 25, and 1.3 of 5 on top.
	if base.Score != 22.3 || base.Max != 30 {
		t.Errorf("base total = %v of %v, want 22.3 of 30", base.Score, base.Max)
	}
	if _, ok := it.Measures["http_routes"]; ok {
		t.Errorf("a route count with no mount file: %+v", it.Measures)
	}
	in.Routes = map[string]int{"base": 7, "cluster": 3}
	if got := item(t, in, "base").Measures["http_routes"]; got != 7 {
		t.Errorf("http_routes = %v, want 7", got)
	}
}

// Origin: the same ruling. A domain no other domain calls a method of has a
// defined score, 5: the points are taken off for other domains being held to
// its concrete types and none is. (Unmeasured would drop the domain's total
// to another max than its neighbours'; zero would charge it for having no
// users.) A tree that was not type-checked leaves the item unmeasured for
// everyone, never zero.
func TestADomainNobodyCallsKeepsTheFivePoints(t *testing.T) {
	in := counted(t, own, core, source{"api/internal/tenant", map[string]string{"t.go": "package tenant\ntype T struct{ A int }\nfunc (T) M() {}\n"}})
	tenant := domain(t, Score(in), "tenant")
	it := tenant.Items[1]
	if !it.Measured || it.Score != 5 || it.Measures["users"] != 0 || it.Measures["interface_share"] != 1 || !strings.HasPrefix(it.Evidence[0], "no other domain calls a method") {
		t.Fatalf("tenant = %+v", it)
	}
	if tenant.Max != 30 {
		t.Errorf("tenant max = %v", tenant.Max)
	}
	// Only functions and fields used: still no method call, still 5.
	in = counted(t, own, core, user("api/internal/plan", "p.go", `var n = core.Open().Name`))
	if it := item(t, in, "base"); it.Score != 5 || it.Measures["function_calls"] != 1 || it.Measures["field_accesses"] != 1 {
		t.Errorf("base used by functions and fields only = %+v", it)
	}
	in.Contracts = nil
	base := domain(t, Score(in), "base")
	if it := base.Items[1]; it.Measured || it.Score != 0 || it.MeasuredMax != 0 || base.Max != 25 {
		t.Errorf("without a type-checked tree: %+v, max %v", it, base.Max)
	}
}

// Origin: the coordinator's ruling of 2026-11-16 names the routes a domain
// mounts (api/internal/server/mount_<domain>.go) as one of the four kinds of
// contract; version 4 reports their count. Guards: a route is counted once
// whatever wraps the router, and a call that is not a route (Use, With, a
// helper called Get with no path) is not.
func TestRoutesOfAMountFileAreCounted(t *testing.T) {
	n, err := CountRoutes([]byte(`package server
func (s *Server) mountOwner(r chi.Router) {
	r.Get("/a", s.a)
	r.With(s.auth.Require(1)).Post("/b/{id}", s.b)
	r.Group(func(r chi.Router) {
		r.Use(s.auth.Require(2))
		r.Method("PUT", "/c", s.c)
		r.Handle("/d", s.d)
	})
	x := s.cache.Get("key")
	_ = s.store.Get(x)
}`))
	if err != nil || n != 4 {
		t.Fatalf("routes = %d, %v; want 4", n, err)
	}
	if _, err = CountRoutes([]byte("not go")); err == nil {
		t.Error("a file that does not parse was counted")
	}
}

// Origin: ruler rule 4 and the version 4 design: an item measured in part
// must say how many of its points are measured, and its parts must add up.
// Guards: a ruler edited to claim more measured points than this code
// computes, or whose parts no longer add up to the item, is refused.
func TestARulerWhoseMeasuredPartsDoNotAddUpIsRefused(t *testing.T) {
	with := func(contracts string) error {
		_, err := ParseRuler([]byte(strings.Replace(testRuler, `"max":75,"by":"machine","measured":false`, `"max":75,"by":"machine",`+contracts, 1)))
		return err
	}
	good := `"measured":true,"measured_max":5,"parts":[{"id":"go","max":5,"measured":true},{"id":"rest","max":70,"measured":false}]`
	if err := with(good); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		`"measured":true,"measured_max":9`,
		`"measured":true`,
		`"measured":false,"measured_max":5`,
		strings.Replace(good, `"max":70`, `"max":60`, 1),
		strings.Replace(good, `"max":70,"measured":false`, `"max":70,"measured":true`, 1),
	} {
		if err := with(bad); err == nil {
			t.Errorf("accepted: %s", bad)
		}
	}
}
