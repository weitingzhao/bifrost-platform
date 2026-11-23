package catalog

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
)

// Classes the map reconciles against the running system besides the trees
// (2026-11-21, main line "看得见、感知得到": 7 of 18 classes were reconciled;
// a registry-only class cannot show what is missing from it). Each check
// compares what a registry says with what the system says each round, and
// keeps both differences; it refuses nothing.

// TopologyPath is the registry of machines (class node).
const TopologyPath = "config/topology.yaml"

// ClassCheck is one class compared with the running system.
type ClassCheck struct {
	Class string `json:"class"`
	// Registered is what the registry says exists; Live what the system
	// says; NotRegistered are live but not in the registry, NotLive in the
	// registry but not live.
	Registered    int      `json:"registered"`
	Live          int      `json:"live"`
	NotRegistered []string `json:"not_registered"`
	NotLive       []string `json:"not_live"`
	// Problems are rows the registry holds that break its own rules.
	Problems []string `json:"problems,omitempty"`
	Measured bool     `json:"measured"`
	Note     string   `json:"note,omitempty"`
}

const keptTopology = "topology"

// topologyClaims are the machines topology.yaml says are cluster nodes, by
// IP (the key topologyview joins the node list on).
type topologyClaims struct {
	Commit string            `json:"commit"`
	Nodes  map[string]string `json:"nodes"` // IP -> the file's id
}

// keepTopology keeps the machines the table repository's topology.yaml claims
// as cluster nodes.
func (s *Scanner) keepTopology(t Tree) {
	raw, ok, err := t.Read(TopologyPath)
	if err != nil || !ok {
		return
	}
	var doc struct {
		Nodes []struct {
			ID           string `yaml:"id"`
			Host         string `yaml:"host"`
			InK3sCluster bool   `yaml:"in_k3s_cluster"`
		} `yaml:"nodes"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		slog.Warn("catalog: topology", "err", err)
		return
	}
	c := topologyClaims{Commit: t.Commit, Nodes: map[string]string{}}
	for _, n := range doc.Nodes {
		if n.InK3sCluster && net.ParseIP(n.Host) != nil {
			c.Nodes[n.Host] = n.ID
		}
	}
	if out, err := json.Marshal(c); err == nil {
		if err := s.kept().Write(keptTopology, out); err != nil {
			slog.Warn("catalog: keep topology", "err", err)
		}
	}
}

// nodeCheck compares the kept claims with the cluster's nodes (IP -> name).
func (s *Scanner) nodeCheck(ctx context.Context, nodes func(context.Context) (map[string]string, bool)) ClassCheck {
	c := ClassCheck{Class: "node", NotRegistered: []string{}, NotLive: []string{}}
	raw, err := s.kept().Read(keptTopology)
	if err != nil || raw == nil {
		c.Note = "no scan has kept " + TopologyPath + " yet"
		return c
	}
	var claims topologyClaims
	if json.Unmarshal(raw, &claims) != nil {
		c.Note = "the kept topology does not read"
		return c
	}
	if nodes == nil {
		c.Note = "this platform does not read a cluster"
		return c
	}
	live, ok := nodes(ctx)
	if !ok {
		c.Note = "the cluster's node list could not be read"
		return c
	}
	c.Measured, c.Registered, c.Live = true, len(claims.Nodes), len(live)
	for ip, name := range live {
		if _, in := claims.Nodes[ip]; !in {
			c.NotRegistered = append(c.NotRegistered, name+" ("+ip+")")
		}
	}
	for ip, id := range claims.Nodes {
		if _, in := live[ip]; !in {
			c.NotLive = append(c.NotLive, id+" ("+ip+")")
		}
	}
	sort.Strings(c.NotRegistered)
	sort.Strings(c.NotLive)
	return c
}

const keptSeats = "seats"

// keptSeatRoster is the seat roster and the charter pages beside it, as the
// scan of the repository the index names for class seat read them.
type keptSeatRoster struct {
	Repo     string    `json:"repo"`
	Commit   string    `json:"commit"`
	Path     string    `json:"path"`
	Charters []string  `json:"charters"`
	Rows     []seatRow `json:"rows"`
}

// seatSource is where the index says the seat roster is ("<repo>:<path>").
func (s *Scanner) seatSource() (repo, file string, ok bool) { return s.classSource("seat") }

// classSource is a class's first source in the kept index, "<repo>:<path>".
func (s *Scanner) classSource(class string) (repo, file string, ok bool) {
	raw, err := s.kept().Read(keptIndex)
	if err != nil || raw == nil {
		return "", "", false
	}
	var ix Index
	if json.Unmarshal(raw, &ix) != nil {
		return "", "", false
	}
	for _, c := range ix.Classes {
		if c.Class != class || len(c.Sources) == 0 {
			continue
		}
		repo, file, ok = strings.Cut(c.Sources[0], ":")
		return repo, file, ok && repo != "" && file != ""
	}
	return "", "", false
}

// keepSeats keeps the roster and its charter pages when repo is the one the
// index names for class seat. The platform names no repository: the index
// does.
func (s *Scanner) keepSeats(repo string, t Tree) {
	srcRepo, file, ok := s.seatSource()
	if !ok || srcRepo != repo {
		return
	}
	raw, found, err := t.Read(file)
	if err != nil || !found {
		return
	}
	var doc struct {
		SeatRows  []seatRow `yaml:"seats"`
		Temporary []seatRow `yaml:"temporary"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		slog.Warn("catalog: seats", "err", err)
		return
	}
	k := keptSeatRoster{Repo: repo, Commit: t.Commit, Path: file, Charters: []string{}}
	dir := path.Dir(file)
	for _, f := range t.Files {
		if path.Dir(f) == dir && strings.HasSuffix(f, ".md") && !strings.EqualFold(path.Base(f), "README.md") {
			k.Charters = append(k.Charters, path.Base(f))
		}
	}
	sort.Strings(k.Charters)
	k.Rows = append(append([]seatRow{}, doc.SeatRows...), doc.Temporary...)
	if out, err := json.Marshal(k); err == nil {
		if err := s.kept().Write(keptSeats, out); err != nil {
			slog.Warn("catalog: keep seats", "err", err)
		}
	}
}

