package catalog

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

// 2026-11-21, Foundation's ask (biz-align/bus-readings-spec-2026-11-21.md
// section 2): Foundation judged ownership wrong three times that day counting
// production imports only; a package used only by tests, or only through an
// event, read as unused. The control is "c": no import of any kind, no user.
func TestUsersCountProductionTestAndEventApart(t *testing.T) {
	pkgs := []GoPackage{
		{Dir: "api/internal/a", Domain: "plan", Imports: []string{"api/internal/b"}},
		{Dir: "api/internal/b", Domain: "foundation", TestImports: []string{"api/internal/b", "api/internal/d"}},
		{Dir: "api/internal/c", Domain: "lineage"},
		{Dir: "api/internal/d", Domain: "approvals"},
		{Dir: "api/internal/e", Domain: "plan", TestImports: []string{"api/internal/d", "api/internal/b"}},
	}
	byPkg := map[string]PackageUsers{}
	for _, u := range packageUsers(pkgs) {
		byPkg[u.Package] = u
	}
	if u := byPkg["api/internal/b"]; len(u.Users.Prod) != 1 || u.Users.Prod[0] != "api/internal/a" || len(u.Users.Test) != 1 || u.UserDomains.Test[0] != "plan" {
		t.Errorf("b: %+v (its own test import is not a use)", u)
	}
	if u := byPkg["api/internal/d"]; len(u.Users.Prod) != 0 || len(u.Users.Test) != 2 || len(u.UserDomains.Test) != 2 {
		t.Errorf("d is used by tests only and must say so: %+v", u)
	}
	if u := byPkg["api/internal/c"]; len(u.Users.Prod)+len(u.Users.Test)+len(u.Users.Event) != 0 || u.Users.Event == nil {
		t.Errorf("c (control): %+v", u)
	}

	uses := &Uses{Base: "foundation", Packages: pkgs, Events: []EventUse{
		{Type: "x.happened", From: "plan", To: "coordination"}, {Type: "y.happened", From: "plan", To: "coordination"},
		{Type: "z.happened", From: "plan", To: "plan"}}}
	edges := []Edge{{From: "plan", To: "foundation", Imports: 1, ToBase: true}}
	refs, toBase := references(edges, nil, uses)
	got := map[string]Reference{}
	for _, r := range refs {
		got[r.Kind+" "+r.From+">"+r.To] = r
	}
	if toBase != 1 || len(refs) != 3 {
		t.Fatalf("refs %+v", refs)
	}
	// b's test import of d crosses foundation > approvals; e's of d plan >
	// approvals; e's of b goes to the base and is not a reference.
	if r := got["import foundation>approvals"]; r.TestImports != 1 || r.ProdImports != 0 {
		t.Errorf("%+v", r)
	}
	if r := got["import plan>approvals"]; r.TestImports != 1 {
		t.Errorf("%+v", r)
	}
	if r := got["event plan>coordination"]; r.Events != 2 {
		t.Errorf("%+v (an event inside one domain is no reference)", r)
	}
}

func TestUsersAreKeptAndServedOnePackageOrAll(t *testing.T) {
	s := statusScanner(t, time.Date(2026, 11, 21, 21, 0, 0, 0, time.UTC))
	calls := 0
	in := StatusInputs{Uses: func(context.Context, string) (Uses, bool, error) {
		calls++
		return Uses{Commit: "c1", Packages: []GoPackage{{Dir: "api/internal/a", Domain: "plan", Imports: []string{"api/internal/b"}}, {Dir: "api/internal/b", Domain: "plan"}}}, true, nil
	}, Boundary: func(context.Context, string) ([]Edge, bool, error) { return nil, true, nil }}
	s.KeepStatus(context.Background(), in)
	if calls != 1 {
		t.Errorf("Uses read %d times in one round, want 1", calls)
	}
	w := httptest.NewRecorder()
	UsersRoute{Scanner: s}.ServeHTTP(w, httptest.NewRequest("GET", "/catalog/users?package=api/internal/b", nil))
	var one PackageUsers
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &one) != nil || len(one.Users.Prod) != 1 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	UsersRoute{Scanner: s}.ServeHTTP(w, httptest.NewRequest("GET", "/catalog/users?package=api/internal/zz", nil))
	if w.Code != 404 {
		t.Errorf("unknown package: %d", w.Code)
	}
	w = httptest.NewRecorder()
	UsersRoute{Scanner: s}.ServeHTTP(w, httptest.NewRequest("GET", "/catalog/users", nil))
	var all UsersDoc
	if json.Unmarshal(w.Body.Bytes(), &all) != nil || !all.Measured || all.Commit != "c1" || len(all.Packages) != 2 {
		t.Errorf("%s", w.Body)
	}
}

// 2026-11-21: the events between domains come from the bus registry this
// binary carries. The control: an event inside one domain is never listed
// with an empty side, and at least one goes across.
func TestRegistryEventsGoBetweenDomains(t *testing.T) {
	evs, ok := RegistryEvents()
	if !ok || len(evs) == 0 {
		t.Fatalf("no events read: %v", ok)
	}
	across := 0
	for _, e := range evs {
		if e.From == "" || e.To == "" {
			t.Errorf("an event with no domain: %+v", e)
		}
		if e.From != e.To {
			across++
		}
	}
	if across == 0 {
		t.Error("no event goes from one domain to another")
	}
}
