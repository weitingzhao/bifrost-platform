import type { ChecklistRow } from './types'

const rows: ChecklistRow[] = [
  // ── REST API (6 Sections, matching coverage CSV order) ──
  {
    id: 'contracts',
    service: 'Contracts',
    group: 'rest',
    description:
      'Massive Options REST: All Contracts (index) and Contract Overview (single contract), plus local DB coverage and contract-level snapshot linking.',
    tierMin: 'starter',
    projectStatus: 'implemented',
    verification:
      'Contracts section: All Contracts (GET /v3/reference/options/contracts via Celery), Contract Overview (GET /v3/reference/options/contracts/{options_ticker}), DB Verify (option_contracts coverage & mapping), Snapshot Link (contract → snapshot end-to-end).',
    purpose:
      'Discover, inspect, and verify option contract reference data from Massive. Coverage and mapping metrics quantify local data quality against the API source of truth.',
    helpVerification:
      '1) All Contracts tab — maps to All Contracts: underlying_ticker, contract_type, expiration_date (YYYY-MM-DD), limit (API default 10, max 1000; UI caps limit). Enqueue feed_option_contracts list job; expect results[].ticker, contract_type, exercise_style, expiration_date, strike_price, etc. '
      + '2) Contract Overview tab — maps to Contract Overview: path parameter options_ticker (deprecated list query param ticker per Massive docs). Enqueue feed_option_contracts mode=detail; expect results object with same contract fields. '
      + '3) DB Verify tab: Check Coverage → GET /market/contracts-coverage (local PG vs API workflow). '
      + '4) Snapshot Link tab: contract-level snapshot job for end-to-end verification.',
  },
  {
    id: 'feed_options_aggregate',
    service: 'Aggregate Bars (OHLC)',
    group: 'rest',
    description:
      'Three Polygon REST DocPage rows under Aggregate Bars (OHLC): Custom Bars (OHLC), Daily Ticker Summary (OHLC), and Previous Day Bar (OHLC). WebSocket aggregate streams are listed separately under WebSocket.',
    tierMin: 'starter',
    projectStatus: 'implemented',
    verification:
      'REST: Enqueue feed_options_aggregate with mode custom_bars | open_close | prev → check Job queue result.',
    purpose:
      'Custom Bars (OHLC): per-contract OHLCV backfills for charting and backtesting. Daily Ticker Summary (OHLC): single-day OHLC with pre/after-hours context. Previous Day Bar (OHLC): prior session OHLC without calendar math.',
    helpVerification:
      'Custom Bars (OHLC): Enqueue feed_options_aggregate (mode custom_bars or omit mode) with options_ticker, symbol, expiry, strike, right, start_ms, end_ms. '
      + 'Daily Ticker Summary (OHLC): mode open_close with options_ticker + date (YYYY-MM-DD). '
      + 'Previous Day Bar (OHLC): mode prev with options_ticker.',
  },
  {
    id: 'feed_option_snapshots',
    service: 'Snapshots',
    group: 'rest',
    description:
      'Three Polygon REST DocPage rows under Snapshots (UI tab order): Option Contract Snapshot (per-contract), Option Chain Snapshot (chain → option_snapshots), Unified Snapshot (cross-asset).',
    tierMin: 'starter',
    projectStatus: 'implemented',
    verification:
      'Enqueue feed_option_snapshots (Massive option chain/contract/unified) → GET /research/option-snapshots or Verify section below',
    purpose: 'Pull delayed chain or contract data from Polygon REST; chain persists bid/ask/last and greeks when returned.',
    helpVerification:
      'Option Chain Snapshot: Celery kind feed_option_snapshots, mode=chain for an underlying. Option Contract Snapshot: mode=contract + option_contract. Unified Snapshot: mode=unified + tickers. '
      + 'Then GET /research/option-snapshots?symbol=&expiration=&source=massive for chain persistence checks.',
  },
  {
    id: 'trades-quotes',
    service: 'Trade & Quotes',
    group: 'rest',
    description:
      'REST (coverage sheet): Last Trade, Quotes, Trades — three DocPage rows. Optional Flat Files pointers: Quotes and Trades bulk downloads (not REST; same section for convenience). '
      + 'Starter: Last Trade and Quotes; Trades requires Developer tier and trades_enabled.',
    tierMin: 'starter',
    projectStatus: 'implemented',
    verification:
      'Trade & Quotes section: REST tabs in order Trades / Last Trade / Quotes; then Flat Files tabs. Trades REST tab shows tier gate when trades_enabled is off.',
    purpose:
      'Query and verify option trade and quote data from Polygon REST. Last Trade: most recent fill; Quotes: BBO history; Trades: tick-level fills.',
    helpVerification:
      '1) Last Trade (DocPage): enter an options ticker, click Fetch. '
      + '2) Quotes (DocPage): optional timestamp range, click Fetch. '
      + '3) Trades (DocPage): optional timestamp range; requires Developer tier. '
      + 'Flat Files tabs describe S3 bulk downloads (informational).',
  },
  {
    id: 'technical-indicators',
    service: 'Technical Indicators',
    group: 'rest',
    description:
      'Four Polygon REST DocPage rows (coverage sheet order): Simple Moving Average (SMA), Exponential Moving Average (EMA), Moving Average Convergence/Divergence (MACD), Relative Strength Index (RSI). '
      + 'Read-only; option tickers (O:) or equities.',
    tierMin: 'starter',
    projectStatus: 'implemented',
    verification:
      'Technical Indicators section: DocPage-named tabs. Each: ticker + parameters, Fetch, data table.',
    purpose:
      'Compute indicators per Polygon REST for trend, momentum, and signal analysis on option or stock tickers.',
    helpVerification:
      'Simple Moving Average (SMA): window/timespan. Exponential Moving Average (EMA): same. '
      + 'Moving Average Convergence/Divergence (MACD): short/long/signal windows. '
      + 'Relative Strength Index (RSI): 0–100 scale.',
  },
  {
    id: 'market-ops',
    service: 'Market Operations',
    group: 'rest',
    description:
      'Four Polygon REST DocPage rows (UI tab order): Exchanges, Market Holidays, Market Status, Condition Codes — venues, holiday calendar, live status, trade/quote conditions.',
    tierMin: 'starter',
    projectStatus: 'implemented',
    verification:
      'Market Operations section: tab order Exchanges → Market Holidays → Market Status → Condition Codes (each maps to one DocPage row).',
    purpose:
      'Lookup cross-asset reference data from Massive. Market Holidays includes comparison with local reference_us_holidays.',
    helpVerification:
      '1) Exchanges: asset class + locale filters. '
      + '2) Market Holidays: Fetch & Compare vs local table. '
      + '3) Market Status: real-time cards per market. '
      + '4) Condition Codes: asset class + data type filters.',
  },
  // ── Project (derived workflows, not in REST Options taxonomy) ──
  {
    id: 'reference',
    service: 'Reference / contracts',
    group: 'project',
    description: 'Expirations and strikes for an underlying',
    tierMin: 'starter',
    projectStatus: 'implemented',
    verification: 'GET /research/option-expirations?provider=massive or Research → Option Discovery',
    purpose: 'Resolve option expirations and strikes for an underlying before building chains or screens.',
    helpVerification:
      'Call GET /research/option-expirations?symbol=SYMBOL&provider=massive. In the app, use Research → Option Discovery which uses the same API when Massive is configured.',
  },
  {
    id: 'daily-oi',
    service: 'Daily open interest',
    group: 'project',
    description: 'Historical daily OI backfill into option_open_interest_daily',
    tierMin: 'starter',
    projectStatus: 'partial',
    verification: 'GET /research/option-oi — worker OI placeholder only',
    purpose: 'Track daily open interest per contract; full Massive OI backfill may be added later.',
    helpVerification:
      'GET /research/option-oi?symbol=… returns rows from option_open_interest_daily when populated. The OI enqueue job is a placeholder; chain snapshot may include OI when available.',
  },
  {
    id: 'corporate-actions',
    service: 'Corporate actions',
    group: 'project',
    description:
      'Dividends, splits, IPOs, and ticker events synced via Polygon REST into massive_corporate_action.',
    tierMin: 'starter',
    projectStatus: 'implemented',
    verification:
      'Subcontractors → Market Data → Coverage (Option) → Corporate actions: Enqueue sync for a ticker, then Load from DB. Or: POST /market/sync kind=feed_stocks_corporate_action, then GET /market/corporate-actions?symbol=AAPL.',
    purpose:
      'Sync dividends, splits, IPOs, and ticker events from Massive Stocks REST and reference APIs into PostgreSQL for corporate-action awareness.',
    helpVerification:
      'POST /market/sync with kind feed_stocks_corporate_action and payload { "symbol": "AAPL" }. Then GET /market/corporate-actions?symbol=AAPL&limit=50. UI: Enqueue sync, then Load from DB.',
  },
  // ── WebSocket (5 Sections, matching coverage CSV order) ──
  {
    id: 'ws-aggregates-s',
    service: 'Aggregates (Per Second)',
    group: 'ws',
    description: 'WebSocket per-second aggregate bars for options channel A.O:{optionsTicker}.',
    tierMin: 'starter',
    projectStatus: 'partial',
    verification:
      'WebSocket → Aggregates (Per Second): copy verify command with --channel "A.O:…" and run in terminal.',
    purpose: 'Stream second-by-second OHLCV bars for a single options contract in real time.',
    helpVerification:
      'Open WebSocket → Aggregates (Per Second), copy command, run python scripts/verify_massive_options_ws.py --config ... --channel "A.O:...".',
    testHint: 'Browser cannot run the Python script; copy the command to a terminal.',
  },
  {
    id: 'ws-aggregates-m',
    service: 'Aggregates (Per Minute)',
    group: 'ws',
    description: 'WebSocket per-minute aggregate bars for options channel AM.O:{optionsTicker}.',
    tierMin: 'starter',
    projectStatus: 'partial',
    verification:
      'WebSocket → Aggregates (Per Minute): copy verify command with --channel "AM.O:…" and run in terminal.',
    purpose: 'Stream minute-by-minute OHLCV bars for a single options contract in real time.',
    helpVerification:
      'Open WebSocket → Aggregates (Per Minute), copy command, run python scripts/verify_massive_options_ws.py --config ... --channel "AM.O:...".',
    testHint: 'Browser cannot run the Python script; copy the command to a terminal.',
  },
  {
    id: 'ws-quotes',
    service: 'Quotes',
    group: 'ws',
    description: 'WebSocket real-time BBO quotes for options channel Q.O:{optionsTicker}.',
    tierMin: 'starter',
    projectStatus: 'partial',
    verification:
      'WebSocket → Quotes: copy verify command with --channel "Q.O:…" and run in terminal.',
    purpose: 'Stream top-of-book bid/ask updates for a single options contract.',
    helpVerification:
      'Open WebSocket → Quotes, copy command, run python scripts/verify_massive_options_ws.py --config ... --channel "Q.O:...".',
    testHint: 'Browser cannot run the Python script; copy the command to a terminal.',
  },
  {
    id: 'ws-trades',
    service: 'Trades',
    group: 'ws',
    description: 'WebSocket tick-level trades for options channel T.O:{optionsTicker}. Requires Developer tier and trades_enabled.',
    tierMin: 'developer',
    requiresTrades: true,
    projectStatus: 'partial',
    verification:
      'WebSocket → Trades: copy verify command with --channel "T.O:…" and run in terminal.',
    purpose: 'Stream tick-by-tick trade prints for a single options contract.',
    helpVerification:
      'Open WebSocket → Trades, copy command, run python scripts/verify_massive_options_ws.py --config ... --channel "T.O:...".',
    testHint: 'Browser cannot run the Python script; copy the command to a terminal.',
  },
  {
    id: 'fmv',
    service: 'Fair Market Value',
    group: 'ws',
    description:
      'Fair Market Value streaming via WebSocket channel FMV.O:{optionsTicker}. '
      + 'Delivers real-time fair market value estimates for option contracts. Requires Business tier.',
    tierMin: 'business',
    projectStatus: 'partial',
    verification:
      'WebSocket → Fair Market Value: WS FMV Channel tab provides a one-click copy of the verify command '
      + 'for python scripts/verify_massive_options_ws.py with --channel "FMV.O:…". '
      + 'Tier & Delivery tab explains entitlement and latency semantics.',
    purpose:
      'Stream real-time fair market value estimates for option contracts via Massive WebSocket. '
      + 'FMV provides a single consolidated price that reflects the best available fair value.',
    helpVerification:
      '1) WS FMV Channel tab: enter an options ticker, copy the verify command, run in a terminal. '
      + 'Expect auth success, subscription confirmation, and FMV messages during market hours. '
      + '2) Tier & Delivery tab: informational; describes Business tier requirement, latency, and delivery semantics.',
  },
  // ── WebSocket: Connectivity verification (not a Massive doc section) ──
  {
    id: 'websocket',
    service: 'Connectivity verification',
    group: 'ws',
    description:
      'Options Starter includes WS access. A standalone verification script is provided; the engine does not maintain a persistent WS bridge.',
    tierMin: 'starter',
    projectStatus: 'partial',
    verification:
      'Run: python scripts/verify_massive_options_ws.py --config config/config.dev.yaml — prints auth status and first messages from the options WS feed.',
    purpose: 'Verify WebSocket connectivity via CLI script. Not a Massive doc section — local verify entry point.',
    helpVerification:
      'From the repo root, run the verify script with your config. Starter plans often use the delayed WebSocket host; the script may retry automatically. Expect subscription success and occasional data during market hours.',
    testHint: 'Browser cannot run the Python script; copy the command to a terminal.',
  },
  {
    id: 'flat-file-day-aggs',
    service: 'Day aggregates',
    group: 'flat',
    description: 'Daily OHLCV across all US options as downloadable S3 flat file.',
    tierMin: 'starter',
    projectStatus: 'not-implemented',
    verification: 'N/A — bulk download not yet integrated.',
    purpose: 'Bulk download of daily aggregates for all US options from S3. Alternative to REST per-contract aggregates.',
    helpVerification: 'Not yet available in this project. See Massive documentation for S3 flat file access.',
  },
  {
    id: 'flat-file-minute-aggs',
    service: 'Minute aggregates',
    group: 'flat',
    description: 'Minute-level OHLCV across all US options as downloadable S3 flat file.',
    tierMin: 'starter',
    projectStatus: 'not-implemented',
    verification: 'N/A — bulk download not yet integrated.',
    purpose: 'Bulk download of minute-level aggregates for all US options from S3.',
    helpVerification: 'Not yet available in this project. See Massive documentation for S3 flat file access.',
  },
  {
    id: 'flat-file-quotes',
    service: 'Quotes',
    group: 'flat',
    description: 'Top-of-book quotes with nanosecond timestamps as downloadable S3 flat file.',
    tierMin: 'starter',
    projectStatus: 'not-implemented',
    verification: 'N/A — bulk download not yet integrated.',
    purpose: 'Bulk download of top-of-book option quotes from S3 for historical analysis.',
    helpVerification: 'Not yet available in this project. See Massive documentation for S3 flat file access.',
  },
  {
    id: 'flat-file-trades',
    service: 'Trades',
    group: 'flat',
    description: 'Tick-level trades with nanosecond timestamps as downloadable S3 flat file. Requires Developer tier.',
    tierMin: 'developer',
    projectStatus: 'not-implemented',
    verification: 'N/A — bulk download not yet integrated.',
    purpose: 'Bulk download of tick-level option trades from S3 for detailed historical analysis.',
    helpVerification: 'Not yet available in this project. Requires Developer tier. See Massive documentation for S3 flat file access.',
  },
]

export const OPTION_CHECKLIST_ROWS = rows
