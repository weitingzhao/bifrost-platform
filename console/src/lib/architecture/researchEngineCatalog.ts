/**
 * Research Engine (OLAP): the names the Ops Console page shows.
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
  platformProxy: 'GET /api/v1/research/*',
} as const

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
