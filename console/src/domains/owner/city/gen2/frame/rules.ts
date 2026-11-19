import { area, GRID, sidesOf, SHAPES, TAPPABLE, type Shape } from './grid'
import type { Place, PlaceKind } from './places'

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
 */

const CHILD: Record<PlaceKind, PlaceKind | null> = { town: 'district', district: 'building', building: 'room', room: null }

export function rulesBroken(content: { tree: Place; functions?: { id: string }[] }): string[] {
  const out: string[] = []
  const fns = content.functions ? new Set(content.functions.map(f => f.id)) : null
  if (content.tree.kind !== 'town') out.push(`the top place must be a town, not a ${content.tree.kind}`)
  const walk = (p: Place, where: string) => {
    if (!(SHAPES as readonly string[]).includes(p.shape)) out.push(`${where}: shape ${String(p.shape)} is not one of ${SHAPES.join(', ')}`)
    else if ((p.kind === 'district' || p.kind === 'building') && sidesOf(p.shape as Shape).short < TAPPABLE) out.push(`${where}: a ${p.kind} is gone into, so it must be at least ${TAPPABLE} wide (it is ${p.shape})`)
    if (p.function && fns && !fns.has(p.function)) out.push(`${where}: function ${p.function} is not one of the content's functions`)
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
