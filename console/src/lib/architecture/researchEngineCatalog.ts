/**
 * Research Engine (OLAP) — architecture contract for Ops Console LLM pack.
 * D10 BLOCKED — governance / observe only; no trade actuation.
 */

export const RESEARCH_ENGINE_ID = 'research-engine'

export const RESEARCH_ENGINE_SUMMARY = {
  id: RESEARCH_ENGINE_ID,
  name: 'Research Engine',
  vendor: 'bifrost-research',
  namespace: 'research',
  apiPort: 8795,
  domain: 'OLAP',
  goldenSource: 'bifrost_golden_source',
  schemas: [
    'dw_stock.*',
    'features.*',
  ] as const,
  engines: [
    'dbt SEPA (canonical read: dw_stock.mart_sepa_*; model write: features.stock_signal_sepa_daily via mart_sepa_feature_daily)',
    'volatility (IV / PCR / Max Pain)',
    'momentum',
    'GEX',
    'IV surface',
    'order flow',
    'market terrain',
    'AI forecast',
    'event radar',
    'backtest / settlement',
  ] as const,
  platformStatus: 'GET /api/v1/research/status → reachable + freshness (Runtime Ignition 2026-08-21)',
  platformProxy: 'GET /api/v1/research/*',
  consoleTab: 'research-engine',
  runtimeIgnition:
    'DONE — research NS live; Dagster multi-schedule husbandry (replicas≥1); NodePort :30301 via Ops Tool Rack; all Golden Source husbandry CronJobs suspended after migrate',
  /** Former Plugin → Analytics page. Hash `#analytics-pipeline` redirects here (dbt / Lineage tab). */
  retiredPluginTab: 'analytics-pipeline',
  elementaryReportProxy: '/api/v1/research/analytics/elementary/files/elementary_report.html',
  elementaryStatus: 'GET /api/v1/research/analytics/elementary',
  /** Legacy CronJob names — suspended; ignition is Dagster schedule/asset (same logical trigger). */
  cronjobTriggers: [
    { trigger_id: 'dbt-sepa', cronjob: 'bifrost-analytics-daily', empty_hint: 'SEPA empty', scheduler: 'dagster' },
    { trigger_id: 'momentum', cronjob: 'research-engines-momentum', empty_hint: 'Momentum empty', scheduler: 'dagster' },
    { trigger_id: 'iv-percentile', cronjob: 'research-iv-percentile', empty_hint: 'IV empty', scheduler: 'dagster' },
    { trigger_id: 'terrain-forecast', cronjob: 'research-engines-forecast', empty_hint: 'Forecast / Terrain empty', scheduler: 'dagster' },
    { trigger_id: 'terrain-intraday', cronjob: 'research-terrain-intraday', empty_hint: 'Intraday empty', scheduler: 'dagster' },
    { trigger_id: 'gex-intraday', cronjob: 'research-gex-intraday', empty_hint: 'GEX empty', scheduler: 'dagster' },
    { trigger_id: 'event-radar', cronjob: 'research-engines-event-radar', empty_hint: 'Events empty', scheduler: 'dagster' },
  ] as const,
} as const

