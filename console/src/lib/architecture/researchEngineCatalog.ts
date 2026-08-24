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
    'features_daily.*',
    'features_option.*',
    'features_signals.*',
    'features_forecasts.*',
    'features_backtests.*',
  ] as const,
  engines: [
    'dbt SEPA',
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
  runtimeIgnition: 'DONE 2026-08-21 — research NS live; CronJobs dbt/volatility/engines/intraday; Dagster replicas:0',
} as const

export const RESEARCH_GOVERNANCE_SURFACES = [
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
    title: 'Pipeline health',
    api: 'GET /health · GET /api/v1/research/status · CronJob lastScheduleTime (research NS) · pipeline schema row freshness',
    metrics: [
      'reachable',
      'CronJob last successful Job',
      'max(trade_date)/max(asof_ts) on features_daily.* / features_option.* / features_signals.* / features_forecasts.* / features_backtests.*',
      'elementary present/mtime when PVC report exists',
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
  lines.push('- Token cost KPIs are placeholders until forecast_session persists token metadata')
  lines.push(
    '- Runtime Ignition 2026-08-21 DONE: research-api + engine CronJobs live; pipeline freshness from CronJob Jobs + table max(trade_date)/asof_ts',
  )
  lines.push('- Dagster orchestration stays replicas:0 (production blockers unchanged); CronJobs are the live scheduler')
  lines.push('- Accuracy aggregates computed in Console from settlement rows (proxy is GET-only)')
  lines.push('- Follow-ons: research-radar-news-source · plugin-options-tape (Operate Queue)')
  return lines.join('\n')
}
