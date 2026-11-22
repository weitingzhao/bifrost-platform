package placement

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/kubemetrics"
)

// Where a task runs (the Owner, 2026-11-18: builds go to the fast node, and to
// the slower one when it is full). The same api build took about 120 s on
// ubt-k3s-07 and 266 s on ubt-k3s-05, whose disk writes take over 20 times as
// long. A soft preference cannot do this: a run's pods follow its affinity
// assistant, and Tekton's anti-affinity between assistants outweighs it (#598
// sent 11 of 13 deliveries to 05). So the caller reads what each node measures
// now and names one. No host is named here: a node that joins and measures well
// is chosen as it is. A caller never waits for this: with nothing measured or
// nothing fit, the scheduler decides as before.
//
// Moved from delivery (build_placement.go) on 2026-11-19 at the CI line's ask:
// the CI line only reports a task's needs (TaskProfile); compute chooses.

const (
	// HostIDLabel names a node's host in selectors.
	HostIDLabel = "bifrost.io/host-id"
	batchLabel  = "bifrost.io/batch"

	placeTimeout      = 5 * time.Second
	placementWhyLimit = 300

	// Average write latency bounds of the disk tiers, and what counts as a full
	// node: the compute line's profile contract v1 (2026-11-18).
	diskTierFastSeconds   = 0.005
	diskTierUsableSeconds = 0.020
	busyLimit             = 0.85
	memoryAvailableFloor  = 0.15
	spreadMargin          = 0.15
	// Nodes kept for the cluster's own work take spread work only while their disk
	// is this free (the compute line's rollback line, 2026-11-18: they hold the
	// databases).
	protectedDiskBusy = 0.60
	// And only while this much memory is available (the compute line, 2026-11-19:
	// ubt-k3s-02 fell from 10.7 to 7.5 GiB as builds moved there; its rollback line
	// is 5 GiB, and 15% of its 15 GiB would only stop at 2.3).
	protectedMemoryFloor = 6 << 30
	// And only while they have written less than this in two hours (the compute
	// line, 2026-11-19): ubt-k3s-07's disk slowed after about 36 GB an hour for six
	// hours, and 02 and 04 were at 70 GB an hour. Better a run waits or goes to a
	// slower disk than a database's disk slows.
	protectedWrittenBytes2h = 75e9
	// 2026-11-22, compute: 02's root disk was about 53% used and growing
	// about 10.8 GB/day. Stop lending database nodes at 70% root disk usage.
	protectedRootDiskUsed = 0.70
)

// LabelRule names nodes by one label: a node carrying Key with Value.
type LabelRule struct {
	Key, Value string
}

// Node labels the task rules exclude by (2026-11-19: by label, not by node name,
// so a renamed or replaced node keeps the rule).
var (
	// EtcdNode is a node that runs the cluster's etcd (k3s sets the label).
	EtcdNode = LabelRule{Key: "node-role.kubernetes.io/etcd", Value: "true"}
	// PostgresPrimary is the database primary; a failover moves the label, and so
	// every exclusion made with it, to the new primary.
	PostgresPrimary = LabelRule{Key: "bifrost.io/postgres-role", Value: "primary"}
)

func (r LabelRule) matches(labels map[string]string) bool {
	v, ok := labels[r.Key]
	return ok && v == r.Value
}

// NotInExpression is the rule as a nodeSelector match expression a node passes
// when the rule does not name it (NotIn also passes a node without the label).
func (r LabelRule) NotInExpression() map[string]any {
	return map[string]any{"key": r.Key, "operator": "NotIn", "values": []any{r.Value}}
}

// TaskProfile is what a kind of work needs from a node.
type TaskProfile struct {
	DiskHeavy   bool
	MaxDiskTier int     // the slowest disk tier a disk-heavy task may go to
	CPUCores    float64 // unrequested CPU the node must have
	MemoryBytes float64 // unrequested memory the node must have
	// MinAvailableMemory is the memory the node must really have available, in
	// bytes; 0 asks nothing beyond the general floor.
	MinAvailableMemory float64
	// Spread picks the least loaded node that is not on the slow disk, and may
	// use nodes batch work is otherwise kept off, except those ExcludeLabels names.
	Spread bool
	// ExcludeLabels: a node any of these rules names is never a candidate.
	ExcludeLabels []LabelRule
}

