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
  /** The first generation's land (the Owner, 2026-11-20: its ground and colours were the beautiful part):
   * the grass's checker, the kerb line between asphalt and pavement, the sea and its sand behind the town,
   * the clouds, and the soft shadow every block casts. */
  grassAlt: number; curb: number; sea: number; wave: number; sand: number; cloud: number; shadow: number
  /** Indoors: the storey's outer walls (inner face, its shaded face, the cap), their glass, and the floor slab's sides. */
  wall: number; wallShade: number; wallTop: number; glass: number; slab: number; slabShade: number
  /** Indoors: the buffer between a corridor and a room's plot. */
  threshold: number
  top: number; left: number; right: number; edge: number
  hover: number; pick: number; inside: number; ok: number; no: number
  /** What steps back while the menu lights up something else: a grey tint, solid. */
  dim: number
  /** Lit windows on a block's sides at night; null by day. */
  glow: number | null
  label: { color: string; background: string }
  miniSky: number
}

export const PALETTE: Record<Light, Palette> = {
  day: {
    sky: 0xf3efe6, ground: 0xe8e4ee, groundEdge: 0xcfc8b8, line: 0xd9d4e2, line4: 0xc8c2d4, earth: 0xcf9f6c, earthShade: 0xab7c4e, ink: 0x2f2d3a,
    grass: 0xd6e8c1, lot: 0xe3efd5, asphalt: 0xb9bad1, kerb: 0xf2eee5, paint: 0xffffff,
    grassAlt: 0xcfe3b8, curb: 0x85829f, sea: 0x9fd6f2, wave: 0xe6f6ff, sand: 0xf3e3b8, cloud: 0xfbfaf6, shadow: 0x7d74d6, floor: 0xece6da, corridor: 0xf7f3ea,
    wall: 0xf1ece2, wallShade: 0xdcd5c7, wallTop: 0xfbf9f4, glass: 0xcfe6f7, slab: 0xd6cfc1, slabShade: 0xc0b8a8, threshold: 0xdcd3c1,
    top: 0xf7f5f0, left: 0xd9d4ca, right: 0xc4beb2, edge: 0x6b6676,
    hover: 0xfff3c4, pick: 0xffd36b, inside: 0x8fc1ff, ok: 0x5cb85c, no: 0xd9534f, dim: 0xa8a8a8,
    glow: null, label: { color: '#3a3646', background: '#ffffffe0' }, miniSky: 0xfbfaf6,
  },
  dusk: {
    sky: 0xf1d9c4, ground: 0xe9d6c6, groundEdge: 0xc7ab92, line: 0xdcc4b0, line4: 0xcbb09a, earth: 0xb9774b, earthShade: 0x93593a, ink: 0x2f2d3a,
    grass: 0xc9cf9c, lot: 0xdde0b8, asphalt: 0xa898a8, kerb: 0xf0dccb, paint: 0xfff0d8,
    grassAlt: 0xc1c893, curb: 0x7a6676, sea: 0x8fb4cf, wave: 0xf3dccb, sand: 0xedcfa4, cloud: 0xfbe7d6, shadow: 0x6a4c78, floor: 0xead8c6, corridor: 0xf5e6d6,
    wall: 0xefdac8, wallShade: 0xd8bea8, wallTop: 0xf8eadc, glass: 0x7d6f8c, slab: 0xd1b8a2, slabShade: 0xb89f89, threshold: 0xd9c2ac,
    top: 0xf6e3d0, left: 0xcfb29c, right: 0xb39580, edge: 0x5e4a52,
    hover: 0xffe7b8, pick: 0xffc46b, inside: 0x9bb8ff, ok: 0x5cb85c, no: 0xd9534f, dim: 0x9a9a9a,
    glow: 0xffd48a, label: { color: '#3d2f36', background: '#fff6ecdd' }, miniSky: 0xf7e6d6,
  },
  night: {
    sky: 0x1d2033, ground: 0x2c3049, groundEdge: 0x3c4160, line: 0x353a56, line4: 0x41466a, earth: 0x5a4030, earthShade: 0x46301f, ink: 0x0f111c,
    grass: 0x2a4232, lot: 0x33503e, asphalt: 0x262a40, kerb: 0x3a3e5c, paint: 0xd8c86a,
    grassAlt: 0x263c2d, curb: 0x14162a, sea: 0x1d3550, wave: 0x3c5a7c, sand: 0x4a4636, cloud: 0x3a3f5e, shadow: 0x05060c, floor: 0x2c3049, corridor: 0x363b58,
    wall: 0x3d4263, wallShade: 0x30344e, wallTop: 0x4a4f72, glass: 0xffd27a, slab: 0x30344e, slabShade: 0x262a40, threshold: 0x262a40,
    top: 0x3d4263, left: 0x2c3049, right: 0x23263b, edge: 0x10121e,
    hover: 0x56609a, pick: 0xd9a83a, inside: 0x4f7fd9, ok: 0x4caf6a, no: 0xd0544f, dim: 0x6a6a7a,
    glow: 0xffd27a, label: { color: '#e6e3f2', background: '#1d2033dd' }, miniSky: 0x24283d,
  },
}

/**
 * The ground's colours (the Owner, 2026-11-20: one ground everywhere; grass is empty, colour is something
 * standing there): the belt (grass), a plot's lot and the slab's sides.
 */
export type TerrainTone = { grass: number; lot: number; earth: number; earthShade: number }
export const TERRAIN: Record<'meadow', Record<Light, TerrainTone>> = {
  meadow: {
    day: { grass: 0xd6e8c1, lot: 0xe3efd5, earth: 0xcf9f6c, earthShade: 0xab7c4e },
    dusk: { grass: 0xb9c48c, lot: 0xdde0b8, earth: 0xb9774b, earthShade: 0x93593a },
    night: { grass: 0x2a4232, lot: 0x33503e, earth: 0x5a4030, earthShade: 0x46301f },
  },
}

/** CSS variables for the corners, per light. */
export const CORNERS: Record<Light, Record<string, string>> = {
  day: { '--g2-sky': '#f3efe6', '--g2-card': '#fffffff2', '--g2-line': '#d8d2c4', '--g2-ink': '#2f2d3a', '--g2-soft': '#5d5868', '--g2-mute': '#8a8478', '--g2-link': '#3d6fd8', '--g2-chip': '#ece7db', '--g2-menu': '#2f2d3aee', '--g2-menu-ink': '#e9e4d8', '--g2-ink-line': '#2f2d3a' },
  dusk: { '--g2-sky': '#f1d9c4', '--g2-card': '#fff7eef2', '--g2-line': '#dcc3ad', '--g2-ink': '#3d2f36', '--g2-soft': '#6b5560', '--g2-mute': '#94776a', '--g2-link': '#3a62c4', '--g2-chip': '#f0dccb', '--g2-menu': '#3d2f36ee', '--g2-menu-ink': '#f4e4d6', '--g2-ink-line': '#3d2f36' },
  night: { '--g2-sky': '#1d2033', '--g2-card': '#262a40f2', '--g2-line': '#3c4160', '--g2-ink': '#e6e3f2', '--g2-soft': '#b4b1c8', '--g2-mute': '#8d8aa6', '--g2-link': '#8fb3ff', '--g2-chip': '#353a58', '--g2-menu': '#0f111ce8', '--g2-menu-ink': '#d9d6ea', '--g2-ink-line': '#0b0c16' },
}
