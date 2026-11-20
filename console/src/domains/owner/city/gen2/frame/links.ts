import { trail, type Path, type Place } from './places'

/**
 * How places talk to one another (the Owner, 2026-11-20): the frame offers the ways, the content says
 * which of its connections takes which way, and the frame decides how much of it a level shows.
 *
 * Two ways, as the system talks today (the Owner: "目前系统的交互是 Bus 和指标两种，2 种足够"):
 *   bus     a message one place sends and others receive: a bus line on the road, stops at both ends
 *   metric  a reading one place gives and another reads on a schedule: a railway, the train on its timetable
 *
 * A connection runs from one place to another anywhere in the tree. Each level shows it at its own grain
 * (like the 16×16 ground, a fixed budget, so a level is never too busy nor too bare):
 *   inside  both ends are under this place, in two different children: one line between those two
 *           children, one per way and pair, however many connections it carries (its count is its weight)
 *   gate    one end is under this place, the other outside: a station on this ground's edge named for the
 *           place outside at the level beside this one, and a line from it to the child it serves
 *   hidden  both ends under the same child: that child's own business, shown when you go in
 * At most LINES lines a level, the heaviest first; the rest are counted. A district's edge takes its stations
 * through edgeSlots: CROSSINGS named crossings, the rest and the unclear in Other, the unowned in Orphan.
 */

export type Way = 'bus' | 'metric'
export const WAYS: readonly Way[] = ['bus', 'metric']

export type Link = {
  way: Way
  from: Path
  to: Path
  /** What it carries (an event type, a metric name). */
  what: string
  /** Where the connection is declared: an API path or a registry, so it is read, not drawn by hand. */
  source: string
  /**
   * How clear it is (the Owner, 2026-11-20: show the mess, never hide it). Unclear: not important, dirty, or
   * not understood yet; it passes through the Other crossing. Orphan: its far end belongs to no one; the far
   * end is not a place, and it passes through the Orphan crossing.
   */
  clarity?: 'clear' | 'unclear' | 'orphan'
}

/** The fixed crossings of a district's edge: this many named ones, then Other and Orphan (the Owner, 2026-11-20). */
export const CROSSINGS = 4
export const OTHER = 'other'
export const ORPHAN = 'orphan'

/** The budget of a level. */
export const LINES = 12
export const GATES = 8

/** A line between two children of the place on screen. */
export type Line = { way: Way; a: string; b: string; both: boolean; carries: string[] }
/** A station on the edge: the outside place it leads to, which child it serves, and which way traffic goes. */
export type Gate = { way: Way; outside: Path; label: string; child: string; dir: 'in' | 'out' | 'both'; carries: string[]; ins: number; outs: number; clarity: 'clear' | 'unclear' | 'orphan' }

export type LevelLinks = { lines: Line[]; gates: Gate[]; more: { lines: number; gates: number } }

const under = (p: Path, at: Path) => at.length <= p.length && at.every((s, i) => p[i] === s)

/** The outside end named at the level beside this one: the town's district, else this place's sibling or what it sits in. */
function outsideOf(end: Path, at: Path): Path {
  let i = 0
  while (i < end.length && i < at.length && end[i] === at[i]) i++
  return end.slice(0, i + 1)
}

