import type { ClusterMetricsResponse, ClusterSummary, Reachability } from '@/api/types'
import {
  ClusterRadialGauge,
  ClusterStatTile,
} from '@/components/cluster/ClusterKpiGauges'

interface ClusterOverviewKpiProps {
  summary: ClusterSummary | undefined
  metrics: ClusterMetricsResponse | undefined
  isLoading: boolean
}

function metricsLamp(available: boolean | undefined): Reachability {
  if (available === true) return 'ok'
  if (available === false) return 'degraded'
  return 'unknown'
}

export function ClusterOverviewKpi({ summary, metrics, isLoading }: ClusterOverviewKpiProps) {
  if (isLoading || summary == null) {
    return (
      <section className="page-section panel-elevated px-4 py-3">
        <span className="text-[var(--muted-foreground)]">Loading cluster health…</span>
      </section>
    )
  }

  const nodePct =
    summary.nodes_total > 0
      ? Math.round((summary.nodes_ready / summary.nodes_total) * 100)
      : 0
  const nodeReach: Reachability =
    summary.nodes_total === 0
      ? 'unknown'
      : summary.nodes_ready === summary.nodes_total
        ? 'ok'
        : summary.nodes_ready === 0
          ? 'fail'
          : 'degraded'
  const failingReach: Reachability = summary.failing_pods > 0 ? 'fail' : 'ok'
  const metricsOk = metrics?.metrics_server_available === true

  return (
    <section className="page-section panel-elevated px-4 py-3">
      <div className="cluster-kpi-grid">
        <ClusterStatTile
          label="API reach"
          value={summary.reachability}
          reach={summary.reachability}
        />
        <ClusterRadialGauge
          label="Nodes ready"
          value={nodePct}
          display={`${summary.nodes_ready}/${summary.nodes_total}`}
          sublabel={summary.nodes_total > 0 ? `${nodePct}%` : undefined}
          reach={nodeReach}
        />
        <ClusterStatTile
          label="Failing pods"
          value={String(summary.failing_pods)}
          reach={failingReach}
        />
        <ClusterStatTile
          label="Metrics server"
          value={metricsOk ? 'ok' : 'n/a'}
          reach={metricsLamp(metrics?.metrics_server_available)}
        />
        <ClusterStatTile
          label="Running pods"
          value={String(summary.running_pods)}
          reach="ok"
        />
        <ClusterStatTile
          label="Pending pods"
          value={String(summary.pending_pods)}
          reach={summary.pending_pods > 0 ? 'degraded' : 'ok'}
        />
        <ClusterRadialGauge
          label="Cluster CPU"
          value={metrics?.cpu_usage_percent}
          reach={metrics?.cpu_reachability ?? 'ok'}
          unavailable={!metricsOk}
          sublabel={summary.cpu_allocatable}
        />
        <ClusterRadialGauge
          label="Cluster memory"
          value={metrics?.memory_usage_percent}
          reach={metrics?.memory_reachability ?? 'ok'}
          unavailable={!metricsOk}
          sublabel={summary.memory_allocatable}
        />
      </div>

      <p className="cluster-kpi-footer m-0 mt-3 font-mono-tabular text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        {summary.distribution.toUpperCase()} @ {summary.api_server.replace('https://', '')}
        {summary.server_version != null && summary.server_version !== '' && (
          <> · {summary.server_version}</>
        )}
      </p>

      {summary.detail !== '' && (
        <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {summary.detail}
        </p>
      )}
      {!metricsOk && metrics?.metrics_server_detail != null && (
        <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {metrics.metrics_server_detail}
        </p>
      )}
    </section>
  )
}
