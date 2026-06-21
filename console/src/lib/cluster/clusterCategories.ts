export const INFRASTRUCTURE_CATEGORIES = [
  'nodes',
  'workloads',
  'governance',
  'observability',
] as const

export type InfrastructureCategory = (typeof INFRASTRUCTURE_CATEGORIES)[number]

/** Known application-stack domain ids from platform-api service readiness. */
export const APPLICATION_DOMAIN_IDS = [
  'database',
  'redis',
  'gpu',
  'warehouse',
  'workers',
  'applications',
  'cicd',
] as const

export type ApplicationDomainCategory = (typeof APPLICATION_DOMAIN_IDS)[number]

export type ClusterCategory = InfrastructureCategory | ApplicationDomainCategory | string

export type ClusterDimension = 'application' | 'infrastructure'

export const CLUSTER_CATEGORY_PARAM = 'category'

export const INFRASTRUCTURE_CATEGORY_LABELS: Record<InfrastructureCategory, string> = {
  nodes: 'Nodes',
  workloads: 'Workloads',
  governance: 'Governance',
  observability: 'Observability',
}

/** Purpose phrases from platform-api service readiness (finalizeDomain). */
export const APPLICATION_DOMAIN_PURPOSE: Record<ApplicationDomainCategory, string> = {
  database: 'CloudNativePG HA @ data NS · local-path + nfs-hot backup',
  redis: 'Bitnami live/queue @ data NS · per-env isolation (phase ⑥)',
  gpu: 'Elastic compute on gpu-server',
  warehouse: 'MinIO object store on gpu-server',
  workers: 'Daemon · Celery · data pipelines',
  applications: 'Frontend · nginx · 9 API domains',
  cicd: 'Gitea · Registry · Tekton builds',
}

const GENERIC_DOMAIN_SUMMARIES = new Set(['All dependencies satisfied'])

export function applicationDomainPurpose(domainId: string): string | undefined {
  if (!isApplicationDomainCategory(domainId)) return undefined
  return APPLICATION_DOMAIN_PURPOSE[domainId]
}

export function applicationDomainHeadline(domain: {
  id: string
  summary: string
}): { headline: string; detail?: string } {
  const summary = domain.summary.trim()
  const purpose = applicationDomainPurpose(domain.id)
  if (GENERIC_DOMAIN_SUMMARIES.has(summary) && purpose != null) {
    return { headline: purpose, detail: summary }
  }
  if (purpose != null && summary === '') {
    return { headline: purpose }
  }
  return { headline: summary || purpose || '—' }
}

export function isInfrastructureCategory(value: string): value is InfrastructureCategory {
  return (INFRASTRUCTURE_CATEGORIES as readonly string[]).includes(value)
}

export function isApplicationDomainCategory(value: string): value is ApplicationDomainCategory {
  return (APPLICATION_DOMAIN_IDS as readonly string[]).includes(value)
}

export function isClusterCategory(value: string | null | undefined): value is ClusterCategory {
  if (value == null || value === '') return false
  return isInfrastructureCategory(value) || isApplicationDomainCategory(value)
}

export function categoryDimension(category: ClusterCategory): ClusterDimension {
  return isInfrastructureCategory(category) ? 'infrastructure' : 'application'
}

export function parseCategoryFromUrl(url: URL = new URL(window.location.href)): ClusterCategory | null {
  const raw = url.searchParams.get(CLUSTER_CATEGORY_PARAM)
  return isClusterCategory(raw) ? raw : null
}

export function writeCategoryToUrl(category: ClusterCategory | null): void {
  const url = new URL(window.location.href)
  if (category == null) {
    url.searchParams.delete(CLUSTER_CATEGORY_PARAM)
  } else {
    url.searchParams.set(CLUSTER_CATEGORY_PARAM, category)
  }
  window.history.pushState({}, '', url)
}
