import { describe, expect, it } from 'vitest'
import { PORTS, SLOTS, edgeSlots, linksAt, linkRulesBroken, type Link } from '../links'
import type { Place } from '../places'

// Origin: the Owner, 2026-11-20 ("框架提供交互方式，系统来配；Bus 和指标两种足够；每层的显示要有规范，
// 太多乱、太少抽象"). These pin the grain each level shows a connection at, and the budget.
const P = (id: string, kind: Place['kind'], children: Place[] = []): Place => ({ id, kind, shape: '4x4', label: id.toUpperCase(), question: '', dashboard: null, children })
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
})

describe('a content’s connections keep the frame’s rules', () => {
  it('a way the frame does not offer, an end that is not a place, or a hand-written source is refused', () => {
    expect(linkRulesBroken(tree, [L('bus', 'work/releases', 'command/hall', 'x')])).toEqual([])
    const bad = linkRulesBroken(tree, [{ way: 'boat' as Link['way'], from: ['work', 'nowhere'], to: ['command'], what: 'x', source: 'I drew it' }])
    expect(bad).toHaveLength(3)
  })
})

// Origin: the Owner, 2026-11-20 ("两边就够：一边进一边出，每边 3 个口子加 Other，再加一个 Orphan"; later the
// same day the named ports went to the busiest partners of what stands on the ground shown, declared or not,
// the undeclared marked, replacing "only the declared"). These pin the sides, the ranking, and that nothing is dropped.
describe('a place’s two sides: its declared partners, Other, Orphan', () => {
  const town: Place = P('town', 'town', ['me', 'a', 'b', 'c', 'd', 'e'].map(id => P(id, 'district', [P('x', 'building')])))
  town.children[0].partners = { in: ['a'], out: ['a', 'b', 'z'] }
  const links: Link[] = [
    ...['a', 'a', 'a', 'b', 'b', 'c', 'd', 'e'].map((to, i) => L('bus', 'me/x', `${to}/x`, `e${i}`)),
    L('metric', 'a/x', 'me/x', 'reading'),
    { ...L('bus', 'me/x', 'a/x', 'fuzzy'), clarity: 'unclear' },
    { way: 'bus', from: ['me', 'x'], to: [], what: 'unowned', source: '/api/v1/bus/contract', clarity: 'orphan' },
  ]
  const label = (id: string) => id.toUpperCase()
  it('the busiest partners get the named ports, ties by declared order then name; the undeclared are marked; the rest and the unclear go to Other', () => {
    const s = edgeSlots(linksAt(town, ['me'], links).gates, town.children[0].partners, label)
    expect(s.out.named.map(p => [p.id, p.n, p.declared])).toEqual([['a', 3, true], ['b', 2, true], ['c', 1, false]])
    expect(s.out.other).toMatchObject({ n: 3 })
    expect(s.in.named).toEqual([expect.objectContaining({ id: 'a', metric: 1, n: 1, declared: true })])
    expect(s.orphan).toMatchObject({ n: 1 })
    expect(s.total).toBe(11)
  })
  it('a declared partner nothing passes to keeps a port when one is left over', () => {
    const quiet: Link[] = [L('bus', 'me/x', 'a/x', 'only')]
    const s = edgeSlots(linksAt(town, ['me'], quiet).gates, town.children[0].partners, label)
    expect(s.out.named.map(p => [p.id, p.n])).toEqual([['a', 1], ['b', 0], ['z', 0]])
  })
  it('a place that declares no one still shows its busiest partners, each marked undeclared', () => {
    const s = edgeSlots(linksAt(town, ['me'], links).gates, undefined, label)
    expect(s.out.named.map(p => [p.id, p.declared])).toEqual([['a', false], ['b', false], ['c', false]])
    expect(s.out.other).toMatchObject({ n: 3 })
  })
  it('an orphan’s far end need not be a place; a declared partner must be a place beside it', () => {
    expect(linkRulesBroken(town, links)).toEqual(['me: partner z is not a place beside it'])
  })
  // The Owner, 2026-11-20: the front edges are what is clean; what is yet to be cleared stands in a row on the back left.
  it('the frame sets nine places outside: three in at the front left, three out at the front right, Other in, Other out and Orphan on the back left', () => {
    expect(SLOTS.filter(s => s.edge === 'y1').map(s => s.side)).toEqual(['in', 'in', 'in'])
    expect(SLOTS.filter(s => s.edge === 'x1').map(s => s.side)).toEqual(['out', 'out', 'out'])
    expect(SLOTS.filter(s => s.edge === 'x0').map(s => s.side)).toEqual(['in', 'out', 'orphan'])
  })
})

// Origin: the Owner, 2026-11-20 ("city 就是整个系统，外面没有东西了"). This pins that the town has no outside:
// an end that is not a place is refused, and no partner can be declared outside.
describe('the town is the whole system', () => {
  const town: Place = P('town', 'town', [P('a', 'district', [P('x', 'building')])])
  it('an end outside the town is refused', () => {
    expect(linkRulesBroken(town, [L('bus', '~/console', 'a/x', 'x')])).toEqual(['bus x: its start ~/console is not a place'])
  })
})
