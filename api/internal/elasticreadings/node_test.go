package elasticreadings_test

import (
	"errors"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/elasticreadings"
	"github.com/weitingzhao/bifrost-platform/api/internal/resourceview"
)

// Origin: 2026-11-16, the node-mode decision left cluster for this package.
// Two readers act on it (the patrol uncordons, the delivery preflight
// refuses a build), and one of its observations is an SSH probe of the host.
// This holds the rule and that nothing is asked beyond what the answer
// needs.
func TestNodeModeAsksNoFurtherThanItNeeds(t *testing.T) {
	for name, c := range map[string]struct {
		ready       bool
		pending     int
		pendingErr  error
		unsatisfied bool
		host        bool
		wantMode    string
		wantDetail  string
		wantAsked   string
	}{
		"a ready node is active, and nothing is asked":            {ready: true, wantMode: "active", wantDetail: "node Ready", wantAsked: ""},
		"pods that cannot be counted":                             {pendingErr: errors.New("api down"), wantMode: "degraded", wantDetail: "api down", wantAsked: "pending"},
		"pods pending on the node":                                {pending: 2, wantMode: "degraded", wantDetail: "2 pending pod(s) on node", wantAsked: "pending"},
		"a workload wants replicas it does not have":              {unsatisfied: true, wantMode: "degraded", wantDetail: "ollama wants 1 replica(s), 0 ready", wantAsked: "pending workloads"},
		"the host answers while the node is not ready":            {host: true, wantMode: "degraded", wantDetail: "host online but node not Ready", wantAsked: "pending workloads host"},
		"nothing waits and the host is off: standby, the control": {wantMode: "standby", wantDetail: "on-demand standby (no pending compute workloads)", wantAsked: "pending workloads host"},
	} {
		asked := ""
		note := func(what string) {
			if asked != "" {
				asked += " "
			}
			asked += what
		}
		mode, detail := elasticreadings.NodeMode(resourceview.ElasticNodeObservation{
			Ready:       c.ready,
			PendingPods: func() (int, error) { note("pending"); return c.pending, c.pendingErr },
			WorkloadsUnsatisfied: func() (bool, string) {
				note("workloads")
				if c.unsatisfied {
					return true, "ollama wants 1 replica(s), 0 ready"
				}
				return false, ""
			},
			HostReachable: func() bool { note("host"); return c.host },
		})
		if mode != c.wantMode || detail != c.wantDetail || asked != c.wantAsked {
			t.Errorf("%s: %q, %q, asked %q; want %q, %q, asked %q", name, mode, detail, asked, c.wantMode, c.wantDetail, c.wantAsked)
		}
	}
}
