import { hashQueryParam, hashQueryWithoutTaskMode } from '@/lib/shell/consoleRoutes'

export const RECORDS_TABS = [
  { value: 'closed', label: 'Closed requests' },
  // Finished work: what was Progress's History (W-31 2.2b).
  { value: 'work', label: 'Work history' },
  { value: 'audit', label: 'Audit' },
  { value: 'patrol', label: 'Patrol' },
] as const

export type RecordsTab = (typeof RECORDS_TABS)[number]['value']

/** `#records?tab=…`; anything else opens Closed requests. */
export function recordsTabFromHash(hash: string): RecordsTab {
  const value = hashQueryParam(hashQueryWithoutTaskMode(hash.replace(/^#/, '')), 'tab')
  return RECORDS_TABS.find(tab => tab.value === value)?.value ?? 'closed'
}
