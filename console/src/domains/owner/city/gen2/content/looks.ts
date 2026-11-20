import type { Path, Place } from '../frame/places'

/**
 * The two looks of one system, side by side (the Owner, 2026-11-19: "游戏的展现形式和传统的 dashboard
 * 是平级的，不应该让 dashboard 包住游戏的页面"). The city fills the screen on its own; the Console is
 * its own screen with its pages and, for every place, a ledger: the same places as rows. A switch at
 * the same spot in both moves between them, and the link carries the place and the picked room.
 *
 * Addresses, all on the Console's `#city` route:
 *   #city?at=system/utilities/ubt-k3s-04&sel=disk          the city, at that place, the disk room picked
 *   #city?look=ledger&at=system/utilities/ubt-k3s-04&sel=disk   the Console's ledger of that place
 */

export const LEDGER = 'ledger'

const atOf = (path: Path) => path.map(encodeURIComponent).join('/')

function query(parts: [string, string | null | undefined][]): string {
  const q = parts.filter((p): p is [string, string] => !!p[1]).map(([k, v]) => `${k}=${v}`).join('&')
  return q ? `?${q}` : ''
}

/** The city at a place, with a room picked. */
export const cityHref = (path: Path, sel?: string | null): string =>
  `#city${query([['at', atOf(path)], ['sel', sel ? encodeURIComponent(sel) : null]])}`

/** The Console's ledger of a place, with a row picked. */
export const ledgerHref = (path: Path, sel?: string | null): string =>
  `#city${query([['look', LEDGER], ['at', atOf(path)], ['sel', sel ? encodeURIComponent(sel) : null]])}`

/** The Console look of a place: its own page when it has one, else its ledger. */
export const consoleHrefOf = (place: Place, path: Path, picked: Place | null): string =>
  place.dashboard ?? ledgerHref(path, picked?.id)

/** What a `#city` address asks for. */
export function readCityQuery(query: string): { look: 'city' | 'ledger'; path: Path; sel: string | null } {
  const q = new URLSearchParams(query)
  const at = q.get('at')
  return {
    look: q.get('look') === LEDGER ? 'ledger' : 'city',
    // URLSearchParams has decoded the segments already.
    path: at ? at.split('/').filter(Boolean) : [],
    sel: q.get('sel'),
  }
}
