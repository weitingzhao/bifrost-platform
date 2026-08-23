/**
 * Market Data Subcontractor — architecture & PG schema contract (catalog-only).
 *
 * Created 2026-07-30 for bifrost-platform-plugin-market-data (Polygon REST ingest).
 *
 * Live state (not this catalog):
 * - Worker health + deployments + freshness tables: Subcontractors → Market Data (observe)
 * - Optional readiness_rollup on GET /api/v1/plugins/market-data/status (Plugin snapshot-coverage + vendor-gap; Trade owns runbook / per-symbol gaps)
 * - Coverage Quality Dashboard (secondary panels): Quality Score · Readiness · Financials · DB Summary · Capability (?tab=coverage&panel=readiness|financials)
 * - Phase / program sign-off: Engineer → Active Session · market-data-subcontractor
 * - Implementation: bifrost-platform-plugin-market-data
 */

export const MARKET_DATA_SUBCONTRACTOR_SOURCE = 'bifrost-platform-plugin-market-data'
export const MARKET_DATA_SUBCONTRACTOR_CATALOG_VERSION = '2026-08-20-dq-dashboard-coverage-panels'

/** Mission Launch lane — publish market-data plugin via kubectl apply (not Tekton). */
export const MARKET_DATA_LAUNCH_LANE = {
  id: 'launch-market-data',
  label: 'Launch Market Data',
  tabId: 'plugin-release',
  programId: 'market-data-subcontractor',
  /** Single Golden Source NS — STG/PROD overlays archived (W2-P2). */
  executor:
    'cd bifrost-platform-plugin-market-data && kubectl apply -k k8s/base  # single Golden Source NS',
  verify: 'kubectl -n plugin-market-data get deploy + /health (make verify-market-data)',
  steps: ['Detect', 'Approve', 'Install', 'Verify', 'Live check'] as const,
  galleryIsNotPublish:
    'Market Data manage page = observe health / deployments / freshness (+ readiness_rollup) and Coverage Quality panels (Readiness / Financials). Launch Plugin → Market Data seat = publish workers + API + CronJobs.',
  d10: 'Market-data REST ingest only — no place_order / no IB socket',
  imageTag: '0.7.0',
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

/** Ten-phase program from config/programs/completed/market-data-subcontractor.yaml. */
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
    deliverable:
      'scheduler/daily.py + CronJobs (incl. reference / fundamentals-rotate) + schedule.yaml',
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
  'Golden Source — single Plugin NS (`plugin-market-data`) serves all Trade environments; one DB (`bifrost_golden_source`).',
  'PG-as-broker — data_ops.job_ingest with SELECT FOR UPDATE SKIP LOCKED; no Celery Beat.',
  'Schema isolation — market.* (public data) + market_analytics.* (derived) + data_ops.* (jobs/ops); single Polygon vendor.',
  'K8s-native workers — stocks + options pools; CronJobs enqueue; NetworkPolicy egress to data NS + HTTPS.',
  'Watchlist union mode — Plugin reads union of all Trade environment watchlists via platform-api.',
  'Deterministic option-refresh rotation — sha256(date) offset covers full watchlist over days.',
  'Reference slot — daily ticker_sync (universe); weekends/holidays allowed (calendar-like).',
  'Fundamentals-rotate — CS universe (market.ticker) financials catch-up; missing income_statement first; skip non-trading days unless --force.',
  'Library SLA — ticker_sync age <24h; financials age <24h; CS financials coverage catch-up then daily rotate.',
  'Trading-calendar guard — stock-eod / eod-pipeline / universe-daily / corporate / fundamentals-rotate skip non-trading days.',
  'Plugin API read layer — Trade consumers read via HTTP (:8790 proxied via platform-api :8780); zero direct SQL.',
  'Data quality self-check — /market/readiness/snapshot-coverage (market.stock_snapshot instrument-type breakdown) + /market/readiness/vendor-gap (session_date vs stock_daily bar gap detection).',
  'D10-safe — REST market data only; no place_order / no IB socket path.',
] as const

export const PG_SCHEMA_CONTRACT = {
  database: 'bifrost_golden_source (single Golden Source — shared CNPG)',
  writerRole: 'data_writer',
  readerRole: 'market_reader',
  marketTables: [
    'market.stock_daily',
    'market.stock_minute',
    'market.stock_snapshot',
    'market.option_daily',
    'market.option_minute',
    'market.option_contract',
    'market.option_snapshot',
    'market.option_expiration',
    'market.option_open_interest',
    'market.ticker',
    'market.stock_financials',
    'market.corporate_action',
    'market.us_market_holiday',
    'market.ticker_related',
    'market.ticker_type',
  ] as const,
  dataOpsTables: [
    'data_ops.job_ingest',
    'data_ops.ingest_freshness',
  ] as const,
  watchlist: 'public.watchlist (Trade schema — SELECT only for scheduler)',
  namespace: 'plugin-market-data',
  deployments: ['polygon-worker-stocks', 'polygon-worker-options'] as const,
  healthService: 'market-data-health-stocks|options:8080/health',
  platformStatus: 'GET /api/v1/plugins/market-data/status',
} as const

export const MARKET_DATA_PROGRESS = {
  streamId: 'market-data-subcontractor',
  done: 10,
  total: 10,
  label: 'Market Data Subcontractor — Polygon → plugin-market-data NS',
} as const

export const MARKET_DATA_RELATED_AUTHORITIES = [
  'Live health + deployments + freshness: Subcontractors → Market Data (observe — not publish)',
  'Readiness rollup KPI: GET /api/v1/plugins/market-data/status → readiness_rollup (Plugin /market/readiness/snapshot-coverage + vendor-gap; Trade Stock Data Readiness owns runbook / per-symbol gaps)',
  'Coverage Quality Dashboard: Subcontractors → Market Data → Coverage → Quality Score / Readiness / Financials / DB Summary / Capability (deep link ?tab=coverage&panel=readiness)',
  'Manage UI: Subcontractors → Market Data (`market-data-manage`) — Overview (incl. Analytics demand) / Coverage / Ingest',
  'Plugin API proxy: GET unauthenticated; POST/DELETE operator-authed then platform-api attaches MARKET_DATA_WRITE_TOKEN toward :8790 (browser never holds the Plugin write secret)',
  'Publish: kubectl apply -k k8s/base + make verify-market-data',
  'Library SLA: ticker_sync <24h · financials cadence <24h · CS financials catch-up then daily rotate · related-companies rotate ≤7 trading days · ticker_type dictionary on-demand (reference + fundamentals-rotate + related-rotate CronJobs; image bifrost-market-data:0.7.6)',
  'Image tag: bifrost-market-data:0.7.6 (k8s/base only — STG/PROD overlays archived W2-P2; stale-running reclaim + job timeout)',
  'Readiness rollup: Plugin-native (universe / snapshot_covered / snapshot_rows / vendor_gap_count / as_of). No longer reads Trade public.stock_readiness_daily.',
  'Program / phase sign-off: Active Session (Engineer → Delivery) · market-data-subcontractor',
  'Implementation: bifrost-platform-plugin-market-data',
  'Spine: config/ops-context.yaml · GET /api/v1/context · milestone market-data-subcontractor',
  'DDL authority: bifrost-platform-plugin-market-data/src/bifrost_market_data/schema/ddl.py',
]

/** Archived phase statuses and spine progress snapshot — live sign-off in Active Session. */
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
    'Live health: Subcontractors → Market Data (observe) — not this catalog.',
    'Sign-off state: Active Session · market-data-subcontractor — not this catalog.',
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