// seatRow is one row of the roster as far as the check reads it.
type seatRow struct {
	ID        string  `json:"id" yaml:"id"`
	Charter   string  `json:"charter" yaml:"charter"`
	DomainID  *string `json:"domain_id" yaml:"domain_id"`
	Kind      string  `json:"kind" yaml:"kind"`
	InvitedBy string  `json:"invited_by,omitempty" yaml:"invited_by"`
}

// seatCheck compares the roster with its charter pages and the domains
// (Crew's measure, 2026-11-21): every page has exactly one row naming it,
// every row's page is there; problems are a domain id the table does not
// have and a temporary seat with no invited_by.
func (s *Scanner) seatCheck(domains []contractDomain) ClassCheck {
	c := ClassCheck{Class: "seat", NotRegistered: []string{}, NotLive: []string{}, Problems: []string{}}
	raw, err := s.kept().Read(keptSeats)
	if err != nil || raw == nil {
		c.Note = "no scan has kept the seat roster yet"
		return c
	}
	var k keptSeatRoster
	if json.Unmarshal(raw, &k) != nil {
		c.Note = "the kept seat roster does not read"
		return c
	}
	known := map[string]bool{}
	for _, d := range domains {
		known[d.ID] = true
	}
	pages := map[string]bool{}
	for _, p := range k.Charters {
		pages[p] = true
	}
	rows := map[string]int{}
	for _, r := range k.Rows {
		if r.Charter != "" {
			rows[r.Charter]++
		}
		if r.Charter != "" && !pages[r.Charter] {
			c.NotLive = append(c.NotLive, r.ID+" ("+r.Charter+")")
		}
		if r.DomainID != nil && !known[*r.DomainID] {
			c.Problems = append(c.Problems, r.ID+": domain_id "+*r.DomainID+" is not a domain of the table")
		}
		if r.Kind == "temporary" && r.InvitedBy == "" {
			c.Problems = append(c.Problems, r.ID+": temporary with no invited_by")
		}
	}
	for _, p := range k.Charters {
		switch rows[p] {
		case 0:
			c.NotRegistered = append(c.NotRegistered, p)
		case 1:
		default:
			c.Problems = append(c.Problems, p+": named by "+strconv.Itoa(rows[p])+" rows")
		}
	}
	sort.Strings(c.NotRegistered)
	sort.Strings(c.NotLive)
	sort.Strings(c.Problems)
	c.Measured, c.Registered, c.Live = true, len(k.Rows), len(k.Charters)
	return c
}