// Excludes says whether one of the task's rules names a node with these labels.
func (t TaskProfile) Excludes(labels map[string]string) bool {
	for _, r := range t.ExcludeLabels {
		if r.matches(labels) {
			return true
		}
	}
	return false
}

// NodeMetrics is one node's measurements. The four core readings are present
// or the node is absent from the reader's answer; supplemental readings may be absent.
type NodeMetrics struct {
	// WriteLatencySeconds is the worse of the 7-day and the last 15 minutes'
	// averages: a quiet slow disk looks fast in a short window, and a disk that
	// has just gone slow looks fine in a long one.
	WriteLatencySeconds float64
	CPUBusy             float64 // 0 to 1, last 5 minutes
	DiskBusy            float64 // 0 to 1, last 5 minutes
	MemoryAvailable     float64 // 0 to 1
	WrittenBytes2h      float64 // written to the node's disks in the last two hours
	RootDiskUsed        float64 // 0 to 1; missing reads leave zero and do not stop work
}

// NodeMetricsReader answers by node InternalIP.
type NodeMetricsReader func(ctx context.Context) (map[string]NodeMetrics, error)

// JoinNodeMetrics keeps the nodes with all four readings, by address; NaN (a
// disk that wrote nothing in the window) is no reading.
func JoinNodeMetrics(latency, cpu, disk, memory map[string]float64) map[string]NodeMetrics {
	out := map[string]NodeMetrics{}
	for address, l := range latency {
		c, okC := cpu[address]
		d, okD := disk[address]
		m, okM := memory[address]
		if l != l || !okC || !okD || !okM {
			continue
		}
		out[address] = NodeMetrics{WriteLatencySeconds: l, CPUBusy: c, DiskBusy: d, MemoryAvailable: m}
	}
	return out
}

// NodeProfile is a candidate as the choice sees it.
type NodeProfile struct {
	Host       string
	Name       string
	Protected  bool   // bifrost.io/batch=deny
	Problem    string // not ready, cordoned or not measured; "" when it can be judged
	Metrics    NodeMetrics
	FreeCPU    float64 // allocatable minus the requests of unfinished pods
	FreeMemory float64
	// AvailableMemory is the bytes the kernel reports available, not requests.
	AvailableMemory float64
}

// BuildPlacement is where a task goes; Node "" leaves it to the scheduler.
type BuildPlacement struct {
	Node string
	Why  string
}

// Placer chooses nodes for tasks from the cluster's nodes and what they measure.
type Placer struct {
	// Clientset reads the nodes and their pods.
	Clientset func() (kubernetes.Interface, error)
	// Metrics reads the node measurements; nil means this platform places nothing.
	Metrics NodeMetricsReader
}

// Place chooses the node for the task now, staying on current when spreading
// finds it close enough; false when this platform reads no node metrics. Node ""
// with a reason: nothing fits or nothing could be read.
func (pl Placer) Place(ctx context.Context, task TaskProfile, current string) (BuildPlacement, bool) {
	if pl.Metrics == nil {
		return BuildPlacement{}, false
	}
	ctx, cancel := context.WithTimeout(kubemetrics.WithSite(ctx, "delivery.build-placement"), placeTimeout)
	defer cancel()
	var p BuildPlacement
	if nodes, err := pl.nodeProfiles(ctx, task); err != nil {
		p.Why = "not placed: " + err.Error()
	} else {
		p = chooseNode(task, nodes)
		if task.Spread {
			p = spreadNode(task, nodes, current, p)
		}
	}
	if len(p.Why) > placementWhyLimit {
		p.Why = p.Why[:placementWhyLimit]
	}
	return p, true
}

func diskTier(latencySeconds float64) int {
	switch {
	case latencySeconds <= diskTierFastSeconds:
		return 0
	case latencySeconds <= diskTierUsableSeconds:
		return 1
	}
	return 2
}

