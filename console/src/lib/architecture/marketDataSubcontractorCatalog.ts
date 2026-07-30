/**
 * Market Data Subcontractor — architecture & PG schema contract (catalog-only).
 *
 * Created 2026-07-30 for bifrost-platform-plugin-market-data (Polygon REST ingest).
 *
 * Live state (not this catalog):
 * - Worker health + deployments: Subcontractors → Plugin Gallery (observe)
 * - Phase / program sign-off: Mission Control → Delivery Board · market-data-subcontractor
 * - Implementation: bifrost-platform-plugin-market-data
 */

export const MARKET_DATA_SUBCONTRACTOR_SOURCE = 'bifrost-platform-plugin-market-data'
export const MARKET_DATA_SUBCONTRACTOR_CATALOG_VERSION = '2026-07-30-p9'

/** Mission Launch lane — publish market-data plugin via make apply (not Tekton). */
export const MARKET_DATA_LAUNCH_LANE = {
  id: 'launch-market-data',
  label: 'Launch Market Data',
  tabId: 'plugin-release',
  programId: 'market-data-subcontractor',
  executor: 'cd bifrost-platform-plugin-market-data && kubectl apply -k k8s/base',
  verify: 'make verify-market-data',
  steps: ['Detect', 'Apply', 'Verify', 'Live check'] as const,
  galleryIsNotPublish:
    'Plugin Gallery = observe health / deployments. Apply kustomize = publish workers + CronJobs.',
  d10: 'Market-data REST ingest only — no place_order / no IB socket',
} as const

export type MarketDataPhaseId =
  | 'P0'
  | 'P1'
  | 'P2'
  | 'P3'
  | 'P4'
  | 'P5'
  | 'P6'
  | 'P7'
  | 'P8'
  | 'P9'

export type MarketDataPhase = {
  id: MarketDataPhaseId
  spineStep: string
  title: string
  summary: string
  deliverable: string
  status: 'done' | 'in_progress' | 'pending'
}

/** Ten-phase program from config/programs/market-data-subcontractor.yaml. */
export const MARKET_DATA_PHASES: MarketDataPhase[] = [
  {
    id: 'P0',
    spineStep: '⓪',
    title: 'Repo skeleton + build framework',
    summary: 'pyproject, Dockerfile, K8s base, Makefile, CLAUDE.md.',
    deliverable: 'bifrost-market-data package installable; lint/test green',
    status: 'done',
  },
  {
    id: 'P1',
    spineStep: '①',
    title: 'DDL design + schema creation',
    summary: 'market.* + data_ops.* schemas; roles; SCHEMA.md.',
    deliverable: 'ddl.py + create_roles.sql + make db-init',
    status: 'done',
  },
  {
    id: 'P2',
    spineStep: '②',
    title: 'Polygon HTTP client + rate limiter',
    summary: 'Async httpx client, token-bucket tiers, endpoint builders.',
    deliverable: 'polygon/client.py + rate_limit.py + endpoints.py',
    status: 'done',
  },
  {
    id: 'P3',
    spineStep: '③',
    title: 'Worker core loop (PG-as-broker)',
    summary: 'SELECT FOR UPDATE SKIP LOCKED claim/dispatch + /health.',
    deliverable: 'worker/loop.py + claim.py + health.py',
    status: 'done',
  },
  {
    id: 'P4',
    spineStep: '④',
    title: 'Ingest handlers (all data types)',
    summary: 'Polygon → market.* upsert for stock/option/corporate/calendar.',
    deliverable: 'ingest/*.py + handler registry',
    status: 'done',
  },
  {
    id: 'P5',
    spineStep: '⑤',
    title: 'Scheduler + K8s CronJob',
    summary: 'CronJob-driven enqueue into data_ops.job_ingest; payload_hash dedup.',
    deliverable: 'scheduler/daily.py + 7 CronJobs + schedule.yaml',
    status: 'done',
  },
  {
    id: 'P6',
    spineStep: '⑥',
    title: 'K8s deployment + Platform integration',
    summary: 'Workers Running; Platform status probe; Console Gallery live.',
    deliverable: 'verify-market-data + /api/v1/plugins/market-data/status',
    status: 'done',
  },
  {
    id: 'P7',
    spineStep: '⑦',
    title: 'Full backfill + data quality verification',
    summary: 'Historical depth + freshness probe via data_ops.ingest_freshness.',
    deliverable: 'verify_data_quality.py + freshness green',
    status: 'done',
  },
  {
    id: 'P8',
    spineStep: '⑧',
    title: 'Trade System consumer switchover',
    summary: 'Trade readers → market.*; Celery massive queues retired.',
    deliverable: 'Trade API/Frontend on market.* schema',
    status: 'done',
  },
  {
    id: 'P9',
    spineStep: '⑨',
    title: 'Cleanup + permission lockdown',
    summary: 'Drop public.* market tables; lock PG roles; remove dead Celery code.',
    deliverable: 'Role isolation + program complete',
    status: 'done',
  },
]

export const MARKET_DATA_DESIGN_PRINCIPLES = [
  'PG-as-broker — data_ops.job_ingest with SELECT FOR UPDATE SKIP LOCKED; no Celery Beat.',
  'Schema isolation — market.* (public data) + data_ops.* (jobs/ops); single Polygon vendor.',
  'K8s-native workers — stocks + options pools; CronJobs enqueue; NetworkPolicy egress to data NS + HTTPS.',
  'Watchlist cross-schema — SELECT from public.watchlist (Trade); data_writer needs GRANT SELECT.',
  'Deterministic option-refresh rotation — sha256(date) offset covers full watchlist over days.',
  'Trading-calendar guard — stock-eod / eod-pipeline / universe-daily / corporate skip non-trading days.',
  'D10-safe — REST market data only; no place_order / no IB socket path.',
] as const

