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
] as const

export function buildResearchEngineLlmPack(): string {
  const lines = [
    '# Research Engine (OLAP governance)',
    '',
    `- Repo: ${RESEARCH_ENGINE_SUMMARY.vendor} · NS ${RESEARCH_ENGINE_SUMMARY.namespace} · API :${RESEARCH_ENGINE_SUMMARY.apiPort}`,
    `- Golden Source: ${RESEARCH_ENGINE_SUMMARY.goldenSource} (${RESEARCH_ENGINE_SUMMARY.schemas.join(', ')})`,
    `- Platform: ${RESEARCH_ENGINE_SUMMARY.platformStatus} · ${RESEARCH_ENGINE_SUMMARY.platformProxy}`,
    `- Console: Plugin → Research (tab ${RESEARCH_ENGINE_SUMMARY.consoleTab})`,
    `- Decision: D13 Research domain · D10 BLOCKED (no trade actuation)`,
    `- Runtime: ${RESEARCH_ENGINE_SUMMARY.runtimeIgnition}`,
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
    '- Sidebar Research Engine icon follows research_olap only — Market missed / Flex source=none do not paint it red',
  )
  lines.push('- OpsVerdictStrip = API reachable ∧ research_olap; Accuracy/Cost are downstream quality tabs')
  lines.push('- Token cost KPIs are placeholders until forecast_session persists token metadata')
  lines.push('- Accuracy aggregates computed in Console from settlement rows (proxy is GET-only)')
  lines.push(
    '- Research Harness observe surface (Wave A+O, package ≥0.47.0): Trade FE `/research/loop/harness` — propose-only objectives/runs; D10 BLOCKED (no ib:operator:cmd)',
  )
  lines.push('- Follow-ons: research-radar-news-source · plugin-options-tape (Operate Queue)')
  return lines.join('\n')
}