export const RESEARCH_GOVERNANCE_SURFACES = [
  {
    id: 'feedstock',
    title: 'Feedstock (upstream)',
    api: 'GET /api/v1/data-husbandry → market_batch + flex_batch; readiness_rollup via market-data status',
    metrics: [
      'market_batch.verdict',
      'flex_batch.verdict',
      'void ≠ fail',
      'readiness_rollup (snap covered/universe · vendor_gap)',
      'deep-link Massive Coverage → Readiness',
    ],
  },
  {
    id: 'batch',
    title: 'Batch compute (Dagster)',
    api: 'GET /research/orchestration/status (ops_dagster; fail-soft)',
    metrics: ['verdict', 'last_run_status', 'overdue', 'research_trading_day'],
  },
  {
    id: 'product-asof',
    title: 'Product asof',
    api: 'GET /research/signal-health',
    metrics: [
      'overall',
      'freshness[].status',
      'age_hours',
      'max_computed_at',
      'age_meter vs 36h SLA',
      'StackedBar fresh/stale mix',
    ],
  },
  {
    id: 'accuracy',
    title: 'Forecast accuracy',
    api: 'GET /research/backtest/settlement',
    metrics: ['path_hit', 'close_miss_pct'],
  },
  {
    id: 'token-cost',
    title: 'Token / AI cost',
    api: 'GET /research/forecast/sessions (llm_provider; token fields when persisted)',
    metrics: ['llm_provider', 'token_usage', 'token_cost_usd'],
  },
  {
    id: 'pipeline-health',
    title: 'Pipeline health (three-layer body)',
    api: 'Feedstock + Batch + Product asof on Research Engine Pipeline health tab; OpsVerdictStrip = API ∧ research_olap',
    metrics: [
      'reachable',
      'research_olap husbandry rollup',
      'orchestration last run',
      'signal-health freshness rows',
    ],
  },
  {
    id: 'run-management',
    title: 'Run management',
    api: 'GET /research/forecast/sessions',
    metrics: ['recent forecast sessions'],
  },
  {
    id: 'dbt-catalog',
    title: 'dbt / Lineage (absorbed from retired Plugin → Analytics)',
    api: 'Catalog is static contract; Elementary HTML via Research API /analytics/elementary/files/*',
    metrics: [
      '21 dbt models (staging / intermediate / marts)',
      'lineage RAW market.* → STG → INT → MART dw_stock.mart_sepa_*',
      'Elementary report open (not CronJob health)',
    ],
  },
] as const

export const RESEARCH_DBT_MODELS: { layer: 'staging' | 'intermediate' | 'marts'; name: string; note: string }[] = [
  { layer: 'staging', name: 'stg_income_stmt', note: 'Polygon income jsonb → columns' },
  { layer: 'staging', name: 'stg_balance_sheet', note: 'Balance sheet extract' },
  { layer: 'staging', name: 'stg_cash_flow', note: 'Cash flow extract' },
  { layer: 'staging', name: 'stg_ratios', note: 'Placeholder (vendor gap)' },
  { layer: 'staging', name: 'stg_short_interest', note: 'Short interest' },
  { layer: 'staging', name: 'stg_short_volume', note: 'Short volume' },
  { layer: 'intermediate', name: 'dim_universe', note: 'CS equity universe' },
  { layer: 'intermediate', name: 'dim_trading_calendar', note: 'US holidays' },
  { layer: 'intermediate', name: 'int_stock_daily_enriched', note: 'SMA / ATR / ROC (incremental)' },
  { layer: 'intermediate', name: 'int_stock_crs', note: '252d CRS (needs depth)' },
  { layer: 'intermediate', name: 'int_financials_yoy', note: 'YoY growth' },
  { layer: 'marts', name: 'mart_sepa_fundamental_eval', note: '8 core fund conditions' },
  { layer: 'marts', name: 'mart_sepa_fundamental_ext', note: '25 extended fund conditions' },
  { layer: 'marts', name: 'mart_sepa_technical_eval', note: '11 tech conditions' },
  { layer: 'marts', name: 'mart_sepa_tier_momentum', note: 'Tier 2 momentum' },
  { layer: 'marts', name: 'mart_sepa_tier_structure', note: 'Tier 3 structure' },
  { layer: 'marts', name: 'mart_sepa_tier_sentiment', note: 'Tier 4 sentiment' },
  { layer: 'marts', name: 'mart_sepa_composite_score', note: 'Weighted composite' },
  { layer: 'marts', name: 'mart_sepa_screening_ranked', note: 'Rank / decile' },
  { layer: 'marts', name: 'mart_sepa_criteria_stats', note: 'Pre-agg pass rates' },
  { layer: 'marts', name: 'mart_sepa_screener_wide', note: 'Wide screener join' },
]

