package telemetry

// GroupCompute selects the compute readings on /telemetry/overview?group=compute.
const GroupCompute = "compute"

// computeWindow is the look-back of every compute reading.
const computeWindow = "7d"

// perHost joins a node-exporter series to its host name, so the readings
// carry "ubt-k3s-06" and not an address. Hosts outside the cluster (the Agent
// host, the two Mac minis, the NAS) are scraped by the same job.
func perHost(expr string) string {
	return `max by (nodename) ((` + expr + `) * on (instance) group_left (nodename) node_uname_info)`
}

// ComputeQueries are the readings of "is the compute fully used" that
// Prometheus can answer today: work waiting while capacity is idle, the
// protected tiers being squeezed, the Agent host queueing, and how honest the
// requests are. Utilisation is deliberately not one of them: high can mean
// well used or crowded. The readings that cannot be measured yet (wall time
// per kind of work, the GPU node, the Owner's interventions) are named on the
// page, not faked here.
func ComputeQueries() []QuerySpec {
	w := computeWindow
	fixed := func(q string) func(string) string { return func(string) string { return q } }
	return []QuerySpec{
		{
			ID:     "compute_pods_pending_avg",
			Title:  "Pods waiting for a node, average",
			Unit:   "pods",
			Target: "0",
			Note:   "Work waiting at the scheduler. It does not see Agent sessions queueing on the Agent host or CI tasks run one after another on one node.",
			Build:  fixed(`avg_over_time(sum(kube_pod_status_phase{phase="Pending"})[` + w + `:5m])`),
		},
		{
			ID:     "compute_schedule_p99",
			Title:  "Time to schedule a pod, p99",
			Unit:   "s",
			Target: "<= 30",
			Note:   "From a pod's creation to its placement.",
			Build:  fixed(`histogram_quantile(0.99, sum by (le) (rate(scheduler_pod_scheduling_sli_duration_seconds_bucket[` + w + `])))`),
		},
		{
			ID:     "compute_cpu_pressure_p99",
			Title:  "CPU pressure per host, p99",
			Unit:   "ratio",
			Target: "does not rise during batch peaks",
			Note:   "Share of time some process waited for a CPU (PSI). The database primary and PROD hosts are the ones to watch.",
			Build:  fixed(perHost(`quantile_over_time(0.99, rate(node_pressure_cpu_waiting_seconds_total[5m])[` + w + `:5m])`)),
		},
		{
			ID:     "compute_cpu_throttled_top",
			Title:  "CPU-throttled containers, worst five",
			Unit:   "ratio",
			Target: "0 (no CPU limit)",
			Note:   "Share of scheduler periods cut short by the container's own CPU limit. CI is left out.",
			Build: fixed(`topk(5, sum by (namespace, container) (increase(container_cpu_cfs_throttled_periods_total{container!="",namespace!="cicd"}[` + w + `]))` +
				` / sum by (namespace, container) (increase(container_cpu_cfs_periods_total{container!="",namespace!="cicd"}[` + w + `])) > 0.01)`),
		},
		{
			ID:     "compute_oom_kills",
			Title:  "Out-of-memory kills per host",
			Unit:   "kills",
			Target: "0",
			Note:   "Processes the kernel killed for memory.",
			Build:  fixed(perHost(`increase(node_vmstat_oom_kill[` + w + `])`)),
		},
		{
			ID:     "compute_pods_evicted",
			Title:  "Evicted pods still listed",
			Unit:   "pods",
			Target: "0",
			Note:   "Pods the kubelet evicted for node pressure.",
			Build:  fixed(`sum(kube_pod_status_reason{reason="Evicted"})`),
		},
		{
			ID:     "compute_load_per_cpu_p99",
			Title:  "Load per CPU thread per host, p99",
			Unit:   "x",
			Target: "<= 1.5",
			Note:   "1-minute load over the thread count. Above 1 means runnable work was queueing; the Agent host is the one to watch.",
			Build:  fixed(perHost(`quantile_over_time(0.99, node_load1[` + w + `]) / on (instance) count by (instance) (node_cpu_seconds_total{mode="idle"})`)),
		},
		{
			ID:     "compute_memory_pressure_max",
			Title:  "Memory pressure per host, highest",
			Unit:   "ratio",
			Target: "near 0",
			Note:   "Share of time some process waited for memory (PSI), worst 5 minutes.",
			Build:  fixed(perHost(`max_over_time(rate(node_pressure_memory_waiting_seconds_total[5m])[` + w + `:5m])`)),
		},
		{
			ID:     "compute_requests_honest_share",
			Title:  "Containers whose CPU request fits their use",
			Unit:   "ratio",
			Target: "a health check, no target",
			Note:   "Share of running containers with a CPU request between 0.7 and 1.5 times their p95 use. CI is left out. The weekly KRR table uses a different rule (highest per-pod p95, deleted pods included).",
			Build: fixed(`count((sum by (namespace, pod, container) (kube_pod_container_resource_requests{resource="cpu",namespace!="cicd"})` +
				` / on (namespace, pod, container) quantile_over_time(0.95, sum by (namespace, pod, container) (rate(container_cpu_usage_seconds_total{container!="",namespace!="cicd"}[5m]))[` + w + `:5m])) >= 0.7 <= 1.5)` +
				` / count(sum by (namespace, pod, container) (kube_pod_container_resource_requests{resource="cpu",namespace!="cicd"}))`),
		},
	}
}
