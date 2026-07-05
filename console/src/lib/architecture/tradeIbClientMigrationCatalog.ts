/**
 * Trade IB Client Migration — refactor Trade stack to consume Platform TWS bus (catalog-only).
 *
 * Prerequisite: IB Gateway Plugin program (IBGP0–4) — Platform owns TWS; Trade becomes bus-only.
 * All phases complete — no Architecture UI page; governance lives in this catalog + Delivery Board.
 *
 * Live state (not this catalog):
 * - Trade IB health aggregate: Rocket → Cluster + Monitor matrix probes
 * - Phase / rollout sign-off: Subcontractors → Delivery Board · trade-ib-migration
 * - Migrate lane: Engineer → Briefing · spine stream trade-ib-client-migration
 */

import {
  TIBM_ROLLOUT_ENV_STEPS,
  TIBM_ROLLOUT_WAVES,
  TRADE_EXECUTION_FREEZE,
} from './tradeIbClientMigrationRolloutCatalog'

export const TRADE_IB_CLIENT_MIGRATION_VERSION = '2026-07-04'
export const TRADE_IB_CLIENT_MIGRATION_STREAM_ID = 'trade-ib-client-migration'

export type TradeIbMigrationPhaseId = 'TIBM0' | 'TIBM1' | 'TIBM2' | 'TIBM3' | 'TIBM4'

export type TradeIbSurfaceStatus = 'on_bus' | 'partial' | 'direct_tws' | 'stale_ref' | 'retired'

export type TradeIbSurface = {
  id: string
  domain: string
  component: string
  repo: string
  path: string
  mode: string
  status: TradeIbSurfaceStatus
  targetPhase: TradeIbMigrationPhaseId
  notes: string
}

export type TradeIbRpcOpRow = {
  op: string
  legacySocket: 'yes' | 'no' | 'partial'
  platformGateway: 'yes' | 'no' | 'partial'
  tradeCallers: string
  targetPhase: TradeIbMigrationPhaseId
}

export const TRADE_IB_MIGRATION_PHASES: {
  id: TradeIbMigrationPhaseId
  title: string
  summary: string
  deliverable: string
  status: 'done' | 'in_progress' | 'pending'
}[] = [
  {
    id: 'TIBM0',
    title: 'Inventory & sign-off',
    summary:
      'Catalog every Trade IB surface: redis-ib read, operator RPC, direct TWS, stale health/UI refs. Lock Phase 1–4 scope.',
    deliverable: 'Console inventory tables + Phase 0 Owner sign-off',
    status: 'done',
  },
  {
    id: 'TIBM1',
    title: 'Gateway RPC parity',
    summary:
      'Extend bifrost-platform-plugin live/mock to match legacy socket operator executor (all ALL_OPS).',
    deliverable: 'Plugin tests + verify-ib-gateway-rpc-parity green with full op matrix',
    status: 'done',
  },
  {
    id: 'TIBM2',
    title: 'Trade read-path + health',
    summary:
      'Daemon heartbeat, Monitor status, API probes derive IB health from Platform gateway keys — not legacy ib_* socket trio.',
    deliverable: 'bifrost_core.monitor + monitor API status aligned to redis-ib',
    status: 'done',
  },
  {
    id: 'TIBM3',
    title: 'Workers RPC-only',
    summary:
      'Celery bars default to IbOperatorClient (fetch_bars_range); remove direct MarketIbClient / missing core ib_clients import.',
    deliverable: 'bifrost-trade-worker data/bars + config use_for_celery_bars default true',
    status: 'done',
  },
  {
    id: 'TIBM4',
    title: 'UI + legacy cleanup',
    summary:
      'Trade Frontend socket ingest UI maps to Platform gateway; ops ingest controls; archive bifrost-trade-socket ib/ references.',
    deliverable: 'FE + ops API + docs; no legacy IB STS paths in active manifests',
    status: 'done',
  },
]

