import type { ClusterMetricsResponse, ClusterSummary, Reachability } from '@/api/types'
import { apiReachability, clusterHealthHint } from '@/lib/cluster/clusterHealthHint'
import {
  ClusterRadialGauge,
  ClusterStatTile,
} from '@/components/cluster/ClusterKpiGauges'

function buildCoreNodesSublabel(
  nodePct: number,
  nodeTotal: number,
  elasticStandby: number,
  elasticDegraded: number,
): string | undefined {
  if (nodeTotal === 0) return undefined
  const parts: string[] = [`${nodePct}%`]
  if (elasticStandby > 0) parts.push(`+${elasticStandby} standby`)
  if (elasticDegraded > 0) parts.push(`${elasticDegraded} degraded`)
  return parts.join(' · ')
}

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
  const elasticStandby = summary.elastic_standby ?? 0
  const elasticDegraded = summary.elastic_degraded ?? 0
  const apiReach = apiReachability(summary)
  const healthHint = clusterHealthHint(summary)

  return (
    <section className="page-section panel-elevated px-4 py-3">
      {/*
       * Single row: gauges (left, wider) · divider · compact stat columns (right)
       * Stat columns (importance): connectivity / alerts / workload
       */}
      <div className="cluster-kpi-row">
        <ClusterRadialGauge
          label="Core nodes ready"
          value={nodePct}
          display={`${summary.nodes_ready}/${summary.nodes_total}`}
          sublabel={buildCoreNodesSublabel(nodePct, summary.nodes_total, elasticStandby, elasticDegraded)}
          sublabelMode="plain"
          reach={nodeReach}
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

        <div className="cluster-kpi-row__divider" aria-hidden="true" />

        <div className="cluster-kpi-stats">
          <div className="cluster-kpi-stat-col">
            <ClusterStatTile
              label="API"
              value={apiReach}
              reach={apiReach}
              hint="K8s API"
            />
            <ClusterStatTile
              label="Health"
              value={summary.reachability}
              reach={summary.reachability}
              hint={healthHint}
            />
          </div>

          <div className="cluster-kpi-stat-col">
            <ClusterStatTile
              label="Failing"
              value={String(summary.failing_pods)}
              reach={failingReach}
            />
            <ClusterStatTile
              label="Metrics"
              value={metricsOk ? 'ok' : 'n/a'}
              reach={metricsLamp(metrics?.metrics_server_available)}
            />
          </div>

          <div className="cluster-kpi-stat-col">
            <ClusterStatTile
              label="Running"
              value={String(summary.running_pods)}
              reach="ok"
            />
            <ClusterStatTile
              label="Pending"
              value={String(summary.pending_pods)}
              reach={summary.pending_pods > 0 ? 'degraded' : 'ok'}
            />
          </div>
        </div>
      </div>

      <p className="cluster-kpi-footer m-0 mt-3 font-mono-tabular text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        Layer A · metrics-server · {summary.distribution.toUpperCase()} @{' '}
        {summary.api_server.replace('https://', '')}
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
