package delivery

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/kubemetrics"
)

// Where a delivery's image builds run (the Owner, 2026-11-18: builds go to the
// fast node, and to the slower one when it is full). The same api build took
// about 120 s on ubt-k3s-07 and 266 s on ubt-k3s-05, whose disk writes take over
// 20 times as long. A soft preference cannot do this: a run's pods follow its
// affinity assistant, and Tekton's anti-affinity between assistants outweighs it
// (#598 sent 11 of 13 deliveries to 05). So the start reads what each node
// measures now and names one in the run's podTemplate. No host is named here: a
// node that joins and measures well is chosen as it is. A release never waits
// for this: with nothing measured or nothing fit, the scheduler decides as before.

const (
	hostIDLabel = "bifrost.io/host-id"
	batchLabel  = "bifrost.io/batch"

	buildPlacementTimeout = 5 * time.Second
	placementWhyLimit     = 300

	// Average write latency bounds of the disk tiers, and what counts as a full
	// node: the compute line's profile contract v1 (2026-11-18).
	diskTierFastSeconds   = 0.005
	diskTierUsableSeconds = 0.020
	busyLimit             = 0.85
	memoryAvailableFloor  = 0.15
)

// TaskProfile is what a kind of work needs from a node.
type TaskProfile struct {
	DiskHeavy   bool
	CPUCores    float64 // unrequested CPU the node must have
	MemoryBytes float64 // unrequested memory the node must have
}

// placedBuildPipelines are the pipelines whose builds are placed. Others follow
// once this one is measured. Room is what one kaniko step may reach.
var placedBuildPipelines = map[string]TaskProfile{
	"bifrost-deliver-platform": {DiskHeavy: true, CPUCores: 4, MemoryBytes: 4 << 30},
}

// NodeMetrics is one node's measurements. All four are present or the node is
// absent from the reader's answer.
type NodeMetrics struct {
	WriteLatencySeconds float64 // average over days: a quiet slow disk looks fast in a short window
	CPUBusy             float64 // 0 to 1, last 5 minutes
	DiskBusy            float64 // 0 to 1, last 5 minutes
	MemoryAvailable     float64 // 0 to 1
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

// SetNodeMetricsReader turns build placement on; unset, runs are not placed.
func (s *Service) SetNodeMetricsReader(r NodeMetricsReader) { s.nodeMetrics = r }

// SetNodeMetricsReader passes the reader to the service.
func (h *Handler) SetNodeMetricsReader(r NodeMetricsReader) { h.svc.SetNodeMetricsReader(r) }

// NodeProfile is a candidate as the choice sees it.
type NodeProfile struct {
	Host       string
	Problem    string // not ready, cordoned or not measured; "" when it can be judged
	Metrics    NodeMetrics
	FreeCPU    float64 // allocatable minus the requests of unfinished pods
	FreeMemory float64
}

// BuildPlacement is where a run's builds go; Node "" leaves it to the scheduler.
type BuildPlacement struct {
	Node string
	Why  string
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
	case m.CPUBusy >= busyLimit:
		return fmt.Sprintf("CPU %.0f%% busy", m.CPUBusy*100)
	case task.DiskHeavy && m.DiskBusy >= busyLimit:
		return fmt.Sprintf("disk %.0f%% busy", m.DiskBusy*100)
	case m.MemoryAvailable < memoryAvailableFloor:
		return fmt.Sprintf("memory %.0f%% available", m.MemoryAvailable*100)
	case n.FreeCPU < task.CPUCores:
		return fmt.Sprintf("%.1f CPU unrequested", n.FreeCPU)
	case n.FreeMemory < task.MemoryBytes:
		return fmt.Sprintf("%.1fGi unrequested", n.FreeMemory/(1<<30))
	}
	return ""
}

// chooseNode ranks the nodes that can take the task, the fastest disk first for
// disk-heavy work, and says why of the first and of each node passed over.
func chooseNode(task TaskProfile, nodes []NodeProfile) BuildPlacement {
	var fit []NodeProfile
	var passed []string
	for _, n := range nodes {
		problem := n.Problem
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
			if a, b := diskTier(fit[i].Metrics.WriteLatencySeconds), diskTier(fit[j].Metrics.WriteLatencySeconds); a != b {
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

// placeBuild chooses the node of a placed pipeline's run and writes it into
// spec; placed says the run's annotations should record the choice.
func (s *Service) placeBuild(ctx context.Context, pipeline string, spec map[string]any) (BuildPlacement, bool) {
	task, wanted := placedBuildPipelines[pipeline]
	if !wanted || s.nodeMetrics == nil {
		return BuildPlacement{}, false
	}
	ctx, cancel := context.WithTimeout(kubemetrics.WithSite(ctx, "delivery.build-placement"), buildPlacementTimeout)
	defer cancel()
	var p BuildPlacement
	if nodes, err := s.nodeProfiles(ctx); err != nil {
		p.Why = "not placed: " + err.Error()
	} else {
		p = chooseNode(task, nodes)
	}
	if p.Node != "" {
		pinBuildNode(spec, p.Node)
	}
	if len(p.Why) > placementWhyLimit {
		p.Why = p.Why[:placementWhyLimit]
	}
	slog.Info("delivery: build placement", "pipeline", pipeline, "node", p.Node, "why", p.Why)
	return p, true
}

// nodeProfiles reads the candidates: every node with a host id that batch work
// is not kept off.
func (s *Service) nodeProfiles(ctx context.Context) ([]NodeProfile, error) {
	cs, _, err := s.cluster.KubernetesClient()
	if err != nil {
		return nil, fmt.Errorf("the cluster could not be read: %w", err)
	}
	metrics, err := s.nodeMetrics(ctx)
	if err != nil {
		return nil, fmt.Errorf("the node metrics could not be read: %w", err)
	}
	list, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: hostIDLabel + "," + batchLabel + "!=deny"})
	if err != nil {
		return nil, fmt.Errorf("the nodes could not be read: %w", err)
	}
	nodes := make([]NodeProfile, 0, len(list.Items))
	for i := range list.Items {
		nodes = append(nodes, nodeProfile(ctx, cs, &list.Items[i], metrics))
	}
	return nodes, nil
}

func nodeProfile(ctx context.Context, cs kubernetes.Interface, node *corev1.Node, metrics map[string]NodeMetrics) NodeProfile {
	p := NodeProfile{Host: node.Labels[hostIDLabel]}
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

// pinBuildNode adds the host to the nodeSelector of the run's podTemplate,
// keeping what the template already says.
func pinBuildNode(spec map[string]any, host string) {
	template, _ := spec["taskRunTemplate"].(map[string]any)
	if template == nil {
		template = map[string]any{}
		spec["taskRunTemplate"] = template
	}
	pod, _ := template["podTemplate"].(map[string]any)
	if pod == nil {
		pod = map[string]any{}
		template["podTemplate"] = pod
	}
	selector, _ := pod["nodeSelector"].(map[string]any)
	if selector == nil {
		selector = map[string]any{}
		pod["nodeSelector"] = selector
	}
	selector[hostIDLabel] = host
}
