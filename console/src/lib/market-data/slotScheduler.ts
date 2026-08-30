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