// nodeFull says why the node cannot take the task now, "" when it can.
func nodeFull(task TaskProfile, n NodeProfile) string {
	m := n.Metrics
	switch {
	case n.Protected && m.DiskBusy >= protectedDiskBusy:
		return fmt.Sprintf("database node, disk %.0f%% busy", m.DiskBusy*100)
	case n.Protected && n.AvailableMemory < protectedMemoryFloor:
		return fmt.Sprintf("database node, %.1fGi available", n.AvailableMemory/(1<<30))
	case n.Protected && m.WrittenBytes2h > protectedWrittenBytes2h:
		return fmt.Sprintf("database node, %.0f GB written in 2 h", m.WrittenBytes2h/1e9)
	case n.Protected && m.RootDiskUsed >= protectedRootDiskUsed:
		return fmt.Sprintf("database node, root disk %.0f%% used", m.RootDiskUsed*100)
	case m.CPUBusy >= busyLimit:
		return fmt.Sprintf("CPU %.0f%% busy", m.CPUBusy*100)
	case task.DiskHeavy && m.DiskBusy >= busyLimit:
		return fmt.Sprintf("disk %.0f%% busy", m.DiskBusy*100)
	case m.MemoryAvailable < memoryAvailableFloor:
		return fmt.Sprintf("memory %.0f%% available", m.MemoryAvailable*100)
	case n.AvailableMemory < task.MinAvailableMemory:
		return fmt.Sprintf("%.1fGi available", n.AvailableMemory/(1<<30))
	case n.FreeCPU < task.CPUCores:
		return fmt.Sprintf("%.1f CPU unrequested", n.FreeCPU)
	case n.FreeMemory < task.MemoryBytes:
		return fmt.Sprintf("%.1fGi unrequested", n.FreeMemory/(1<<30))
	}
	return ""
}

// chooseNode ranks the nodes that can take the task, the lowest write latency
// first for disk-heavy work (by the number, not the tier: on 2026-11-18 CI load
// took ubt-k3s-07 from 6.8 to 14.8 ms, and two nodes in one tier would otherwise
// fall to name order, the slow disk first), and says why of the first and of each node passed over.
func chooseNode(task TaskProfile, nodes []NodeProfile) BuildPlacement {
	var fit []NodeProfile
	var passed []string
	for _, n := range nodes {
		problem := n.Problem
		if problem == "" && task.DiskHeavy && diskTier(n.Metrics.WriteLatencySeconds) > task.MaxDiskTier {
			problem = fmt.Sprintf("disk tier %d is too slow", diskTier(n.Metrics.WriteLatencySeconds))
		}
		if problem == "" {
			if full := nodeFull(task, n); full != "" {
				problem = "full: " + full
			}
		}
		if problem != "" {
			passed = append(passed, n.Host+" "+problem)
			continue
		}
		fit = append(fit, n)
	}
	sort.SliceStable(fit, func(i, j int) bool {
		if task.DiskHeavy {
			if a, b := fit[i].Metrics.WriteLatencySeconds, fit[j].Metrics.WriteLatencySeconds; a != b {
				return a < b
			}
		}
		return fit[i].Host < fit[j].Host
	})
	var p BuildPlacement
	var why []string
	for i, n := range fit {
		line := fmt.Sprintf("%s disk tier %d (%.1f ms write)", n.Host, diskTier(n.Metrics.WriteLatencySeconds), n.Metrics.WriteLatencySeconds*1000)
		if i == 0 {
			p.Node = n.Host
		} else {
			line = "then " + line
		}
		why = append(why, line)
	}
	if p.Node == "" {
		why = append(why, "no node fits")
	}
	p.Why = strings.Join(append(why, passed...), "; ")
	return p
}

