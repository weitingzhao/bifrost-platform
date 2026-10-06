/**
 * Massive schedule slot → the Dagster schedule that fires it.
 *
 * The mapping comes from Research's roster: each row of
 * GET /research/orchestration/status carries `market_slots`, the plugin slots
 * that schedule enqueues. Console used to keep its own slot → schedule table;
 * it drifted (corporate still pointed at the renamed
 * market_corporate_trades_schedule and seven newer slots had no row), so there
 * is no list here any more (TD-108).
 */

import type { OrchestrationScheduleRow } from '@/api/researchEngine'

/** slot → Dagster schedule name. */
export type SlotScheduleIndex = ReadonlyMap<string, string>

export const EMPTY_SLOT_INDEX: SlotScheduleIndex = new Map()

/**
 * Build slot → schedule from the status rows. When several schedules fire one
 * slot (intraday-chain: 10:30 / 13:00 / 15:30), the first row wins; they share
 * one job, so its last run is the same either way.
 */
export function buildSlotScheduleIndex(
  rows: readonly Pick<OrchestrationScheduleRow, 'name' | 'market_slots'>[] | null | undefined,
): SlotScheduleIndex {
  const index = new Map<string, string>()
  for (const row of rows ?? []) {
    for (const slot of row.market_slots ?? []) {
      const key = slot.trim().toLowerCase()
      if (key !== '' && !index.has(key)) index.set(key, row.name)
    }
  }
  return index
}

/** Analytics slots moved to Research — not Massive Cron. */
const RESEARCH_MIGRATED = new Set(['max-pain', 'atm-iv-pcr', 'iv-percentile'])

export type SlotSchedulerKind = 'dagster' | 'research' | 'cron' | 'unknown'

export function slotSchedulerKind(slot: string, index: SlotScheduleIndex): SlotSchedulerKind {
  const s = slot.trim().toLowerCase()
  if (index.has(s)) return 'dagster'
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

export function dagsterScheduleForSlot(slot: string, index: SlotScheduleIndex): string | null {
  return index.get(slot.trim().toLowerCase()) ?? null
}
