import { describe, expect, it } from 'vitest'
import { GATES, LINES, PORTS, SLOTS, edgeSlots, linksAt, linkRulesBroken, type Link } from '../links'
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

// Origin: the Owner, 2026-11-20 ("两边就够：一边进一边出，每边 3 个口子加 Other，再加一个 Orphan；乱的不藏，量出多大").
// These pin the two sides, the fixed ports and that nothing is dropped.
describe('a district’s two sides: in and out, three ports and Other each, one Orphan', () => {
  const town: Place = P('town', 'town', ['me', 'a', 'b', 'c', 'd', 'e'].map(id => P(id, 'district', [P('x', 'building')])))
  const links: Link[] = [
    ...['a', 'a', 'a', 'b', 'b', 'c', 'd', 'e'].map((to, i) => L('bus', 'me/x', `${to}/x`, `e${i}`)),
    L('metric', 'a/x', 'me/x', 'reading'),
    { ...L('bus', 'me/x', 'a/x', 'fuzzy'), clarity: 'unclear' },
    { way: 'bus', from: ['me', 'x'], to: [], what: 'unowned', source: '/api/v1/bus/contract', clarity: 'orphan' },
  ]
  it('out keeps the heaviest three named, the rest with the unclear in Other; in has its own; the unowned in Orphan', () => {
    const s = edgeSlots(linksAt(town, ['me'], links).gates)
    expect(s.out.named.map(p => p.id)).toEqual(['a', 'b', expect.any(String)])
    expect(s.out.named).toHaveLength(PORTS)
    expect(s.out.other).toMatchObject({ n: 3 })
    expect(s.in.named).toEqual([expect.objectContaining({ id: 'a', metric: 1, n: 1 })])
    expect(s.in.other).toBeNull()
    expect(s.orphan).toMatchObject({ n: 1 })
    expect(s.total).toBe(11)
  })
  it('an orphan’s far end need not be a place', () => {
    expect(linkRulesBroken(town, links)).toEqual([])
  })
  it('the frame sets nine places outside: four in, four out, one Orphan', () => {
    expect(SLOTS.filter(s => s.side === 'in')).toHaveLength(PORTS + 1)
    expect(SLOTS.filter(s => s.side === 'out')).toHaveLength(PORTS + 1)
    expect(SLOTS.filter(s => s.side === 'orphan')).toHaveLength(1)
  })
})
