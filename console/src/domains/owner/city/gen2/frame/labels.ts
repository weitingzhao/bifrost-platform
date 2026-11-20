/**
 * Where the plates go so none covers another (the Owner, 2026-11-20: labels of any size, on blocks that move;
 * a program, not hand-placed). Pure, so it is tested on its own and runs on every draw:
 *   fixed    keep their places (the port signs: the district's edge, its contract)
 *   movable  each, in the order given, takes the first free place in its own column: where it is, then one
 *            row down, one up, two down, ... up to `reach` rows; a row is its own height plus the gap
 *   none     free within reach: it stays where it is and is marked, to be drawn see-through
 */
export type Box = { x: number; y: number; w: number; h: number }
export type Placement = { dy: number; free: boolean }

export function placeLabels(fixed: readonly Box[], movable: readonly Box[], gap = 3, reach = 3): Placement[] {
  const hits = (a: Box, b: Box) => a.x < b.x + b.w + gap && b.x < a.x + a.w + gap && a.y < b.y + b.h + gap && b.y < a.y + a.h + gap
  const taken: Box[] = [...fixed]
  const tries = [0, ...Array.from({ length: reach }, (_, i) => [i + 1, -(i + 1)]).flat()]
  return movable.map(b0 => {
    const step = b0.h + gap
    for (const k of tries) {
      const b = { ...b0, y: b0.y + k * step }
      if (!taken.some(t => hits(b, t))) { taken.push(b); return { dy: k * step, free: true } }
    }
    taken.push(b0)
    return { dy: 0, free: false }
  })
}
