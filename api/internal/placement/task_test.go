package placement

import (
	"context"
	"math"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

// These tests moved from delivery (run_refusal_test.go) with the choice on
// 2026-11-19. The task needs below are the ones delivery declares (CITask,
// CIRunTask and UnitTask in delivery/ci_node.go and build_placement.go), kept
// here so the choice is tested on the needs it is given.
var (
	ciTask    = TaskProfile{DiskHeavy: true, MaxDiskTier: 2, CPUCores: 4, MemoryBytes: 4 << 30, Spread: true, ExcludeLabels: []LabelRule{EtcdNode}}
	ciRunTask = TaskProfile{DiskHeavy: true, MaxDiskTier: 2, CPUCores: 4, MemoryBytes: 4 << 30, Spread: true, ExcludeLabels: []LabelRule{EtcdNode, PostgresPrimary}, MinAvailableMemory: 7 << 30}
	unitTask  = TaskProfile{DiskHeavy: true, MaxDiskTier: 1, CPUCores: 4, MemoryBytes: 4 << 30, Spread: true, ExcludeLabels: []LabelRule{EtcdNode}}
)

func fakePlacer(objects []runtime.Object, metrics NodeMetricsReader) Placer {
	cs := k8sfake.NewSimpleClientset(objects...)
	return Placer{Clientset: func() (kubernetes.Interface, error) { return cs, nil }, Metrics: metrics}
}

func placementNode(host string, ready, cordoned bool, cpu string) *corev1.Node {
	status := corev1.ConditionTrue
	if !ready {
		status = corev1.ConditionFalse
	}
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: host, Labels: map[string]string{HostIDLabel: host, "kubernetes.io/arch": "amd64"}},
		Spec:       corev1.NodeSpec{Unschedulable: cordoned},
		Status: corev1.NodeStatus{
			Addresses:   []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "ip-" + host}},
			Allocatable: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(cpu), corev1.ResourceMemory: resource.MustParse("16Gi")},
			Conditions:  []corev1.NodeCondition{{Type: corev1.NodeReady, Status: status}},
		},
	}
}

