import { useEffect, useState } from 'react'
import type {
  ClusterGovernanceResponse,
  ClusterMetricsResponse,
  ClusterObservabilityResponse,
  ClusterPlacementResponse,
  ClusterServiceReadinessResponse,
  ClusterSummary,
  ServiceDomain,
} from '@/api/clusterTypes'
import type { Reachability } from '@/api/matrixTypes'
import type { LucideIcon } from 'lucide-react'
import { ChevronDown } from 'lucide-react'
import { StatusLamp } from '@/components/StatusLamp'
import { ClusterCategoryCard } from '@/components/cluster/ClusterCategoryCard'
import type { ClusterCategory, ClusterDimension } from '@/lib/cluster/clusterCategories'
import {
  applicationDomainHeadline,
  categoryDimension,
  FACILITY_CATEGORIES,
  FACILITY_CATEGORY_LABELS,
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
  placement: ClusterPlacementResponse | undefined
  placementLoading?: boolean
  metrics: ClusterMetricsResponse | undefined
  selectedCategory: ClusterCategory | null
  onSelectCategory: (category: ClusterCategory) => void
  categoryCopyId?: ClusterCategory | null
  categoryCopyState?: CategoryCopyState
  onCopyCategory?: (category: ClusterCategory, title: string) => void
}

