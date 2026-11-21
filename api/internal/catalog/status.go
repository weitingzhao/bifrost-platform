package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"
)

// The status column (segment 4, 2026-11-20; plan section 6, after
// Kubernetes: the definition is written by people, the status by the
// machine). Every domain has four lights, each good, problem, bad or
// unknown; what cannot be measured is unknown, never good. A domain's
// overall light is its worst. The workers compute it after each scan round
// and keep it; GET /catalog/status reads what was kept.

// Light values.
const (
	Good    = "good"
	Problem = "problem"
	Bad     = "bad"
	Unknown = "unknown"
)

// MinRequests is the floor under which the interfaces light says too few
// samples rather than judge.
const MinRequests = 20

// Health is one workload's replicas as the cluster scan read them. Judged is
// false for a kind with no replicas to count (a CronJob).
type Health struct {
	Desired int  `json:"desired"`
	Ready   int  `json:"ready"`
	Judged  bool `json:"judged"`
}

// RouteCount is the requests one route answered in the window and how many
// of them were 5xx.
type RouteCount struct {
	Total  float64 `json:"total"`
	Errors float64 `json:"errors"`
}

// Edge is one domain importing another's packages; WrongWay as the domain
// graph says.
type Edge struct {
	From     string
	To       string
	Imports  int
	WrongWay bool
	// ToBase is an edge into the domain every other stands on (the table's
	// default): that way is the right way, and no contract is asked of it.
	ToBase bool
	// Pairs are the imports, importing package then imported package.
	Pairs [][2]string
}

// Reference is the imports of one domain's packages from another's, and how
// many of them the imported domain's contract registers (2026-11-21, the
// chief of staff's acceptance of "perceive first", item 4: City shows the
// cross-domain references that happen without being registered, who to
// whom). It counts by package: a pair (importing package, imported package)
// is registered when the imported domain's go_surface names an item of that
// package with the importing package among its users. Counting by name
// takes the type checker, which only the CI tool runs.
type Reference struct {
	From         string `json:"from"`
	To           string `json:"to"`
	Imports      int    `json:"imports"`
	Registered   int    `json:"registered"`
	Unregistered int    `json:"unregistered"`
	WrongWay     bool   `json:"wrong_way"`
}

// StatusInputs are what the status reads besides the scan's own keeping. A
// nil function leaves its light unknown.
type StatusInputs struct {
	// Health is the workloads' replicas, keyed "<namespace>/<name>".
	Health func() map[string]Health
	// Requests answers routes (chi patterns) with their counts over window.
	Requests func(ctx context.Context, routes []string, window time.Duration) (map[string]RouteCount, error)
	// Windows are read shortest first; a domain with fewer than MinRequests
	// in one is judged on the next (2026-11-20: on PROD, lineage had 1
	// request in an hour and about 570 in a day).
	Windows []time.Duration
	// Hosts are the workloads ("<namespace>/<name>") that run this
	// platform's own binaries. A domain with no workload of its own whose Go
	// packages are in the table's repository runs inside them, and its alive
	// light reads them.
	Hosts []string
	// Boundary is the domain graph of the repository whose table names the
	// domains: its edges, and false when it was not measured.
	Boundary func(ctx context.Context, repo string) ([]Edge, bool, error)
}

// Light is one light of one domain.
type Light struct {
	Value string `json:"value"`
	// Why says, in numbers, what the value was read from.
	Why string `json:"why"`
}

// DomainStatus is one domain's lights.
type DomainStatus struct {
	ID         string `json:"id"`
	Overall    string `json:"overall"`
	Alive      Light  `json:"alive"`
	Interfaces Light  `json:"interfaces"`
	Boundary   Light  `json:"boundary"`
	Fresh      Light  `json:"fresh"`
}

// Status is the answer of GET /catalog/status.
type Status struct {
	Version int            `json:"version"`
	At      time.Time      `json:"at"`
	Commit  string         `json:"commit"`
	Domains []DomainStatus `json:"domains"`
	// Inventory is the equation of every repository the last round scanned
	// and of the cluster: total = on map + off map, gap 0 (2026-11-20,
	// perceive first). A gap that is not 0 turns every fresh light bad.
	Inventory []RepoCount `json:"inventory"`
	// OffMapByDomain counts the pool's entries by the domain a review claims
	// them for; "unclaimed" have none (no review, or delete or unknown).
	OffMapByDomain map[string]int `json:"off_map_by_domain"`
	// References are the imports between the domains above the base, who to
	// whom, registered or not (Reference); nil when the domain graph was
	// not measured. ToBase counts the imports of the base, which ask no
	// contract.
	References []Reference `json:"references"`
	ToBase     int         `json:"references_to_base"`
	Note       string      `json:"note,omitempty"`
}

