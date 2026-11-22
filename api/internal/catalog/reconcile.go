package catalog

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"sort"

	"gopkg.in/yaml.v3"
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
	Measured      bool     `json:"measured"`
	Note          string   `json:"note,omitempty"`
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