function domainReach(status: string, reachability: Reachability): Reachability {
  if (status === 'unavailable') return 'fail'
  // Standby (elastic powered-off / scaled-to-zero, no demand) is NEUTRAL — not degraded.
  if (status === 'standby') return 'unknown'
  if (status === 'partial') return 'degraded'
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

function standbyDomainHeadline(domain: ServiceDomain): { headline: string; detail?: string } {
  const summary = domain.summary.trim()
  if (summary !== '' && /demand|needed|offline|unavailable/i.test(summary)) {
    return { headline: summary }
  }
  if (domain.id === 'gpu' || domain.id === 'warehouse') {
    return {
      headline: 'Standby — no demand',
      detail: summary !== '' && !/standby/i.test(summary) ? summary : undefined,
    }
  }
  return {
    headline: summary !== '' ? summary : 'Standby — no demand',
  }
}

function appDomainCard(domain: ServiceDomain) {
  const reach = domainReach(domain.status, domain.reachability)
  const isHealthy = reach === 'ok'

  let headline: string
  let detail: string | undefined
  if (domain.status === 'standby') {
    const result = standbyDomainHeadline(domain)
    headline = result.headline
    detail = result.detail
  } else if (!isHealthy) {
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

function placementReach(data: ClusterPlacementResponse | undefined): Reachability {
  if (data == null) return 'unknown'
  const critical = data.violations.filter(v => v.severity === 'critical').length
  const amd64Ci = data.pools.find(p => p.id === 'amd64_ci')
  if (critical > 0 || (amd64Ci != null && amd64Ci.nodes_ready === 0)) return 'fail'
  return data.reachability
}

function facilityCardSummaries(data: ClusterPlacementResponse | undefined) {
  const reach = placementReach(data)
  const pools = data?.pools ?? []
  const rules = data?.rules ?? []
  const satisfied = rules.filter(r => r.satisfied).length
  const critical = data?.violations.filter(v => v.severity === 'critical').length ?? 0
  const amd64Ci = pools.find(p => p.id === 'amd64_ci')
  const amd64Ready = amd64Ci?.nodes_ready ?? 0

  return {
    node_pools: {
      reach,
      headline:
        pools.length === 0
          ? 'No pools'
          : `${pools.length} pools · ${pools.filter(p => p.status === 'live').length} live`,
      detail: data?.detail,
      meta: amd64Ci != null ? `amd64_ci ${amd64Ready}/${amd64Ci.nodes_total}` : undefined,
    },
    policy_matrix: {
      reach: rules.length === 0 ? ('unknown' as Reachability) : satisfied === rules.length ? 'ok' : reach,
      headline: rules.length === 0 ? 'No rules' : `${satisfied}/${rules.length} rules satisfied`,
      detail: critical > 0 ? `${critical} critical violation${critical === 1 ? '' : 's'}` : undefined,
      meta: critical > 0 ? `${critical} critical` : '0 critical',
    },
    ci_readiness: {
      reach: amd64Ready > 0 ? ('ok' as Reachability) : data == null ? ('unknown' as Reachability) : 'fail',
      headline:
        amd64Ci == null
          ? 'amd64_ci unknown'
          : amd64Ready > 0
            ? 'Kaniko build ready'
            : 'Kaniko blocked — no amd64_ci Ready',
      detail: amd64Ci != null ? `amd64_ci Ready ${amd64Ready}/${amd64Ci.nodes_total}` : undefined,
      meta: 'deliver-stg',
    },
  }
}

function worstReach(reaches: Reachability[]): Reachability {
  if (reaches.includes('fail')) return 'fail'
  if (reaches.includes('degraded')) return 'degraded'
  if (reaches.includes('unknown')) return 'unknown'
  if (reaches.length === 0) return 'unknown'
  return 'ok'
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
  placement,
  placementLoading = false,
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
  const facility = facilityCardSummaries(placement)
  const metricsOk = metrics?.metrics_server_available === true

  type Card = {
    category: ClusterCategory
    title: string
    reach: Reachability
    headline: string
    detail?: string
    meta?: string
    icon?: LucideIcon
    loading: boolean
  }

  const infraCards: Card[] = [
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

  const facilityCards: Card[] = FACILITY_CATEGORIES.map(id => ({
    category: id,
    title: FACILITY_CATEGORY_LABELS[id],
    ...facility[id],
    icon: categoryIcon(id),
    loading: placementLoading,
  }))

  const appReadyCount = domains.filter(d => d.status === 'ready').length
  const rulesSatisfied = (placement?.rules ?? []).filter(r => r.satisfied).length
  const rulesTotal = placement?.rules.length ?? 0

  const [openDimension, setOpenDimension] = useState<ClusterDimension | null>(() =>
    selectedCategory != null ? categoryDimension(selectedCategory) : null,
  )

  useEffect(() => {
    if (selectedCategory == null) return
    setOpenDimension(categoryDimension(selectedCategory))
  }, [selectedCategory])

  const infraReach = worstReach(infraCards.map(c => (c.loading ? 'unknown' : c.reach)))
  const appReach = serviceReadinessLoading
    ? ('unknown' as Reachability)
    : worstReach(appCards.map(c => c.reach))
  const facilityReach = worstReach(facilityCards.map(c => (c.loading ? 'unknown' : c.reach)))

  const dimensionSummaries: {
    id: ClusterDimension
    title: string
    reach: Reachability
    headline: string
    meta: string
    loading: boolean
  }[] = [
    {
      id: 'infrastructure',
      title: 'Infrastructure',
      reach: infraReach,
      headline: 'Nodes · workloads · governance · observability',
      meta: `${infraCards.length} categories`,
      loading: summaryLoading || governanceLoading || observabilityLoading,
    },
    {
      id: 'application',
      title: 'Application stack',
      reach: appReach,
      headline:
        domains.length === 0
          ? 'Workload domains'
          : `${appReadyCount}/${domains.length} domains ready`,
      meta: domains.length > 0 ? `${domains.length} domains` : 'DB · cache · workers · apps',
      loading: serviceReadinessLoading,
    },
    {
      id: 'facility',
      title: 'Facility',
      reach: facilityReach,
      headline:
        rulesTotal > 0
          ? `${rulesSatisfied}/${rulesTotal} policies ok`
          : 'Node pools · policy · CI readiness',
      meta: `${facilityCards.length} categories`,
      loading: placementLoading,
    },
  ]

  function renderCards(cards: Card[]) {
    return cards.map(card => (
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
    ))
  }

  function toggleDimension(id: ClusterDimension) {
    setOpenDimension(prev => (prev === id ? null : id))
  }

  return (
    <div className="cluster-category-grid">
      <div className="cluster-category-grid__summaries" role="tablist" aria-label="Category dimensions">
        {dimensionSummaries.map(dim => {
          const open = openDimension === dim.id
          return (
            <button
              key={dim.id}
              type="button"
              role="tab"
              aria-selected={open}
              aria-expanded={open}
              className={`cluster-category-dim${open ? ' cluster-category-dim--open' : ''}`}
              onClick={() => toggleDimension(dim.id)}
            >
              <div className="cluster-category-dim__head">
                <StatusLamp
                  value={dim.loading ? 'unknown' : dim.reach}
                  kind="reach"
                  variant={open ? 'filled' : 'outline'}
                />
                <span className="cluster-category-dim__title">{dim.title}</span>
                <ChevronDown className="cluster-category-dim__chevron" aria-hidden="true" />
              </div>
              <p className="cluster-category-dim__headline">
                {dim.loading ? 'Loading…' : dim.headline}
              </p>
              <p className="cluster-category-dim__meta">{dim.meta}</p>
            </button>
          )
        })}
      </div>

      {openDimension === 'infrastructure' && (
        <section className="cluster-category-grid__section" aria-label="Infrastructure categories">
          <p className="cluster-category-grid__desc">
            Nodes, workloads, governance, observability.
          </p>
          <div className="cluster-category-grid__cards cluster-category-grid__cards--infra">
            {renderCards(infraCards)}
          </div>
        </section>
      )}

      {openDimension === 'application' && (
        <section className="cluster-category-grid__section" aria-label="Application stack categories">
          <p className="cluster-category-grid__desc">
            Workload domains — DB, cache, workers, apps, CI/CD.
          </p>
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
              renderCards(
                appCards.map(card => ({
                  ...card,
                  loading: false,
                })),
              )
            )}
          </div>
        </section>
      )}

      {openDimension === 'facility' && (
        <section className="cluster-category-grid__section" aria-label="Facility categories">
          <p className="cluster-category-grid__desc">
            Fleet facility constraints — node pools, scheduling policy, CI readiness.
          </p>
          <div className="cluster-category-grid__cards cluster-category-grid__cards--infra">
            {renderCards(facilityCards)}
          </div>
        </section>
      )}
    </div>
  )
}