/** Ground-truth inventory — Phase 0 deliverable (code survey 2026-07-04). */
export const TRADE_IB_SURFACES: TradeIbSurface[] = [
  {
    id: 'S01',
    domain: 'Market data',
    component: 'Live quotes / ticks',
    repo: 'bifrost-trade-core',
    path: 'core/realtime/redis_quotes.py',
    mode: 'redis_ib read (ib:ingester:tick:*)',
    status: 'on_bus',
    targetPhase: 'TIBM2',
    notes: 'Gateway writes; daemon + Market API read. Already cutover-ready.',
  },
  {
    id: 'S02',
    domain: 'Market data',
    component: 'Quote SSE / Market API',
    repo: 'bifrost-trade-api',
    path: 'market/ (RedisQuotesReader)',
    mode: 'redis_ib read',
    status: 'on_bus',
    targetPhase: 'TIBM2',
    notes: 'Depends on redis_ib host in overlay config + ExternalName.',
  },
  {
    id: 'S03',
    domain: 'Account',
    component: 'Account snapshots',
    repo: 'bifrost-platform-plugin',
    path: 'ib_gateway/live.py → ib:account:*',
    mode: 'redis_ib write (Gateway)',
    status: 'on_bus',
    targetPhase: 'TIBM1',
    notes: 'Trade reads hashes; no direct TWS from Trade pods.',
  },
  {
    id: 'S04',
    domain: 'Operator RPC',
    component: 'ib:operator:cmd stream',
    repo: 'bifrost_core',
    path: 'ib_operator/client.py',
    mode: 'redis_ib RPC',
    status: 'on_bus',
    targetPhase: 'TIBM1',
    notes: 'IbOperatorClient + Gateway live/mock — all 9 ALL_OPS implemented (TIBM1).',
  },
  {
    id: 'S05',
    domain: 'Operator RPC',
    component: 'Monitor fetch accounts',
    repo: 'bifrost-trade-api',
    path: 'monitor/routers/daemon.py',
    mode: 'redis_ib RPC (fetch_accounts_snapshot)',
    status: 'partial',
    targetPhase: 'TIBM1',
    notes: 'Uses IbOperatorClient; works when Gateway op implemented.',
  },
  {
    id: 'S06',
    domain: 'Celery data',
    component: 'Bars backfill',
    repo: 'bifrost-trade-worker',
    path: 'data/bars/tasks.py',
    mode: 'Platform Gateway RPC (fetch_bars_range via IbOperatorBarsAdapter)',
    status: 'on_bus',
    targetPhase: 'TIBM3',
    notes: 'Default transport TIBM3 — no direct TWS; use_for_celery_bars defaults true in bifrost_core 0.2.10+.',
  },
  {
    id: 'S07',
    domain: 'Daemon',
    component: 'ib_connected heartbeat',
    repo: 'bifrost-trade-core',
    path: 'monitor/integrations/daemon_ib_edge.py',
    mode: 'derived from legacy ib_operator + ib_ingestor + ib_account_agent health',
    status: 'on_bus',
    targetPhase: 'TIBM2',
    notes: 'derive_daemon_ib_heartbeat_from_redis — platform_ib_gateway rollup when plugin=ib-gateway on redis-ib.',
  },
  {
    id: 'S08',
    domain: 'Daemon',
    component: 'Order execution',
    repo: 'bifrost-trade-worker',
    path: 'daemon/execution/, daemon/fsm/',
    mode: 'FSM place_order — no ib_insync in worker yet',
    status: 'partial',
    targetPhase: 'TIBM1',
    notes: 'Architecture doc says ib_operator RPC; execution path not wired — design for Gateway RPC in Phase 1+.',
  },
  {
    id: 'S09',
    domain: 'Monitor / Ops',
    component: 'Socket status API',
    repo: 'bifrost-trade-api',
    path: 'monitor/routers/status.py',
    mode: 'redis health + legacy socket labels',
    status: 'on_bus',
    targetPhase: 'TIBM2',
    notes: 'GET /status socket.platform_ib_gateway aggregate; legacy ib_* blocks tagged transport=platform_gateway.',
  },
  {
    id: 'S10',
    domain: 'Ops control',
    component: 'Market ingest restart',
    repo: 'bifrost-trade-api',
    path: 'ops/routers/market_ingest.py',
    mode: 'Platform IB Gateway @ redis-ib (K8s data/ib-gateway)',
    status: 'on_bus',
    targetPhase: 'TIBM4',
    notes: 'GET /ops/market-ingest/services — platform_gateway_managed when plugin=ib-gateway; STG control → data/ib-gateway rollout.',
  },
  {
    id: 'S11',
    domain: 'Frontend',
    component: 'Socket ingest / IB broker UI',
    repo: 'bifrost-trade-frontend',
    path: 'pages/settings/socket/, utils/socketIngestLamp.ts',
    mode: 'Monitor /status platform_ib_gateway + component health',
    status: 'on_bus',
    targetPhase: 'TIBM4',
    notes: 'socketIngestLamp + ibBrokerConnectionModel — Platform IB Gateway labels; prefers socket.platform_ib_gateway aggregate.',
  },
  {
    id: 'S12',
    domain: 'Frontend',
    component: 'IB Connection settings',
    repo: 'bifrost-trade-frontend',
    path: 'pages/settings/IbConnectionPage.tsx',
    mode: 'read-only client_id from Monitor API',
    status: 'on_bus',
    targetPhase: 'TIBM4',
    notes: 'Displays config; no direct TWS. client_id sync via infra scripts (spine coupling_surface).',
  },
  {
    id: 'S13',
    domain: 'Legacy',
    component: 'trade-socket ib/*',
    repo: 'bifrost-trade-socket',
    path: 'ib/ingestor, account_agent, operator',
    mode: 'direct TWS (ib_insync)',
    status: 'retired',
    targetPhase: 'TIBM4',
    notes: 'K8s STS retired IBGP3; code retained as reference for RPC parity in Phase 1.',
  },
  {
    id: 'S14',
    domain: 'Config',
    component: 'redis_ib + ib_operator YAML',
    repo: 'bifrost-trade-infra',
    path: 'k8s/overlays/*/config/config.*.yaml',
    mode: 'redis_ib ExternalName + operator stream keys',
    status: 'on_bus',
    targetPhase: 'TIBM2',
    notes: 'Passwords synced via plugin make sync-redis-ib-secrets.',
  },
]