// 2026-11-18, the Owner: builds go to the fast node, and to the slower one when
// it is full. #598's soft preference sent 11 of 13 deliveries to 05, where the
// same build takes twice as long. The latencies are the 7-day readings of that
// day. Whatever the nodes say, a release must not wait for one of them.
// Positive control: without the tier in the sort, "today" picks ubt-k3s-05 (the
// first by name); the "requests" case fails if finished pods are not skipped.
func TestBuildPlacementFollowsTheProfiles(t *testing.T) {
	const fast, slow, added, batch = "ubt-k3s-07", "ubt-k3s-05", "ubt-k3s-08", "ubt-k3s-01"
	quiet := func(latency float64) NodeMetrics {
		return NodeMetrics{WriteLatencySeconds: latency, CPUBusy: 0.2, DiskBusy: 0.1, MemoryAvailable: 0.6}
	}
	today := func() map[string]NodeMetrics {
		return map[string]NodeMetrics{"ip-" + fast: quiet(0.0068), "ip-" + slow: quiet(0.161), "ip-" + added: quiet(0.001), "ip-" + batch: quiet(0.0009)}
	}
	with := func(change func(m map[string]NodeMetrics)) map[string]NodeMetrics {
		m := today()
		change(m)
		return m
	}
	set := func(host string, change func(*NodeMetrics)) map[string]NodeMetrics {
		return with(func(m map[string]NodeMetrics) { n := m["ip-"+host]; change(&n); m["ip-"+host] = n })
	}
	denied := placementNode(batch, true, false, "16")
	denied.Labels[batchLabel] = "deny"
	two := []runtime.Object{placementNode(fast, true, false, "16"), placementNode(slow, true, false, "16"), denied}
	for _, c := range []struct {
		name    string
		objects []runtime.Object
		metrics map[string]NodeMetrics
		want    string
		why     string
	}{
		{"today: 07 first, the batch-deny node never", append(two[:3:3], placementPod("done", fast, "15", corev1.PodSucceeded)), today(), fast, "disk tier 1"},
		// Positive control: ordering by tier then name picks ubt-k3s-05 here.
		{"two slow disks: the less slow one", two, set(fast, func(n *NodeMetrics) { n.WriteLatencySeconds = 0.03 }), fast, "disk tier 2 (30.0 ms"},
		{"a new node that measures well is chosen", append(two[:3:3], placementNode(added, true, false, "16")), today(), added, "disk tier 0"},
		{"CPU busy", two, set(fast, func(n *NodeMetrics) { n.CPUBusy = 0.9 }), slow, "CPU 90% busy"},
		{"disk busy", two, set(fast, func(n *NodeMetrics) { n.DiskBusy = 0.9 }), slow, "disk 90% busy"},
		{"memory short", two, set(fast, func(n *NodeMetrics) { n.MemoryAvailable = 0.1 }), slow, "memory 10% available"},
		// The fake client ignores the pod list's node filter, so 05 is made big enough to hold 07's pod too.
		{"requests leave no room", []runtime.Object{placementNode(fast, true, false, "16"), placementNode(slow, true, false, "32"), placementPod("busy", fast, "13", corev1.PodRunning)}, today(), slow, "CPU unrequested"},
		{"cordoned", []runtime.Object{placementNode(fast, true, true, "16"), placementNode(slow, true, false, "16")}, today(), slow, "cordoned"},
		{"not ready", []runtime.Object{placementNode(fast, false, false, "16"), placementNode(slow, true, false, "16")}, today(), slow, "not ready"},
		{"not measured", two, with(func(m map[string]NodeMetrics) { delete(m, "ip-"+fast) }), slow, "not measured"},
		{"nothing fits: left to the scheduler", two, map[string]NodeMetrics{}, "", "no node fits"},
	} {
		t.Run(c.name, func(t *testing.T) {
			pl := fakePlacer(c.objects, func(context.Context) (map[string]NodeMetrics, error) { return c.metrics, nil })
			task := TaskProfile{DiskHeavy: true, MaxDiskTier: 2, CPUCores: 4, MemoryBytes: 4 << 30}
			got, placed := pl.Place(context.Background(), task, "")
			if !placed || got.Node != c.want || !strings.Contains(got.Why, c.why) {
				t.Fatalf("got %+v placed=%v, want node %q and a reason with %q", got, placed, c.want, c.why)
			}
		})
	}
}

// Same origin: a node missing one reading is "not measured", never judged on
// zeros, which would read as an idle node with a perfect disk.
// Positive control: dropping the presence checks makes 10.0.0.2 appear.
func TestJoinNodeMetricsKeepsOnlyCompleteNodes(t *testing.T) {
	got := JoinNodeMetrics(
		map[string]float64{"10.0.0.1": 0.007, "10.0.0.2": 0.001, "10.0.0.3": math.NaN()},
		map[string]float64{"10.0.0.1": 0.5, "10.0.0.3": 0.1},
		map[string]float64{"10.0.0.1": 0.2, "10.0.0.2": 0.1, "10.0.0.3": 0.1},
		map[string]float64{"10.0.0.1": 0.6, "10.0.0.2": 0.9, "10.0.0.3": 0.9},
	)
	if len(got) != 1 || got["10.0.0.1"].WriteLatencySeconds != 0.007 || got["10.0.0.1"].MemoryAvailable != 0.6 {
		t.Fatalf("got %+v", got)
	}
}

// Same origin: memory volumes take from the available memory, so a CI run needs
// 7 GiB of it on any node, which is what bounds ubt-k3s-07's 3.3 GiB of room when
// several runs share it. Positive control: at 7 GiB the quieter node is chosen.
func TestACIRunNeedsAvailableMemoryOnEveryNode(t *testing.T) {
	m := func(cpu float64) NodeMetrics {
		return NodeMetrics{WriteLatencySeconds: 0.001, CPUBusy: cpu, DiskBusy: 0.1, MemoryAvailable: 0.4}
	}
	nodes := []NodeProfile{
		{Host: "quiet", Name: "quiet", FreeCPU: 8, FreeMemory: 8 << 30, AvailableMemory: 7<<30 - 1, Metrics: m(0.1)},
		{Host: "other", Name: "other", FreeCPU: 8, FreeMemory: 8 << 30, AvailableMemory: 12 << 30, Metrics: m(0.5)},
	}
	if p := spreadNode(ciRunTask, nodes, "", BuildPlacement{}); p.Node != "other" {
		t.Fatalf("low memory: %+v", p)
	}
	nodes[0].AvailableMemory = 7 << 30
	if p := spreadNode(ciRunTask, nodes, "", BuildPlacement{}); p.Node != "quiet" {
		t.Fatalf("at the floor: %+v", p)
	}
}

