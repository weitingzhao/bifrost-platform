import { STOCK_CHECKLIST_ROWS } from './stockChecklistRows'
import type { ChecklistRow, CapabilityGroup } from './types'
import { CAPABILITY_GROUP_ORDER } from './types'

export { shortServiceLabel, checklistEffectiveStatusLabel } from './displayHelpers'

export function groupedStockChecklistRows(): { group: CapabilityGroup; rows: ChecklistRow[] }[] {
  return CAPABILITY_GROUP_ORDER.map(g => ({
    group: g,
    rows: STOCK_CHECKLIST_ROWS.filter(r => r.group === g),
  })).filter(g => g.rows.length > 0)
}
