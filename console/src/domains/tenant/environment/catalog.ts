import type { ScopeRow, HardwareRow } from '@/domains/foundation/environment-catalog/contracts'

export const tenantScopeRows: Record<string, ScopeRow> = {
  'TRADE-FE': {
    tag: 'TRADE-FE',
    component: 'bifrost-trade-frontend',
    technology: 'React 18 · Vite · TanStack Query · shadcn/ui',
    notes: 'Trade monitoring SPA :5173. Separate from Platform Console. Phase 1: New FE + Legacy API equivalence.',
  },
  'TRADE-API': {
    tag: 'TRADE-API',
    component: 'bifrost-trade-api (path domains; massive retired)',
    technology: 'FastAPI · ports 8765–8773 via nginx /api/{domain}/',
    notes:
      'monitor · docs · ops · trading · strategy · portfolio · market · research (massive REST retired → Market Data Plugin). Platform probes health endpoints only.',
  },
  'WORKER': {
    tag: 'WORKER',
    component: 'bifrost-trade-worker',
    technology: 'Python · daemon (GsTrading FSM) · account-sync',
    notes: 'Daemon reads Redis quotes/account; orders via ib_operator RPC. Does not connect to TWS directly.',
  },
  'CORE': {
    tag: 'CORE',
    component: 'bifrost-trade-core',
    technology: 'Python package bifrost_core — config, persistence, portfolio, monitor',
    notes: 'Shared library pip-installed by api/worker/socket. Version via BIFROST_CORE_REF tag in Docker builds.',
  },
  'INFRA': {
    tag: 'INFRA',
    component: 'bifrost-trade-infra',
    technology: 'Docker Compose · nginx · Makefile · config/*.yaml',
    notes:
      'Prod: docker-compose.yml on mini-pc-a. Dev: docker-compose.dev.yml. make prod-health · prod-preflight · release_gate (planned).',
  },
  'REDIS': {
    tag: 'REDIS',
    component: 'Redis',
    technology: 'Prod on mini-pc-a :6379 · Dev local or co-located',
    notes: 'Streams: ib:ingester:tick:* · RPC: ib:operator:cmd · Health keys bifrost:health:ws_*',
  },
}

export const tenantHardware: Record<string, HardwareRow> = {
  'win11-host': {
    id: 'win11-host',
    host: '(LAN TBD)',
    roleCompose: 'TWS Host account',
    roleK3s: 'Never in cluster',
  },
  'win11-secondary': {
    id: 'win11-secondary',
    host: '(LAN TBD)',
    roleCompose: 'TWS Secondary account',
    roleK3s: 'Never in cluster',
  },
}
