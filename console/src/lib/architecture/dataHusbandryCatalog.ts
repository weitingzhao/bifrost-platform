/**
 * Data husbandry catalog — Golden Source pipelines (Plan: Data Husbandry + Dagster).
 *
 * All Golden Source husbandry periodic ignition → Dagster Data Assets (multi-schedule).
 * Plugin workers stay executors (PG-as-broker). IB Gateway / realtime buses stay
 * Deployments — never assets.
 *
 * Success truth = table asof / freshness / coverage — never K8s Job Complete alone.
 * source_void ≠ fail; enqueue must fail-closed when credentials missing.
 */

export const DATA_HUSBANDRY_VERSION = '2026-08-30-w6'
export const DATA_HUSBANDRY_SOURCE =
  'console/src/lib/architecture/dataHusbandryCatalog.ts'

export type HusbandryLaneId = 'market_batch' | 'flex_batch' | 'research_olap'

/** Research Engine observe stack — nav lamp follows research_olap only. */
export type ResearchHealthLayerId = 'feedstock' | 'batch' | 'product_asof'

export type ResearchHealthLayer = {
  id: ResearchHealthLayerId
  label: string
  evidence: string
  navAffectsResearchIcon: boolean
}

export const RESEARCH_HEALTH_LAYERS: readonly ResearchHealthLayer[] = [
  {
    id: 'feedstock',
    label: 'Feedstock (Massive / Flex upstream)',
    evidence:
      'data-husbandry market_batch + flex_batch — void ≠ fail; shown on Research Engine page, not nav icon',
    navAffectsResearchIcon: false,
  },
  {
    id: 'batch',
    label: 'Batch compute (Dagster)',
    evidence:
      'GET /research/orchestration/status — research_trading_day + market_* / research_* schedules; SLA vs 22:30 ET for trading-day job',
    navAffectsResearchIcon: true,
  },
  {
    id: 'product_asof',
    label: 'Product asof (signal-health)',
    evidence:
      'GET /research/signal-health — features.* table freshness; research_olap Product half',
    navAffectsResearchIcon: true,
  },
] as const

export type HusbandryLane = {
  id: HusbandryLaneId
  label: string
  owner: string
  groundTruth: string
  schedulerTarget: 'dagster' | 'cron_until_migrated' | 'resident'
  mustNot: string
}

export const HUSBANDRY_LANES: readonly HusbandryLane[] = [
  {
    id: 'market_batch',
    label: 'Market batch (Massive / Polygon)',
    owner: 'plugin-market-data',
    groundTruth:
      'ops_jobs.ingest_freshness + queue-dashboard.husbandry + /market/readiness/summary (void ≠ fail)',
    schedulerTarget: 'dagster',
    mustNot: 'Treat CronJob Complete as healthy; rewrite Polygon workers into Dagster',
  },
  {
    id: 'flex_batch',
    label: 'IB Flex day-end',
    owner: 'plugin-flex-query',
    groundTruth:
      'ops_jobs.flex_ingest_freshness + /flex/coverage/* + config source=secret',
    schedulerTarget: 'dagster',
    mustNot: 'Enqueue without Flex tokens (fail-closed); call IB Flex HTTPS from Dagster',
  },
  {
    id: 'research_olap',
    label: 'Research OLAP (dbt → features)',
    owner: 'bifrost-research',
    groundTruth:
      'Product asof (signal-health) + Batch (orchestration/status) — Feedstock is separate lanes; nav lamp = research_olap only',
    schedulerTarget: 'dagster',
    mustNot:
      'Dual-write same features.* day via Cron and Dagster; put IB Client in the asset graph; paint Research nav red from Market missed / Flex source=none',
  },
] as const

/**
 * Scheduler topology — all husbandry Cron → Dagster (multi-schedule, UTC preserved
 * for Massive slots). Outside the graph: IB Gateway / Client / realtime WS only.
 */
export const HUSBANDRY_SCHEDULER_NOTE =
  'Scheduler center = Dagster multi-schedule (research_trading_day_schedule ET + market_* / research_* UTC schedules; each must be RUNNING in ops_dagster — DefaultScheduleStatus only applies on first insert). All Golden Source husbandry CronJobs suspended after migrate (Massive full SLOT_NAMES, Flex, Research day/short/agents/maintenance). Keep outside Dagster: IB Gateway / IB Client / realtime WS Deployments only. Plugin workers remain executors (HTTP enqueue). Verify: bifrost-research make verify-husbandry-schedulers.'

export const HUSBANDRY_RULES: string[] = [
  'All Golden Source husbandry periodic ignition = one problem class → Dagster Data Assets (multi-schedule; preserve cadence).',
  'Plugin workers + ops_jobs.* remain the executors; Dagster only schedules / gates / observes via HTTP enqueue.',
  'IB Gateway / IB Client / realtime WS buses = resident Deployments + health probes — never Dagster assets.',
  'K8s Job Complete ≠ husbandry success; ground truth is freshness / coverage / signal-health asof.',
  'source_void (vendor permanently empty) is gray, not red — void ≠ fail.',
  'Enqueue must fail-closed when credentials are missing (Flex source=none → exit 1).',
  'STG Trade DB clone freshness is not Golden Source husbandry authority.',
  'Research Engine health = three layers (feedstock / batch / product_asof); sidebar Research icon follows research_olap only (Product+Batch), never Market/Flex alone.',
  'Research Engine page OpsVerdictStrip = API reachable ∧ research_olap; Accuracy/Cost are downstream quality tabs, not husbandry verdict.',
  'D10 BLOCKED — husbandry never writes ib:operator:cmd or scales daemon.',
  'Migrate one slot → suspend its Cron before enabling the Dagster schedule (no dual-write).',
  HUSBANDRY_SCHEDULER_NOTE,
]

export function husbandryLaneById(id: HusbandryLaneId): HusbandryLane {
  const found = HUSBANDRY_LANES.find(l => l.id === id)
  if (found == null) throw new Error(`Unknown husbandry lane: ${id}`)
  return found
}

export function buildDataHusbandryLlmPack(): string {
  const lines = [
    '## Data husbandry (batch Golden Source)',
    `Source: ${DATA_HUSBANDRY_SOURCE} · ${DATA_HUSBANDRY_VERSION}`,
    '',
    '### Lanes',
    ...HUSBANDRY_LANES.map(
      l =>
        `- **${l.label}** [${l.id}]: owner=${l.owner} · scheduler=${l.schedulerTarget} · truth=${l.groundTruth} · must-not: ${l.mustNot}`,
    ),
    '',
    '### Research Engine health layers',
    ...RESEARCH_HEALTH_LAYERS.map(
      l =>
        `- **${l.label}** [${l.id}]: ${l.evidence} · navAffectsResearchIcon=${l.navAffectsResearchIcon}`,
    ),
    '',
    '### Rules',
    ...HUSBANDRY_RULES.map(r => `- ${r}`),
  ]
  return lines.join('\n')
}