// 2026-11-18, the compute line's reading after CI moved: one preferred node sent
// 52% of CI pods to the slow disk while 02 and 04 idled. CI takes the least loaded
// node off the slow disk and stays put within the margin. (Never the etcd node:
// delivery's TestCINeverGoesToTheEtcdNodeWhateverItIsCalled, since 2026-11-19.)
// Positive control: without the margin the third check moves to the quieter node.
func TestCISpreadsOffTheSlowDiskAndKeepsOffTheExcludedNode(t *testing.T) {
	node := func(host string, latency, cpu, disk float64) NodeProfile {
		return NodeProfile{Host: host, Name: host, FreeCPU: 8, FreeMemory: 8 << 30, Metrics: NodeMetrics{WriteLatencySeconds: latency, CPUBusy: cpu, DiskBusy: disk, MemoryAvailable: 0.6}}
	}
	nodes := []NodeProfile{node("ubt-k3s-07", 0.007, 0.6, 0.5), node("ubt-k3s-05", 0.16, 0.1, 0.1), node("ubt-k3s-04", 0.001, 0.2, 0.1), node("ubt-k3s-02", 0.001, 0.3, 0.1)}
	fallback := BuildPlacement{Node: "ubt-k3s-05"}
	if p := spreadNode(ciTask, nodes, "", fallback); p.Node != "ubt-k3s-04" {
		t.Fatalf("least loaded off the slow disk: %+v", p)
	}
	if p := spreadNode(ciTask, nodes, "ubt-k3s-07", fallback); p.Node != "ubt-k3s-04" {
		t.Fatalf("far above the best it moves: %+v", p)
	}
	if p := spreadNode(ciTask, nodes, "ubt-k3s-02", fallback); p.Node != "ubt-k3s-02" {
		t.Fatalf("within the margin it stays: %+v", p)
	}
	if p := spreadNode(ciTask, nodes[1:2], "", fallback); p.Node != "ubt-k3s-05" {
		t.Fatalf("only the slow disk left: %+v", p)
	}
}

// 2026-11-18: ubt-k3s-07's disk answered in 799 ms while its 7-day average said
// 15, and builds pinned there took twice as long. The worse figure counts; a quiet
// slow disk keeps its long one. Positive control: returning recent alone fails the
// second check, long alone the first.
func TestWorseLatencyTakesTheHigherAverage(t *testing.T) {
	got := WorseLatency(map[string]float64{"a": 0.015, "b": 0.16, "c": 0.002}, map[string]float64{"a": 0.8, "b": 0.006, "c": math.NaN()})
	if got["a"] != 0.8 {
		t.Fatalf("just gone slow: %v", got)
	}
	if got["b"] != 0.16 || got["c"] != 0.002 {
		t.Fatalf("quiet: %v", got)
	}
}

// Same origin, the compute line's rollback line: a database node takes this work
// only while its disk is under 60% busy. Positive control: without the check the
// busy node, the least loaded by CPU, is chosen.
func TestABusyProtectedNodeTakesNoSpreadWork(t *testing.T) {
	m := func(cpu, disk float64) NodeMetrics {
		return NodeMetrics{WriteLatencySeconds: 0.001, CPUBusy: cpu, DiskBusy: disk, MemoryAvailable: 0.6}
	}
	nodes := []NodeProfile{
		{Host: "db", Name: "db", Protected: true, FreeCPU: 8, FreeMemory: 8 << 30, AvailableMemory: 8 << 30, Metrics: m(0.61, 0.61)},
		{Host: "other", Name: "other", Protected: true, FreeCPU: 8, FreeMemory: 8 << 30, AvailableMemory: 8 << 30, Metrics: m(0.7, 0.2)},
	}
	if p := spreadNode(ciTask, nodes, "", BuildPlacement{}); p.Node != "other" {
		t.Fatalf("placement = %+v", p)
	}
}

