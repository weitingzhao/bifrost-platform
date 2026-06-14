/** Owner selects work intent before generating Agent briefing content. */
export type WorkIntent =
  | 'ops'
  | 'feature'
  | 'debug'
  | 'release'
  | 'cluster'
  | 'frontend'

export interface WorkIntentOption {
  id: WorkIntent
  label: string
  shortLabel: string
  description: string
  agentMode: 'Ops' | 'Product' | 'Promote'
}

export const WORK_INTENT_OPTIONS: WorkIntentOption[] = [
  {
    id: 'ops',
    label: 'Operations & governance',
    shortLabel: 'Ops',
    description: 'Matrix probes, spine, platform-api, infra YAML, K3s planning, actuation guardrails.',
    agentMode: 'Ops',
  },
  {
    id: 'feature',
    label: 'Feature extension',
    shortLabel: 'Feature',
    description: 'New Console/API capabilities, Cluster panels, Delivery/GitOps UI, MCP prep.',
    agentMode: 'Ops',
  },
  {
    id: 'debug',
    label: 'Troubleshooting',
    shortLabel: 'Debug',
    description: 'Failing probes, cluster reachability, workload errors, connectivity blockers.',
    agentMode: 'Ops',
  },
  {
    id: 'release',
    label: 'Release & promote',
    shortLabel: 'Release',
    description: 'Flywheel A/B readiness, cutover blockers, release gate, prod matrix sign-off.',
    agentMode: 'Promote',
  },
  {
    id: 'cluster',
    label: 'Cluster & K3s',
    shortLabel: 'Cluster',
    description: 'Nodes, namespaces, workloads, metrics-server (Layer A), observability stack (Layer B).',
    agentMode: 'Ops',
  },
  {
    id: 'frontend',
    label: 'Trade frontend migration',
    shortLabel: 'FE',
    description: 'bifrost-trade-frontend pages, Dense UI, Legacy API equivalence — Phase 1 discipline.',
    agentMode: 'Product',
  },
]

export function workIntentById(id: WorkIntent): WorkIntentOption {
  return WORK_INTENT_OPTIONS.find(o => o.id === id) ?? WORK_INTENT_OPTIONS[0]
}
