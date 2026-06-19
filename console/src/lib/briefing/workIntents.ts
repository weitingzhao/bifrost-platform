/** Owner selects work intent before generating Agent briefing content. */
export type WorkIntent =
  | 'ops'
  | 'feature'
  | 'debug'
  | 'release'
  | 'cluster'
  | 'frontend'
  | 'business'

/** Three-layer Agent architecture (from Vision). */
export type AgentLayer = 'Dev' | 'Ops' | 'Business'

export interface WorkIntentOption {
  id: WorkIntent
  label: string
  shortLabel: string
  description: string
  agentMode: 'Ops' | 'Product' | 'Promote'
  agentLayer: AgentLayer
}

export const WORK_INTENT_OPTIONS: WorkIntentOption[] = [
  {
    id: 'ops',
    label: 'Operations & governance',
    shortLabel: 'Ops',
    description: 'Matrix probes, spine, platform-api, infra YAML, K3s planning, actuation guardrails, Vision/Blueprint alignment.',
    agentMode: 'Ops',
    agentLayer: 'Ops',
  },
  {
    id: 'feature',
    label: 'Feature extension',
    shortLabel: 'Feature',
    description: 'New Console/API capabilities, Cluster panels, Delivery/GitOps UI, MCP server implementation.',
    agentMode: 'Ops',
    agentLayer: 'Dev',
  },
  {
    id: 'debug',
    label: 'Troubleshooting',
    shortLabel: 'Debug',
    description: 'Failing probes, cluster reachability, workload errors, connectivity blockers.',
    agentMode: 'Ops',
    agentLayer: 'Ops',
  },
  {
    id: 'release',
    label: 'Release & promote',
    shortLabel: 'Release',
    description: 'Flywheel A/B readiness, cutover blockers, release gate, prod matrix sign-off.',
    agentMode: 'Promote',
    agentLayer: 'Ops',
  },
  {
    id: 'cluster',
    label: 'Cluster & K3s',
    shortLabel: 'Cluster',
    description: 'Nodes, namespaces, workloads, data layer (Redis HA, PostgreSQL, MinIO), metrics-server (Layer A), observability stack (Layer B).',
    agentMode: 'Ops',
    agentLayer: 'Ops',
  },
  {
    id: 'frontend',
    label: 'Trade frontend migration',
    shortLabel: 'FE',
    description: 'bifrost-trade-frontend pages, Dense UI, Legacy API equivalence — Phase 1 discipline.',
    agentMode: 'Product',
    agentLayer: 'Dev',
  },
  {
    id: 'business',
    label: 'Trade analysis & advisory',
    shortLabel: 'Biz',
    description: 'Read-only market/portfolio/strategy analysis via Trade APIs; Greeks monitoring, SEPA research, risk advisory. No write operations.',
    agentMode: 'Ops',
    agentLayer: 'Business',
  },
]

export function workIntentById(id: WorkIntent): WorkIntentOption {
  return WORK_INTENT_OPTIONS.find(o => o.id === id) ?? WORK_INTENT_OPTIONS[0]
}
