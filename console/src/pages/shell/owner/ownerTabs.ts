import { formatShellHash, hashQueryParam } from '@/lib/shell/consoleRoutes'

/** The Owner Console's quick-switch tabs (Owner 2026-10-16). `all` stacks every block. */
export const OWNER_TABS = [
  { id: 'all', label: 'All', short: 'All' },
  { id: 'you', label: 'For you', short: 'For you' },
  { id: 'chief', label: 'Chief of staff', short: 'Chief' },
  { id: 'work', label: 'Work', short: 'Work' },
  { id: 'system', label: 'System', short: 'System' },
  { id: 'data', label: 'Data', short: 'Data' },
] as const

export type OwnerTab = (typeof OWNER_TABS)[number]['id']

const TAB_IDS: readonly string[] = OWNER_TABS.map(tab => tab.id)

export function isOwnerTab(value: string): value is OwnerTab {
  return TAB_IDS.includes(value)
}

/** A phone opens on For you; a tablet or a desktop on All. */
export function defaultOwnerTab(narrow: boolean): OwnerTab {
  return narrow ? 'you' : 'all'
}

/** `tab=` from the Owner Console hash (`#owner?tab=work`), or null when absent or unknown. */
export function ownerTabFromHash(hash: string): OwnerTab | null {
  const raw = hash.replace(/^#/, '')
  const q = raw.indexOf('?')
  if (q < 0) return null
  const tab = hashQueryParam(raw.slice(q + 1), 'tab')
  return tab != null && isOwnerTab(tab) ? tab : null
}

export function ownerTabHash(tab: OwnerTab): string {
  return formatShellHash('owner', new URLSearchParams({ tab }).toString())
}

/** Which blocks a tab shows. */
export function ownerBlocks(tab: OwnerTab) {
  const all = tab === 'all'
  return {
    you: all || tab === 'you',
    chief: all || tab === 'chief',
    work: all || tab === 'work',
    system: all || tab === 'system',
    data: all || tab === 'data',
  }
}
