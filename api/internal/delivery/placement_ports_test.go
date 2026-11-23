package delivery

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// Delivery's tests run with a stand-in for compute's placement, bound before
// any test (TestPlacementUnbound unbinds it for a moment). Compute's own
// choices (slow disk, busy or protected nodes, memory) are tested in
// placement; the assembled ones (delivery's task profiles chosen by compute)
// in server/delivery_placement_test.go. Here delivery's side is tested: what
// it does with a choice, a failed read, a role's label.
// Origin: 2026-11-22, delivery's tests stopped importing placement (the
// tests-reach list's row run_refusal_test.go -> placement).
//
// The stand-in: no metrics reader, nothing placed; a failed read, placed
// with no node; otherwise the least CPU-busy node that has metrics and holds
// none of the task's excluded roles (ties by name), or no node.
const testHostIDLabel = "bifrost.io/host-id"

var testRoleLabels = map[NodeRole][2]string{
	NodeEtcd:            {"node-role.kubernetes.io/etcd", "true"},
	NodeDatabasePrimary: {"bifrost.io/postgres-role", "primary"},
}

func init() { bindTestPlacement() }

func bindTestPlacement() {
	BindPlacementReadings(PlacementReadings{
		HostIDLabel: testHostIDLabel,
		RoleLabel: func(role NodeRole) (string, string, bool) {
			r, ok := testRoleLabels[role]
			return r[0], r[1], ok
		},
		Place:             testPlace,
		CIPreflightReason: testPreflightReason,
	})
}

// testAsked is the task the stand-in was last asked to place.
var testAsked TaskProfile

func testPlace(ctx context.Context, clientset func() (kubernetes.Interface, error), metrics NodeMetricsReader, task TaskProfile, _ string) (BuildPlacement, bool) {
	testAsked = task
	if metrics == nil {
		return BuildPlacement{}, false
	}
	m, err := metrics(ctx)
	if err != nil {
		return BuildPlacement{Why: "node metrics could not be read: " + err.Error()}, true
	}
	cs, err := clientset()
	if err != nil {
		return BuildPlacement{Why: "nodes could not be read: " + err.Error()}, true
	}
	list, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return BuildPlacement{Why: "nodes could not be read: " + err.Error()}, true
	}
	type candidate struct {
		name string
		busy float64
	}
	var fit []candidate
	for _, n := range list.Items {
		if excludedByRole(n, task.ExcludeNodes) {
			continue
		}
		for _, a := range n.Status.Addresses {
			if got, ok := m[a.Address]; ok && a.Type == corev1.NodeInternalIP {
				fit = append(fit, candidate{n.Name, got.CPUBusy})
			}
		}
	}
	if len(fit) == 0 {
		return BuildPlacement{Why: "test placer: nothing fits"}, true
	}
	sort.Slice(fit, func(i, j int) bool {
		if fit[i].busy != fit[j].busy {
			return fit[i].busy < fit[j].busy
		}
		return fit[i].name < fit[j].name
	})
	return BuildPlacement{Node: fit[0].name, Why: fmt.Sprintf("test placer chose %s (CPU %.0f%% busy)", fit[0].name, fit[0].busy*100)}, true
}

func excludedByRole(n corev1.Node, roles []NodeRole) bool {
	for _, role := range roles {
		if r, ok := testRoleLabels[role]; ok && n.Labels[r[0]] == r[1] {
			return true
		}
	}
	return false
}

func testPreflightReason(nodes []NodeInput) string {
	for _, n := range nodes {
		if n.Architecture == "amd64" && n.Reachability == probe.ReachOK {
			return ""
		}
	}
	return "test placer: no Ready amd64 node for CI builds"
}