const keptMCPTools = "mcp-tools"

// mcpToolCall finds a tool's registration in the MCP servers' TypeScript:
// reg('name', …), registerTool('name', …) or server.tool('name', …).
var mcpToolCall = regexp.MustCompile(`(?:\breg|registerTool|server\.tool)\(\s*'([a-z][a-z0-9_]*)'`)

type keptTools struct {
	Commit string            `json:"commit"`
	Tools  map[string]string `json:"tools"` // name -> server (mcp/<server>)
}

// keepMCPTools keeps the MCP tools the table repository's servers register
// (mcp/<server>/src, tests left out).
func (s *Scanner) keepMCPTools(t Tree) {
	k := keptTools{Commit: t.Commit, Tools: map[string]string{}}
	for _, f := range t.Files {
		parts := strings.Split(f, "/")
		if len(parts) < 4 || parts[0] != "mcp" || parts[2] != "src" || !strings.HasSuffix(f, ".ts") || strings.Contains(f, ".test.") {
			continue
		}
		raw, ok, err := t.Read(f)
		if err != nil || !ok {
			continue
		}
		for _, m := range mcpToolCall.FindAllSubmatch(raw, -1) {
			k.Tools[string(m[1])] = parts[1]
		}
	}
	if len(k.Tools) == 0 {
		return
	}
	if out, err := json.Marshal(k); err == nil {
		if err := s.kept().Write(keptMCPTools, out); err != nil {
			slog.Warn("catalog: keep mcp tools", "err", err)
		}
	}
}

// mcpToolCheck compares the registered tools with the domains' contracts
// (commands of kind mcp_tool).
func (s *Scanner) mcpToolCheck(domains []contractDomain) ClassCheck {
	c := ClassCheck{Class: "mcp-tool", NotRegistered: []string{}, NotLive: []string{}}
	raw, err := s.kept().Read(keptMCPTools)
	if err != nil || raw == nil {
		c.Note = "no scan has kept the MCP servers' tools yet"
		return c
	}
	var k keptTools
	if json.Unmarshal(raw, &k) != nil {
		c.Note = "the kept MCP tools do not read"
		return c
	}
	contract := map[string]bool{}
	for _, d := range domains {
		for _, cmd := range d.Contracts.Commands {
			if cmd.Kind == "mcp_tool" && cmd.Name != "" {
				contract[cmd.Name] = true
			}
		}
	}
	for name, server := range k.Tools {
		if !contract[name] {
			c.NotRegistered = append(c.NotRegistered, name+" ("+server+")")
		}
	}
	for name := range contract {
		if _, ok := k.Tools[name]; !ok {
			c.NotLive = append(c.NotLive, name)
		}
	}
	sort.Strings(c.NotRegistered)
	sort.Strings(c.NotLive)
	c.Measured, c.Registered, c.Live = true, len(contract), len(k.Tools)
	return c
}

const keptRules = "prometheus-rules"

// keptRuleGroups are the rule groups the PrometheusRule manifests under the
// index's directory for class prometheus-rule declare.
type keptRuleGroups struct {
	Repo   string              `json:"repo"`
	Commit string              `json:"commit"`
	Dir    string              `json:"dir"`
	Groups map[string]ruleFile `json:"groups"`
}

type ruleFile struct {
	File  string   `json:"file"`
	Rules []string `json:"rules"`
}

