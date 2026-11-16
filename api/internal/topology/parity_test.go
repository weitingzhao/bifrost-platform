package topology

import (
	"encoding/json"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Frozen from 9e5f7eb5 before extraction. One inferred target per case avoids
// the pre-existing Go map iteration nondeterminism; edge and live order stay exact.
func TestProjectionParity(t *testing.T) {
	cases := []struct {
		name, env, target string
		state             probe.Reachability
	}{
		{"dev-api", "dev", "api-ops", probe.ReachOK},
		{"prod-api", "prod", "api-monitor", probe.ReachFail},
		{"stg-ops", "stg", "ops-capabilities", probe.ReachDegraded},
		{"prod-nginx", "prod", "nginx-spa", probe.ReachUnknown},
		{"prod-pg", "prod", "postgres", probe.ReachOK},
		{"dev-redis", "dev", "redis", probe.ReachFail},
		{"missing", "prod", "", probe.ReachUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			topo := &config.TopologyFile{DeploymentPhase: "compose", Nodes: []config.TopologyNode{
				{ID: "macbook", Host: "127.0.0.1", Group: "mac", InK3sCluster: true, Grid: config.GridPos{Row: 2}},
				{ID: "mac-mini-1", Host: "10.0.0.1", Group: "mac"},
				{ID: "mini-pc-a", Host: "10.0.0.2", Group: "linux"},
				{ID: "mini-pc-c", Host: "10.0.0.3", Group: "linux"},
				{ID: "external", Group: "external"},
				{ID: "empty"},
				{ID: "explicit", Host: "10.0.0.4", MatrixTargets: []string{c.target, "absent", c.target}},
			}, Edges: []config.TopologyEdge{
				{ID: "ib", From: "external", To: "mini-pc-a", Kind: "ib"},
				{ID: "static", From: "external", To: "mini-pc-a", Kind: "http"},
				{ID: "bound", From: "explicit", To: "mini-pc-a", Kind: "ib", MatrixTarget: c.target},
				{ID: "missing", From: "explicit", To: "mini-pc-a", Kind: "data", MatrixTarget: "absent"},
				{ID: "filtered", From: "external", To: "mini-pc-a", Environments: []string{"other"}},
			}}
			env := config.Environment{ID: c.env, Label: c.env, Postgres: config.HostPort{Host: "10.0.0.2"}, Redis: config.HostPort{Host: "127.0.0.1"}}
			matrix := probe.MatrixResponse{}
			if c.target != "" {
				matrix.Targets = []probe.Target{{ID: c.target, Reachability: c.state, Detail: "fixture", Category: "fixture"}}
			}
			live := Live{ClusterKnown: c.name != "missing", ClusterNodes: map[string]string{"10.0.0.2": "node-a", "10.0.0.8": "new-b", "10.0.0.7": "new-a"}, Scraped: map[string]string{"10.0.0.8": "new-b", "10.0.0.9": "scraped"}}
			got := Build(topo, env, matrix, live)
			got.GeneratedAt = time.Time{}
			raw, err := json.MarshalIndent(got, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", c.name+".json")
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != string(want) {
				t.Fatalf("projection differs from pre-extraction baseline: %s", path)
			}
		})
	}
}
