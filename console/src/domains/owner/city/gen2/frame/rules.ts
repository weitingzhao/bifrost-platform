import { area, assign, decodePlan, GRID, plotsOf, sidesOf, SHAPES, TAPPABLE, type Shape } from './grid'
import { LOOKS, STATES, type Place, type PlaceKind } from './places'

/**
 * The frame's rules, checked on the content it is given (the Owner, 2026-11-19: the ground, its
 * cuts and the sizes are the frame's, fixed; a domain that wants this look uses them as they are
 * and cannot change them). Content that breaks one is not drawn: the frame lists what it broke, so
 * the domain fixes its content, not the frame.
 *
 *   levels   a town holds districts, a district buildings, a building rooms; nothing below a room
 *   shapes   every place is one of the frame's shapes: 1×1, 1×2, 2×2, 2×4, 4×4, 4×8 or 8×8
 *   doors    a place you go into (a district, a building) is at least 2 wide, for a finger
 *   ground   what a place holds fits its 16×16 ground
 *   names    ids are unique among siblings and safe in an address
 *   function a place's function, if it says one, is one of the content's functions
 *   states   a state is one of the four words; a look one of the five
 *   readings a meter says where it was read; numbers are not negative and used is not past its whole;
 *            an unread thing has no number (unread is not zero)
 *   plan     a domain's own ground plan keeps the cutting rules, and everything it holds finds its plot
 *   made up  a number must come from a read: its source is a query or an API path, never words
 *            (the Owner, 2026-11-19: nothing on the screen is made up or written in once and left)
 */

/** A source a number can come from: a PromQL query (a function or a selector) or a platform API path. */
const READ_SOURCE = /^\/api\/|[a-z_:]+\s*\(|[a-z_:]+\{/i

const CHILD: Record<PlaceKind, PlaceKind | null> = { town: 'district', district: 'building', building: 'room', room: null }

export function rulesBroken(content: { tree: Place; functions?: { id: string }[] }): string[] {
  const out: string[] = []
  const fns = content.functions ? new Set(content.functions.map(f => f.id)) : null
  if (content.tree.kind !== 'town') out.push(`the top place must be a town, not a ${content.tree.kind}`)
  const walk = (p: Place, where: string) => {
    if (!(SHAPES as readonly string[]).includes(p.shape)) out.push(`${where}: shape ${String(p.shape)} is not one of ${SHAPES.join(', ')}`)
    else if ((p.kind === 'district' || p.kind === 'building') && sidesOf(p.shape as Shape).short < TAPPABLE) out.push(`${where}: a ${p.kind} is gone into, so it must be at least ${TAPPABLE} wide (it is ${p.shape})`)
    if (p.function && fns && !fns.has(p.function)) out.push(`${where}: function ${p.function} is not one of the content's functions`)
    if (p.state && !(STATES as readonly string[]).includes(p.state)) out.push(`${where}: state ${p.state} is not one of ${STATES.join(', ')}`)
    if (p.look && !(LOOKS as readonly string[]).includes(p.look)) out.push(`${where}: look ${p.look} is not one of ${LOOKS.join(', ')}`)
    const t = p.face?.tally
    if (t && t.n != null && (t.n < 0 || !READ_SOURCE.test(t.source))) out.push(`${where}: a tally is a count read from a query or an API path, not "${t.source}"`)
    for (const u of p.units ?? []) if (!(STATES as readonly string[]).includes(u)) out.push(`${where}: a unit's state ${u} is not one of ${STATES.join(', ')}`)
    const m = p.meter
    if (m) {
      if (!m.source) out.push(`${where}: a reading must say where it was read`)
      if (m.unit !== 'cores' && m.unit !== 'bytes') out.push(`${where}: unit ${String(m.unit)} is not cores or bytes`)
      if ((m.used != null && m.used < 0) || (m.of != null && m.of < 0)) out.push(`${where}: a reading is not negative`)
      if (m.used != null && m.of != null && m.used > m.of * 1.05) out.push(`${where}: used ${m.used} is past its whole ${m.of}`)
      if (p.state === 'unread' && m.used != null) out.push(`${where}: an unread thing has no reading (unread is not zero)`)
      if ((m.used != null || m.of != null) && !READ_SOURCE.test(m.source)) out.push(`${where}: a number must come from a read (a query or an API path), not from "${m.source}"`)
    }
    if (p.arrangement) {
      const plan = decodePlan(p.arrangement.plan)
      if (plan === null) out.push(`${where}: its ground plan is not a plan of halvings`)
      else {
        const { homeless } = assign(p.children.filter(c => (SHAPES as readonly string[]).includes(c.shape)), plotsOf(plan), p.arrangement.at)
        if (homeless.length) out.push(`${where}: its ground plan has no plot for ${homeless.map(h => h.id).join(', ')}`)
      }
    }
    const want = CHILD[p.kind]
    const seen = new Set<string>()
    for (const c of p.children) {
      const at = `${where}/${c.id}`
      if (!c.id || /[/#?\s]/.test(c.id)) out.push(`${at}: an id must be non-empty, without / # ? or spaces`)
      if (seen.has(c.id)) out.push(`${at}: two places here share this id`)
      seen.add(c.id)
      if (want === null) out.push(`${at}: a room holds nothing; there is no level below it`)
      else if (c.kind !== want) out.push(`${at}: a ${p.kind} holds ${want}s, not a ${c.kind}`)
    }
    const valid = p.children.filter(c => (SHAPES as readonly string[]).includes(c.shape))
    if (area(valid) > GRID * GRID) out.push(`${where}: what it holds takes ${area(valid)} cells; its ground has ${GRID * GRID}`)
    for (const c of p.children) walk(c, `${where}/${c.id}`)
  }
  walk(content.tree, content.tree.id)
  return out
}