export const TRADE_IB_RPC_OP_MATRIX: TradeIbRpcOpRow[] = [
  { op: 'ping', legacySocket: 'yes', platformGateway: 'yes', tradeCallers: 'verify scripts, ops', targetPhase: 'TIBM1' },
  { op: 'disconnect_all', legacySocket: 'yes', platformGateway: 'yes', tradeCallers: 'ops market ingest reset', targetPhase: 'TIBM1' },
  { op: 'reconnect_all', legacySocket: 'yes', platformGateway: 'yes', tradeCallers: 'ops (legacy)', targetPhase: 'TIBM1' },
  { op: 'fetch_accounts_snapshot', legacySocket: 'yes', platformGateway: 'yes', tradeCallers: 'Monitor API daemon router', targetPhase: 'TIBM1' },
  { op: 'fetch_bars', legacySocket: 'yes', platformGateway: 'yes', tradeCallers: 'ad-hoc / tests', targetPhase: 'TIBM1' },
  { op: 'fetch_bars_range', legacySocket: 'yes', platformGateway: 'yes', tradeCallers: 'Celery bars backfill (IbOperatorBarsAdapter)', targetPhase: 'TIBM1' },
  { op: 'fetch_executions', legacySocket: 'yes', platformGateway: 'yes', tradeCallers: 'research / monitor (potential)', targetPhase: 'TIBM1' },
  { op: 'fetch_option_expirations', legacySocket: 'yes', platformGateway: 'yes', tradeCallers: 'research screener (potential)', targetPhase: 'TIBM1' },
  { op: 'fetch_option_snapshot', legacySocket: 'yes', platformGateway: 'yes', tradeCallers: 'research (potential)', targetPhase: 'TIBM1' },
]

export const TRADE_IB_MIGRATION_PRINCIPLES = [
  'Platform Gateway is the only process that opens TWS sockets (Host + Secondary slots).',
  'Trade pods are IB-transport ignorant — read redis-ib, write operator RPC only.',
  'No new direct ib_insync connections from Trade worker, api, or daemon containers.',
  'Health and UI must reflect Platform gateway aggregate — not retired socket StatefulSet names.',
  'R-DV3: Platform Agent must not auto-trigger trading write paths (order placement remains Trade daemon + operator approval).',
  'Post TIBM-PC rollout: live trading execution BLOCKED (spine D10) until Owner explicit unlock — see tradeIbClientMigrationRolloutCatalog.ts.',
] as const

export const TRADE_IB_MIGRATION_RELATED_AUTHORITIES = [
  'Trade IB health aggregate: Rocket → Cluster + Monitor matrix probes',
  'Program / rollout sign-off: Subcontractors → Delivery Board · trade-ib-migration',
  'Migrate lane + spine stream: Engineer → Briefing · trade-ib-client-migration',
  'Platform TWS bus prerequisite: ibGatewayPluginCatalog.ts · Rocket → Cluster (ib-gateway)',
  'Trading execution freeze: spine decision D10 · tradeIbClientMigrationRolloutCatalog.ts TRADE_EXECUTION_FREEZE',
  'Spine: config/ops-context.yaml · GET /api/v1/context',
]

