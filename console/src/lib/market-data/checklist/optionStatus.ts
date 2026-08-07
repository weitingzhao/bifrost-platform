import { OPTION_CHECKLIST_ROWS } from './optionChecklistRows'
import type { ChecklistRow, CapabilityGroup } from './types'
import { CAPABILITY_GROUP_ORDER } from './types'

/** Shared REST capabilities shown under Coverage → Common. */
export const MARKET_DATA_COMMON_CAP_IDS = ['technical-indicators', 'market-ops'] as const
const COMMON_CAP_ID_SET = new Set<string>(MARKET_DATA_COMMON_CAP_IDS)

export function optionFeedChecklistRows(): ChecklistRow[] {
  return OPTION_CHECKLIST_ROWS.filter(r => !COMMON_CAP_ID_SET.has(r.id))
}

export function commonFeedChecklistRows(): ChecklistRow[] {
  return OPTION_CHECKLIST_ROWS.filter(r => COMMON_CAP_ID_SET.has(r.id))
}

export function shortServiceLabel(row: ChecklistRow): string {
  const s = row.service.trim()
  if (s.length <= 22) return s
  return `${s.slice(0, 20)}…`
}

export function groupedOptionFeedChecklistRows(): {
  group: CapabilityGroup
  rows: ChecklistRow[]
}[] {
  return CAPABILITY_GROUP_ORDER.map(g => ({
    group: g,
    rows: optionFeedChecklistRows().filter(r => r.group === g),
  })).filter(g => g.rows.length > 0)
}

export function groupedCommonFeedChecklistRows(): {
  group: CapabilityGroup
  rows: ChecklistRow[]
}[] {
  return CAPABILITY_GROUP_ORDER.map(g => ({
    group: g,
    rows: commonFeedChecklistRows().filter(r => r.group === g),
  })).filter(g => g.rows.length > 0)
}
