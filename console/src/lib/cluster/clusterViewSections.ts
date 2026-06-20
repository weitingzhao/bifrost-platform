export type ClusterViewSection = 'nodes' | 'workloads' | 'platform'

export const CLUSTER_VIEW_SECTIONS: {
  value: ClusterViewSection
  label: string
  hint: string
}[] = [
  {
    value: 'nodes',
    label: 'Nodes',
    hint: 'Node maintenance wizard and live node inventory.',
  },
  {
    value: 'workloads',
    label: 'Workloads',
    hint: 'Namespaces, deployments, and pod actuation.',
  },
  {
    value: 'platform',
    label: 'Platform',
    hint: 'Service readiness, governance capabilities, and observability stack.',
  },
]

export const DEFAULT_CLUSTER_VIEW_SECTION: ClusterViewSection = 'nodes'
