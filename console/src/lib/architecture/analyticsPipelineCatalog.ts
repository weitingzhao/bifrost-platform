/**
 * Analytics Pipeline (dbt + Elementary) — architecture contract for Ops Console LLM pack.
 */

export const ANALYTICS_PIPELINE_ID = 'analytics-pipeline'

export const ANALYTICS_PIPELINE_SUMMARY = {
  id: ANALYTICS_PIPELINE_ID,
  name: 'Analytics Pipeline',
  vendor: 'dbt-core + Elementary OSS',
  namespace: 'plugin-market-data',
  schedule: '0 3 * * 1-5 America/New_York',
  cronJob: 'bifrost-analytics-daily',
  docsService: 'analytics-docs:8061',
  outputSchema: 'analytics',
  elementarySchema: 'analytics_elementary',
  modelsTotal: 21,
  layers: ['staging', 'intermediate', 'marts'] as const,
}

export const ANALYTICS_MODEL_LAYERS = {
  staging: [
    'stg_income_stmt',
    'stg_balance_sheet',
    'stg_cash_flow',
    'stg_ratios',
    'stg_short_interest',
    'stg_short_volume',
  ],
  intermediate: [
    'dim_universe',
    'dim_trading_calendar',
    'int_stock_daily_enriched',
    'int_stock_crs',
    'int_financials_yoy',
  ],
  marts: [
    'sepa_fundamental_eval',
    'sepa_fundamental_ext',
    'sepa_technical_eval',
    'sepa_tier_momentum',
    'sepa_tier_structure',
    'sepa_tier_sentiment',
    'sepa_composite_score',
    'sepa_screening_ranked',
    'sepa_criteria_stats',
    'sepa_screener_wide',
  ],
} as const

export function buildAnalyticsPipelineLlmPack(): string {
  const lines = [
    '# Analytics Pipeline (dbt + Elementary)',
    '',
    `- Namespace: ${ANALYTICS_PIPELINE_SUMMARY.namespace}`,
    `- CronJob: ${ANALYTICS_PIPELINE_SUMMARY.cronJob} @ ${ANALYTICS_PIPELINE_SUMMARY.schedule}`,
    `- Output: Golden Source schemas \`${ANALYTICS_PIPELINE_SUMMARY.outputSchema}.*\` + \`${ANALYTICS_PIPELINE_SUMMARY.elementarySchema}.*\``,
    `- Docs: ${ANALYTICS_PIPELINE_SUMMARY.docsService} (Elementary HTML report)`,
    `- Platform API: GET /api/v1/plugins/analytics/status · GET /api/v1/plugins/analytics/api/*`,
    `- Console: Plugin → Analytics (tab analytics-pipeline)`,
    '',
    '## Layers',
  ]
  for (const [layer, models] of Object.entries(ANALYTICS_MODEL_LAYERS)) {
    lines.push(`### ${layer}`)
    for (const m of models) lines.push(`- ${m}`)
  }
  lines.push('')
  lines.push('## Notes')
  lines.push('- Trade API consumes analytics via SEPA_USE_ANALYTICS=true')
  lines.push('- Technical/CRS marts need ≥252 trading days of market.stock_daily')
  lines.push('- Elementary edr report regenerated after each CronJob')
  return lines.join('\n')
}
