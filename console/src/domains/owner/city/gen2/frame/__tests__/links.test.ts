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

// Origin: the Owner, 2026-11-20 ("两边就够：一边进一边出，每边 3 个口子加 Other，再加一个 Orphan"; then "先立规矩":
// the named ports are the partners a place declares, not the busiest). These pin the sides, the declared
// ports, that undeclared traffic goes to Other, and that nothing is dropped.
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
  it('the declared are the named ports in their order, even with nothing passing; the undeclared and the unclear go to Other', () => {
    const s = edgeSlots(linksAt(town, ['me'], links).gates, town.children[0].partners, label)
    expect(s.out.named.map(p => [p.id, p.n])).toEqual([['a', 3], ['b', 2], ['z', 0]])
    expect(s.out.other).toMatchObject({ n: 4 })
    expect(s.in.named).toEqual([expect.objectContaining({ id: 'a', metric: 1, n: 1 })])
    expect(s.orphan).toMatchObject({ n: 1 })
    expect(s.total).toBe(11)
  })
  it('a place that declares no one sends everything through Other', () => {
    const s = edgeSlots(linksAt(town, ['me'], links).gates, undefined, label)
    expect(s.out.named).toEqual([])
    expect(s.out.other).toMatchObject({ n: 9 })
  })
  it('an orphan’s far end need not be a place; a declared partner must be a place beside it', () => {
    expect(linkRulesBroken(town, links)).toEqual(['me: partner z is not a place beside it nor the world outside'])
  })
  // The Owner, 2026-11-20: the front edges are what is clean, the back edges what is yet to be cleared.
  it('the frame sets nine places outside: three in at the front left, three out at the front right, the two Others at the back left, Orphan at the back right', () => {
    expect(SLOTS.filter(s => s.edge === 'y1').map(s => s.side)).toEqual(['in', 'in', 'in'])
    expect(SLOTS.filter(s => s.edge === 'x1').map(s => s.side)).toEqual(['out', 'out', 'out'])
    expect(SLOTS.filter(s => s.edge === 'x0').map(s => [s.side, s.i])).toEqual([['in', PORTS], ['out', PORTS]])
    expect(SLOTS.filter(s => s.edge === 'y0').map(s => s.side)).toEqual(['orphan'])
  })
})

// Origin: the Owner, 2026-11-20 ("先做第一条和第二条"): most traffic is with users outside the system, which
// are not places; they need a place to show. These pin how the outside world shows on the town and inside it.
describe('the world outside the town', () => {
  const town: Place = P('town', 'town', [P('a', 'district', [P('x', 'building')]), P('b', 'district', [P('x', 'building')])])
  const parties = [{ id: 'console', label: 'Console' }, { id: 'mcp', label: 'MCP' }]
  const links: Link[] = [L('bus', '~/console', 'a/x', 'asked'), L('bus', '~/mcp', 'a/x', 'called'), L('metric', 'b/x', '~/console', 'read')]
  it('on the town’s edge each party is its own port', () => {
    const g = linksAt(town, [], links, parties).gates
    expect(g.map(x => [x.outside.join('/'), x.label, x.ins, x.outs]).sort()).toEqual([['~/console', 'Console', 0, 1], ['~/console', 'Console', 1, 0], ['~/mcp', 'MCP', 1, 0]])
  })
  it('inside the town all of outside comes through one port', () => {
    const g = linksAt(town, ['a'], links, parties).gates
    expect(new Set(g.map(x => x.outside.join('/')))).toEqual(new Set(['~']))
    expect(g.reduce((t, x) => t + x.ins, 0)).toBe(2)
  })
  it('an end outside must be a declared party', () => {
    expect(linkRulesBroken(town, [L('bus', '~/nobody', 'a/x', 'x')], parties)).toEqual(['bus x: its start ~/nobody is not a place'])
  })
})
