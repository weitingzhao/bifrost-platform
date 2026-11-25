import { ENTRY_ROUTE_IDS, SHELL_NAV, type ShellRouteId } from '@/lib/shell/consoleRoutes'

/**
 * The Console's functions: its menu entries, read from its routes. They are the bar along the bottom,
 * the dimension by function (ISSUE-9: the districts are the domains, the menu is another
 * dimension; look at the two apart). A place that serves one says so in its `function`; the menu lights
 * up what serves it on every level.
 *
 * Until 2026-11-20 the districts themselves were these entries and each detail page a building in one;
 * that tree was taken out the day the districts became the domains (domainPlaces.ts).
 */
export const consoleFunctions = () => {
  const nav = SHELL_NAV as readonly { id: ShellRouteId; label: string }[]
  return ENTRY_ROUTE_IDS.map(id => ({ id, label: nav.find(x => x.id === id)!.label }))
}
