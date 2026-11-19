/**
 * Day, dusk and night (the Owner, 2026-11-19: like a UI's light and dark,
 * the town has its time of day; at least day and night, dusk if it reads).
 * By default the light follows the viewer's clock; he can also pin one.
 *
 * One palette per light, used by both halves: the canvas (ground, blocks,
 * lines) and the corners (CSS variables on the page's root).
 */

export type Light = 'day' | 'dusk' | 'night'
export type LightChoice = 'auto' | Light

/** Dawn and dusk are an hour and a half each side of day: 06:00–07:30 and 17:30–19:00. */
export function lightAt(d: Date): Light {
  const m = d.getHours() * 60 + d.getMinutes()
  if (m >= 7 * 60 + 30 && m < 17 * 60 + 30) return 'day'
  if ((m >= 6 * 60 && m < 7 * 60 + 30) || (m >= 17 * 60 + 30 && m < 19 * 60)) return 'dusk'
  return 'night'
}

export const lightOf = (choice: LightChoice, now: Date): Light => choice === 'auto' ? lightAt(now) : choice

/** The order a single button steps through. */
export const NEXT: Record<LightChoice, LightChoice> = { auto: 'day', day: 'dusk', dusk: 'night', night: 'auto' }

export type Palette = {
  sky: number; ground: number; groundEdge: number; line: number; line4: number
  /** The slab's earth sides and the ink of every outline. */
  earth: number; earthShade: number; ink: number
  /** Outdoors: green belts, the street's asphalt, its kerb and paint. Indoors: the floor and its corridors. */
  grass: number; asphalt: number; kerb: number; paint: number; floor: number; corridor: number
  /** A plot's own lot, inside its green belt. */
  lot: number
  /** Indoors: the storey's outer walls (inner face, its shaded face, the cap), their glass, and the floor slab's sides. */
  wall: number; wallShade: number; wallTop: number; glass: number; slab: number; slabShade: number
  top: number; left: number; right: number; edge: number
  hover: number; pick: number; inside: number; ok: number; no: number
  /** Lit windows on a block's sides at night; null by day. */
  glow: number | null
  label: { color: string; background: string }
  miniSky: number
}

export const PALETTE: Record<Light, Palette> = {
  day: {
    sky: 0xf3efe6, ground: 0xe8e4ee, groundEdge: 0xcfc8b8, line: 0xd9d4e2, line4: 0xc8c2d4, earth: 0xc98a5a, earthShade: 0xa86c43, ink: 0x2f2d3a,
    grass: 0xbcdba0, lot: 0xe3efd5, asphalt: 0x9a98a8, kerb: 0xe8e4ee, paint: 0xfbfaf6, floor: 0xece6da, corridor: 0xf7f3ea,
    wall: 0xf1ece2, wallShade: 0xdcd5c7, wallTop: 0xfbf9f4, glass: 0xcfe6f7, slab: 0xd6cfc1, slabShade: 0xc0b8a8,
    top: 0xf7f5f0, left: 0xd9d4ca, right: 0xc4beb2, edge: 0x6b6676,
    hover: 0xfff3c4, pick: 0xffd36b, inside: 0x8fc1ff, ok: 0x5cb85c, no: 0xd9534f,
    glow: null, label: { color: '#3a3646', background: '#ffffffe0' }, miniSky: 0xfbfaf6,
  },
  dusk: {
    sky: 0xf1d9c4, ground: 0xe9d6c6, groundEdge: 0xc7ab92, line: 0xdcc4b0, line4: 0xcbb09a, earth: 0xb9774b, earthShade: 0x93593a, ink: 0x2f2d3a,
    grass: 0xb9c48c, lot: 0xdde0b8, asphalt: 0x8a7a86, kerb: 0xe9d6c6, paint: 0xfff0d8, floor: 0xead8c6, corridor: 0xf5e6d6,
    wall: 0xefdac8, wallShade: 0xd8bea8, wallTop: 0xf8eadc, glass: 0x7d6f8c, slab: 0xd1b8a2, slabShade: 0xb89f89,
    top: 0xf6e3d0, left: 0xcfb29c, right: 0xb39580, edge: 0x5e4a52,
    hover: 0xffe7b8, pick: 0xffc46b, inside: 0x9bb8ff, ok: 0x5cb85c, no: 0xd9534f,
    glow: 0xffd48a, label: { color: '#3d2f36', background: '#fff6ecdd' }, miniSky: 0xf7e6d6,
  },
  night: {
    sky: 0x1d2033, ground: 0x2c3049, groundEdge: 0x3c4160, line: 0x353a56, line4: 0x41466a, earth: 0x5a4030, earthShade: 0x46301f, ink: 0x0f111c,
    grass: 0x2a4232, lot: 0x33503e, asphalt: 0x1f2234, kerb: 0x3a3e5c, paint: 0xd8c86a, floor: 0x2c3049, corridor: 0x363b58,
    wall: 0x3d4263, wallShade: 0x30344e, wallTop: 0x4a4f72, glass: 0xffd27a, slab: 0x30344e, slabShade: 0x262a40,
    top: 0x3d4263, left: 0x2c3049, right: 0x23263b, edge: 0x10121e,
    hover: 0x56609a, pick: 0xd9a83a, inside: 0x4f7fd9, ok: 0x4caf6a, no: 0xd0544f,
    glow: 0xffd27a, label: { color: '#e6e3f2', background: '#1d2033dd' }, miniSky: 0x24283d,
  },
}

/** CSS variables for the corners, per light. */
export const CORNERS: Record<Light, Record<string, string>> = {
  day: { '--g2-sky': '#f3efe6', '--g2-card': '#fffffff2', '--g2-line': '#d8d2c4', '--g2-ink': '#2f2d3a', '--g2-soft': '#5d5868', '--g2-mute': '#8a8478', '--g2-link': '#3d6fd8', '--g2-chip': '#ece7db', '--g2-menu': '#2f2d3aee', '--g2-menu-ink': '#e9e4d8', '--g2-ink-line': '#2f2d3a' },
  dusk: { '--g2-sky': '#f1d9c4', '--g2-card': '#fff7eef2', '--g2-line': '#dcc3ad', '--g2-ink': '#3d2f36', '--g2-soft': '#6b5560', '--g2-mute': '#94776a', '--g2-link': '#3a62c4', '--g2-chip': '#f0dccb', '--g2-menu': '#3d2f36ee', '--g2-menu-ink': '#f4e4d6', '--g2-ink-line': '#3d2f36' },
  night: { '--g2-sky': '#1d2033', '--g2-card': '#262a40f2', '--g2-line': '#3c4160', '--g2-ink': '#e6e3f2', '--g2-soft': '#b4b1c8', '--g2-mute': '#8d8aa6', '--g2-link': '#8fb3ff', '--g2-chip': '#353a58', '--g2-menu': '#0f111ce8', '--g2-menu-ink': '#d9d6ea', '--g2-ink-line': '#0b0c16' },
}