// keepRules keeps the manifests' rule groups when repo is the one the index
// names for class prometheus-rule.
func (s *Scanner) keepRules(repo string, t Tree) {
	srcRepo, dir, ok := s.classSource("prometheus-rule")
	if !ok || srcRepo != repo {
		return
	}
	dir = strings.TrimSuffix(dir, "/")
	k := keptRuleGroups{Repo: repo, Commit: t.Commit, Dir: dir, Groups: map[string]ruleFile{}}
	// Only what the directory's kustomization lists is declared to run
	// (2026-11-22, the metrics line: a file kept out of it on purpose, as
	// td-d2-postgres-rules.yaml waits on TD-134, is not a missing group).
	listed := map[string]bool{}
	if raw, ok, err := t.Read(dir + "/kustomization.yaml"); err == nil && ok {
		var kz struct {
			Resources []string `yaml:"resources"`
		}
		if yaml.Unmarshal(raw, &kz) == nil {
			for _, r := range kz.Resources {
				listed[path.Clean(dir+"/"+r)] = true
			}
		}
	}
	for _, f := range t.Files {
		if !strings.HasPrefix(f, dir+"/") || !(strings.HasSuffix(f, ".yaml") || strings.HasSuffix(f, ".yml")) {
			continue
		}
		if len(listed) > 0 && !listed[f] {
			continue
		}
		raw, found, err := t.Read(f)
		if err != nil || !found || !strings.Contains(string(raw), "PrometheusRule") {
			continue
		}
		dec := yaml.NewDecoder(strings.NewReader(string(raw)))
		for {
			var doc struct {
				Kind string `yaml:"kind"`
				Spec struct {
					Groups []struct {
						Name  string `yaml:"name"`
						Rules []struct {
							Alert  string `yaml:"alert"`
							Record string `yaml:"record"`
						} `yaml:"rules"`
					} `yaml:"groups"`
				} `yaml:"spec"`
			}
			if err := dec.Decode(&doc); err != nil {
				break
			}
			if doc.Kind != "PrometheusRule" {
				continue
			}
			for _, g := range doc.Spec.Groups {
				rf := ruleFile{File: f, Rules: []string{}}
				for _, r := range g.Rules {
					if r.Alert != "" {
						rf.Rules = append(rf.Rules, r.Alert)
					} else if r.Record != "" {
						rf.Rules = append(rf.Rules, r.Record)
					}
				}
				k.Groups[g.Name] = rf
			}
		}
	}
	if out, err := json.Marshal(k); err == nil {
		if err := s.kept().Write(keptRules, out); err != nil {
			slog.Warn("catalog: keep rules", "err", err)
		}
	}
}

// RuleGroup is one rule group Prometheus has loaded.
type RuleGroup struct {
	Name  string
	Rules []string
}

// ruleCheck compares the manifests' rule groups with the groups Prometheus
// has loaded: groups loaded but declared by no manifest here (a chart's
// defaults, a rule applied by hand), groups declared but not loaded.
func (s *Scanner) ruleCheck(ctx context.Context, loaded func(context.Context) ([]RuleGroup, bool)) ClassCheck {
	c := ClassCheck{Class: "prometheus-rule", NotRegistered: []string{}, NotLive: []string{}}
	raw, err := s.kept().Read(keptRules)
	if err != nil || raw == nil {
		c.Note = "no scan has kept the rule manifests yet"
		return c
	}
	var k keptRuleGroups
	if json.Unmarshal(raw, &k) != nil {
		c.Note = "the kept rule manifests do not read"
		return c
	}
	if loaded == nil {
		c.Note = "this platform does not read Prometheus"
		return c
	}
	groups, ok := loaded(ctx)
	if !ok {
		c.Note = "Prometheus's rules could not be read"
		return c
	}
	live := map[string]bool{}
	for _, g := range groups {
		live[g.Name] = true
		if _, declared := k.Groups[g.Name]; !declared {
			c.NotRegistered = append(c.NotRegistered, g.Name+" ("+strconv.Itoa(len(g.Rules))+" rules)")
		}
	}
	for name, rf := range k.Groups {
		if !live[name] {
			c.NotLive = append(c.NotLive, name+" ("+rf.File+")")
		}
	}
	sort.Strings(c.NotRegistered)
	sort.Strings(c.NotLive)
	c.Measured, c.Registered, c.Live = true, len(k.Groups), len(live)
	return c
}

