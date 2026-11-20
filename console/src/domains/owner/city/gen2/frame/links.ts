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
 * At most LINES lines and GATES gates a level, the heaviest first; the rest are counted, not drawn.
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
}

/** The budget of a level. */
export const LINES = 12
export const GATES = 8

/** A line between two children of the place on screen. */
export type Line = { way: Way; a: string; b: string; both: boolean; carries: string[] }
/** A station on the edge: the outside place it leads to, which child it serves, and which way traffic goes. */
export type Gate = { way: Way; outside: Path; label: string; child: string; dir: 'in' | 'out' | 'both'; carries: string[] }

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
      const k = `${l.way}|${outside.join('/')}|${child}`
      const g = gates.get(k) ?? { way: l.way, outside, label: trail(tree, outside).at(-1)?.label ?? outside.at(-1) ?? '', child, dir: fi ? 'out' : 'in', carries: [] }
      if ((g.dir === 'out' && !fi) || (g.dir === 'in' && fi)) g.dir = 'both'
      g.carries.push(l.what)
      gates.set(k, g)
    }
  }
  const heavy = <T extends { carries: string[] }>(xs: T[]) => xs.sort((p, q) => q.carries.length - p.carries.length)
  const ls = heavy([...lines.values()].map(e => e.line))
  const gs = heavy([...gates.values()])
  return { lines: ls.slice(0, LINES), gates: gs.slice(0, GATES), more: { lines: Math.max(0, ls.length - LINES), gates: Math.max(0, gs.length - GATES) } }
}

/** The rules a content's connections keep (rules.ts lists them with the rest). */
export function linkRulesBroken(tree: Place, links: readonly Link[]): string[] {
  const out: string[] = []
  const exists = (p: Path) => p.length > 0 && trail(tree, p).length === p.length + 1
  for (const l of links) {
    const name = `${l.way} ${l.what}`
    if (!(WAYS as readonly string[]).includes(l.way)) out.push(`${name}: the way must be one of ${WAYS.join(', ')}`)
    if (!exists(l.from)) out.push(`${name}: its start ${l.from.join('/')} is not a place`)
    if (!exists(l.to)) out.push(`${name}: its end ${l.to.join('/')} is not a place`)
    if (!/^\/api\/|\.(ya?ml|json)$/.test(l.source)) out.push(`${name}: a connection is read from an API path or a registry file, not "${l.source}"`)
  }
  return out
}
