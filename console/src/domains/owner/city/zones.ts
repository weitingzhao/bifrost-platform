import type { Zone } from './builderDoc'

/**
 * The districts the Owner draws (2026-11-18: he zones the land himself; where
 * things stand is only how they are shown, never the system's logic). A
 * district is a named rectangle of tinted ground; the usual names come with
 * their tint, any other name is allowed.
 */
export const ZONE_NAMES = ['Launch site', 'Satellite campuses', 'Suppliers', 'Town', 'Power yard', 'Off the grid'] as const
const TINTS: Record<string, string> = { 'Launch site': '#e3e8ee', 'Satellite campuses': '#e7e1f1', Suppliers: '#e0edf0', 'Off the grid': '#e2e0e8', Town: '#f1e8d6', 'Power yard': '#e9e6da' }
const OTHER = ['#e2eedf', '#f3e2e2', '#e0edf0', '#efe9d0']

export function zoneTint(name: string): string {
  if (TINTS[name]) return TINTS[name]
  let h = 0
  for (const ch of name) h = (h * 31 + ch.charCodeAt(0)) >>> 0
  return OTHER[h % OTHER.length]
}

/** A new district's name: the first usual one not on the land yet, else "District n". */
export function nextZoneName(zones: readonly Zone[]): string {
  const used = new Set(zones.map(z => z.name))
  return ZONE_NAMES.find(n => !used.has(n)) ?? `District ${zones.length + 1}`
}
