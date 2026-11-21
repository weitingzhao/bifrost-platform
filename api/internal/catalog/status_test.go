package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// Origin: 0.2 map line, 2026-11-20, segment 4 (plan section 6): 0 of 9
// domains had a status. Each light is measured or says unknown; none
// defaults to good.

func statusScanner(t *testing.T, now time.Time) *Scanner {
	t.Helper()
	home := tree("api/internal/a/a.go")
	read := home.Read
	home.Read = func(p string) ([]byte, bool, error) {
		if p == OwnershipPath {
			return []byte(`{"domains":[
  {"id":"plan","contracts":{"request_response":[{"method":"GET","path":"/api/v1/p"},{"method":"POST","path":"/api/v1/p"}]}},
  {"id":"tenant","name":"Tenants","tier":3,"contracts":{"request_response":[{"method":"GET","path":"/api/v1/t"}]}},
  {"id":"owner","contracts":{}}],
 "packages":{"api/internal/a":{"domains":["plan"]}},
 "workloads":{"ns-plan":{"domains":["plan"]},"ns-tenant":{"domains":["tenant"]}}}`), true, nil
		}
		return read(p)
	}
	s := &Scanner{Kept: memKept{}, Now: func() time.Time { return now }}
	s.Source = func(_ context.Context, repo string) (Tree, error) {
		if repo == ClusterRepo {
			rows, commit := s.WorkloadRows()
			return WorkloadTree(commit, []string{"ns-plan/web/deployment", "ns-tenant/api/deployment", "ns-tenant/cron/cronjob"}, rows)
		}
		return home, nil
	}
	for _, r := range []string{"home", ClusterRepo} {
		if _, err := s.ScanOnce(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func byID(st Status) map[string]DomainStatus {
	out := map[string]DomainStatus{}
	for _, d := range st.Domains {
		out[d.ID] = d
	}
	return out
}

func TestTheFourLightsAreMeasuredOrUnknown(t *testing.T) {
	now := time.Date(2026, 11, 20, 17, 0, 0, 0, time.UTC)
	s := statusScanner(t, now)
	in := StatusInputs{
		Windows: []time.Duration{time.Hour, 24 * time.Hour},
		Health: func() map[string]Health {
			return map[string]Health{
				"ns-plan/web":     {Desired: 2, Ready: 2, Judged: true},
				"ns-tenant/api":   {Desired: 2, Ready: 1, Judged: true},
				"ns-tenant/cron":  {},
				"elsewhere/thing": {Desired: 1, Ready: 0, Judged: true},
			}
		},
		Requests: func(_ context.Context, routes []string, w time.Duration) (map[string]RouteCount, error) {
			if w == time.Hour {
				return map[string]RouteCount{"/api/v1/p": {Total: 100, Errors: 1}, "/api/v1/t": {Total: 5}}, nil
			}
			return map[string]RouteCount{"/api/v1/p": {Total: 2400, Errors: 24}, "/api/v1/t": {Total: 15}}, nil
		},
		Boundary: func(_ context.Context, repo string) ([]Edge, bool, error) {
			if repo != "home" {
				t.Errorf("boundary asked for %q, want the repository whose table names the domains", repo)
			}
			return []Edge{{From: "tenant", To: "plan", Imports: 2, WrongWay: true}, {From: "plan", To: "owner", Imports: 1}}, true, nil
		},
	}
	d := byID(s.ComputeStatus(context.Background(), in))
	if len(d) != 3 {
		t.Fatalf("domains %v", d)
	}
	p, tn, o := d["plan"], d["tenant"], d["owner"]
	if p.Alive.Value != Good || tn.Alive.Value != Problem || o.Alive.Value != Unknown {
		t.Errorf("alive: plan %+v tenant %+v owner %+v", p.Alive, tn.Alive, o.Alive)
	}
	if p.Interfaces.Value != Good || tn.Interfaces.Value != Unknown || o.Interfaces.Value != Unknown {
		t.Errorf("interfaces: plan %+v tenant %+v (5 in 1h, 15 in 24h: too few) owner %+v (no contract)", p.Interfaces, tn.Interfaces, o.Interfaces)
	}
	if !strings.Contains(p.Interfaces.Why, "in 1h") || !strings.Contains(tn.Interfaces.Why, "15 requests in 24h") {
		t.Errorf("the reading names the window it was judged on: plan %q tenant %q", p.Interfaces.Why, tn.Interfaces.Why)
	}
	if p.Boundary.Value != Problem || tn.Boundary.Value != Problem || o.Boundary.Value != Good {
		t.Errorf("boundary: plan %+v tenant %+v owner %+v", p.Boundary, tn.Boundary, o.Boundary)
	}
	if p.Fresh.Value != Good {
		t.Errorf("fresh: %+v", p.Fresh)
	}
	if p.Overall != Problem || o.Overall != Unknown {
		t.Errorf("overall: plan %s owner %s; the worst light wins and unknown is not good", p.Overall, o.Overall)
	}
}

func TestNothingReadIsUnknownNotGood(t *testing.T) {
	now := time.Date(2026, 11, 20, 17, 0, 0, 0, time.UTC)
	s := statusScanner(t, now)
	in := StatusInputs{Boundary: func(context.Context, string) ([]Edge, bool, error) { return nil, false, errors.New("gitea down") }}
	for _, d := range s.ComputeStatus(context.Background(), in).Domains {
		if d.Alive.Value != Unknown || d.Interfaces.Value != Unknown || d.Boundary.Value != Unknown {
			t.Errorf("%s: %+v", d.ID, d)
		}
	}
	s.Now = func() time.Time { return now.Add(time.Hour) }
	if f := s.ComputeStatus(context.Background(), in).Domains[0].Fresh; f.Value != Problem {
		t.Errorf("an hour without a scan: fresh %+v, want problem", f)
	}
}

// Origin: map line, 2026-11-20, PROD readings at 18:21Z: plan, lineage and
// coordination read "no running workload of it on the map" though their code
// answered 222, 1 and 711 requests that hour inside platform-api; lineage had
// 1 request in an hour and about 570 in a day.
func TestADomainWithNoWorkloadReadsTheProcessesItRunsIn(t *testing.T) {
	now := time.Date(2026, 11, 20, 17, 0, 0, 0, time.UTC)
	s := statusScanner(t, now)
	hosts := []string{"pf/platform-api", "pf/platform-workers"}
	health := map[string]Health{
		"ns-plan/web":         {Desired: 1, Ready: 1, Judged: true},
		"pf/platform-api":     {Desired: 2, Ready: 2, Judged: true},
		"pf/platform-workers": {Desired: 1, Ready: 1, Judged: true},
	}
	in := StatusInputs{Hosts: hosts, Health: func() map[string]Health { return health }}
	d := byID(s.ComputeStatus(context.Background(), in))
	if a := d["plan"].Alive; a.Value != Good || strings.Contains(a.Why, "runs inside") {
		t.Errorf("plan has a workload of its own, read that: %+v", a)
	}
	if d["owner"].Alive.Value != Unknown {
		t.Errorf("owner has no package in the repository, not hosted: %+v", d["owner"].Alive)
	}
	delete(health, "ns-plan/web")
	d = byID(s.ComputeStatus(context.Background(), in))
	if a := d["plan"].Alive; a.Value != Good || !strings.Contains(a.Why, "pf/platform-api 2/2 ready") {
		t.Errorf("plan hosted, both ready: %+v", a)
	}
	health["pf/platform-workers"] = Health{Desired: 1, Ready: 0, Judged: true}
	if a := byID(s.ComputeStatus(context.Background(), in))["plan"].Alive; a.Value != Problem {
		t.Errorf("one host down: %+v, want problem", a)
	}
	delete(health, "pf/platform-api")
	delete(health, "pf/platform-workers")
	if a := byID(s.ComputeStatus(context.Background(), in))["plan"].Alive; a.Value != Unknown {
		t.Errorf("hosts not read: %+v, want unknown (positive control of the good case above)", a)
	}
}

// Origin: 2026-11-20, perceive-first brief: "total = on map + off map, for
// every repository and the cluster, counted by the machine; a gap is a
// scan that missed, and the light goes bad". And 90 of 107 workloads were
// on the map only through a whole namespace's row, not listed one by one.
func TestTheStatusCarriesEachEquationAndAGapTurnsFreshBad(t *testing.T) {
	now := time.Date(2026, 11, 20, 17, 0, 0, 0, time.UTC)
	s := statusScanner(t, now)
	st := s.ComputeStatus(context.Background(), StatusInputs{})
	if len(st.Inventory) != 2 {
		t.Fatalf("inventory %+v", st.Inventory)
	}
	for _, c := range st.Inventory {
		if !c.Measured || c.Gap != 0 || c.Total != c.OnMap+c.OffMap || c.Total == 0 {
			t.Errorf("%+v", c)
		}
	}
	if byID(st)["plan"].Fresh.Value != Good {
		t.Errorf("fresh %+v", byID(st)["plan"].Fresh)
	}
	cl, _ := s.Last(ClusterRepo)
	if len(cl.Covered) != 3 || !cl.Covered[0].ByNamespace || cl.Covered[0].Path != "ns-plan/web" || cl.Covered[0].Kind != "deployment" {
		t.Errorf("covered %+v", cl.Covered)
	}
	// Positive control: a kept pool whose numbers do not add up.
	cl.Gap = 1
	raw, _ := json.Marshal(cl)
	_ = s.kept().Write(keptName(ClusterRepo), raw)
	if f := byID(s.ComputeStatus(context.Background(), StatusInputs{}))["plan"].Fresh; f.Value != Bad || !strings.Contains(f.Why, "off by 1") {
		t.Errorf("gap: %+v", f)
	}
}

// Origin: 2026-11-21, the chief of staff's acceptance of "perceive first",
// item 4: City had each domain's lights and off-map count but not the
// cross-domain references that happen without being registered, who to whom.
func TestReferencesCountWhoToWhomAndWhatTheContractRegisters(t *testing.T) {
	var domains []contractDomain
	if err := json.Unmarshal([]byte(`[
  {"id":"coordination","contracts":{"go_surface":[{"name":"api/internal/threadinbox.Inbox.Post","users":["api/internal/approvals"]}]}},
  {"id":"owner","contracts":{}}]`), &domains); err != nil {
		t.Fatal(err)
	}
	edges := []Edge{
		{From: "owner", To: "coordination", Imports: 2, Pairs: [][2]string{
			{"api/internal/approvals", "api/internal/threadinbox"}, {"api/internal/questions", "api/internal/threadinbox"}}},
		{From: "owner", To: "foundation", Imports: 5, ToBase: true},
	}
	refs, toBase := references(edges, domains)
	if toBase != 5 || len(refs) != 1 {
		t.Fatalf("refs %+v, to base %d", refs, toBase)
	}
	if r := refs[0]; r.From != "owner" || r.To != "coordination" || r.Imports != 2 || r.Registered != 1 || r.Unregistered != 1 {
		t.Errorf("%+v", r)
	}
	// Positive control: with no contract, both pairs are unregistered.
	if refs, _ := references(edges, nil); refs[0].Unregistered != 2 {
		t.Errorf("no contract: %+v", refs)
	}
	if got := packageOf("api/internal/threadinbox.Inbox.Post"); got != "api/internal/threadinbox" {
		t.Errorf("packageOf %q", got)
	}

	s := statusScanner(t, time.Date(2026, 11, 21, 7, 0, 0, 0, time.UTC))
	if st := s.ComputeStatus(context.Background(), StatusInputs{}); st.References != nil {
		t.Errorf("no graph read: %+v", st.References)
	}
	calls := 0
	in := StatusInputs{Boundary: func(context.Context, string) ([]Edge, bool, error) { calls++; return edges, true, nil }}
	st := s.ComputeStatus(context.Background(), in)
	if len(st.References) != 1 || st.References[0].Unregistered != 2 || st.ToBase != 5 || calls != 1 {
		t.Errorf("status refs %+v, to base %d, graph read %d times", st.References, st.ToBase, calls)
	}
}

// 2026-11-21, City ask (m_01M7XYBJ): City names and orders its nine districts
// by the table's name and tier; before this they were not in the status JSON,
// so City showed bare ids. owner is the control: no name in the table, none out.
func TestDomainStatusCarriesTheTablesNameAndTier(t *testing.T) {
	s := statusScanner(t, time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC))
	got := byID(s.ComputeStatus(context.Background(), StatusInputs{}))
	if d := got["tenant"]; d.Name != "Tenants" || d.Tier != 3 {
		t.Fatalf("tenant = %q tier %d, want Tenants tier 3", d.Name, d.Tier)
	}
	if d := got["owner"]; d.Name != "" || d.Tier != 0 {
		t.Fatalf("owner = %q tier %d, want empty", d.Name, d.Tier)
	}
}
