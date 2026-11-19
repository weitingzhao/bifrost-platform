/**
 * Where everything sits, for any screen (the Owner, 2026-11-19: phone,
 * tablet and computer must each feel right before anything else is built;
 * changing this later breaks everything on top of it).
 *
 * One function decides every rectangle: the four corners, the menu, the levels, the card,
 * and the world area the map is fitted into. The page places its corners
 * from it and the canvas places its cameras from it, so the two never drift.
 *
 *   desktop  ≥1100 wide, short side ≥500: corners as cards, card on the right
 *   tablet   <1100 wide, short side ≥500: the same, smaller; in portrait the
 *            map starts below the summary
 *   phone    short side <500: summary compact (tap for the question), card
 *            as a bottom sheet; in landscape the trail moves into the summary
 *            and alerts sit as a badge beside the minimap (in portrait, at
 *            the end of the trail's row)
 *
 * Touch targets are at least 44 px on phone and tablet. Safe areas (notch,
 * home bar) are kept clear.
 *
 * The panels (the Owner, 2026-11-19: the 2.5D middle shows where things
 * stand; tables and charts beside it say how much, and they are the
 * dashboard's own sections; the empty corners of the diamond hold them, as
 * map and city games do): small cards docked in the two bottom corners
 * where the screen is wide (desktop, tablet held flat), a drawer pulled up
 * from the bottom elsewhere (phone, tablet upright). Any card opens large in
 * the middle. Search and the tools sit in a bar at the top centre.
 */

export type Device = 'phone' | 'tablet' | 'desktop'
export type Rect = { x: number; y: number; w: number; h: number }
export type Insets = { top: number; right: number; bottom: number; left: number }

export type Frame = {
  device: Device
  landscape: boolean
  /** Smallest thing a finger or pointer must hit, px. */
  tap: number
  summary: Rect & { compact: boolean }
  /** The minimap's whole frame; its camera shows below the title strip. */
  mini: Rect
  miniTitle: number
  /**
   * The levels (the Owner, 2026-11-19: three levels, town, district, building, and a control of their
   * own to move between them, not mixed into the district menu): three rows under the minimap.
   */
  levels: Rect & { row: number }
  /** The menu along the bottom: the Console's functions, a second way in beside the levels (the Owner, 2026-11-19). */
  menu: Rect
  /**
   * Alerts: a row under the levels, where a game keeps its notices by the minimap (the Owner,
   * 2026-11-19: not on the bottom edge). No trail: the levels say where you are and take you up.
   */
  alerts: Rect & { badge: boolean }
  /**
   * Search and the tools beside it: a bar at the top centre where there is room (the convention of
   * map and city games), a round button by the summary on a phone.
   */
  search: Rect & { mode: 'bar' | 'icon' }
  /**
   * The panels (tables, charts) as small cards in the two bottom corners, which the diamond of the
   * ground leaves empty: charts on the left, tables and a picked room's card on the right. Null where
   * the screen is upright and narrow: there they live in the drawer.
   */
  docks: { left: Rect; right: Rect } | null
  /** The drawer (phone, tablet upright): its open size; unused where there are docks. */
  rail: Rect & { mode: 'dock' | 'drawer' }
  /** The drawer's handle while it is closed; null where there are docks. */
  peek: Rect | null
  /** Where a card opens large: the middle of the screen, over the dimmed stage. */
  focus: Rect
  /** Where the map is fitted: clear of the summary, the rail or the drawer's handle, and the bottom rows. */
  world: Rect
}

export const NO_INSETS: Insets = { top: 0, right: 0, bottom: 0, left: 0 }

export function deviceOf(w: number, h: number): Device {
  if (Math.min(w, h) < 500) return 'phone'
  return w < 1100 ? 'tablet' : 'desktop'
}

const MINI: Record<Device, { w: number; h: number }> = {
  desktop: { w: 200, h: 130 },
  tablet: { w: 168, h: 112 },
  phone: { w: 132, h: 92 },
}

