/** Workload explorer — flat category tabs. */
export type NsFilterType = 'trade' | 'platform' | 'storage' | 'gpu' | 'cicd' | 'infra' | 'all'

export const NS_FILTER_GROUPS: Record<Exclude<NsFilterType, 'all'>, string[]> = {
  trade: ['bifrost-dev', 'bifrost-stg', 'bifrost-prod'],
  platform: ['bifrost-platform-stg'],
  storage: ['cnpg-system', 'data', 'data-warehouse'],
  gpu: ['ai'],
  cicd: ['cicd', 'tekton-pipelines', 'tekton-pipelines-resolvers'],
  infra: ['kube-system', 'monitoring'],
}

/** @deprecated alias */
export const NS_SUB_GROUPS = NS_FILTER_GROUPS
/** @deprecated alias */
export const NS_GROUPS = NS_FILTER_GROUPS

export const NS_FILTER_LABELS: Record<Exclude<NsFilterType, 'all'>, string> = {
  trade: 'Trade',
  platform: 'Platform',
  storage: 'Storage',
  gpu: 'RTX4090',
  cicd: 'CI/CD',
  infra: 'Infra',
}

/** Operator-facing labels on namespace chips (≠ K8s namespace name). */
export const NS_DISPLAY_LABELS: Record<string, string> = {
  'bifrost-dev': 'trade-dev',
  'bifrost-stg': 'trade-stg',
  'bifrost-prod': 'trade-prod',
  'bifrost-platform-stg': 'ops-stg',
  'cnpg-system': 'PgSQL',
  data: 'Redis',
  ai: 'RTX4090',
}

export const DEPRECATED_NAMESPACES = new Set([
  'bifrost',
  'default',
  'kube-node-lease',
  'kube-public',
])

export function nsFilterForNamespace(k8sName: string): NsFilterType {
  for (const [filter, names] of Object.entries(NS_FILTER_GROUPS) as [Exclude<NsFilterType, 'all'>, string[]][]) {
    if (names.includes(k8sName)) return filter
  }
  return 'all'
}

export function allowedNamespaceNames(filter: NsFilterType): string[] | null {
  if (filter === 'all') return null
  return NS_FILTER_GROUPS[filter]
}

export function namespaceDisplayLabel(k8sName: string): string {
  return NS_DISPLAY_LABELS[k8sName] ?? k8sName
}

export function namespaceShowsK8sHint(k8sName: string): boolean {
  const label = NS_DISPLAY_LABELS[k8sName]
  return label != null && label !== k8sName
}
