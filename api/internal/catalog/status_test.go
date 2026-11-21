package catalog

import (
	"context"
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
  {"id":"tenant","contracts":{"request_response":[{"method":"GET","path":"/api/v1/t"}]}},
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