export function linksAt(tree: Place, at: Path, links: readonly Link[]): LevelLinks {
  const lines = new Map<string, { line: Line; dirs: Set<boolean> }>()
  const gates = new Map<string, Gate>()
  const d = at.length
  for (const l of links) {
    const fi = under(l.from, at) && l.from.length > d, ti = under(l.to, at) && l.to.length > d
    const clarity = l.clarity ?? 'clear'
    if (clarity === 'orphan') {
      // Its own end is here (from); the other belongs to no one.
      if (!fi) continue
      const child = l.from[d], k = `${l.way}|${ORPHAN}|${child}`
      const g = gates.get(k) ?? { way: l.way, outside: [ORPHAN], label: 'Orphan', child, dir: 'both', carries: [], ins: 0, outs: 0, clarity }
      g.carries.push(l.what); g.ins++; g.outs++
      gates.set(k, g)
      continue
    }
    if (fi && ti) {
      const a = l.from[d], b = l.to[d]
      if (a === b) continue
      const [x, y] = a < b ? [a, b] : [b, a]
      const k = `${l.way}|${x}|${y}`
      const e = lines.get(k) ?? { line: { way: l.way, a: x, b: y, both: false, carries: [] }, dirs: new Set<boolean>() }
      // Both ways when the pair carries traffic in each direction.
      e.dirs.add(a === x)
      e.line.both = e.dirs.size === 2
      e.line.carries.push(l.what)
      lines.set(k, e)
    } else if (fi || ti) {
      const inner = fi ? l.from : l.to, outer = fi ? l.to : l.from
      const outside = outsideOf(outer, at)
      const child = inner[d]
      const k = `${l.way}|${outside.join('/')}|${child}|${clarity}`
      const g = gates.get(k) ?? { way: l.way, outside, label: trail(tree, outside).at(-1)?.label ?? outside.at(-1) ?? '', child, dir: fi ? 'out' : 'in', carries: [], ins: 0, outs: 0, clarity }
      if ((g.dir === 'out' && !fi) || (g.dir === 'in' && fi)) g.dir = 'both'
      g.carries.push(l.what)
      if (fi) g.outs++; else g.ins++
      gates.set(k, g)
    }
  }
  const heavy = <T extends { carries: string[] }>(xs: T[]) => xs.sort((p, q) => q.carries.length - p.carries.length)
  const ls = heavy([...lines.values()].map(e => e.line))
  const gs = heavy([...gates.values()])
  // Lines past the budget are counted; every gate is kept, for the edge puts what is past its named crossings into Other.
  return { lines: ls.slice(0, LINES), gates: gs, more: { lines: Math.max(0, ls.length - LINES), gates: Math.max(0, gs.length - GATES) } }
}

/** The rules a content's connections keep (rules.ts lists them with the rest). */
export function linkRulesBroken(tree: Place, links: readonly Link[]): string[] {
  const out: string[] = []
  const exists = (p: Path) => p.length > 0 && trail(tree, p).length === p.length + 1
  for (const l of links) {
    const name = `${l.way} ${l.what}`
    if (!(WAYS as readonly string[]).includes(l.way)) out.push(`${name}: the way must be one of ${WAYS.join(', ')}`)
    if (!exists(l.from)) out.push(`${name}: its start ${l.from.join('/')} is not a place`)
    if (l.clarity !== 'orphan' && !exists(l.to)) out.push(`${name}: its end ${l.to.join('/')} is not a place`)
    if (!/^\/api\/|\.(ya?ml|json)$/.test(l.source)) out.push(`${name}: a connection is read from an API path or a registry file, not "${l.source}"`)
  }
  return out
}

/** What passes one crossing: whom it leads to, and how many things go in and out, by way. */
export type Pass = { id: string; label: string; bus: { in: number; out: number }; metric: { in: number; out: number }; n: number; who: string[] }

/**
 * A district's edge (the Owner, 2026-11-20: a fixed number of crossings, plus Other and Orphan, so the mess
 * shows and its size is counted): one named crossing per district it deals with clearly, the heaviest
 * CROSSINGS of them; everything unclear and every district past those goes through Other; whatever belongs
 * to no one through Orphan. `total` is every thing passing, so Other's and Orphan's share can be told.
 */
export function edgeSlots(gates: readonly Gate[]): { named: Pass[]; other: Pass | null; orphan: Pass | null; total: number } {
  const add = (p: Pass, g: Gate) => { p[g.way].in += g.ins; p[g.way].out += g.outs; p.n += g.carries.length }
  const fresh = (id: string, label: string): Pass => ({ id, label, bus: { in: 0, out: 0 }, metric: { in: 0, out: 0 }, n: 0, who: [] })
  const clear = new Map<string, Pass>()
  const other = fresh(OTHER, 'Other'), orphan = fresh(ORPHAN, 'Orphan')
  for (const g of gates) {
    if (g.clarity === 'orphan') { add(orphan, g); continue }
    if (g.clarity === 'unclear') { add(other, g); if (!other.who.includes(g.label)) other.who.push(g.label); continue }
    const id = g.outside[0]
    const p = clear.get(id) ?? fresh(id, g.label)
    add(p, g)
    clear.set(id, p)
  }
  const ranked = [...clear.values()].sort((a, b) => b.n - a.n)
  for (const p of ranked.slice(CROSSINGS)) {
    other.bus.in += p.bus.in; other.bus.out += p.bus.out; other.metric.in += p.metric.in; other.metric.out += p.metric.out
    other.n += p.n; other.who.push(p.label)
  }
  const total = gates.reduce((t, g) => t + g.carries.length, 0)
  return { named: ranked.slice(0, CROSSINGS), other: other.n ? other : null, orphan: orphan.n ? orphan : null, total }
}
