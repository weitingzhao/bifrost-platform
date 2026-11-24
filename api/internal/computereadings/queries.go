// Package computereadings is the compute domain's readings of "is the
// compute fully used", as Prometheus queries. They were written in
// foundation's telemetry package until 2026-11-22; the compute line asked for
// them back (its contract, law 95), and telemetry now serves any group a
// domain registers (telemetry.Handler.SetGroup, bound in assembly).
package computereadings

import "github.com/weitingzhao/bifrost-platform/api/internal/telemetry"

// Group selects the compute readings on /telemetry/overview?group=compute.
const Group = "compute"

// computeWindow is the look-back of historical compute readings.
const computeWindow = "7d"

// computeWindowDays is computeWindow in days, for per-day rates.
const computeWindowDays = "7"

// perHost joins a node-exporter series to its host name, so the readings
// carry "ubt-k3s-06" and not an address. Hosts outside the cluster (the Agent
// host, the two Mac minis, the NAS) are scraped by the same job.
func perHost(expr string) string {
	return `max by (nodename) ((` + expr + `) * on (instance) group_left (nodename) node_uname_info)`
}

// diskWriteMs is the q-quantile over the window of each cluster node NVMe
// disk's 5-minute mean write time, in milliseconds.
func diskWriteMs(q, window string) string {
	disks := `{device=~"nvme[0-9]n1",pod!=""}`
	return `max by (nodename, device) (quantile_over_time(` + q + `, (rate(node_disk_write_time_seconds_total` + disks + `[5m])` +
		` / rate(node_disk_writes_completed_total` + disks + `[5m]) > 0)[` + window + `:5m]) * on (instance) group_left (nodename) node_uname_info) * 1000`
}