// RepoCount is one repository's (or the cluster's) equation.
type RepoCount struct {
	Repo     string `json:"repo"`
	Commit   string `json:"commit"`
	Measured bool   `json:"measured"`
	Whole    bool   `json:"whole,omitempty"`
	Total    int    `json:"total"`
	OnMap    int    `json:"on_map"`
	OffMap   int    `json:"off_map"`
	Gap      int    `json:"gap"`
	Entries  int    `json:"entries"`
}

// keptRepos is the repositories the last round scanned.
const keptRepos = "repos"

// inventory reads the kept pools of the last round's repositories (the
// table's repository and the cluster when no round was kept).
func (s *Scanner) inventory(tableRepo string) ([]RepoCount, map[string]int) {
	var repos []string
	if raw, err := s.kept().Read(keptRepos); err == nil && raw != nil {
		_ = json.Unmarshal(raw, &repos)
	}
	if len(repos) == 0 {
		repos = []string{tableRepo, ClusterRepo}
	}
	out, by := []RepoCount{}, map[string]int{}
	for _, r := range repos {
		p, _ := s.Last(r)
		if p == nil {
			out = append(out, RepoCount{Repo: r})
			continue
		}
		out = append(out, RepoCount{Repo: r, Commit: p.Commit, Measured: p.Measured, Whole: p.Whole,
			Total: p.Total, OnMap: p.OnMap, OffMap: p.OffMap, Gap: p.Gap, Entries: len(p.Entries)})
		for _, e := range p.Entries {
			d := "unclaimed"
			if e.Review != nil && e.Review.Verdict == VerdictClaim && e.Review.Domain != "" {
				d = e.Review.Domain
			}
			by[d]++
		}
	}
	return out, by
}

const keptStatus = "status"

type contractDomain struct {
	ID        string `json:"id"`
	Contracts struct {
		RequestResponse []struct {
			Method string `json:"method"`
			Path   string `json:"path"`
		} `json:"request_response"`
		GoSurface []struct {
			Name  string   `json:"name"`
			Users []string `json:"users"`
		} `json:"go_surface"`
	} `json:"contracts"`
}

// ComputeStatus reads the kept table, pools and owners, and in.
func (s *Scanner) ComputeStatus(ctx context.Context, in StatusInputs) Status {
	now := s.now()
	st := Status{Version: Version, At: now, Domains: []DomainStatus{}}
	k, ok := s.keptSections()
	var domains []contractDomain
	if ok {
		_ = json.Unmarshal(k.Domains, &domains)
	}
	if len(domains) == 0 {
		st.Note = "no scanned table names its domains yet"
		return st
	}
	st.Commit = k.Commit
	fresh := s.freshLight(k.Repo, now)
	st.Inventory, st.OffMapByDomain = s.inventory(k.Repo)
	for _, c := range st.Inventory {
		if c.Measured && c.Gap != 0 {
			fresh.Value = Bad
			fresh.Why = joinWhy([]string{fresh.Why, fmt.Sprintf("%s: the equation is off by %d (total %d, on map %d, off map %d): the scan misses or double-counts", c.Repo, c.Gap, c.Total, c.OnMap, c.OffMap)})
		}
	}
	if in.Boundary != nil {
		// one read of the graph for the boundary lights and the references
		edges, measured, err := in.Boundary(ctx, k.Repo)
		in.Boundary = func(context.Context, string) ([]Edge, bool, error) { return edges, measured, err }
		if measured {
			st.References, st.ToBase = references(edges, domains)
		}
	}
	alive := s.aliveLights(k.Repo, domains, in)
	ifaces := interfaceLights(ctx, domains, in)
	bound := boundaryLights(ctx, k.Repo, domains, in)
	for _, d := range domains {
		ds := DomainStatus{ID: d.ID, Alive: alive[d.ID], Interfaces: ifaces[d.ID], Boundary: bound[d.ID], Fresh: fresh}
		ds.Overall = worst(ds.Alive.Value, ds.Interfaces.Value, ds.Boundary.Value, ds.Fresh.Value)
		st.Domains = append(st.Domains, ds)
	}
	return st
}

// worst orders bad, problem, unknown, good.
func worst(vs ...string) string {
	rank := map[string]int{Good: 0, Unknown: 1, Problem: 2, Bad: 3}
	out := Good
	for _, v := range vs {
		if rank[v] > rank[out] {
			out = v
		}
	}
	return out
}

