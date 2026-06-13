export type FlywheelId = 'A' | 'B' | 'coupling'

export type BayDefinition = {
  id: string
  label: string
  flywheel: FlywheelId
  description: string
  /** Matrix target IDs; empty = special logic */
  matrixTargets?: string[]
  /** Match targets whose id starts with any prefix */
  matrixIdPrefixes?: string[]
  /** Use prod env only */
  prodOnly?: boolean
  /** Static unknown lamp until CI wired */
  staticLamp?: 'unknown'
  externalUrlEnv?: string
}

export const BAY_REGISTRY: BayDefinition[] = [
  {
    id: 'bay_fe_migration',
    label: 'FE migration',
    flywheel: 'A',
    description: 'Page-by-page Dense UI migration vs Legacy (Phase 1)',
    staticLamp: 'unknown',
  },
  {
    id: 'bay_trade_reactor',
    label: 'Trade Reactor',
    flywheel: 'A',
    description: 'Business service topology on Trade monitoring UI',
    staticLamp: 'unknown',
    externalUrlEnv: 'VITE_TRADE_FRONTEND_URL',
  },
  {
    id: 'bay_promote_gate',
    label: 'Promote gate',
    flywheel: 'coupling',
    description: 'Release readiness and cutover blockers',
  },
  {
    id: 'bay_runtime_prod',
    label: 'Prod runtime',
    flywheel: 'B',
    description: 'Production nginx + probes (excl. forbidden)',
    prodOnly: true,
    matrixIdPrefixes: ['nginx', 'api-', 'ops-'],
  },
  {
    id: 'bay_data_pg_redis',
    label: 'PG / Redis',
    flywheel: 'B',
    description: 'Datastore TCP reachability',
    matrixTargets: ['postgres', 'redis'],
    prodOnly: true,
  },
  {
    id: 'bay_apis',
    label: 'Trade APIs',
    flywheel: 'B',
    description: 'All api-* health endpoints',
    matrixIdPrefixes: ['api-'],
    prodOnly: true,
  },
]

export function baysForFlywheel(fw: FlywheelId): BayDefinition[] {
  return BAY_REGISTRY.filter(b => b.flywheel === fw)
}

export function getBay(id: string): BayDefinition | undefined {
  return BAY_REGISTRY.find(b => b.id === id)
}