export function frameOf(width: number, height: number, safe: Insets = NO_INSETS): Frame {
  const device = deviceOf(width, height)
  const landscape = width > height
  const phone = device === 'phone'
  const flat = phone && landscape
  const m = phone ? 10 : 14
  const gap = 8
  const tap = device === 'desktop' ? 28 : 44
  const L = safe.left + m, R = width - safe.right - m, T = safe.top + m, B = height - safe.bottom - (phone ? 8 : 12)

  const menuH = flat ? 40 : 48
  const menuW = phone ? R - L : Math.min(R - L, 620)
  const menu = { x: phone ? L : Math.round((width - menuW) / 2), y: B - menuH, w: menuW, h: menuH }

  const mm = flat ? { w: 120, h: 80 } : MINI[device]
  const mini = { x: R - mm.w, y: T, w: mm.w, h: mm.h }
  const miniTitle = flat ? 20 : 22
  const levelRow = device === 'desktop' ? 30 : flat ? 36 : 40
  const levels = { x: mini.x, y: mini.y + mini.h + gap, w: mini.w, h: levelRow * 3, row: levelRow }

  const alerts = { x: levels.x, y: levels.y + levels.h + gap, w: levels.w, h: levelRow, badge: false }

  // Search: a bar at the top centre between the summary and the minimap; on a phone a round button.
  const searchIcon = phone
  const iconSearch = { x: mini.x - gap - tap, y: T, w: tap, h: tap }

  const compact = phone
  // Room for the search: as a button beside the minimap when the bar would not fit (decided below, from the same numbers).
  const barFits = !searchIcon && Math.min(480, (mini.x - gap * 2) - (L + Math.min(device === 'desktop' ? 320 : 300, mini.x - gap - L) + gap * 2)) >= 440
  const summaryRight = (searchIcon || !barFits ? iconSearch.x : mini.x) - gap
  const summary = {
    x: L, y: T,
    w: Math.max(120, Math.min(device === 'desktop' ? 320 : 300, summaryRight - L)),
    h: compact ? (flat ? 52 : 60) : device === 'desktop' ? 136 : 124,
    compact,
  }

  const barL = summary.x + summary.w + gap * 2, barR = mini.x - gap * 2
  const barW = Math.min(480, barR - barL)
  // The bar holds the box and three tools: under 440 px it would squeeze one or the other, so it becomes a button.
  const search = searchIcon || barW < 440
    ? { ...iconSearch, mode: 'icon' as const }
    : { x: Math.round(Math.max(barL, Math.min(barR - barW, width / 2 - barW / 2))), y: T, w: barW, h: 44, mode: 'bar' as const }

  const bottomRow = menu.y
  const docked = device === 'desktop' || (device === 'tablet' && landscape)
  const peekH = 44
  const dockW = device === 'desktop' ? 330 : 280
  const docks = docked
    ? (() => {
        const bottom = bottomRow - gap
        const lh = Math.min(380, bottom - (summary.y + summary.h + gap))
        const rh = Math.min(380, bottom - (alerts.y + alerts.h + gap))
        return { left: { x: L, y: bottom - lh, w: dockW, h: lh }, right: { x: R - dockW, y: bottom - rh, w: dockW, h: rh } }
      })()
    : null
  const rail = docks
    ? { ...docks.right, mode: 'dock' as const }
    : (() => { const h = Math.round((bottomRow - gap - T) * (flat ? 0.75 : phone ? 0.55 : 0.45)); return { x: L, y: bottomRow - gap - h, w: R - L, h, mode: 'drawer' as const } })()
  const peek = docks ? null : { x: L, y: bottomRow - gap - peekH, w: R - L, h: peekH }

  // The map: on a phone and a tablet in portrait it starts below the summary; elsewhere the corners float over its edges.
  // It ends above the drawer's handle; the docks sit in the corners the diamond leaves empty.
  const top = phone || (device === 'tablet' && !landscape) ? summary.y + summary.h + gap : docks ? search.y + search.h + gap : T
  const bottom = peek ? peek.y - gap : bottomRow - gap
  const world = { x: safe.left, y: top, w: width - safe.right - safe.left, h: Math.max(80, bottom - top) }

  // A card opened large: centred, most of the screen on a computer, all of it on a phone.
  const fw = phone ? R - L : Math.min(920, R - L - 80), fh = phone ? bottomRow - gap - T : Math.min(660, bottomRow - gap - T - 40)
  const focus = { x: Math.round(width / 2 - fw / 2), y: phone ? T : Math.round(T + (bottomRow - gap - T) / 2 - fh / 2), w: fw, h: fh }

  return { device, landscape, tap, summary, mini, miniTitle, levels, alerts, menu, search, docks, rail, peek, focus, world }
}

export const overlaps = (a: Rect, b: Rect) => a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h
export const inside = (a: Rect, w: number, h: number) => a.x >= 0 && a.y >= 0 && a.x + a.w <= w && a.y + a.h <= h

/**
 * The middle swaps (the Owner, 2026-11-19: the map is the lead and the panels support it, but any
 * panel may take the middle, and then the map supports it). When a panel docked on `side` takes the
 * middle, the map goes to that dock as a medium card and the panel fills the space between the docks.
 * Without docks (phone, tablet upright) the map takes the top part of the stage and the panel the rest.
 */
export function swapRects(f: Frame, side: 'left' | 'right'): { map: Rect; panel: Rect } {
  const gap = 8
  if (f.docks) {
    const map = f.docks[side]
    const x0 = f.docks.left.x + f.docks.left.w + gap * 2, x1 = f.docks.right.x - gap * 2
    return { map, panel: { x: x0, y: f.world.y, w: x1 - x0, h: f.world.h } }
  }
  const x = f.summary.x, w = f.mini.x + f.mini.w - f.summary.x
  const mh = Math.round(f.world.h * 0.4)
  // A rectangle beside the right column (levels, alerts under the minimap) stops short of it; below it runs full width.
  const colBottom = f.alerts.y + f.alerts.h
  const clear = (r: Rect): Rect => r.y < colBottom + gap && r.y + r.h > f.levels.y ? { ...r, w: Math.min(r.w, f.levels.x - gap - r.x) } : r
  const map = clear({ x, y: f.world.y, w, h: mh })
  return { map, panel: clear({ x, y: map.y + mh + gap, w, h: f.world.y + f.world.h - (map.y + mh + gap) }) }
}