// Queries are the readings of "is the compute fully used" that
// Prometheus can answer today: work waiting while capacity is idle, the
// protected tiers being squeezed, the Agent host queueing, and how honest the
// requests are. Utilisation is deliberately not one of them: high can mean
// well used or crowded. The readings that cannot be measured yet (wall time
// per kind of work, the GPU node, the Owner's interventions) are named on the
// page, not faked here.
func Queries() []telemetry.QuerySpec {
	w := computeWindow
	fixed := func(q string) func(string) string { return func(string) string { return q } }
	// Cluster nodes only: their node exporter runs as a pod, the exporters on
	// the hosts outside the cluster do not.
	clusterBusy := `sum(rate(node_cpu_seconds_total{mode!="idle",mode!="iowait",pod!=""}[5m]))`
	clusterThreads := `count(node_cpu_seconds_total{mode="idle",pod!=""})`
	// The database homes as a node set, and the CI guests running on them now.
	databaseHomes := `max by (node) (label_replace(bifrost_node:home_info{home=~"database-.*"}` +
		` * on (instance) group_left (nodename) node_uname_info, "node", "$1", "nodename", "(.*)"))`
	guestsOnDatabaseHomes := `(kube_pod_status_phase{namespace="cicd",phase="Running"} == 1)` +
		` * on (namespace, pod) group_left (node) kube_pod_info{namespace="cicd",created_by_kind="TaskRun"}` +
		` * on (node) group_left () ` + databaseHomes
	return []telemetry.QuerySpec{
		{
			ID:     "compute_cluster_cpu_used_p50",
			Title:  "Cluster CPU in use, half of the time at most",
			Unit:   "ratio",
			Target: "a signal, no target",
			Note:   "Share of the cluster's CPU threads busy, median. With the next two rows this is the signal for \"is compute getting tight\": it is not a goal to raise.",
			Build:  fixed(`quantile_over_time(0.5, ` + clusterBusy + `[` + w + `:5m]) / ` + clusterThreads),
		},
		{
			ID:     "compute_cluster_cpu_used_p95",
			Title:  "Cluster CPU in use, 95% of the time at most",
			Unit:   "ratio",
			Target: "a signal, no target",
			Note:   "Same share at the 95th percentile: how full the cluster gets when it is busy.",
			Build:  fixed(`quantile_over_time(0.95, ` + clusterBusy + `[` + w + `:5m]) / ` + clusterThreads),
		},
		{
			ID:     "compute_node_cpu_used_avg",
			Title:  "CPU in use per cluster node, average",
			Unit:   "ratio",
			Target: "even across nodes",
			Note:   "How evenly the work is spread. One node far above the rest means the work is pinned or the requests mislead the scheduler, not that the cluster is full.",
			Build:  fixed(perHost(`avg_over_time((1 - avg by (instance) (rate(node_cpu_seconds_total{mode="idle",pod!=""}[5m])))[` + w + `:5m])`)),
		},
		{
			ID:     "compute_pods_pending_avg",
			Title:  "Pods waiting for a node, average",
			Unit:   "pods",
			Target: "0",
			Note:   "Work waiting at the scheduler. It does not see Agent sessions queueing on the Agent host or CI tasks run one after another on one node.",
			// 1h steps: at 5m (2026-11-21) and 15m (2026-11-22) the readings, all queried at once, ran past the 10s client timeout on PROD ("context deadline exceeded").
			Build: fixed(`avg_over_time(sum(kube_pod_status_phase{phase="Pending"})[` + w + `:1h])`),
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
		// Law 97 (home / borrow / reclaim), read now rather than over the
		// window: which node is whose home, which pain lines are crossed, and
		// how many CI guests run on the database homes. The home register is
		// the recording rule bifrost_node:home_info, kept in infra
		// k8s/monitoring/bifrost-compute-module-readout.yaml.
		{
			ID:     "compute_node_home_now",
			Title:  "Whose home each node is",
			Unit:   "home",
			Target: "every cluster node listed",
			Note:   "The home register. A node missing here is either down or not registered, and its pain and borrowing are not read.",
			Build:  fixed(`max by (nodename, home) (bifrost_node:home_info * on (instance) group_left (nodename) node_uname_info)`),
		},
		{
			ID:     "compute_pain_lines_now",
			Title:  "Pain lines crossed now",
			Unit:   "alerts",
			Target: "0",
			Note:   "ComputePain alerts firing. DiskFlush, Replication and ProdThrottle wake the compute seat to take guests back; GuestLoad is information only.",
			Build:  fixed(`sum by (alertname) (ALERTS{alertname=~"ComputePain.*",alertstate="firing"}) or on () vector(0)`),
		},
		{
			ID:     "compute_guests_on_protected_now",
			Title:  "CI guests running on the database homes",
			Unit:   "guests",
			Target: "a signal, no target",
			Note:   "Running TaskRun pods on nodes whose home is the database. Borrowing is allowed; these are the pods taken back when a pain line is crossed.",
			Build:  fixed(`count by (node) (` + guestsOnDatabaseHomes + `) or on () vector(0)`),
		},
		// Main line "seen and felt" (2026-11-21): what Prometheus already keeps
		// but the page did not show. History of the pain lines and of the
		// guests, the guests by name, and the nodes that are down (a down node
		// drops out of the home register instead of showing as down).
		{
			ID:     "compute_guests_on_protected_list_now",
			Title:  "Which CI guests run on the database homes now",
			Unit:   "list",
			Target: "a signal, no target",
			Note:   "The pods counted in the row above, by node. A reclaim deletes the newest of them.",
			Build:  fixed(`max by (node, pod) (` + guestsOnDatabaseHomes + `)`),
		},
		{
			ID:     "compute_pain_minutes",
			Title:  "Minutes each pain line fired",
			Unit:   "min",
			Target: "0 for DiskFlush, Replication and ProdThrottle",
			Note:   "Firing minutes per ComputePain alert over the window. Replication spikes last about 30 s and are harmless (asynchronous replication, the database owner's line is 5 s sustained); GuestLoad is information only.",
			Build:  fixed(`sum by (alertname) (count_over_time(ALERTS{alertname=~"ComputePain.*",alertstate="firing"}[` + w + `:1m]))`),
		},
		{
			ID:     "compute_guests_on_protected_max",
			Title:  "Most CI guests at once on each database home",
			Unit:   "guests",
			Target: "a signal, no target",
			Note:   "Highest number of running CI guests on a database home, sampled every hour (finer steps passed the 10 s timeout), so a burst shorter than an hour can be missed.",
			Build:  fixed(`max_over_time((count by (node) (` + guestsOnDatabaseHomes + `))[` + w + `:1h])`),
		},
		{
			ID:     "compute_nodes_without_home_now",
			Title:  "Cluster nodes missing from the home register",
			Unit:   "list",
			Target: "only nodes powered off on purpose",
			Note:   "Nodes Kubernetes knows that have no home reading: powered off, their node exporter gone, or never registered. The elastic GPU node stays here until it is woken.",
			Build: fixed(`max by (node) (kube_node_info) unless on (node) max by (node) (label_replace(bifrost_node:home_info` +
				` * on (instance) group_left (nodename) node_uname_info, "node", "$1", "nodename", "(.*)"))`),
		},
		// What each node's disks are and how they behave under the real load
		// (Owner 2026-11-22, relayed by the hardware line: placing a fast-disk
		// job on a slow disk is the mistake to avoid). The brand does not tell:
		// two nodes with the same model differ by two orders of magnitude in
		// write tail, so the readings are the model and the measured latency.
		{
			ID:     "compute_disks_now",
			Title:  "Disks in each cluster node",
			Unit:   "list",
			Target: "a register, no target",
			Note:   "NVMe model and firmware as each node reports them (node exporter). The model alone does not say how fast a disk is: read the write latency rows below.",
			Build:  fixed(`max by (nodename, device, model, firmware_revision) (node_nvme_info{pod!=""} * on (instance) group_left (nodename) node_uname_info)`),
		},
		{
			ID:     "compute_disk_write_ms_p50",
			Title:  "Disk write latency per node, typical",
			Unit:   "ms",
			Target: "a signal, no target",
			Note:   "Median over the window of the 5-minute mean time per completed write, NVMe disks of cluster nodes. A disk replaced in the window mixes the old disk's history in.",
			Build:  fixed(diskWriteMs("0.5", w)),
		},
		{
			ID:     "compute_disk_write_ms_p99",
			Title:  "Disk write latency per node, worst 1% of the time",
			Unit:   "ms",
			Target: "a signal, no target",
			Note:   "99th percentile of the same 5-minute means: the tail a database or a build writing under load meets. A cache-less disk shows here when a long write fills its fast area.",
			Build:  fixed(diskWriteMs("0.99", w)),
		},
		// Headroom and root-disk growth, the charter's R2 table and the CI
		// line's one-off disk reading (2026-11-15, 2026-11-20), computed by
		// the system instead of copied by hand (main line "seen and felt").
		{
			ID:     "compute_cpu_headroom_cores",
			Title:  "CPU headroom per node, cores",
			Unit:   "cores",
			Target: "a signal, no target",
			Note:   "Allocatable CPU minus the highest hourly-sampled container use over the window. Peaks on different nodes need not coincide; this is not a budget.",
			Build:  fixed(`max by (node) (kube_node_status_allocatable{resource="cpu"}) - max_over_time((sum by (node) (rate(container_cpu_usage_seconds_total{container!=""}[5m])))[` + w + `:1h])`),
		},
		{
			ID:     "compute_memory_headroom_gib",
			Title:  "Memory headroom per node, GiB",
			Unit:   "GiB",
			Target: "a signal, no target",
			Note:   "Allocatable memory minus the highest hourly-sampled container working set over the window.",
			Build:  fixed(`(max by (node) (kube_node_status_allocatable{resource="memory"}) - max_over_time((sum by (node) (container_memory_working_set_bytes{container!=""}))[` + w + `:1h])) / 2^30`),
		},
		{
			ID:     "compute_root_disk_used_now",
			Title:  "Root disk in use per cluster node, now",
			Unit:   "ratio",
			Target: "a signal, no target",
			Note:   "Current share of / unavailable to applications (1 - available bytes / total bytes). Read with the growth row to spot nodes running out of root-disk space.",
			Build:  fixed(perHost(`1 - node_filesystem_avail_bytes{mountpoint="/",pod!=""} / node_filesystem_size_bytes{mountpoint="/",pod!=""}`)),
		},
		{
			ID:     "compute_root_disk_growth_gb_per_day",
			Title:  "Root disk growth per node, GB a day",
			Unit:   "GB/day",
			Target: "a signal; look when a node fills within a month",
			Note:   "Free space lost on / over the window, divided by its days. A node younger than the window has no value.",
			Build:  fixed(`max by (nodename) (((node_filesystem_avail_bytes{mountpoint="/",pod!=""} offset ` + w + `) - node_filesystem_avail_bytes{mountpoint="/",pod!=""}) / ` + computeWindowDays + ` / 1e9 * on (instance) group_left (nodename) node_uname_info)`),
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
