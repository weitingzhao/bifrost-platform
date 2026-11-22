package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Origin: map line, 2026-11-20, charter against law 50: "what does domain Y
// own, what does it open, is it healthy" had no read; it took the owner read
// path by path plus the table by hand.

func TestOneDomainsPageIsWhatTheScanKept(t *testing.T) {
	now := time.Date(2026, 11, 20, 17, 0, 0, 0, time.UTC)
	s := statusScanner(t, now)
	s.KeepStatus(context.Background(), StatusInputs{})

	d, ok := s.DomainOf("plan")
	if !ok {
		t.Fatal("plan not found")
	}
	if len(d.Packages) != 1 || d.Packages[0].Path != "api/internal/a" {
		t.Errorf("packages %+v", d.Packages)
	}
	if len(d.Workloads) != 1 || d.Workloads[0].Path != "ns-plan" {
		t.Errorf("workloads %+v", d.Workloads)
	}
	if !strings.Contains(string(d.Contract), "/api/v1/p") {
		t.Errorf("contract %s", d.Contract)
	}
	if d.Status == nil || d.Status.ID != "plan" {
		t.Errorf("status %+v", d.Status)
	}
	if _, ok := s.DomainOf("nobody"); ok {
		t.Error("a domain the table does not name was found")
	}

	rec := httptest.NewRecorder()
	DomainRoute{Scanner: s}.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/catalog/domain?id=tenant", nil))
	var got Domain
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil || got.ID != "tenant" || len(got.Workloads) != 1 {
		t.Errorf("route: %d %s", rec.Code, rec.Body.String())
	}
	for q, want := range map[string]int{"id=": 400, "id=nobody": 404, "id=../x": 400} {
		rec := httptest.NewRecorder()
		DomainRoute{Scanner: s}.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/catalog/domain?"+q, nil))
		if rec.Code != want {
			t.Errorf("%s: %d, want %d", q, rec.Code, want)
		}
	}
}

func TestARowNamingTwoDomainsIsMarkedShared(t *testing.T) {
	rows := rowsOf("plan", map[string][]string{"a": {"plan"}, "b": {"plan", "owner"}, "c": {"owner"}})
	if len(rows) != 2 || rows[0].Shared != nil || len(rows[1].Shared) != 1 || rows[1].Shared[0] != "owner" {
		t.Errorf("%+v", rows)
	}
}

// 2026-11-21, City (箭10): a district's second layer had no buildings, only a
// list of packages (foundation 73), and asked the map line to lead the
// grouping. The default is computed from the paths, never typed: the first
// directory under the Go root and "internal". The control: a package
// directly under the root is its own building, not dropped.
func TestBuildingsGroupPackagesByTheirFirstDirectory(t *testing.T) {
	rows := []Row{{Path: "api/internal/bus/relay"}, {Path: "api/internal/bus/gate"}, {Path: "api/internal/catalog"},
		{Path: "api/cmd/domainscore"}, {Path: "api/internal/bus"}, {Path: "tools"}}
	got := buildingsOf(rows, []string{"api"})
	if len(got) != 4 || got[0].ID != "bus" || len(got[0].Packages) != 3 {
		t.Fatalf("%+v", got)
	}
	ids := map[string]bool{}
	for _, b := range got {
		ids[b.ID] = true
	}
	if !ids["cmd"] || !ids["catalog"] || !ids["tools"] {
		t.Errorf("%+v", got)
	}
}
