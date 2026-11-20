import { describe, expect, it } from 'vitest'
import { CROSSINGS, GATES, LINES, SLOTS, edgeSlots, linksAt, linkRulesBroken, seat, type Link } from '../links'
import type { Place } from '../places'

// Origin: the Owner, 2026-11-20 ("框架提供交互方式，系统来配；Bus 和指标两种足够；每层的显示要有规范，
// 太多乱、太少抽象"). These pin the grain each level shows a connection at, and the budget.
const P = (id: string, kind: Place['kind'], children: Place[] = []): Place => ({ id, kind, shape: '2x2', label: id.toUpperCase(), question: '', dashboard: null, children })
const tree = P('town', 'town', [
  P('work', 'district', [P('releases', 'building'), P('hall', 'building')]),
  P('command', 'district', [P('hall', 'building'), P('records', 'building')]),
])
const L = (way: Link['way'], from: string, to: string, what: string): Link => ({ way, from: from.split('/'), to: to.split('/'), what, source: '/api/v1/bus/contract' })

describe('a connection at each level’s grain', () => {
  const links = [
    L('bus', 'work/releases', 'command/hall', 'release.finished'),
    L('bus', 'work/releases', 'command/records', 'release.recorded'),
    L('bus', 'command/hall', 'work/releases', 'approval.granted'),
    L('metric', 'work/hall', 'command/hall', 'ci_runs_total'),
    L('bus', 'work/hall', 'work/releases', 'ci.passed'),
  ]
  it('on the town: one line per pair and way, both ways when traffic runs each way, weighed by what it carries', () => {
    const t = linksAt(tree, [], links)
    expect(t.lines).toEqual([
      { way: 'bus', a: 'command', b: 'work', both: true, carries: ['release.finished', 'release.recorded', 'approval.granted'] },
      { way: 'metric', a: 'command', b: 'work', both: false, carries: ['ci_runs_total'] },
    ])
    expect(t.gates).toEqual([])
  })
  it('inside a district: its own lines, and a station named for the district outside, serving the building', () => {
    const w = linksAt(tree, ['work'], links)
    expect(w.lines).toEqual([{ way: 'bus', a: 'hall', b: 'releases', both: false, carries: ['ci.passed'] }])
    const bus = w.gates.find(g => g.way === 'bus' && g.child === 'releases')!
    expect(bus).toMatchObject({ outside: ['command'], label: 'COMMAND', dir: 'both' })
    expect(bus.carries).toHaveLength(3)
    expect([bus.ins, bus.outs]).toEqual([1, 2])
    expect(w.gates.find(g => g.way === 'metric')).toMatchObject({ child: 'hall', dir: 'out' })
  })
  it('a level shows at most its budget, heaviest first, and counts the rest', () => {
    const many = Array.from({ length: LINES + GATES + 5 }, (_, i) => L('bus', 'work/releases', 'command/hall', `e${i}`))
    expect(linksAt(tree, ['work'], many).gates).toHaveLength(1)
    const wide: Place = P('town', 'town', Array.from({ length: 8 }, (_, i) => P(`d${i}`, 'district')))
    const pairs: Link[] = []
    for (let i = 0; i < 8; i++) for (let j = i + 1; j < 8; j++) pairs.push(L('bus', `d${i}`, `d${j}`, `${i}-${j}`))
    const t = linksAt(wide, [], pairs)
    expect(t.lines).toHaveLength(LINES)
    expect(t.more.lines).toBe(28 - LINES)
  })
})

describe('a content’s connections keep the frame’s rules', () => {
  it('a way the frame does not offer, an end that is not a place, or a hand-written source is refused', () => {
    expect(linkRulesBroken(tree, [L('bus', 'work/releases', 'command/hall', 'x')])).toEqual([])
    const bad = linkRulesBroken(tree, [{ way: 'boat' as Link['way'], from: ['work', 'nowhere'], to: ['command'], what: 'x', source: 'I drew it' }])
    expect(bad).toHaveLength(3)
  })
})

// Origin: the Owner, 2026-11-20 ("固定格子 + Other + Orphan；乱的不藏，量出多大"). These pin that the edge keeps
// CROSSINGS named crossings, puts the unclear and the rest into Other, the unowned into Orphan, and counts all.
describe('a district’s edge: named crossings, Other, Orphan', () => {
  const town: Place = P('town', 'town', ['me', 'a', 'b', 'c', 'd', 'e'].map(id => P(id, 'district', [P('x', 'building')])))
  const links: Link[] = [
    ...['a', 'a', 'a', 'b', 'b', 'c', 'd', 'e'].map((to, i) => L('bus', 'me/x', `${to}/x`, `e${i}`)),
    { ...L('bus', 'me/x', 'a/x', 'fuzzy'), clarity: 'unclear' },
    { way: 'bus', from: ['me', 'x'], to: [], what: 'unowned', source: '/api/v1/bus/contract', clarity: 'orphan' },
  ]
  it('keeps the heaviest named, the rest with the unclear in Other, the unowned in Orphan', () => {
    const s = edgeSlots(linksAt(town, ['me'], links).gates)
    expect(s.named.map(p => p.id)).toHaveLength(CROSSINGS)
    expect(s.named[0]).toMatchObject({ id: 'a', n: 3 })
    expect(s.other).toMatchObject({ n: 2 })
    expect(s.orphan).toMatchObject({ n: 1 })
    expect(s.total).toBe(10)
  })
  it('an orphan’s far end need not be a place, and nothing is dropped', () => {
    expect(linkRulesBroken(town, links)).toEqual([])
  })
})

// Origin: the Owner, 2026-11-20 ("格子之外预设固定的格子，现在没有就空着，将来填满；边界和规则先定死"). These pin
// the fixed slots outside a district and how a neighbour takes the side facing it.
describe('the fixed slots outside a district', () => {
  const pass = (id: string) => ({ id, label: id, bus: { in: 0, out: 0 }, metric: { in: 0, out: 0 }, n: 1, who: [] })
  it('are six: one named on each side, then Other and Orphan', () => {
    expect(SLOTS.map(s => s.id).sort()).toEqual(['orphan', 'other', 'x0', 'x1', 'y0', 'y1'])
  })
  it('a neighbour takes the side facing it; when that is taken, the free side nearest that way', () => {
    const dir: Record<string, { dx: number; dy: number }> = { east: { dx: 5, dy: 0 }, east2: { dx: 4, dy: 1 }, north: { dx: 0, dy: -3 } }
    const s = seat([pass('east'), pass('east2'), pass('north')], id => dir[id])
    expect(s.get('east')).toBe('x1')
    expect(s.get('east2')).toBe('y1')
    expect(s.get('north')).toBe('y0')
  })
})
