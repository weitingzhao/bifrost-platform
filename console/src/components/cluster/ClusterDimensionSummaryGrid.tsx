import type {
  ClusterGovernanceResponse,
  ClusterMetricsResponse,
  ClusterObservabilityResponse,
  ClusterSummary,
  Reachability,
} from '@/api/types'
import { ClusterDimensionSummaryCard } from '@/components/cluster/ClusterDimensionSummaryCard'
import type { ClusterViewSection } from '@/lib/cluster/clusterViewSections'

interface ClusterDimensionSummaryGridProps {
  summary: ClusterSummary | undefined
  summaryLoading?: boolean
  governance: ClusterGovernanceResponse | undefined
  governanceLoading?: boolean
  observability: ClusterObservabilityResponse | undefined
  observabilityLoading?: boolean
  metrics: ClusterMetricsResponse | undefined
  onNavigate: (section: ClusterViewSection) => void
}

function nodesReach(summary: ClusterSummary | undefined): Reachability {
  if (summary == null || summary.nodes_total === 0) return 'unknown'
  if (summary.nodes_ready === summary.nodes_total) return 'ok'
  if (summary.nodes_ready === 0) return 'fail'
  return 'degraded'
}

function workloadsReach(summary: ClusterSummary | undefined): Reachability {
  if (summary == null) return 'unknown'
  if (summary.failing_pods > 0) return 'fail'
  if (summary.pending_pods > 0) return 'degraded'
  return 'ok'
}

function governanceReach(data: ClusterGovernanceResponse | undefined): Reachability {
  if (data == null) return 'unknown'
  const caps = data.cluster_capabilities ?? []
  if (caps.length === 0) return data.reachability
  const ok = caps.filter(c => c.reachability === 'ok').length
  if (ok === caps.length) return 'ok'
  if (ok === 0) return 'fail'
  return 'degraded'
}

function observabilityReach(data: ClusterObservabilityResponse | undefined): Reachability {
  if (data == null) return 'unknown'
  switch (data.layer_b_status) {
    case 'ready':
      return 'ok'
    case 'partial':
      return 'degraded'
    default:
      return 'unknown'
  }
}

function nodesSummary(summary: ClusterSummary | undefined): {
  reach: Reachability
  headline: string
  detail?: string
} {
  if (summary == null) return { reach: 'unknown', headline: '—' }
  const parts: string[] = [`${summary.nodes_ready}/${summary.nodes_total} core ready`]
  const elasticStandby = summary.elastic_standby ?? 0
  const elasticDegraded = summary.elastic_degraded ?? 0
  if (elasticStandby > 0) parts.push(`${elasticStandby} elastic standby`)
  if (elasticDegraded > 0) parts.push(`${elasticDegraded} elastic degraded`)
  const detail =
    elasticDegraded > 0
      ? 'Elastic nodes need attention'
      : summary.nodes_ready < summary.nodes_total
        ? 'Core node not Ready'
        : elasticStandby > 0
          ? 'Elastic capacity on standby'
          : 'All core nodes Ready'
  return {
    reach: nodesReach(summary),
    headline: parts.join(' · '),
    detail,
  }
}

function workloadsSummary(summary: ClusterSummary | undefined): {
  reach: Reachability
  headline: string
  detail?: string
} {
  if (summary == null) return { reach: 'unknown', headline: '—' }
  const headline = `${summary.running_pods} running · ${summary.failing_pods} failing · ${summary.pending_pods} pending`
  let detail = 'Pod workload stable'
  if (summary.failing_pods > 0) detail = `${summary.failing_pods} pod${summary.failing_pods === 1 ? '' : 's'} in Failed phase`
  else if (summary.pending_pods > 0) detail = `${summary.pending_pods} pod${summary.pending_pods === 1 ? '' : 's'} pending scheduling`
  return { reach: workloadsReach(summary), headline, detail }
}

function governanceSummary(data: ClusterGovernanceResponse | undefined): {
  reach: Reachability
  headline: string
  detail?: string
} {
  const caps = data?.cluster_capabilities ?? []
  const coverage = data?.node_coverage ?? []
  if (caps.length === 0 && coverage.length === 0) {
    return { reach: data?.reachability ?? 'unknown', headline: 'No governance data' }
  }
  const capsOk = caps.filter(c => c.reachability === 'ok').length
  const gaps = coverage.filter(c => c.reachability !== 'ok')
  const headline =
    caps.length > 0
      ? `${capsOk}/${caps.length} cluster capabilities ok`
      : `${coverage.length} node capability rules`
  const detail =
    gaps.length > 0
      ? `${gaps.length} coverage gap${gaps.length === 1 ? '' : 's'} — ${gaps[0]?.label ?? ''}`
      : 'Labels and probes aligned'
  return { reach: governanceReach(data), headline, detail }
}

function observabilitySummary(data: ClusterObservabilityResponse | undefined): {
  reach: Reachability
  headline: string
  detail?: string
} {
  if (data == null) return { reach: 'unknown', headline: '—' }
  const components = data.components ?? []
  const readyComponents = components.filter(c => c.reachability === 'ok').length
  let headline: string
  switch (data.layer_b_status) {
    case 'ready':
      headline = 'Layer B ready'
      break
    case 'partial':
      headline = 'Layer B partial'
      break
    default:
      headline = 'Layer A only · Layer B planned'
  }
  const detail =
    components.length > 0
      ? `${readyComponents}/${components.length} observability components`
      : data.detail !== ''
        ? data.detail
        : 'metrics-server on Layer A'
  return { reach: observabilityReach(data), headline, detail }
}

export function ClusterDimensionSummaryGrid({
  summary,
  summaryLoading = false,
  governance,
  governanceLoading = false,
  observability,
  observabilityLoading = false,
  metrics,
  onNavigate,
}: ClusterDimensionSummaryGridProps) {
  const nodes = nodesSummary(summary)
  const workloads = workloadsSummary(summary)
  const gov = governanceSummary(governance)
  const obs = observabilitySummary(observability)

  const metricsOk = metrics?.metrics_server_available === true

  return (
    <div className="cluster-dimension-grid">
      <p className="cluster-dimension-grid__kicker">Infrastructure</p>
      <div className="cluster-dimension-grid__cards cluster-dimension-grid__cards--infra">
        <ClusterDimensionSummaryCard
          title="Nodes"
          reach={nodes.reach}
          headline={nodes.headline}
          detail={nodes.detail}
          loading={summaryLoading}
          onDetails={() => onNavigate('nodes')}
        />
        <ClusterDimensionSummaryCard
          title="Workloads"
          reach={workloads.reach}
          headline={workloads.headline}
          detail={workloads.detail}
          loading={summaryLoading}
          onDetails={() => onNavigate('workloads')}
        />
        <ClusterDimensionSummaryCard
          title="Governance"
          reach={gov.reach}
          headline={gov.headline}
          detail={gov.detail}
          loading={governanceLoading}
          onDetails={() => onNavigate('platform')}
        />
        <ClusterDimensionSummaryCard
          title="Observability"
          reach={obs.reach}
          headline={obs.headline}
          detail={obs.detail}
          meta={metricsOk ? 'metrics-server' : 'metrics n/a'}
          loading={observabilityLoading}
          onDetails={() => onNavigate('platform')}
        />
      </div>
    </div>
  )
}
