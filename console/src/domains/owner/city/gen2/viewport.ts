/**
 * Where everything sits, for any screen (the Owner, 2026-11-19: phone,
 * tablet and computer must each feel right before anything else is built;
 * changing this later breaks everything on top of it).
 *
 * One function decides every rectangle: the four corners, the menu, the card,
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
 * dashboard's own sections): a rail on the right where the screen is wide
 * (desktop, tablet held flat), a drawer pulled up from the bottom elsewhere
 * (phone, tablet upright), shown closed as a handle. A picked thing's card
 * opens in the same place.
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
  /** The trail as its own corner, or null when it sits inside the summary. */
  trail: Rect | null
  /** Alerts: a corner with words, or a round badge. */
  alerts: Rect & { badge: boolean }
  menu: Rect
  /** The panels (tables, charts) and a picked thing's card: a rail on the right, or a drawer from the bottom (its open size). */
  rail: Rect & { mode: 'side' | 'drawer' }
  /** The drawer's handle while it is closed; null for a side rail. */
  peek: Rect | null
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

  const row = 36
  // On a phone alerts are a round badge: beside the minimap in landscape, at the end of the trail's row in portrait.
  const alertsBadge = phone
  const alerts = flat
    ? { x: mini.x - gap - tap, y: T, w: tap, h: tap, badge: true }
    : phone
      ? { x: R - tap, y: menu.y - gap - tap, w: tap, h: tap, badge: true }
      : { x: R - 124, y: menu.y - gap - row, w: 124, h: row, badge: false }

  const trail = flat ? null : { x: L, y: menu.y - gap - (alertsBadge ? tap : row), w: Math.min(alerts.x - gap - L, 520), h: alertsBadge ? tap : row }

  const compact = phone
  const summaryRight = (flat ? alerts.x : mini.x) - gap
  const summary = {
    x: L, y: T,
    w: Math.max(120, Math.min(device === 'desktop' ? 320 : 300, summaryRight - L)),
    h: compact ? (flat ? 52 : 60) : device === 'desktop' ? 136 : 124,
    compact,
  }

  const bottomRow = trail ? trail.y : menu.y
  const side = device === 'desktop' || (device === 'tablet' && landscape)
  const peekH = 44
  const rail = side
    ? (() => { const y = mini.y + mini.h + 12; const w = device === 'desktop' ? 360 : 300; return { x: R - w, y, w, h: Math.max(160, bottomRow - gap - y), mode: 'side' as const } })()
    : (() => { const h = Math.round((bottomRow - gap - T) * (flat ? 0.75 : phone ? 0.55 : 0.45)); return { x: L, y: bottomRow - gap - h, w: R - L, h, mode: 'drawer' as const } })()
  const peek = side ? null : { x: L, y: bottomRow - gap - peekH, w: R - L, h: peekH }

  // The map: on a phone and a tablet in portrait it starts below the summary; elsewhere the corners float over its edges.
  // It ends left of a side rail, or above the drawer's handle.
  const top = phone || (device === 'tablet' && !landscape) ? summary.y + summary.h + gap : T
  const bottom = peek ? peek.y - gap : bottomRow - gap
  const world = { x: safe.left, y: top, w: (side ? rail.x - gap : width - safe.right) - safe.left, h: Math.max(80, bottom - top) }

  return { device, landscape, tap, summary, mini, miniTitle, trail, alerts, menu, rail, peek, world }
}

export const overlaps = (a: Rect, b: Rect) => a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h
export const inside = (a: Rect, w: number, h: number) => a.x >= 0 && a.y >= 0 && a.x + a.w <= w && a.y + a.h <= h