// freshLight: the table's repository and the cluster were scanned, measured,
// within three rounds.
func (s *Scanner) freshLight(repo string, now time.Time) Light {
	var parts []string
	value := Good
	for _, r := range []string{repo, ClusterRepo} {
		p, _ := s.Last(r)
		switch {
		case p == nil:
			return Light{Value: Unknown, Why: r + " not scanned yet"}
		case !p.Measured:
			value = worst(value, Bad)
			parts = append(parts, fmt.Sprintf("%s: last scan not measured (%s)", r, p.Note))
		default:
			age := now.Sub(p.ScannedAt)
			if age > 3*Every {
				value = worst(value, Problem)
			}
			parts = append(parts, fmt.Sprintf("%s: scanned %d min ago at %.8s", r, int(age.Minutes()), p.Commit))
		}
	}
	return Light{Value: value, Why: joinWhy(parts)}
}

func (s *Scanner) aliveLights(repo string, domains []contractDomain, in StatusInputs) map[string]Light {
	out := map[string]Light{}
	if in.Health == nil {
		for _, d := range domains {
			out[d.ID] = Light{Value: Unknown, Why: "the cluster's workloads are not read on this platform"}
		}
		return out
	}
	owners, _ := s.lastOwners(ClusterRepo)
	type tally struct{ judged, down, degraded, cron int }
	t := map[string]*tally{}
	health := in.Health()
	for key, h := range health {
		o := OwnerOf(owners, nil, key)
		if !o.Perceived || len(o.Domains) != 1 {
			continue
		}
		c := t[o.Domains[0]]
		if c == nil {
			c = &tally{}
			t[o.Domains[0]] = c
		}
		if !h.Judged {
			c.cron++
			continue
		}
		if h.Desired == 0 {
			continue // scaled to zero on purpose (the daemon under D10)
		}
		c.judged++
		switch {
		case h.Ready == 0:
			c.down++
		case h.Ready < h.Desired:
			c.degraded++
		}
	}
	coded := s.domainsWithPackages(repo)
	for _, d := range domains {
		c := t[d.ID]
		switch {
		case (c == nil || c.judged == 0) && coded[d.ID] && len(in.Hosts) > 0:
			out[d.ID] = hostedLight(in.Hosts, health)
		case c == nil || c.judged == 0:
			why := "no running workload of it on the map"
			if c != nil && c.cron > 0 {
				why = fmt.Sprintf("%d CronJobs only (not judged)", c.cron)
			}
			out[d.ID] = Light{Value: Unknown, Why: why}
		case c.down == 0 && c.degraded == 0:
			out[d.ID] = Light{Value: Good, Why: fmt.Sprintf("%d of %d workloads fully ready", c.judged, c.judged)}
		case c.down == c.judged:
			out[d.ID] = Light{Value: Bad, Why: fmt.Sprintf("none of its %d workloads has a ready replica", c.judged)}
		default:
			out[d.ID] = Light{Value: Problem, Why: fmt.Sprintf("%d workloads: %d with no ready replica, %d short of replicas", c.judged, c.down, c.degraded)}
		}
	}
	return out
}

// domainsWithPackages: the domains a package row of repo's table names.
func (s *Scanner) domainsWithPackages(repo string) map[string]bool {
	out := map[string]bool{}
	o, _ := s.lastOwners(repo)
	if o == nil {
		return out
	}
	for _, ds := range o.Packages {
		for _, d := range ds {
			out[d] = true
		}
	}
	return out
}

// hostedLight judges the workloads that run the platform's binaries.
func hostedLight(hosts []string, health map[string]Health) Light {
	var parts []string
	judged, down, degraded := 0, 0, 0
	for _, k := range hosts {
		h, ok := health[k]
		if !ok || !h.Judged || h.Desired == 0 {
			parts = append(parts, k+" not read")
			continue
		}
		judged++
		switch {
		case h.Ready == 0:
			down++
		case h.Ready < h.Desired:
			degraded++
		}
		parts = append(parts, fmt.Sprintf("%s %d/%d ready", k, h.Ready, h.Desired))
	}
	why := "no workload of its own; runs inside " + joinWhy(parts)
	switch {
	case judged == 0:
		return Light{Value: Unknown, Why: why}
	case down == judged:
		return Light{Value: Bad, Why: why}
	case down > 0 || degraded > 0 || judged < len(hosts):
		return Light{Value: Problem, Why: why}
	}
	return Light{Value: Good, Why: why}
}

