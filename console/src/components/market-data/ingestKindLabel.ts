const KIND_ALIAS: Record<string, string> = {
  option_open_interest: 'opt_oi',
  option_snapshot: 'opt_snap',
  option_daily: 'opt_day',
  option_minute: 'opt_min',
  option_trades: 'opt_trd',
  option_contract: 'opt_ctr',
  option_expiration: 'opt_exp',
  stock_daily: 'stk_day',
  stock_daily_grouped: 'stk_grp',
  stock_minute: 'stk_min',
  stock_snapshot: 'stk_snap',
  ticker_sync: 'tkr_sync',
  ticker_related: 'tkr_rel',
  short_interest: 'short_int',
  short_volume: 'short_vol',
}

export function shortIngestKind(kind: string | undefined): string {
  if (kind == null || kind === '') return '—'
  return KIND_ALIAS[kind] ?? kind
}
