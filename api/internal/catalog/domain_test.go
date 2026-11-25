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

// Origin: map line, 2026-11-20, charter against LAW-10: "what does domain Y
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

// 2026-11-22, LAW-11: more than 16 in one layer is split too fine; City
// draws 16. The largest 15 keep their own building and the rest is one
// "other", counted. The control: 16 buildings are not folded.
func TestBuildingsPastSixteenFoldIntoOther(t *testing.T) {
	var b []Building
	for i := 0; i < 20; i++ {
		b = append(b, Building{ID: string(rune('a' + i)), Packages: []string{string(rune('a' + i))}})
	}
	got, over := capBuildings(b)
	if len(got) != MaxPerLayer || over != 5 || !got[15].Other || len(got[15].Packages) != 5 {
		t.Fatalf("%d buildings, over %d: %+v", len(got), over, got[15])
	}
	if got, over := capBuildings(b[:16]); len(got) != 16 || over != 0 {
		t.Errorf("16 folded: %d %d", len(got), over)
	}
}

// 2026-11-22, LAW-10: each domain explains itself in content, read on demand
// through the domain route; someone who wants a thing done asks by the task.
// The control: a word no task holds finds nothing, and a domain that wrote
// no tasks is named as silent rather than skipped.
func TestTasksAreFoundByWhatSomeoneWantsDone(t *testing.T) {
	s := &Scanner{Kept: memKept{}}
	rows, _ := json.Marshal(keptRows{Repo: "r", Commit: "c", Domains: []byte(`[
  {"id":"verification","about":"merge and CI","tasks":[{"task":"why is my commit red","use":["get_unit_runs","GET /api/v1/units/plan"]},{"task":"rerun CI","use":["start_pipeline_run"]}]},
  {"id":"plan"}]`)})
	_ = s.kept().Write(keptWorkloads, rows)
	m, silent := s.TasksFor("Commit RED")
	if len(m) != 1 || m[0].Domain != "verification" || m[0].Use[0] != "get_unit_runs" {
		t.Fatalf("%+v", m)
	}
	if len(silent) != 1 || silent[0] != "plan" {
		t.Errorf("silent %v", silent)
	}
	if m, _ := s.TasksFor("payroll"); len(m) != 0 {
		t.Errorf("control: %+v", m)
	}
	w := httptest.NewRecorder()
	DomainRoute{Scanner: s}.ServeHTTP(w, httptest.NewRequest("GET", "/catalog/domain?task=rerun", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "start_pipeline_run") {
		t.Errorf("%d %s", w.Code, w.Body)
	}
}

// 2026-11-22, Approvals: api/cmd/owner-exec is the command of ownerexec and
// belongs in its building. The control: a command with no such building
// stays in cmd.
func TestACommandJoinsTheBuildingOfItsPackage(t *testing.T) {
	got := buildingsOf([]Row{{Path: "api/internal/ownerexec"}, {Path: "api/cmd/owner-exec"}, {Path: "api/cmd/lonely"}}, []string{"api"})
	by := map[string][]string{}
	for _, b := range got {
		by[b.ID] = b.Packages
	}
	if len(by["ownerexec"]) != 2 || len(by["cmd"]) != 1 || by["cmd"][0] != "api/cmd/lonely" {
		t.Fatalf("%+v", got)
	}
}

// 2026-11-22, LAW-10, CI first: CI wrote its self-description beside the
// landing script (bring, refusals, must_answer) and the map reads the file
// the domain row names each round; its lines are found by task. The
// control: a row naming a file that is not there keeps a note, not a
// silent empty description.
func TestSelfDescriptionIsReadFromTheFileTheRowNames(t *testing.T) {
	s := &Scanner{Kept: memKept{}}
	rows, _ := json.Marshal(keptRows{Repo: "platform", Commit: "c", Domains: []byte(`[
  {"id":"verification","self":"infra:scripts/landing/admission.yaml"},
  {"id":"plan","self":"infra:nowhere.yaml"}]`)})
	_ = s.kept().Write(keptWorkloads, rows)
	doc := "about: merge and verification\nbring:\n  - {id: trailers, what: every commit carries four trailers, command: git commit -F msg}\nrefusals:\n  - {match: \"refused: no CI run of\", kind: you, do: see why CI is red}\nmust_answer:\n  - why a commit did not land\n"
	s.keepSelf("infra", Tree{Commit: "i1", Read: func(p string) ([]byte, bool, error) {
		if p == "scripts/landing/admission.yaml" {
			return []byte(doc), true, nil
		}
		return nil, false, nil
	}})
	d, ok := s.DomainOf("verification")
	if !ok || d.Self == nil || d.About != "merge and verification" || d.Self.Commit != "i1" {
		t.Fatalf("%+v", d.Self)
	}
	if m, _ := s.TasksFor("did not land"); len(m) != 1 || m[0].Note != "must answer" {
		t.Errorf("must_answer: %+v", m)
	}
	if m, _ := s.TasksFor("trailers"); len(m) != 1 || m[0].Use[0] != "git commit -F msg" {
		t.Errorf("bring: %+v", m)
	}
	if p, _ := s.DomainOf("plan"); p.Self == nil || p.Self.Note == "" {
		t.Errorf("control: %+v", p.Self)
	}
}

// 2026-11-22, LAW-11, Approvals: a domain merges buildings its own way
// without moving code, by naming path prefixes in its row. The control: a
// package no declared building names is grouped by directory as before.
func TestADomainCanDeclareItsBuildings(t *testing.T) {
	rows := []Row{{Path: "api/internal/agentrt/questionbridge"}, {Path: "api/internal/agentrt/answerack"}, {Path: "api/internal/approvals"}}
	got := declaredBuildings(rows, []string{"api"}, []declaredBuilding{{ID: "question-bridge", Paths: []string{"api/internal/agentrt/questionbridge", "api/internal/agentrt/answerack/"}}})
	if len(got) != 2 || got[0].ID != "question-bridge" || !got[0].Declared || len(got[0].Packages) != 2 || got[1].ID != "approvals" {
		t.Fatalf("%+v", got)
	}
}
