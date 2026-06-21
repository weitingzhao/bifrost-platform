import type {
  ClusterGovernanceResponse,
  ClusterMetricsResponse,
  ClusterObservabilityResponse,
  ClusterServiceReadinessResponse,
  ClusterSummary,
  Reachability,
  ServiceDomain,
} from '@/api/types'
import type { LucideIcon } from 'lucide-react'
import { ClusterCategoryCard } from '@/components/cluster/ClusterCategoryCard'
import type { ClusterCategory } from '@/lib/cluster/clusterCategories'
import {
  applicationDomainHeadline,
  INFRASTRUCTURE_CATEGORY_LABELS,
} from '@/lib/cluster/clusterCategories'
import { categoryIcon } from '@/lib/cluster/clusterCategoryIcons'

type CategoryCopyState = 'idle' | 'copied' | 'error'

interface ClusterCategoryGridProps {
  summary: ClusterSummary | undefined
  summaryLoading?: boolean
  serviceReadiness: ClusterServiceReadinessResponse | undefined
  serviceReadinessLoading?: boolean
  governance: ClusterGovernanceResponse | undefined
  governanceLoading?: boolean
  observability: ClusterObservabilityResponse | undefined
  observabilityLoading?: boolean
  metrics: ClusterMetricsResponse | undefined
  selectedCategory: ClusterCategory | null
  onSelectCategory: (category: ClusterCategory) => void
  categoryCopyId?: ClusterCategory | null
  categoryCopyState?: CategoryCopyState
  onCopyCategory?: (category: ClusterCategory, title: string) => void
}

function domainReach(status: string, reachability: Reachability): Reachability {
  if (status === 'unavailable') return 'fail'
  if (status === 'partial' || status === 'standby') return 'degraded'
  if (status === 'ready') return reachability === 'ok' ? 'ok' : reachability
  return reachability
}

function depsMeta(domain: ServiceDomain): string {
  const deps = domain.dependencies ?? []
  if (deps.length === 0) return 'No dependencies'
  const ok = deps.filter(d => d.reachability === 'ok').length
  return `${ok}/${deps.length} deps ok`
}

function degradedHeadline(domain: ServiceDomain): string {
  const deps = domain.dependencies ?? []
  const failing = deps.filter(d => d.reachability !== 'ok')
  if (failing.length === 0) return domain.summary || '—'
  const first = failing[0].label
  if (failing.length === 1) return first
  return `${first} +${failing.length - 1} gap${failing.length - 1 === 1 ? '' : 's'}`
}