// A domain's self-description (law 77, 2026-11-22; format the map line's
// default with Station): a YAML file beside the code it describes, named
// by the domain row's "self" ("<repo>:<path>"), checked against that code
// by the domain's own test. Top level: about (one line), bring (what to
// bring to use it), refusals (what it says when it refuses, and what to
// do), must_answer (what it must answer when asked); other sections are
// kept as written. "What I am, what I open, how I am" come from the map.

// keptSelf is "self-<domain>".
func keptSelf(domain string) string { return "self-" + domain }

// SelfDescription is one domain's self-description as kept.
type SelfDescription struct {
	Source string         `json:"source"`
	Commit string         `json:"commit"`
	Doc    map[string]any `json:"doc"`
	Note   string         `json:"note,omitempty"`
}

// keepSelf keeps the self-descriptions whose source is in repo.
func (s *Scanner) keepSelf(repo string, t Tree) {
	k, ok := s.keptSections()
	if !ok {
		return
	}
	var rows []struct {
		ID   string `json:"id"`
		Self string `json:"self"`
	}
	if json.Unmarshal(k.Domains, &rows) != nil {
		return
	}
	for _, r := range rows {
		srcRepo, file, ok := strings.Cut(r.Self, ":")
		if !ok || srcRepo != repo || file == "" {
			continue
		}
		d := SelfDescription{Source: r.Self, Commit: t.Commit}
		raw, found, err := t.Read(file)
		switch {
		case err != nil || !found:
			d.Note = "the file the row names is not in " + repo + " at " + t.Commit
		default:
			if err := yaml.Unmarshal(raw, &d.Doc); err != nil {
				d.Note = "the file does not parse: " + err.Error()
				d.Doc = nil
			}
		}
		if out, err := json.Marshal(d); err == nil {
			if err := s.kept().Write(keptSelf(r.ID), out); err != nil {
				slog.Warn("catalog: keep self-description", "domain", r.ID, "err", err)
			}
		}
	}
}

// selfOf is the kept self-description of a domain (nil for none).
func (s *Scanner) selfOf(domain string) *SelfDescription {
	raw, err := s.kept().Read(keptSelf(domain))
	if err != nil || raw == nil {
		return nil
	}
	var d SelfDescription
	if json.Unmarshal(raw, &d) != nil {
		return nil
	}
	return &d
}

// selfTasks turns a self-description's bring, refusals and must_answer
// into tasks someone can find by words.
func selfTasks(d *SelfDescription) []Task {
	if d == nil || d.Doc == nil {
		return nil
	}
	var out []Task
	str := func(v any) string { s, _ := v.(string); return s }
	if items, ok := d.Doc["bring"].([]any); ok {
		for _, it := range items {
			if m, ok := it.(map[string]any); ok {
				out = append(out, Task{Task: str(m["what"]), Use: nonEmpty(str(m["command"]), str(m["get_from"])), Note: "bring " + str(m["id"])})
			}
		}
	}
	if items, ok := d.Doc["refusals"].([]any); ok {
		for _, it := range items {
			if m, ok := it.(map[string]any); ok {
				out = append(out, Task{Task: str(m["match"]), Use: nonEmpty(str(m["do"])), Note: "refusal, " + str(m["kind"])})
			}
		}
	}
	if items, ok := d.Doc["must_answer"].([]any); ok {
		for _, it := range items {
			if q := str(it); q != "" {
				out = append(out, Task{Task: q, Use: []string{}, Note: "must answer"})
			}
		}
	}
	return out
}