// spreadNode keeps many runs from piling onto the fastest disk (2026-11-18: with
// one preferred node, 52% of CI pods went to the slow disk once 07 filled while 02
// and 04 sat at 15% CPU): the fitting node off the slow disk with the lowest of
// max(CPU, disk) busy, staying on current unless another is spreadMargin lower.
// With none off the slow disk, fallback (chooseNode's answer) stands.
func spreadNode(task TaskProfile, nodes []NodeProfile, current string, fallback BuildPlacement) BuildPlacement {
	load := func(n NodeProfile) float64 { return max(n.Metrics.CPUBusy, n.Metrics.DiskBusy) }
	var best, kept *NodeProfile
	for i := range nodes {
		n := &nodes[i]
		if n.Problem != "" || nodeFull(task, *n) != "" || diskTier(n.Metrics.WriteLatencySeconds) >= 2 {
			continue
		}
		if best == nil || load(*n) < load(*best) || (load(*n) == load(*best) && n.Host < best.Host) {
			best = n
		}
		if n.Host == current {
			kept = n
		}
	}
	if best == nil {
		return fallback
	}
	if kept != nil && load(*kept)-load(*best) < spreadMargin {
		best = kept
	}
	return BuildPlacement{Node: best.Host, Why: fmt.Sprintf("%s least loaded: CPU %.0f%%, disk %.0f%% busy", best.Host, best.Metrics.CPUBusy*100, best.Metrics.DiskBusy*100)}
}

// nodeProfiles reads the candidates: every node with a host id that batch work
// is not kept off (any, for spread work), less those the task's rules exclude.
func (pl Placer) nodeProfiles(ctx context.Context, task TaskProfile) ([]NodeProfile, error) {
	cs, err := pl.Clientset()
	if err != nil {
		return nil, fmt.Errorf("the cluster could not be read: %w", err)
	}
	metrics, err := pl.Metrics(ctx)
	if err != nil {
		return nil, fmt.Errorf("the node metrics could not be read: %w", err)
	}
	selector := HostIDLabel + "," + batchLabel + "!=deny"
	if task.Spread {
		selector = HostIDLabel
	}
	list, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, fmt.Errorf("the nodes could not be read: %w", err)
	}
	nodes := make([]NodeProfile, 0, len(list.Items))
	for i := range list.Items {
		if !task.Excludes(list.Items[i].Labels) {
			nodes = append(nodes, nodeProfile(ctx, cs, &list.Items[i], metrics))
		}
	}
	return nodes, nil
}

func nodeProfile(ctx context.Context, cs kubernetes.Interface, node *corev1.Node, metrics map[string]NodeMetrics) NodeProfile {
	p := NodeProfile{Host: node.Labels[HostIDLabel], Name: node.Name, Protected: node.Labels[batchLabel] == "deny"}
	if !nodeUsable(node) {
		p.Problem = "not ready or cordoned"
		return p
	}
	m, ok := metrics[nodeInternalIP(node)]
	if !ok {
		p.Problem = "not measured"
		return p
	}
	p.Metrics = m
	p.AvailableMemory = m.MemoryAvailable * node.Status.Allocatable.Memory().AsApproximateFloat64()
	pods, err := cs.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{FieldSelector: "spec.nodeName=" + node.Name})
	if err != nil {
		p.Problem = "requests not read"
		return p
	}
	cpu, memory := node.Status.Allocatable.Cpu().DeepCopy(), node.Status.Allocatable.Memory().DeepCopy()
	for i := range pods.Items {
		if phase := pods.Items[i].Status.Phase; phase == corev1.PodSucceeded || phase == corev1.PodFailed {
			continue
		}
		for _, c := range pods.Items[i].Spec.Containers {
			cpu.Sub(*c.Resources.Requests.Cpu())
			memory.Sub(*c.Resources.Requests.Memory())
		}
	}
	p.FreeCPU, p.FreeMemory = cpu.AsApproximateFloat64(), memory.AsApproximateFloat64()
	return p
}

func nodeInternalIP(n *corev1.Node) string {
	for _, a := range n.Status.Addresses {
		if a.Type == corev1.NodeInternalIP {
			return a.Address
		}
	}
	return ""
}

func nodeUsable(n *corev1.Node) bool {
	if n.Spec.Unschedulable {
		return false
	}
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}