function appDomainCard(domain: ServiceDomain) {
  const reach = domainReach(domain.status, domain.reachability)
  const isHealthy = reach === 'ok'

  let headline: string
  let detail: string | undefined
  if (!isHealthy) {
    headline = degradedHeadline(domain)
    detail = domain.summary !== '' ? domain.summary : undefined
  } else {
    const result = applicationDomainHeadline(domain)
    headline = result.headline
    detail = undefined
  }

  return {
    category: domain.id as ClusterCategory,
    title: domain.label,
    reach,
    headline,
    detail,
    meta: depsMeta(domain),
    icon: categoryIcon(domain.id),
  }
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

function nodesSummary(summary: ClusterSummary | undefined) {
  if (summary == null) return { reach: 'unknown' as Reachability, headline: '—', detail: undefined }
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
          : undefined
  return { reach: nodesReach(summary), headline: parts.join(' · '), detail }
}

function workloadsSummary(summary: ClusterSummary | undefined) {
  if (summary == null) return { reach: 'unknown' as Reachability, headline: '—', detail: undefined }
  const headline = `${summary.running_pods} running · ${summary.failing_pods} failing · ${summary.pending_pods} pending`
  let detail: string | undefined
  if (summary.failing_pods > 0) detail = `${summary.failing_pods} pod${summary.failing_pods === 1 ? '' : 's'} in Failed phase`
  else if (summary.pending_pods > 0) detail = `${summary.pending_pods} pod${summary.pending_pods === 1 ? '' : 's'} pending scheduling`
  return { reach: workloadsReach(summary), headline, detail }
}

function governanceSummary(data: ClusterGovernanceResponse | undefined) {
  const caps = data?.cluster_capabilities ?? []
  const coverage = data?.node_coverage ?? []
  if (caps.length === 0 && coverage.length === 0) {
    return { reach: data?.reachability ?? ('unknown' as Reachability), headline: 'No governance data', detail: undefined }
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
      : undefined
  return { reach: governanceReach(data), headline, detail }
}

function observabilitySummary(data: ClusterObservabilityResponse | undefined) {
  if (data == null) return { reach: 'unknown' as Reachability, headline: '—', detail: undefined, meta: undefined }
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
    data.layer_b_status === 'partial'
      ? `${readyComponents}/${components.length} observability components`
      : undefined
  const meta =
    components.length > 0
      ? `${readyComponents}/${components.length} components`
      : 'metrics-server'
  return { reach: observabilityReach(data), headline, detail, meta }
}

export function ClusterCategoryGrid({
  summary,
  summaryLoading = false,
  serviceReadiness,
  serviceReadinessLoading = false,
  governance,
  governanceLoading = false,
  observability,
  observabilityLoading = false,
  metrics,
  selectedCategory,
  onSelectCategory,
  categoryCopyId = null,
  categoryCopyState = 'idle',
  onCopyCategory,
}: ClusterCategoryGridProps) {
  const domains = serviceReadiness?.domains ?? []
  const appCards = domains.map(appDomainCard)

  const nodes = nodesSummary(summary)
  const workloads = workloadsSummary(summary)
  const gov = governanceSummary(governance)
  const obs = observabilitySummary(observability)
  const metricsOk = metrics?.metrics_server_available === true

  const infraCards: {
    category: ClusterCategory
    title: string
    reach: Reachability
    headline: string
    detail?: string
    meta?: string
    icon?: LucideIcon
    loading: boolean
  }[] = [
    { category: 'nodes', title: INFRASTRUCTURE_CATEGORY_LABELS.nodes, ...nodes, icon: categoryIcon('nodes'), loading: summaryLoading },
    { category: 'workloads', title: INFRASTRUCTURE_CATEGORY_LABELS.workloads, ...workloads, icon: categoryIcon('workloads'), loading: summaryLoading },
    { category: 'governance', title: INFRASTRUCTURE_CATEGORY_LABELS.governance, ...gov, icon: categoryIcon('governance'), loading: governanceLoading },
    {
      category: 'observability',
      title: INFRASTRUCTURE_CATEGORY_LABELS.observability,
      ...obs,
      meta: metricsOk ? 'metrics-server' : 'metrics n/a',
      icon: categoryIcon('observability'),
      loading: observabilityLoading,
    },
  ]

  const appReadyCount = domains.filter(d => d.status === 'ready').length

  return (
    <div className="cluster-category-grid">
      <section className="cluster-category-grid__section">
        <div className="cluster-category-grid__section-head">
          <p className="cluster-category-grid__kicker">Application stack</p>
          {!serviceReadinessLoading && domains.length > 0 && (
            <span className="cluster-category-grid__meta">
              {appReadyCount}/{domains.length} domains ready
            </span>
          )}
        </div>
        <p className="cluster-category-grid__desc">Workload domains — DB, cache, workers, apps, CI/CD.</p>
        <div className="cluster-category-grid__cards cluster-category-grid__cards--app">
          {serviceReadinessLoading ? (
            <ClusterCategoryCard
              title="Loading"
              reach="unknown"
              headline="Loading domains…"
              loading
              selected={false}
              onSelect={() => {}}
            />
          ) : domains.length === 0 ? (
            <p className="cluster-category-grid__empty">Cluster unreachable</p>
          ) : (
            appCards.map(card => (
              <ClusterCategoryCard
                key={card.category}
                title={card.title}
                reach={card.reach}
                headline={card.headline}
                detail={card.detail}
                meta={card.meta}
                icon={card.icon}
                selected={selectedCategory === card.category}
                copyState={categoryCopyId === card.category ? categoryCopyState : 'idle'}
                onSelect={() => onSelectCategory(card.category)}
                onCopyForLlm={
                  onCopyCategory != null ? () => onCopyCategory(card.category, card.title) : undefined
                }
              />
            ))
          )}
        </div>
      </section>

      <div className="cluster-home-summaries__divider" aria-hidden="true" />

      <section className="cluster-category-grid__section">
        <p className="cluster-category-grid__kicker">Infrastructure</p>
        <p className="cluster-category-grid__desc">Nodes, workloads, governance, observability.</p>
        <div className="cluster-category-grid__cards cluster-category-grid__cards--infra">
          {infraCards.map(card => (
            <ClusterCategoryCard
              key={card.category}
              title={card.title}
              reach={card.reach}
              headline={card.headline}
              detail={card.detail}
              meta={card.meta}
              icon={card.icon}
              loading={card.loading}
              selected={selectedCategory === card.category}
              copyState={categoryCopyId === card.category ? categoryCopyState : 'idle'}
              onSelect={() => onSelectCategory(card.category)}
              onCopyForLlm={
                onCopyCategory != null && !card.loading
                  ? () => onCopyCategory(card.category, card.title)
                  : undefined
              }
            />
          ))}
        </div>
      </section>
    </div>
  )
}