func interfaceLights(ctx context.Context, domains []contractDomain, in StatusInputs) map[string]Light {
	out := map[string]Light{}
	var all []string
	for _, d := range domains {
		for _, r := range d.Contracts.RequestResponse {
			all = append(all, r.Path)
		}
	}
	windows := in.Windows
	if len(windows) == 0 {
		windows = []time.Duration{time.Hour}
	}
	counts := make([]map[string]RouteCount, len(windows))
	var err error
	if in.Requests != nil && len(all) > 0 {
		for i, w := range windows {
			if counts[i], err = in.Requests(ctx, all, w); err != nil {
				break
			}
		}
	}
	for _, d := range domains {
		if len(d.Contracts.RequestResponse) == 0 {
			out[d.ID] = Light{Value: Unknown, Why: "its contract names no interface"}
			continue
		}
		if in.Requests == nil || err != nil {
			why := "the request readings are not reachable from this platform"
			if err != nil {
				why = "the request readings could not be read: " + err.Error()
			}
			out[d.ID] = Light{Value: Unknown, Why: why}
			continue
		}
		var total, errs float64
		var why string
		for i, w := range windows {
			total, errs = 0, 0
			seen := map[string]bool{}
			for _, r := range d.Contracts.RequestResponse {
				if seen[r.Path] {
					continue // one count per route, whatever the methods
				}
				seen[r.Path] = true
				total += counts[i][r.Path].Total
				errs += counts[i][r.Path].Errors
			}
			why = fmt.Sprintf("%.0f requests in %.0fh to its %d routes, %.0f 5xx", total, w.Hours(), len(seen), errs)
			if total >= MinRequests {
				break
			}
		}
		switch {
		case total < MinRequests:
			out[d.ID] = Light{Value: Unknown, Why: "too few samples: " + why}
		case errs/total >= 0.5:
			out[d.ID] = Light{Value: Bad, Why: why}
		case errs/total >= 0.05:
			out[d.ID] = Light{Value: Problem, Why: why}
		default:
			out[d.ID] = Light{Value: Good, Why: why}
		}
	}
	return out
}

func boundaryLights(ctx context.Context, repo string, domains []contractDomain, in StatusInputs) map[string]Light {
	out := map[string]Light{}
	var edges []Edge
	measured := false
	var err error
	if in.Boundary != nil {
		edges, measured, err = in.Boundary(ctx, repo)
	}
	for _, d := range domains {
		if !measured {
			why := "the domain graph is not measured yet"
			if in.Boundary == nil {
				why = "the domain graph is not read on this platform"
			} else if err != nil {
				why = "the domain graph could not be read: " + err.Error()
			}
			out[d.ID] = Light{Value: Unknown, Why: why}
			continue
		}
		var parts []string
		n := 0
		for _, e := range edges {
			if e.WrongWay && (e.From == d.ID || e.To == d.ID) {
				n += e.Imports
				parts = append(parts, fmt.Sprintf("%s -> %s (%d)", e.From, e.To, e.Imports))
			}
		}
		sort.Strings(parts)
		if n == 0 {
			out[d.ID] = Light{Value: Good, Why: "no import against the tiers in or out of it"}
			continue
		}
		out[d.ID] = Light{Value: Problem, Why: fmt.Sprintf("%d imports against the tiers: %s", n, joinWhy(parts))}
	}
	return out
}

// references judges every edge above the base against the contracts.
func references(edges []Edge, domains []contractDomain) ([]Reference, int) {
	registered := map[[2]string]bool{} // (user package, offered package)
	for _, d := range domains {
		for _, item := range d.Contracts.GoSurface {
			pkg := packageOf(item.Name)
			for _, u := range item.Users {
				registered[[2]string{u, pkg}] = true
			}
		}
	}
	out, toBase := []Reference{}, 0
	for _, e := range edges {
		if e.ToBase {
			toBase += e.Imports
			continue
		}
		r := Reference{From: e.From, To: e.To, Imports: e.Imports, WrongWay: e.WrongWay}
		for _, p := range e.Pairs {
			if registered[p] {
				r.Registered++
			}
		}
		r.Unregistered = r.Imports - r.Registered
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		return out[i].To < out[j].To
	})
	return out, toBase
}

// packageOf is the package of a go_surface name: "api/internal/x.Name" and
// "api/internal/x.Type.Method" are both of "api/internal/x".
func packageOf(name string) string {
	slash := strings.LastIndex(name, "/")
	if dot := strings.Index(name[slash+1:], "."); dot >= 0 {
		return name[:slash+1+dot]
	}
	return name
}

func joinWhy(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "; "
		}
		out += p
	}
	return out
}

// KeepStatus computes and keeps the status.
func (s *Scanner) KeepStatus(ctx context.Context, in StatusInputs) {
	st := s.ComputeStatus(ctx, in)
	if len(st.Domains) == 0 {
		return // nothing scanned yet: the route says so without a kept file
	}
	raw, err := json.Marshal(st)
	if err != nil {
		return
	}
	if err := s.kept().Write(keptStatus, raw); err != nil {
		slog.Warn("catalog: keep status", "err", err)
	}
}

// StatusRoute is GET /catalog/status: the status last kept.
type StatusRoute struct{ Scanner *Scanner }

func (rt StatusRoute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, err := rt.Scanner.kept().Read(keptStatus)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if raw == nil {
		writeJSON(w, http.StatusOK, Status{Version: Version, Domains: []DomainStatus{}, Note: "not computed yet"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}