// 2026-11-19, the compute line: 02 and 04 wrote about 70 GB an hour, and ubt-k3s-07's
// disk slowed after 36 GB an hour for six hours. A database node over 75 GB in two
// hours takes no more of this work, not even as the fallback when nothing else fits.
// Positive control: at the limit it is chosen again.
func TestADatabaseNodeThatWroteTooMuchTakesNoWork(t *testing.T) {
	node := NodeProfile{Host: "db", Name: "db", Protected: true, FreeCPU: 8, FreeMemory: 8 << 30, AvailableMemory: 12 << 30,
		Metrics: NodeMetrics{WriteLatencySeconds: 0.001, CPUBusy: 0.1, DiskBusy: 0.1, MemoryAvailable: 0.6, WrittenBytes2h: 75e9 + 1}}
	if p := spreadNode(unitTask, []NodeProfile{node}, "", chooseNode(unitTask, []NodeProfile{node})); p.Node != "" {
		t.Fatalf("over the limit: %+v", p)
	}
	node.Metrics.WrittenBytes2h = 75e9
	if p := spreadNode(unitTask, []NodeProfile{node}, "", BuildPlacement{}); p.Node != "db" {
		t.Fatalf("at the limit: %+v", p)
	}
}

// 2026-11-19, the compute line: ubt-k3s-02 (15 GiB) fell to 7.5 GiB available as CI
// moved there, towards its 5 GiB rollback line. Under 6 GiB a database node takes no
// spread work. Positive control: at 6 GiB the quieter node is chosen again.
func TestAProtectedNodeLowOnMemoryTakesNoSpreadWork(t *testing.T) {
	m := func(cpu float64) NodeMetrics {
		return NodeMetrics{WriteLatencySeconds: 0.001, CPUBusy: cpu, DiskBusy: 0.1, MemoryAvailable: 0.4}
	}
	nodes := []NodeProfile{
		{Host: "small", Name: "small", Protected: true, FreeCPU: 8, FreeMemory: 8 << 30, AvailableMemory: 6<<30 - 1, Metrics: m(0.1)},
		{Host: "other", Name: "other", Protected: true, FreeCPU: 8, FreeMemory: 8 << 30, AvailableMemory: 12 << 30, Metrics: m(0.5)},
	}
	if p := spreadNode(ciTask, nodes, "", BuildPlacement{}); p.Node != "other" {
		t.Fatalf("low memory: %+v", p)
	}
	nodes[0].AvailableMemory = 6 << 30
	if p := spreadNode(ciTask, nodes, "", BuildPlacement{}); p.Node != "small" {
		t.Fatalf("at the floor: %+v", p)
	}
}

func placementPod(name, node, cpu string, phase corev1.PodPhase) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "cicd"},
		Spec: corev1.PodSpec{NodeName: node, Containers: []corev1.Container{{Name: "c", Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(cpu)}}}}},
		Status: corev1.PodStatus{Phase: phase},
	}
}

// 2026-11-18, the coordinator: the daily unit run does not land on 05, where two
// tasks timed out. Unlike a release it can wait, so a task allowed only disk tier 1
// refuses the slow disk. Moved here from delivery/run_refusal_test.go on
// 2026-11-22 (delivery tests what it asks, compute how it answers). Positive
// control: ciTask (tier 2) picks 05 here.
func TestATierOneTaskRefusesTheSlowDisk(t *testing.T) {
	metrics := func(context.Context) (map[string]NodeMetrics, error) {
		return map[string]NodeMetrics{"ip-ubt-k3s-05": {WriteLatencySeconds: 0.161, MemoryAvailable: 0.6}}, nil
	}
	nodes := []runtime.Object{placementNode("ubt-k3s-05", true, false, "16")}
	if p, placed := fakePlacer(nodes, metrics).Place(context.Background(), unitTask, ""); !placed || p.Node != "" || !strings.Contains(p.Why, "too slow") {
		t.Fatalf("tier 1: %+v %v", p, placed)
	}
	if p, _ := fakePlacer(nodes, metrics).Place(context.Background(), ciTask, ""); p.Node != "ubt-k3s-05" {
		t.Fatalf("tier 2: %+v", p)
	}
}
