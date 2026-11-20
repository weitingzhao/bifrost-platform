package catalog

import (
	"context"
	"errors"
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
		Window: time.Hour,
		Health: func() map[string]Health {
			return map[string]Health{
				"ns-plan/web":     {Desired: 2, Ready: 2, Judged: true},
				"ns-tenant/api":   {Desired: 2, Ready: 1, Judged: true},
				"ns-tenant/cron":  {},
				"elsewhere/thing": {Desired: 1, Ready: 0, Judged: true},
			}
		},
		Requests: func(_ context.Context, routes []string) (map[string]RouteCount, error) {
			return map[string]RouteCount{"/api/v1/p": {Total: 100, Errors: 1}, "/api/v1/t": {Total: 5}}, nil
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
		t.Errorf("interfaces: plan %+v tenant %+v (5 requests: too few) owner %+v (no contract)", p.Interfaces, tn.Interfaces, o.Interfaces)
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