export const PG_SCHEMA_CONTRACT = {
  database: 'bifrost_dev | bifrost_prod (shared CNPG)',
  writerRole: 'data_writer',
  readerRole: 'market_reader',
  marketTables: [
    'market.stock_daily',
    'market.stock_minute',
    'market.option_daily',
    'market.option_minute',
    'market.option_contract',
    'market.option_snapshot',
    'market.option_expiration',
    'market.option_open_interest',
    'market.ticker',
    'market.stock_financials',
    'market.corporate_action',
  ] as const,
  dataOpsTables: [
    'data_ops.job_ingest',
    'data_ops.ingest_freshness',
    'data_ops.us_trading_calendar',
  ] as const,
  watchlist: 'public.watchlist (Trade schema — SELECT only for scheduler)',
  namespace: 'plugin-market-data',
  deployments: ['polygon-worker-stocks', 'polygon-worker-options'] as const,
  healthService: 'market-data-health-stocks|options:8080/health',
  platformStatus: 'GET /api/v1/plugins/market-data/status',
} as const

export const MARKET_DATA_PROGRESS = {
  streamId: 'market-data-subcontractor',
  done: 5,
  total: 10,
  label: 'Market Data Subcontractor — Polygon → plugin-market-data NS',
} as const

export const MARKET_DATA_RELATED_AUTHORITIES = [
  'Live health + deployments: Subcontractors → Plugin Gallery (observe — not publish)',
  'Publish: kubectl apply -k k8s/base + make verify-market-data',
  'Program / phase sign-off: Delivery Board · market-data-subcontractor',
  'Implementation: bifrost-platform-plugin-market-data',
  'Spine: config/ops-context.yaml · GET /api/v1/context · milestone market-data-subcontractor',
  'DDL authority: bifrost-platform-plugin-market-data/src/bifrost_market_data/schema/ddl.py',
]

/** Archived phase statuses and spine progress snapshot — live sign-off in Delivery Board. */
export function buildMarketDataHistoricalAppendix(): string {
  const lines: string[] = [
    '## Historical progress (archived — do not treat as live)',
    '',
    `Progress snapshot: ${MARKET_DATA_PROGRESS.done}/${MARKET_DATA_PROGRESS.total} — ${MARKET_DATA_PROGRESS.label}`,
    `Spine stream: \`${MARKET_DATA_PROGRESS.streamId}\``,
    '',
    '### Phases (status snapshot)',
    ...MARKET_DATA_PHASES.map(
      p => `- ${p.spineStep} **${p.id}** [${p.status}] ${p.title} — ${p.deliverable}`,
    ),
  ]
  return lines.join('\n')
}

export function buildMarketDataSubcontractorLlmPack(): string {
  const lines = [
    '# Market Data Subcontractor — implementation program',
    `Version: ${MARKET_DATA_SUBCONTRACTOR_CATALOG_VERSION}`,
    `Repo: ${MARKET_DATA_SUBCONTRACTOR_SOURCE}`,
    'Live health: Subcontractors → Plugin Gallery (observe) — not this catalog.',
    'Sign-off state: Delivery Board · market-data-subcontractor — not this catalog.',
    '',
    '## Launch lane',
    `- Label: ${MARKET_DATA_LAUNCH_LANE.label}`,
    `- Steps: ${MARKET_DATA_LAUNCH_LANE.steps.join(' → ')}`,
    `- Executor: ${MARKET_DATA_LAUNCH_LANE.executor}`,
    `- Verify: ${MARKET_DATA_LAUNCH_LANE.verify}`,
    `- ${MARKET_DATA_LAUNCH_LANE.galleryIsNotPublish}`,
    `- D10: ${MARKET_DATA_LAUNCH_LANE.d10}`,
    '',
    '## Design principles',
    ...MARKET_DATA_DESIGN_PRINCIPLES.map(p => `- ${p}`),
    '',
    '## PG schema contract',
    `- Database: ${PG_SCHEMA_CONTRACT.database}`,
    `- Writer: ${PG_SCHEMA_CONTRACT.writerRole} · Reader: ${PG_SCHEMA_CONTRACT.readerRole}`,
    `- Namespace: ${PG_SCHEMA_CONTRACT.namespace}`,
    `- Deployments: ${PG_SCHEMA_CONTRACT.deployments.join(', ')}`,
    `- Health: ${PG_SCHEMA_CONTRACT.healthService}`,
    `- Platform: ${PG_SCHEMA_CONTRACT.platformStatus}`,
    `- Watchlist: ${PG_SCHEMA_CONTRACT.watchlist}`,
    '- market tables:',
    ...PG_SCHEMA_CONTRACT.marketTables.map(t => `  - ${t}`),
    '- data_ops tables:',
    ...PG_SCHEMA_CONTRACT.dataOpsTables.map(t => `  - ${t}`),
    '',
    '## Phases (definitions)',
    ...MARKET_DATA_PHASES.map(
      p => `- ${p.spineStep} ${p.id} ${p.title} — ${p.summary} · Deliverable: ${p.deliverable}`,
    ),
    '',
    '## Related authorities',
    ...MARKET_DATA_RELATED_AUTHORITIES.map(a => `- ${a}`),
    '',
    buildMarketDataHistoricalAppendix(),
  ]
  return lines.join('\n')
}