func nonEmpty(v ...string) []string {
	out := []string{}
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// eventCheck compares the event types the register declares (the bus's
// topics and the ledger's own types, as this binary carries them) with the
// types the ledger holds events of in the last seven days: types written
// that the register lacks, and declared types with no event in the window
// (silent, which may be rare rather than dead: shown, not judged).
func eventCheck(ctx context.Context, seen func(context.Context) (map[string]int, bool)) ClassCheck {
	c := ClassCheck{Class: "event", NotRegistered: []string{}, NotLive: []string{}}
	declared := map[string]bool{}
	tenants, err := registry.Load()
	if err != nil {
		c.Note = "the bus register does not read: " + err.Error()
		return c
	}
	for _, tn := range tenants {
		for typ := range tn.Channels {
			declared[typ] = true
		}
	}
	if lts, err := registry.LedgerTypes(); err == nil {
		for _, lt := range lts {
			declared[lt.Type] = true
		}
	}
	if seen == nil {
		c.Note = "this platform does not read the ledger"
		return c
	}
	counts, ok := seen(ctx)
	if !ok {
		c.Note = "the ledger's event types could not be read"
		return c
	}
	for typ, n := range counts {
		if !declared[typ] {
			c.NotRegistered = append(c.NotRegistered, typ+" ("+strconv.Itoa(n)+" in 7 days)")
		}
	}
	for typ := range declared {
		if counts[typ] == 0 {
			c.NotLive = append(c.NotLive, typ)
		}
	}
	sort.Strings(c.NotRegistered)
	sort.Strings(c.NotLive)
	c.Measured, c.Registered, c.Live = true, len(declared), len(counts)
	c.Note = "live: types with an event in the ledger in the last 7 days; registered, not live means silent in that window"
	return c
}

const keptPlaces = "places"

// keptPlaceRegistries are the namespaces the table repository's cluster and
// environment registries name.
type keptPlaceRegistries struct {
	Commit       string            `json:"commit"`
	Clusters     []string          `json:"clusters"`
	ClusterNS    map[string]string `json:"cluster_namespaces"`     // namespace -> cluster id
	Environments map[string]string `json:"environment_namespaces"` // environment id -> namespace ("" for none)
}

// keepPlaces keeps the namespaces config/clusters.yaml and
// config/environments.yaml name.
func (s *Scanner) keepPlaces(t Tree) {
	k := keptPlaceRegistries{Commit: t.Commit, ClusterNS: map[string]string{}, Environments: map[string]string{}}
	if raw, ok, err := t.Read("config/clusters.yaml"); err == nil && ok {
		var doc struct {
			Clusters []struct {
				ID         string   `yaml:"id"`
				Namespaces []string `yaml:"bifrost_namespaces"`
				// Others are namespaces the cluster has that are not
				// Bifrost's (the system's, an operator's, a tenant's):
				// registered so the map knows them, kept apart so the
				// Bifrost list stays what it filters (Foundation 11-22).
				Others []string `yaml:"other_namespaces"`
			} `yaml:"clusters"`
		}
		if yaml.Unmarshal(raw, &doc) == nil {
			for _, c := range doc.Clusters {
				k.Clusters = append(k.Clusters, c.ID)
				for _, ns := range append(append([]string{}, c.Namespaces...), c.Others...) {
					k.ClusterNS[ns] = c.ID
				}
			}
		}
	}
	if raw, ok, err := t.Read("config/environments.yaml"); err == nil && ok {
		var doc struct {
			Environments []struct {
				ID        string `yaml:"id"`
				Namespace string `yaml:"namespace"`
			} `yaml:"environments"`
		}
		if yaml.Unmarshal(raw, &doc) == nil {
			for _, e := range doc.Environments {
				k.Environments[e.ID] = e.Namespace
			}
		}
	}
	if len(k.Clusters) == 0 && len(k.Environments) == 0 {
		return
	}
	if out, err := json.Marshal(k); err == nil {
		if err := s.kept().Write(keptPlaces, out); err != nil {
			slog.Warn("catalog: keep places", "err", err)
		}
	}
}

// placeChecks compares the cluster registry's namespaces and the
// environments' namespaces with the namespaces the cluster has.
func (s *Scanner) placeChecks(ctx context.Context, namespaces func(context.Context) ([]string, bool)) []ClassCheck {
	cl := ClassCheck{Class: "cluster", NotRegistered: []string{}, NotLive: []string{}}
	env := ClassCheck{Class: "environment", NotRegistered: []string{}, NotLive: []string{}}
	raw, err := s.kept().Read(keptPlaces)
	var k keptPlaceRegistries
	if err != nil || raw == nil || json.Unmarshal(raw, &k) != nil {
		cl.Note, env.Note = "no scan has kept the cluster and environment registries yet", "no scan has kept the cluster and environment registries yet"
		return []ClassCheck{cl, env}
	}
	if namespaces == nil {
		cl.Note, env.Note = "this platform does not read a cluster", "this platform does not read a cluster"
		return []ClassCheck{cl, env}
	}
	live, ok := namespaces(ctx)
	if !ok {
		cl.Note, env.Note = "the cluster's namespaces could not be read", "the cluster's namespaces could not be read"
		return []ClassCheck{cl, env}
	}
	has := map[string]bool{}
	for _, ns := range live {
		has[ns] = true
		if _, in := k.ClusterNS[ns]; !in {
			cl.NotRegistered = append(cl.NotRegistered, ns)
		}
	}
	for ns, id := range k.ClusterNS {
		if !has[ns] {
			cl.NotLive = append(cl.NotLive, ns+" ("+id+")")
		}
	}
	cl.Measured, cl.Registered, cl.Live = true, len(k.ClusterNS), len(live)
	cl.Note = "namespaces: registered are the clusters' bifrost_namespaces and other_namespaces"
	envWithNS := 0
	for id, ns := range k.Environments {
		if ns == "" {
			continue
		}
		envWithNS++
		if !has[ns] {
			env.NotLive = append(env.NotLive, id+" ("+ns+")")
		}
	}
	env.Measured, env.Registered, env.Live = true, envWithNS, envWithNS-len(env.NotLive)
	env.Note = "environments that name a namespace, against the cluster's namespaces; an environment with no namespace (a thin client) is not compared"
	for _, c := range []*ClassCheck{&cl, &env} {
		sort.Strings(c.NotRegistered)
		sort.Strings(c.NotLive)
	}
	return []ClassCheck{cl, env}
}

const keptReleaseTargets = "running-images"

// keepReleaseTargets keeps the deployments config/running-images.yaml lists
// per lane and environment ("<namespace>/<name>" -> "<lane> <env>").
func (s *Scanner) keepReleaseTargets(t Tree) {
	raw, ok, err := t.Read("config/running-images.yaml")
	if err != nil || !ok {
		return
	}
	var doc struct {
		Lanes []struct {
			Lane string `yaml:"lane"`
			Envs []struct {
				Env         string `yaml:"env"`
				Deployments []struct {
					Namespace string `yaml:"namespace"`
					Name      string `yaml:"name"`
				} `yaml:"deployments"`
			} `yaml:"envs"`
		} `yaml:"lanes"`
	}
	if yaml.Unmarshal(raw, &doc) != nil {
		return
	}
	k := map[string]string{}
	for _, l := range doc.Lanes {
		for _, e := range l.Envs {
			for _, d := range e.Deployments {
				k[d.Namespace+"/"+d.Name] = l.Lane + " " + e.Env
			}
		}
	}
	if out, err := json.Marshal(k); err == nil {
		if err := s.kept().Write(keptReleaseTargets, out); err != nil {
			slog.Warn("catalog: keep running images", "err", err)
		}
	}
}

// releaseTargetCheck compares the listed deployments with the deployments
// the cluster scan saw (its pool's covered workloads).
func (s *Scanner) releaseTargetCheck() ClassCheck {
	c := ClassCheck{Class: "running-image", NotRegistered: []string{}, NotLive: []string{}}
	raw, err := s.kept().Read(keptReleaseTargets)
	var listed map[string]string
	if err != nil || raw == nil || json.Unmarshal(raw, &listed) != nil {
		c.Note = "no scan has kept config/running-images.yaml yet"
		return c
	}
	p, _ := s.Last(ClusterRepo)
	if p == nil || !p.Measured {
		c.Note = "the cluster has not been scanned"
		return c
	}
	seen := map[string]bool{}
	for _, w := range p.Covered {
		if w.Kind == "deployment" {
			seen[w.Path] = true
		}
	}
	for _, e := range p.Entries { // off-map workloads are workloads too
		seen[e.Path] = true
	}
	for d, where := range listed {
		if !seen[d] {
			c.NotLive = append(c.NotLive, d+" ("+where+")")
		}
	}
	sort.Strings(c.NotLive)
	c.Measured, c.Registered, c.Live = true, len(listed), len(listed)-len(c.NotLive)
	c.Note = "the deployments each release lane lists, against the deployments the cluster scan saw; the file lists no images, so which image runs is not compared"
	return c
}