export function surfaceStatusLabel(status: TradeIbSurfaceStatus): string {
  switch (status) {
    case 'on_bus':
      return 'On redis-ib bus'
    case 'partial':
      return 'Partial / RPC gap'
    case 'direct_tws':
      return 'Direct TWS (migrate)'
    case 'stale_ref':
      return 'Stale reference'
    case 'retired':
      return 'Retired (reference only)'
  }
}

export function surfaceStatusVariant(
  status: TradeIbSurfaceStatus,
): 'success' | 'warning' | 'danger' | 'neutral' {
  switch (status) {
    case 'on_bus':
      return 'success'
    case 'partial':
      return 'warning'
    case 'direct_tws':
    case 'stale_ref':
      return 'danger'
    case 'retired':
      return 'neutral'
  }
}

/** Archived phase statuses, surface tags, rollout wave snapshots — live sign-off on Delivery Board. */
export function buildTradeIbClientMigrationHistoricalAppendix(): string {
  const lines: string[] = [
    '## Historical progress (archived — do not treat as live)',
    '',
    `Spine stream: \`${TRADE_IB_CLIENT_MIGRATION_STREAM_ID}\` · all TIBM0–4 phases complete.`,
    '',
    '### Migration phases (status snapshot)',
    ...TRADE_IB_MIGRATION_PHASES.map(
      p => `- **${p.id}** [${p.status}] ${p.title} — ${p.deliverable}`,
    ),
    '',
    '### Surface inventory (status snapshot)',
    ...TRADE_IB_SURFACES.map(
      s =>
        `- ${s.id} [${s.status}] ${s.domain}/${s.component} (${s.repo}) → ${s.mode}; target ${s.targetPhase}`,
    ),
    '',
    '### Rollout waves (scope snapshot)',
    ...TIBM_ROLLOUT_WAVES.map(
      w =>
        `- **${w.id}** ${w.title} [${w.scope}] — ${w.notes} · Targets: ${w.targets.join('; ')}`,
    ),
    '',
    '### Rollout environment order (snapshot)',
    ...TIBM_ROLLOUT_ENV_STEPS.map(s => `- **${s.env}**: ${s.action} · Gate: ${s.gate}`),
    '',
    '### Trading execution freeze (D10 snapshot)',
    `Status: ${TRADE_EXECUTION_FREEZE.status} · spine ${TRADE_EXECUTION_FREEZE.spineDecision}`,
    TRADE_EXECUTION_FREEZE.rationale,
  ]
  return lines.join('\n')
}

export function buildTradeIbClientMigrationLlmPack(): string {
  const lines: string[] = [
    '# Trade IB Client Migration',
    `Version: ${TRADE_IB_CLIENT_MIGRATION_VERSION}`,
    `Stream: ${TRADE_IB_CLIENT_MIGRATION_STREAM_ID}`,
    'Live health + rollout state: Rocket → Cluster / Subcontractors → Delivery Board — not this catalog.',
    '',
    '## Prerequisite',
    'IB Gateway Plugin (IBGP0–4) — Platform TWS bus @ data/redis-ib.',
    '',
    '## Principles',
    ...TRADE_IB_MIGRATION_PRINCIPLES.map(p => `- ${p}`),
    '',
    '## Surface inventory (architecture reference)',
    ...TRADE_IB_SURFACES.map(
      s =>
        `- ${s.id} ${s.domain}/${s.component} (${s.repo} @ ${s.path}) → ${s.mode}; target ${s.targetPhase}. ${s.notes}`,
    ),
    '',
    '## Operator RPC parity matrix',
    ...TRADE_IB_RPC_OP_MATRIX.map(
      r =>
        `- ${r.op}: legacy=${r.legacySocket} gateway=${r.platformGateway} callers=${r.tradeCallers} → ${r.targetPhase}`,
    ),
    '',
    '## Migration phases (definitions)',
    ...TRADE_IB_MIGRATION_PHASES.map(
      p => `- **${p.id}** ${p.title} — ${p.summary} · Deliverable: ${p.deliverable}`,
    ),
    '',
    '## Related authorities',
    ...TRADE_IB_MIGRATION_RELATED_AUTHORITIES.map(a => `- ${a}`),
    '',
    buildTradeIbClientMigrationHistoricalAppendix(),
  ]
  return lines.join('\n')
}
