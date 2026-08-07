import type { ChecklistRow, EffectiveServiceStatus } from './types'

export function shortServiceLabel(row: ChecklistRow): string {
  const s = row.service.trim()
  if (s.length <= 22) return s
  return `${s.slice(0, 20)}…`
}

export function checklistEffectiveStatusLabel(eff: EffectiveServiceStatus): string {
  if (eff === 'implemented') return 'Implemented'
  if (eff === 'partial') return 'Partial'
  if (eff === 'not-on-tier') return 'Not on tier'
  return 'Not implemented'
}
