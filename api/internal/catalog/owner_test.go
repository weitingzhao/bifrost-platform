package catalog

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// Origin: 0.2 map line, 2026-11-20, segment 1 ("who owns X" answered by the
// system; the table of contents names each class's one source). Owner
// 11-20: the map is the one place everyone reads ownership from.

func scanned(t *testing.T, files ...string) *Scanner {
	t.Helper()
	tr := tree(files...)
	read := tr.Read
	tr.Files = append(tr.Files, IndexPath)
	tr.Read = func(p string) ([]byte, bool, error) {
		if p == IndexPath {
			return []byte(`{"version":1,"classes":[
 {"class":"go-package","sources":["config/domain-ownership.json"],"reader":"api/internal/a","check":"scanned"},
 {"class":"node","sources":["config/topology.yaml"],"also":["elsewhere"],"check":"registry"},
 {"class":"seat","sources":[],"check":"unseen"}]}`), true, nil
		}
		return read(p)
	}
	s := &Scanner{Kept: memKept{}, Source: func(context.Context, string) (Tree, error) { return tr, nil }}
	if _, err := s.ScanOnce(context.Background(), "r"); err != nil {
		t.Fatal(err)
	}
	return s
}

func owner(t *testing.T, s *Scanner, p string) Owner {
	t.Helper()
	w := httptest.NewRecorder()
	OwnerRoute{Scanner: s}.ServeHTTP(w, httptest.NewRequest("GET", "/catalog/owner?repo=r&path="+p, nil))
	var o Owner
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil || w.Code != 200 {
		t.Fatalf("%s: code %d body %s", p, w.Code, w.Body.String())
	}
	return o
}

func TestOwnerAnswersFromTheKeptTables(t *testing.T) {
	s := scanned(t, "api/internal/a/a.go", "api/internal/a/sub/x.json", "api/internal/m/m.go", "api/internal/b/b.go", "console/src/pages/x/y.tsx", "mcp/p/q.ts")
	if o := owner(t, s, "api/internal/a/sub/x.json"); !o.Perceived || o.By != "package api/internal/a" || len(o.Domains) != 1 || o.Domains[0] != "plan" {
		t.Errorf("a/sub: %+v", o)
	}
	if o := owner(t, s, "api/internal/m/m.go"); !o.Perceived || len(o.Domains) != 2 || o.Note == "" {
		t.Errorf("mixed: %+v, want two domains and a debt note", o)
	}
	if o := owner(t, s, "console/src/pages/x/y.tsx"); !o.Perceived || o.By != "group console/src/pages/x" {
		t.Errorf("group: %+v", o)
	}
	// The table's default would give b to foundation; the map does not.
	if o := owner(t, s, "api/internal/b/b.go"); o.Perceived || o.Entry == nil || o.Entry.Path != "api/internal/b" {
		t.Errorf("no row: %+v, want unperceived in entry api/internal/b", o)
	}
	if o := owner(t, s, "mcp/p/q.ts"); o.Perceived || o.Entry == nil || o.Entry.Path != "mcp/p" {
		t.Errorf("mcp: %+v", o)
	}
}

func TestOwnerBeforeAnyScanSaysSo(t *testing.T) {
	s := &Scanner{Kept: memKept{}}
	if o := owner(t, s, "x"); o.Perceived || o.Note != "not scanned yet" {
		t.Fatalf("%+v", o)
	}
}

func TestIndexIsKeptWithMissingSourcesAndCounts(t *testing.T) {
	s := scanned(t, "api/internal/a/a.go")
	w := httptest.NewRecorder()
	IndexRoute{Scanner: s}.ServeHTTP(w, httptest.NewRequest("GET", "/catalog/index", nil))
	var ix Index
	if err := json.Unmarshal(w.Body.Bytes(), &ix); err != nil {
		t.Fatal(err, w.Body.String())
	}
	// Origin: 2026-11-20 perceive first: each class says whether the machine
	// reconciles it with the real system or only has the registration.
	if ix.Scanned != 1 || ix.RegistryOnly != 1 || ix.Unseen != 1 {
		t.Errorf("checks %d/%d/%d, want 1/1/1", ix.Scanned, ix.RegistryOnly, ix.Unseen)
	}
	if _, err := ParseIndex([]byte(`{"classes":[{"class":"a","check":"maybe"}]}`), "c", nil, Table{}); err == nil {
		t.Error("a class with no valid check was accepted")
	}
	if ix.Measured != 2 || ix.Unmeasured != 1 || ix.Duplicated != 1 {
		t.Errorf("counts %+v", ix)
	}
	if len(ix.Missing) != 1 || ix.Missing[0] != "config/topology.yaml" {
		t.Errorf("missing = %v, want config/topology.yaml (the fixture tree has no such file)", ix.Missing)
	}
	if d := ix.Domain["go-package"]; len(d) != 1 || d[0] != "plan" {
		t.Errorf("domain of go-package = %v, want its reader's row (plan)", d)
	}
}

func TestIndexRefusesAClassNamedTwice(t *testing.T) {
	if _, err := ParseIndex([]byte(`{"classes":[{"class":"a"},{"class":"a"}]}`), "c", nil, Table{}); err == nil {
		t.Fatal("a class named twice must be refused: one class, one source")
	}
}
