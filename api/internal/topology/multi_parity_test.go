package topology

import (
	"encoding/json"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// Only test canonicalization ignores the pre-existing map-order ambiguity
// in services/detail fragments. Node/edge order and all other fields stay exact.
func TestMultipleTargetsProjectionParity(t *testing.T) {
	for _, environment := range []string{"dev", "prod"} {
		t.Run(environment, func(t *testing.T) {
			topo := &config.TopologyFile{DeploymentPhase: "compose", Nodes: []config.TopologyNode{
				{ID: "macbook", Host: "127.0.0.1"}, {ID: "mac-mini-1", Host: "10.0.0.1"}, {ID: "mini-pc-a", Host: "10.0.0.2", MatrixTargets: []string{"api-ops", "absent", "postgres", "api-ops"}},
			}, Edges: []config.TopologyEdge{
				{ID: "ordered-first", From: "mini-pc-a", To: "macbook", MatrixTarget: "api-ops"},
				{ID: "ordered-second", From: "mini-pc-a", To: "macbook", MatrixTarget: "postgres"},
				{ID: "ib-bound", From: "mini-pc-a", To: "mac-mini-1", Kind: "ib", MatrixTarget: "redis"},
				{ID: "ib-plain", From: "mini-pc-a", To: "mac-mini-1", Kind: "ib"},
			}}
			env := config.Environment{ID: environment, Postgres: config.HostPort{Host: "10.0.0.2"}, Redis: config.HostPort{Host: "127.0.0.1"}}
			matrix := probe.MatrixResponse{Targets: []probe.Target{
				{ID: "api-ops", Reachability: probe.ReachOK, Detail: "api ok"}, {ID: "api-account", Reachability: probe.ReachFail, Detail: "api fail"},
				{ID: "nginx-spa", Reachability: probe.ReachDegraded, Detail: "nginx degraded"}, {ID: "ops-capabilities", Reachability: probe.ReachUnknown, Detail: "capability unknown"},
				{ID: "postgres", Reachability: probe.ReachOK, Detail: "pg ok"}, {ID: "redis", Reachability: probe.ReachFail, Detail: "redis fail"},
			}}
			got := Build(topo, env, matrix, Live{})
			got.GeneratedAt = time.Time{}
			for i := range got.Nodes {
				n := &got.Nodes[i]
				sort.Slice(n.MatrixServices, func(i, j int) bool { return n.MatrixServices[i].ID < n.MatrixServices[j].ID })
				parts := strings.Split(n.Detail, "; ")
				sort.Strings(parts)
				n.Detail = strings.Join(parts, "; ")
			}
			raw, err := json.MarshalIndent(got, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", "multi-"+environment+".json")
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != string(want) {
				t.Fatalf("multi-target projection differs from baseline: %s", path)
			}
		})
	}
}