export const RESEARCH_DBT_LINEAGE: { id: string; title: string; detail: string }[] = [
  {
    id: 'raw',
    title: 'RAW · raw_market.*',
    detail: 'Golden Source producer tables (stock_daily, stock_financials, ticker, holidays)',
  },
  {
    id: 'stg',
    title: 'STG · staging',
    detail: 'jsonb → scalar columns (income / balance / cash flow / short interest)',
  },
  {
    id: 'int',
    title: 'INT · intermediate',
    detail: 'Universe, calendar, enriched bars, CRS, YoY financials',
  },
  {
    id: 'mart',
    title: 'MART · dw_stock.mart_sepa_*',
    detail: 'Fundamental / technical / tiers → composite → screener wide',
  },
]

export const RESEARCH_DBT_MODEL_COUNT = RESEARCH_DBT_MODELS.length

export function buildResearchEngineLlmPack(): string {
  const lines = [
    '# Research Engine (OLAP governance)',
    '',
    `- Repo: ${RESEARCH_ENGINE_SUMMARY.vendor} · NS ${RESEARCH_ENGINE_SUMMARY.namespace} · API :${RESEARCH_ENGINE_SUMMARY.apiPort}`,
    `- Golden Source: ${RESEARCH_ENGINE_SUMMARY.goldenSource} (${RESEARCH_ENGINE_SUMMARY.schemas.join(', ')})`,
    `- Platform: ${RESEARCH_ENGINE_SUMMARY.platformStatus} · ${RESEARCH_ENGINE_SUMMARY.platformProxy}`,
    `- Console: Satellite → Research Engine (tab ${RESEARCH_ENGINE_SUMMARY.consoleTab})`,
    `- Decision: D13 Research domain · D10 BLOCKED (no trade actuation)`,
    `- Runtime: ${RESEARCH_ENGINE_SUMMARY.runtimeIgnition}`,
    `- Retired: Plugin → Analytics (\`${RESEARCH_ENGINE_SUMMARY.retiredPluginTab}\`) — hash redirects here; dbt catalog is the dbt / Lineage tab`,
    '',
    '## Engines',
  ]
  for (const e of RESEARCH_ENGINE_SUMMARY.engines) {
    lines.push(`- ${e}`)
  }
  lines.push('')
  lines.push('## Governance surfaces')
  for (const s of RESEARCH_GOVERNANCE_SURFACES) {
    lines.push(`### ${s.title}`)
    lines.push(`- API: ${s.api}`)
    lines.push(`- Metrics: ${s.metrics.join(', ')}`)
  }
  lines.push('')
  lines.push('## Notes')
  lines.push(
    '- Health layers: feedstock (Market/Flex) · batch (Dagster orchestration) · product_asof (signal-health)',
  )
  lines.push(
    '- Always-on three lamps under OpsVerdictStrip; default Console tab = Pipeline health; human copy never dumps SQL',
  )
  lines.push(
    '- Feedstock → Massive Readiness deep-link + readiness_rollup one-liner; Product age meters (36h SLA FillBar, not chart library)',
  )
  lines.push(
    '- Sidebar Research Engine icon tint follows research_olap human copy (no trailing StatusLamp — icon color is the signal). Market missed / Flex source=none do not paint it red',
  )
  lines.push('- OpsVerdictStrip = API reachable ∧ research_olap; Accuracy/Cost are downstream quality tabs')
  lines.push('- Token cost KPIs are placeholders until forecast_session persists token metadata')
  lines.push('- Accuracy aggregates computed in Console from settlement rows (proxy is GET-only)')
  lines.push(
    `- dbt catalog: ${RESEARCH_DBT_MODEL_COUNT} models on dw_stock.*; CronJob bifrost-analytics-daily is NOT a health signal (Dagster dbt-sepa)`,
  )
  lines.push(
    `- Elementary HTML: ${RESEARCH_ENGINE_SUMMARY.elementaryReportProxy} · status ${RESEARCH_ENGINE_SUMMARY.elementaryStatus}`,
    '- Retired Plugin → Analytics HTTP: /plugins/analytics/* → 308 /research/analytics/elementary*',
  )
  lines.push(
    '- Research Harness observe surface (Wave A+O, package ≥0.47.0): Trade FE `/research/loop/harness` — propose-only objectives/runs; D10 BLOCKED (no ib:operator:cmd)',
  )
  lines.push('- Follow-ons: research-radar-news-source · plugin-options-tape (Operate Queue)')
  return lines.join('\n')
}
