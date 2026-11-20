import type { LandmarkId } from './library/landmarkLooks'
import type { Partners } from './links'
import type { Shape } from './grid'
import type { Sprite } from './kit'

/**
 * The shape of a place, as the frame knows it (the Owner, 2026-11-19: the frame is the skeleton,
 * like Bootstrap; what the places are and hold is content, a plug-in, changed without touching the
 * frame). Content builds a tree of these; the frame draws it, walks it and addresses it.
 */

export type PlaceKind = 'town' | 'district' | 'building' | 'room'

/**
 * How a thing stands: the only four words (RULES.md 13). City runs in reverse (rule 0): these come
 * from the system, never from a person.
 *   online   it is there and reporting
 *   standby  it is off on purpose and can be woken (not a fault)
 *   down     it should be there and is not
 *   unread   it exists, but nothing reads it: say so, never guess
 */
export const STATES = ['online', 'standby', 'down', 'unread'] as const
export type State = (typeof STATES)[number]

/** A reading (RULES.md 12): how much is used of how much, in a unit, and where it was read. Null is not read, never zero. */
export type Meter = { used: number | null; of: number | null; unit: 'cores' | 'bytes'; source: string }

/** What a building looks like, for content with more than one kind of thing (hardware: RULES.md §2.4). */
export const LOOKS = ['linux', 'windows', 'mac', 'storage', 'network'] as const
export type Look = (typeof LOOKS)[number]

export type Place = {
  /** The path segment: unique among its siblings. */
  id: string
  kind: PlaceKind
  /** Its footprint on its parent's 16×16 ground (1×1 … 8×8): a design decision by its weight, made here, never on the page. */
  shape: Shape
  label: string
  /** The business question the place answers. */
  question: string
  /** The same place in the dashboard style, or null when it has none of its own yet. */
  dashboard: string | null
  /**
   * The function it serves (one of the content's functions, content.ts), if it has its own; a place
   * without one serves its parent's. The menu lights a function up across the map (the Owner,
   * 2026-11-19: function and domain are two views of one system; the 2.5D shows both).
   */
  function?: string
  /**
   * The domain's own ground plan for what it holds (grid.ts: a plan string and which plot each thing
   * stands in), used until the Owner arranges the place himself. It keeps the frame's cutting rules;
   * the frame checks it (rules.ts).
   */
  arrangement?: { plan: string; at: Record<string, string> }
  /** Its state and reading, read from the system (optional: greybox places have none). */
  state?: State
  meter?: Meter
  /** The physical units a thing is made of, each with how it stands (a NAS volume: the disks of its array). Read, never assumed. */
  units?: State[]
  look?: Look
  /** A district: which of its things stands for it on the town (its landmark); else its hall's landmark, else its largest. */
  icon?: string
  /** Whom it deals with on purpose, up to three a side (links.ts Partners); the rest is undeclared and goes to Other. */
  partners?: Partners
  /** A first-generation landmark it stands as (library/landmarkLooks.ts); content says which, and why. */
  landmark?: LandmarkId
  /**
   * What its surfaces show (the Owner, 2026-11-19: every roof and wall is a place to show something;
   * waste none): the roof its number, seen from far off; the front its sign, what it is for; a wall
   * a tally, how many of something it holds now (a reading, with its source). All read, none set.
   */
  face?: { roof?: string; sign?: string; tally?: { n: number | null; what: string; source: string } }
  /**
   * What it looks like inside, when its content draws it (a room's own module: the CPU's workshop, the
   * memory's cold store, the disk's warehouse; the Owner, 2026-11-19: put the rooms into use, refine them
   * later, one module at a time). `key` changes whenever the picture would; `make` draws it for the
   * footprint it stands in. Without one, the frame draws its plain room. `scale` says the ruler it is drawn
   * at when that is coarser than the standard (one machine for 2 cores): shown on its name, never hidden.
   */
  picture?: { key: string; w: number; d: number; make: () => Sprite; scale?: string }
  children: Place[]
}

export type Path = string[]

/** The function a place serves: its own, or the nearest one above it. */
export function functionAt(tree: Place, path: Path): string | undefined {
  let found = tree.function
  let node: Place | undefined = tree
  for (const seg of path) {
    node = node?.children.find(c => c.id === seg)
    if (!node) break
    if (node.function) found = node.function
  }
  return found
}

/**
 * Whether a place is wired to the system (the Owner, 2026-11-19: what is real is drawn solid; what is not
 * there yet, see-through, "虚空", so it is plain which is which). A room is real when it shows a reading
 * (its module's picture, or a reading read); anything else when the system told its state, or something
 * inside it is real. A place with nothing read in it is a ghost: its name stands, nothing more.
 */
export function isWired(p: Place): boolean {
  if (p.kind === 'room') return !!p.picture || (p.meter?.used != null)
  if (p.state && p.state !== 'unread') return true
  return p.children.some(isWired)
}

export const TOWN_ID = 'town'

/** The places along a path, town first; stops at the first segment that is not there. */
export function trail(tree: Place, path: Path): Place[] {
  const out = [tree]
  for (const seg of path) {
    const next = out[out.length - 1].children.find(c => c.id === seg)
    if (!next) break
    out.push(next)
  }
  return out
}

/** A path made valid: the part of it that exists. */
export const resolve = (tree: Place, path: Path): Path => trail(tree, path).slice(1).map(p => p.id)

export const here = (tree: Place, path: Path): Place => trail(tree, path).at(-1)!

/** `#/system/infrastructure` ⇄ ['system', 'infrastructure'] — every place has an address, so Back works and a link can be sent. */
export const parsePath = (hash: string): Path => hash.replace(/^#\/?/, '').split('/').filter(Boolean).map(decodeURIComponent)
export const formatPath = (path: Path): string => `#/${path.map(encodeURIComponent).join('/')}`

/** A meter in words: '21%  3.4 of 16 cores', '5.0 of 22.8 GB', or 'unread'. */
export function meterText(m: Meter | undefined): string | null {
  if (!m) return null
  if (m.of == null && m.used == null) return 'unread'
  const n = (v: number) => m.unit === 'cores' ? `${v < 10 ? v.toFixed(1) : Math.round(v)}` : bytes(v)
  const unit = m.unit === 'cores' ? ' cores' : ''
  if (m.used == null) return `— of ${n(m.of!)}${unit}`
  if (m.of == null) return `${n(m.used)}${unit}`
  return `${Math.round(100 * m.used / m.of)}%  ${n(m.used)} of ${n(m.of)}${unit}`
}

const bytes = (v: number) => {
  const u = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let i = 0
  while (v >= 1000 && i < u.length - 1) { v /= 1000; i++ }
  return `${v < 10 ? v.toFixed(1) : Math.round(v)} ${u[i]}`
}
