import type { ChecklistRow } from './types'

const rows: ChecklistRow[] = [
  // ── REST API — matching Polygon Stocks website menu order ──────
  {
    id: 'stock-tickers',
    service: 'Tickers',
    group: 'rest',
    description:
      'Four Polygon REST reference endpoints: All Tickers (universe search and discovery with market, type, exchange, search, and active filters — up to 1,000 results per page with cursor pagination), '
      + 'Ticker Overview (single-ticker fundamental data: SIC code, market cap, total employees, address, branding logo and icon URLs, CIK, composite FIGI, list date, ticker_root), '
      + 'Ticker Types (reference classification: returns all supported instrument type codes with descriptions, filterable by asset_class and locale), '
      + 'and Related Tickers (peer and competitor discovery via Massive news-coverage and returns-based similarity analysis).',
    tierMin: 'starter',
    projectStatus: 'implemented',
    verification:
      'Subcontractors → Market Data → Coverage (Stock) → Tickers: each sub-tab has Execute → JSON. '
      + 'Proxies: GET /market/tickers, GET /market/tickers/{ticker}, GET /market/tickers/types, GET /market/related-companies/{ticker}.',
    purpose:
      'Foundation layer for stock research: discover and filter the full Massive ticker universe, enrich any ticker with company '
      + 'fundamentals and branding assets, populate type filter dropdowns from the canonical Ticker Types reference, '
      + 'and identify thematically related peers for comparative analysis and portfolio construction.',
    helpVerification:
      'All Tickers: GET /v3/reference/tickers — optional params: ticker, type (use Ticker Types API for valid codes), market (stocks|crypto|fx|otc|indices), '
      + 'exchange (ISO 10383 MIC), cusip, cik, date (YYYY-MM-DD), search (name/ticker keyword), active (bool), limit (max 1000), sort, order. '
      + 'Response: results[].ticker, name, market, locale, primary_exchange, type, active, currency_name, composite_figi, cik, last_updated_utc. '
      + 'Ticker Overview: GET /v3/reference/tickers/{ticker} — path: ticker (required, case-sensitive, e.g. AAPL). Query: date (optional). '
      + 'Response: active, address (address1/city/state/postal_code), branding (logo_url, icon_url), cik, composite_figi, currency_name, description, '
      + 'homepage_url, list_date, locale, market, market_cap, name, phone_number, primary_exchange, round_lot, share_class_figi, '
      + 'share_class_shares_outstanding, sic_code, sic_description, ticker, ticker_root, ticker_suffix, total_employees, weighted_shares_outstanding. '
      + 'Ticker Types: GET /v3/reference/tickers/types — optional params: asset_class (stocks|options|crypto|fx|indices), locale (us|global). '
      + 'Response: results[].code (e.g. CS, ETF, ADRC, WARRANT), description, asset_class, locale. '
      + 'Related Tickers: GET /v1/related-companies/{ticker} — path: ticker (required). '
      + 'Response: ticker (query subject), results[].ticker (related symbols ranked by news/returns similarity analysis).',
  },
  {
    id: 'stock-aggregates',
    service: 'Aggregate Bars (OHLC)',
    group: 'rest',
    description:
      'Four Polygon REST aggregate endpoints for stocks: Custom Bars (OHLC), Daily Market Summary (OHLC), '
      + 'Daily Ticker Summary (OHLC), and Previous Day Bar (OHLC).',
    tierMin: 'starter',
    projectStatus: 'implemented',
    verification:
      'Subcontractors → Market Data → Coverage (Stock) → Aggregate Bars (OHLC): each sub-tab has Execute → JSON. '
      + 'Proxies: GET /market/stocks/bars/range, GET /market/stocks/bars/grouped-daily/{date}, '
      + 'GET /market/stocks/bars/open-close/{ticker}/{date}, GET /market/stocks/bars/prev/{ticker}.',
    purpose:
      'Custom Bars: charting, backtesting, and interval research. Daily Market Summary: one-shot EOD-style coverage for the full U.S. equity set. '
      + 'Daily Ticker Summary: single-name daily OHLC with extended-hours context. Previous Day Bar: prior session OHLC without calendar math.',
    helpVerification:
      'Custom Bars (OHLC): GET /v2/aggs/ticker/{ticker}/range/{multiplier}/{timespan}/{from}/{to} — proxy query: ticker, multiplier, timespan, start_ms, end_ms. '
      + 'Daily Market Summary (OHLC): GET /v2/aggs/grouped/locale/us/market/stocks/{date}. '
      + 'Daily Ticker Summary (OHLC): GET /v1/open-close/{ticker}/{date}. '
      + 'Previous Day Bar (OHLC): GET /v2/aggs/ticker/{ticker}/prev. '
      + 'PolygonClient: fetch_stock_aggs, fetch_stock_grouped_daily, fetch_stock_open_close, fetch_stock_previous_day.',
  },
  {
    id: 'stock-snapshots',
    service: 'Snapshots',
    group: 'rest',
    description:
      'Real-time snapshots: All Tickers (universe-wide), Single Ticker, and Gainers/Losers. '
      + '15-minute delay on Starter tier.',
    tierMin: 'starter',
    projectStatus: 'not-implemented',
    verification: 'N/A — not yet implemented.',
    purpose:
      'Quick current-state lookups for stocks. All Tickers snapshot enables screener inputs; Gainers/Losers for market movers.',
    helpVerification:
      'Not yet implemented. Target endpoints: GET /v2/snapshot/locale/us/markets/stocks/tickers, '
      + 'GET /v2/snapshot/locale/us/markets/stocks/tickers/{ticker}, '
      + 'GET /v2/snapshot/locale/us/markets/stocks/{direction}.',
  },
  {
    id: 'stock-trades-quotes',
    service: 'Trades & Quotes',
    group: 'rest',
    description:
      'Four REST endpoints: Historical Trades, Last Trade, Historical Quotes (NBBO), and Last Quote (NBBO).',
    tierMin: 'starter',
    projectStatus: 'not-implemented',
    verification: 'N/A — not yet implemented.',
    purpose:
      'Query tick-level trade and quote data for stocks. Useful for spread analysis, trade tape replay, and real-time lookups.',
    helpVerification:
      'Not yet implemented. Target endpoints: GET /v3/trades/{ticker}, GET /v2/last/trade/{ticker}, '
      + 'GET /v3/quotes/{ticker}, GET /v2/last/nbbo/{ticker}.',
  },
  {
    id: 'stock-corporate-actions',
    service: 'Corporate Actions',
    group: 'rest',
    description:
      'Dividends, splits, IPOs, and ticker events synced via Polygon REST into massive_corporate_action. '
      + 'Already implemented and shared with Massive Option.',
    tierMin: 'starter',
    projectStatus: 'implemented',
    verification:
      'Subcontractors → Market Data → Coverage (Option) → Corporate actions: enqueue sync for a ticker, then Load from DB.',
    purpose:
      'Sync stock dividends, splits, IPOs, and ticker lifecycle events from Massive Stocks REST and reference APIs into PostgreSQL.',
    helpVerification:
      'POST /market/sync with kind feed_stocks_corporate_action and payload { "symbol": "AAPL" }. '
      + 'Then GET /market/corporate-actions?symbol=AAPL&limit=50. '
      + 'UI: Massive Option → Corporate actions → Enqueue sync, then Load from DB.',
  },
  {
    id: 'stock-fundamentals',
    service: 'Fundamentals',
    group: 'rest',
    description:
      'Seven Polygon REST endpoints covering financial statements, ratios, and short data: '
      + 'Income Statements (revenue, net income, EPS — quarterly/annual/TTM), '
      + 'Balance Sheets (assets, liabilities, equity snapshot), '
      + 'Cash Flow Statements (OCF, CapEx, financing), '
      + 'Ratios (ROE, ROA, D/E, margins — computed from vX financials), '
      + 'Short Interest (shares short, days-to-cover), '
      + 'Short Volume (daily venue-level short volume + ratio), '
      + 'and Float (free float shares and percent).',
    tierMin: 'starter',
    projectStatus: 'implemented',
    verification:
      'Subcontractors → Market Data → Coverage (Stock) → Fundamentals: each sub-tab has Execute → JSON. '
      + 'Proxies: GET /market/stocks/fundamentals/income-statements, '
      + 'GET /market/stocks/fundamentals/balance-sheets, '
      + 'GET /market/stocks/fundamentals/cash-flow-statements, '
      + 'GET /market/stocks/fundamentals/ratios, '
      + 'GET /market/stocks/fundamentals/short-interest, '
      + 'GET /market/stocks/fundamentals/short-volume, '
      + 'GET /market/stocks/fundamentals/float.',
    purpose:
      'Pull structured financial statements, valuation ratios, and short-selling data from Massive to support '
      + 'fundamental screening, short squeeze monitoring, and float-adjusted analysis.',
    helpVerification:
      'Income Statements / Balance Sheets / Cash Flow: backed by GET /vX/reference/financials (starter tier). '
      + 'Ratios: computed from vX financials data (ROE, ROA, D/E, current ratio, margins, EPS). '
      + 'Short Interest: GET /stocks/v1/short-interest. '
      + 'Short Volume: GET /stocks/v1/short-volume. '
      + 'Float: GET /stocks/vX/float. '
      + 'All require ticker param; financials also accept timeframe (quarterly|annual|trailing_twelve_months), '
      + 'fiscal_year, fiscal_quarter, period_end, filing_date.',
  },
  {
    id: 'stock-filings',
    service: 'Filings & Disclosures',
    group: 'rest',
    description:
      'Eight Polygon REST endpoints covering SEC filings and insider ownership: '
      + 'Edgar Index (EDGAR filing discovery across all form types with powerful filtering), '
      + '10-K Sections (plain-text section extraction from annual reports), '
      + '8-K Text (parsed Items text from current event reports), '
      + '13-F Filings (institutional holdings from Form 13-F), '
      + 'Risk Factors (standardized risk factor disclosures with taxonomy), '
      + 'Risk Categories (hierarchical risk factor taxonomy reference), '
      + 'Form 3 (initial insider ownership statements), '
      + 'and Form 4 (changes in insider ownership).',
    tierMin: 'starter',
    projectStatus: 'implemented',
    verification:
      'Subcontractors → Market Data → Coverage (Stock) → Filings & Disclosures: each sub-tab has Execute → JSON. '
      + 'Proxies: GET /market/stocks/filings/edgar-index, '
      + 'GET /market/stocks/filings/10k-sections, '
      + 'GET /market/stocks/filings/8k-text, '
      + 'GET /market/stocks/filings/13f, '
      + 'GET /market/stocks/filings/risk-factors, '
      + 'GET /market/stocks/filings/risk-categories, '
      + 'GET /market/stocks/filings/form-3, '
      + 'GET /market/stocks/filings/form-4.',
    purpose:
      'Comprehensive SEC filing access: discover filings via EDGAR Index, extract 10-K section text for NLP, '
      + 'stream 8-K event text for real-time alerts, analyze institutional positioning via 13-F, '
      + 'monitor standardized risk disclosures across companies, and track insider buying/selling via Form 3 & 4.',
    helpVerification:
      'Edgar Index: GET /stocks/filings/vX/index — params: ticker, cik, form_type, filing_date (gt/gte/lt/lte), limit (max 50000). '
      + 'Response: results[].accession_number, ticker, issuer_name, form_type, filing_date, filing_url. '
      + '10-K Sections: GET /stocks/filings/10-K/vX/sections — params: ticker, cik, section, filing_date, period_end, limit (max 99). '
      + 'Response: results[].cik, ticker, filing_date, period_end, section, text, filing_url. '
      + '8-K Text: GET /stocks/filings/8-K/vX/text — params: ticker, cik, form_type, filing_date, limit (max 99). '
      + 'Response: results[].accession_number, ticker, filing_date, items_text. '
      + '13-F Filings: GET /stocks/filings/vX/13-F — params: filer_cik, filing_date (gt/gte/lt/lte), limit (max 1000). '
      + 'Response: results[].accession_number, filer_cik, filing_date, issuer_name, market_value, shares_or_principal_amount, period. '
      + 'Risk Factors: GET /stocks/filings/vX/risk-factors — params: ticker, cik, filing_date, limit (max 49999). '
      + 'Response: results[].cik, ticker, filing_date, primary_category, secondary_category, tertiary_category, supporting_text. '
      + 'Risk Categories: GET /stocks/taxonomies/vX/risk-factors — params: taxonomy, primary/secondary/tertiary_category, limit (max 999). '
      + 'Response: results[].description, primary_category, secondary_category, tertiary_category, taxonomy. '
      + 'Form 3: GET /stocks/filings/vX/form-3 — params: issuer_cik, owner_cik, tickers, form_type, filing_date, limit (max 10000). '
      + 'Response: results[].accession_number, filing_date, issuer_name, owner_name, security_title, shares_owned, is_director/is_officer. '
      + 'Form 4: GET /stocks/filings/vX/form-4 — params: issuer_cik, owner_cik, tickers, transaction_code, filing_date, limit (max 10000). '
      + 'Response: results[].accession_number, filing_date, issuer_name, owner_name, transaction_shares, transaction_price_per_share, transaction_value.',
  },
  {
    id: 'stock-news',
    service: 'News',
    group: 'rest',
    description:
      'Market news articles for stocks via Polygon REST news endpoint, filterable by ticker, publisher, and date range.',
    tierMin: 'starter',
    projectStatus: 'implemented',
    verification:
      'Subcontractors → Market Data → Coverage (Stock) → News: set query filters and Execute. '
      + 'Proxy: GET /market/stocks/news.',
    purpose:
      'Fetch and display news articles relevant to a stock ticker from Massive. Supports event correlation and sentiment research.',
    helpVerification:
      'GET /v2/reference/news — supports ticker, published_utc.gte, published_utc.lte, limit, sort, order.',
  },
  // ── WebSocket ──────────────────────────────────────────────────────────────
  {
    id: 'stock-ws-aggregates-s',
    service: 'Aggregates (Per Second)',
    group: 'ws',
    description: 'WebSocket per-second aggregate bars for stocks channel A.{ticker}.',
    tierMin: 'starter',
    projectStatus: 'not-implemented',
    verification: 'N/A — not yet implemented.',
    purpose: 'Stream second-by-second OHLCV bars for a stock ticker in real time.',
    helpVerification:
      'Not yet implemented. Target: wss://socket.polygon.io/stocks, subscribe to A.{ticker}.',
  },
  {
    id: 'stock-ws-aggregates-m',
    service: 'Aggregates (Per Minute)',
    group: 'ws',
    description: 'WebSocket per-minute aggregate bars for stocks channel AM.{ticker}.',
    tierMin: 'starter',
    projectStatus: 'not-implemented',
    verification: 'N/A — not yet implemented.',
    purpose: 'Stream minute-by-minute OHLCV bars for a stock ticker in real time.',
    helpVerification:
      'Not yet implemented. Target: wss://socket.polygon.io/stocks, subscribe to AM.{ticker}.',
  },
  {
    id: 'stock-ws-trades',
    service: 'Trades',
    group: 'ws',
    description: 'WebSocket tick-level trades for stocks channel T.{ticker}.',
    tierMin: 'starter',
    projectStatus: 'not-implemented',
    verification: 'N/A — not yet implemented.',
    purpose: 'Stream tick-by-tick trade prints for a stock ticker.',
    helpVerification:
      'Not yet implemented. Target: wss://socket.polygon.io/stocks, subscribe to T.{ticker}.',
  },
  {
    id: 'stock-ws-quotes',
    service: 'Quotes',
    group: 'ws',
    description: 'WebSocket real-time NBBO quotes for stocks channel Q.{ticker}.',
    tierMin: 'starter',
    projectStatus: 'not-implemented',
    verification: 'N/A — not yet implemented.',
    purpose: 'Stream top-of-book bid/ask updates for a stock ticker.',
    helpVerification:
      'Not yet implemented. Target: wss://socket.polygon.io/stocks, subscribe to Q.{ticker}.',
  },
  // ── Flat Files ─────────────────────────────────────────────────────────────
  {
    id: 'stock-flat-file-day-aggs',
    service: 'Day aggregates',
    group: 'flat',
    description: 'Daily OHLCV across all US stocks as downloadable S3 flat file.',
    tierMin: 'starter',
    projectStatus: 'not-implemented',
    verification: 'N/A — bulk download not yet integrated.',
    purpose: 'Bulk download of daily stock aggregates from S3.',
    helpVerification: 'Not yet available. See Massive documentation for S3 flat file access.',
  },
  {
    id: 'stock-flat-file-minute-aggs',
    service: 'Minute aggregates',
    group: 'flat',
    description: 'Minute-level OHLCV across all US stocks as downloadable S3 flat file.',
    tierMin: 'starter',
    projectStatus: 'not-implemented',
    verification: 'N/A — bulk download not yet integrated.',
    purpose: 'Bulk download of minute-level stock aggregates from S3.',
    helpVerification: 'Not yet available. See Massive documentation for S3 flat file access.',
  },
  {
    id: 'stock-flat-file-trades',
    service: 'Trades',
    group: 'flat',
    description: 'Tick-level stock trades with nanosecond timestamps as downloadable S3 flat file. Requires Developer tier.',
    tierMin: 'developer',
    projectStatus: 'not-implemented',
    verification: 'N/A — bulk download not yet integrated.',
    purpose: 'Bulk download of tick-level stock trades from S3.',
    helpVerification: 'Not yet available. Requires Developer tier. See Massive documentation for S3 flat file access.',
  },
  {
    id: 'stock-flat-file-quotes',
    service: 'Quotes',
    group: 'flat',
    description: 'Top-of-book stock quotes with nanosecond timestamps as downloadable S3 flat file.',
    tierMin: 'starter',
    projectStatus: 'not-implemented',
    verification: 'N/A — bulk download not yet integrated.',
    purpose: 'Bulk download of top-of-book stock quotes from S3.',
    helpVerification: 'Not yet available. See Massive documentation for S3 flat file access.',
  },
]

export const STOCK_CHECKLIST_ROWS = rows
