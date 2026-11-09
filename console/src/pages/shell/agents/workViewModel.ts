import { useEffect, useState } from 'react'
import { formatShellHash, hashQueryParam } from '@/lib/shell/consoleRoutes'
import { canonicalQuery } from '@/lib/shell/useHashSection'

/*
 * Which view of Work the address opens (R5:5.5-2, Design v3.6 WorkPage): the round summary and its four tabs, or one
 * of the pages Work keeps one step away (goals, activity, code health, commit lineage). Every address Work took before
 * still lands where it did: `goal=` on the goal cards, `section=signoff` on the sign-offs, `section=plan` / `node=` on
 * the round, `root=` / `round=` on that round.
 */

export const WORK_TABS = ['round', 'signoffs', 'live', 'changes'] as const
export type WorkTab = (typeof WORK_TABS)[number]
export const WORK_SECTIONS = ['goals', 'activity', 'code-health', 'commit-lineage', 'check-value', 'delivery-boundaries'] as const
export type WorkSection = (typeof WORK_SECTIONS)[number]
export type WorkView = { kind: 'tab'; tab: WorkTab } | { kind: 'section'; section: WorkSection }

const isTab = (v: string | null): v is WorkTab => WORK_TABS.includes(v as WorkTab)
const isSection = (v: string | null): v is WorkSection => WORK_SECTIONS.includes(v as WorkSection)

/** The view an address's query names. */
export function workViewOf(query: string): WorkView {
  const goal = hashQueryParam(query, 'goal')
  const section = hashQueryParam(query, 'section')
  if ((goal && /^M-\d+$/.test(goal)) || section === 'goals') return { kind: 'section', section: 'goals' }
  if (isSection(section)) return { kind: 'section', section }
  const tab = hashQueryParam(query, 'tab')
  if (isTab(tab)) return { kind: 'tab', tab }
  if (section === 'signoff') return { kind: 'tab', tab: 'signoffs' }
  return { kind: 'tab', tab: 'round' }
}

/** The view the current address opens, following every hash change. */
export function useWorkView(): WorkView {
  const read = () => workViewOf(canonicalQuery(window.location.hash))
  const [view, setView] = useState(read)
  useEffect(() => {
    const sync = () => setView(current => {
      const next = read()
      return next.kind === current.kind && (next.kind === 'tab' ? current.kind === 'tab' && next.tab === current.tab : current.kind === 'section' && next.section === current.section) ? current : next
    })
    window.addEventListener('hashchange', sync)
    return () => window.removeEventListener('hashchange', sync)
  }, [])
  return view
}

/** A tab of the round the address shows: the round (`root=`) is kept, a module or step (`node=`) is not. */
export function workTabHref(tab: WorkTab, root: string | null, named: boolean): string {
  const query = new URLSearchParams()
  if (tab !== 'round') query.set('tab', tab)
  if (root && named) query.set('root', root)
  return formatShellHash('work', query.toString())
}

/** One of Work's pages a step away; with a round, the one the address names (Activity's Needs cleanup reads it). */
export const workSectionHref = (section: WorkSection, root: string | null = null) =>
  formatShellHash('work', new URLSearchParams({ section, ...(root ? { root } : {}) }).toString())

export const WORK_SECTION_TITLES: Record<WorkSection, string> = {
  goals: 'Goals', activity: 'Activity', 'code-health': 'Code health', 'commit-lineage': 'Commit lineage', 'check-value': 'Check value',
  'delivery-boundaries': 'Delivery boundaries',
}
