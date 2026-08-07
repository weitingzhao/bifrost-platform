/** Static CronJob schedule summary for Market Data analytics / ingest slots. */
export const MARKET_DATA_SCHEDULE_SLOTS: ReadonlyArray<{
  id: string
  cron: string
  note: string
}> = [
  { id: 'stock-eod', cron: '30 21 * * *', note: 'Stock EOD bars' },
  { id: 'eod-pipeline', cron: '0 22 * * *', note: 'EOD pipeline' },
  { id: 'universe-daily', cron: '0 22 * * *', note: 'Universe daily' },
  { id: 'corporate', cron: '0 23 * * *', note: 'Corporate actions' },
  { id: 'option-refresh', cron: '20 */6 * * *', note: 'Option contract refresh' },
  { id: 'option-bars', cron: '45 22 * * *', note: 'Option daily bars' },
  { id: 'stock-snapshot', cron: '5 21 * * *', note: 'Stock snapshots' },
  { id: 'stock-movers', cron: '10 21 * * *', note: 'Stock movers' },
  { id: 'oi-gap-heal', cron: '0 4 * * 6', note: 'OI gap heal (weekly)' },
  { id: 'max-pain', cron: '45 22 * * *', note: 'Max Pain analytics' },
  { id: 'atm-iv-pcr', cron: '0 23 * * *', note: 'ATM IV + PCR' },
  { id: 'iv-percentile', cron: '15 23 * * *', note: 'IV Percentile' },
  { id: 'fundamentals-rotate', cron: '0 3 * * *', note: 'Fundamentals rotate' },
  { id: 'trim', cron: '15 2 * * *', note: 'Job queue trim' },
]
