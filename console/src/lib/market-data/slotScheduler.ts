/**
 * Massive schedule slot → ignition scheduler after husbandry migrate.
 * All Golden Source Massive SLOT_NAMES are Dagster-owned; Cron suspended.
 */

const DAGSTER_SLOTS = new Set([
  'stock-eod',
  'eod-pipeline',
  'universe-daily',
  'corporate',
  'calendar',
  'stock-snapshot',
  'stock-movers',
  'oi-gap-heal',
  'option-bars',
  'minute-bars',
  'option-trades',
  'option-refresh',
  'reference',
  'fundamentals-rotate',
  'related-rotate',
  'trim',
])

/** Slot → Dagster schedule name (UTC market_* + trading_day for EOD). */
export const SLOT_TO_DAGSTER_SCHEDULE: Readonly<Record<string, string>> = {
  'stock-eod': 'research_trading_day_schedule',
  'eod-pipeline': 'research_trading_day_schedule',
  'stock-snapshot': 'market_snapshot_schedule',
  'stock-movers': 'market_movers_schedule',
  reference: 'market_reference_schedule',
  'universe-daily': 'market_universe_calendar_schedule',
  calendar: 'market_universe_calendar_schedule',
  'related-rotate': 'market_related_schedule',
  'option-bars': 'market_option_bars_schedule',
  corporate: 'market_corporate_trades_schedule',
  'option-trades': 'market_corporate_trades_schedule',
  'minute-bars': 'market_minute_bars_schedule',
  'fundamentals-rotate': 'market_fundamentals_rotate_schedule',
  'option-refresh': 'market_option_refresh_schedule',
  trim: 'market_trim_schedule',
  'oi-gap-heal': 'market_oi_gap_heal_schedule',
}

/** Analytics slots moved to Research — not Massive Cron. */
const RESEARCH_MIGRATED = new Set(['max-pain', 'atm-iv-pcr', 'iv-percentile'])

export type SlotSchedulerKind = 'dagster' | 'research' | 'cron' | 'unknown'

export function slotSchedulerKind(slot: string): SlotSchedulerKind {
  const s = slot.trim().toLowerCase()
  if (DAGSTER_SLOTS.has(s)) return 'dagster'
  if (RESEARCH_MIGRATED.has(s)) return 'research'
  if (s === 'readiness-refresh') return 'research'
  return 'unknown'
}

export function slotSchedulerLabel(kind: SlotSchedulerKind): string {
  if (kind === 'dagster') return 'Dagster'
  if (kind === 'research') return 'Research'
  if (kind === 'cron') return 'Cron'
  return '—'
}

export function dagsterScheduleForSlot(slot: string): string | null {
  const s = slot.trim().toLowerCase()
  return SLOT_TO_DAGSTER_SCHEDULE[s] ?? null
}
