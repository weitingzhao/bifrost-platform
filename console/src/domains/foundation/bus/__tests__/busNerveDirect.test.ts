import { describe, expect, it } from 'vitest'
import { MIN_APART, alongAxis, arcCurve, arcsOf, cubicAt, funnelOf, funnelRadius, halosOf, layerPositions, median, offAxis, routesOf } from '../busNerveDirect'
import type { BusContract } from '../busNextgen'
import type { BusListening } from '../busNerve'

// Origin (ISSUE-53): everything ran into the
// bus and out of it, and nobody could see who listens to whom. Domains stand
// on floors by their tier (from the ownership table), a topic one domain
// sends and another's reader takes is one arc between them, the bus is not
// drawn, and a topic nobody takes is a halo round its sender.

const contract: BusContract = {
  topics: [
    { tenant: 'rocket', type: 'git.main.advanced', version: 1, publisher: 'gate-git', domain: 'verification', guarantee: 'ordered', consumers: ['headrelease', 'console'] },
    { tenant: 'rocket', type: 'ci.run.finished', version: 1, publisher: 'gate-cluster', domain: 'verification', guarantee: 'best-effort', consumers: ['headrelease', 'postrelease', 'console'] },
    { tenant: 'rocket', type: 'alert.rule.fired', version: 1, publisher: 'gate-db', domain: 'foundation', guarantee: 'best-effort', consumers: ['console'] },
    { tenant: 'rocket', type: 'compute.node.changed', version: 1, publisher: 'gate-cluster', domain: 'compute', guarantee: 'best-effort', consumers: ['console'] },
    { tenant: 'rocket', type: 'ci.run.started', version: 1, publisher: 'gate-cluster', domain: 'verification', guarantee: 'best-effort', consumers: ['console'] },
  ],
  subscriptions: [
    { tenant: 'rocket', id: 'alert-firing-to-duty', on: ['alert.rule.fired'], deliver: 'wake', to: 'thread:x', by: 'dispatcher', domain: 'coordination' },
    { tenant: 'rocket', id: 'land-refused', on: ['ci.run.finished'], deliver: 'wake', to: 'commit.session', by: 'dispatcher', domain: 'coordination' },
  ],
  clients: [
    { tenant: 'rocket', name: 'gate:stream:console', kind: 'reader' },
    { tenant: 'rocket', name: 'headrelease', kind: 'durable', domain: 'delivery' },
    { tenant: 'rocket', name: 'postrelease', kind: 'durable', domain: 'delivery' },
  ],
}
const domainOfType = new Map(contract.topics.map(t => [t.type, t.domain as string]))
const listening: BusListening = {
  hours: 24,
  topics: [
    { type: 'git.main.advanced', published: 10, readers: [], heard: 10, followed: 0 },
    { type: 'ci.run.finished', published: 40, readers: [], heard: 40, followed: 0 },
    { type: 'alert.rule.fired', published: 5, readers: [], heard: 0, followed: 0 },
    { type: 'compute.node.changed', published: 7, readers: [], heard: 0, followed: 0 },
    { type: 'ci.run.started', published: 40, readers: [], heard: 0, followed: 0, display_only: true },
  ],
}

describe('layerPositions', () => {
  // 事故 2026-11-17, on PROD: a disc in a circle read as a roulette
  // wheel; then the big bang: the base is the core, the tiers open out from
  // it as a cone, distance from the core is the tier; no two clouds run into
  // one another (coordination and compute overlapped seen from the side).
  const domains = [
    { id: 'owner', tier: 3 }, { id: 'plan', tier: 3 }, { id: 'foundation', tier: 0 }, { id: 'compute', tier: 0 }, { id: 'coordination', tier: 1 },
    { id: 'verification', tier: 2 }, { id: 'delivery', tier: 2 }, { id: 'lineage', tier: 2 }, { id: 'tenant', tier: 2 },
  ]
  it('puts the base at the core and each tier further out along the cone, every domain of it', () => {
    const p = layerPositions(domains)
    const out = (t: number) => domains.filter(d => d.tier === t).map(d => alongAxis(p.get(d.id)!))
    for (const t of [1, 2, 3]) expect(Math.min(...out(t))).toBeGreaterThan(Math.max(...out(t - 1)))
    for (const id of ['foundation', 'compute']) expect(Math.abs(alongAxis(p.get(id)!))).toBeLessThan(30)
  })
  it('keeps every two domains apart, and is the same for the same input', () => {
    const p = layerPositions(domains)
    const v = [...p.values()]
    for (let i = 0; i < v.length; i++) for (let j = i + 1; j < v.length; j++) {
      expect(Math.hypot(v[i].x - v[j].x, v[i].y - v[j].y, v[i].z - v[j].z)).toBeGreaterThanOrEqual(MIN_APART - 0.5)
    }
    expect(layerPositions([...domains].reverse())).toEqual(p)
  })
  // ISSUE-53: the big bang is a trumpet with an outline over everything, sized from where things are.
  it('wraps every domain in a funnel that only widens going out, with a ring at each tier', () => {
    const p = layerPositions(domains)
    const f = funnelOf(p, new Map(domains.map(d => [d.id, d.tier])))!
    for (const v of p.values()) expect(funnelRadius(f, alongAxis(v))).toBeGreaterThanOrEqual(offAxis(v) + 31)
    for (let s = f.start; s < f.start + f.length; s += 10) expect(funnelRadius(f, s + 10)).toBeGreaterThanOrEqual(funnelRadius(f, s))
    expect(funnelRadius(f, f.start + f.length)).toBeGreaterThan(funnelRadius(f, f.start) * 1.5)
    expect(f.tiers.map(t => t.tier)).toEqual([0, 1, 2, 3])
  })
  it('stands a domain with no declared tier beyond the declared ones', () => {
    const p = layerPositions([{ id: 'a', tier: 0 }, { id: 'b', tier: 2 }, { id: 'new' }])
    expect(alongAxis(p.get('new')!)).toBeGreaterThan(alongAxis(p.get('b')!))
  })
})

describe('routes and arcs', () => {
  it('draws one arc per pair of domains, from the readers the code runs and the rows, never the page itself', () => {
    const routes = routesOf(contract)
    expect(routes.some(r => r.reader === 'console')).toBe(false)
    const arcs = arcsOf(routes, domainOfType, listening)
    expect(arcs.map(a => a.id)).toEqual(['arc:foundation>coordination', 'arc:verification>coordination', 'arc:verification>delivery'])
    const toDelivery = arcs.find(a => a.id === 'arc:verification>delivery')!
    expect(toDelivery.topics).toEqual(['ci.run.finished', 'git.main.advanced'])
    expect(toDelivery.readers).toEqual(['headrelease', 'postrelease'])
    expect(toDelivery.count).toBe(50)
    expect(arcs.find(a => a.id === 'arc:verification>coordination')!.readers).toEqual(['sub-land-refused'])
  })
  it('counts only rows the dispatcher delivers (11-18: approval.card.* was delivered by the approvals code, off the bus)', () => {
    const c: BusContract = { ...contract, subscriptions: [...contract.subscriptions, { tenant: 'rocket', id: 'card-concluded', on: ['alert.rule.fired'], deliver: 'wake', to: 'ref.requester', by: 'approvals.thread-inbox', domain: 'coordination' }] }
    expect(routesOf(c).some(r => r.reader === 'sub-card-concluded')).toBe(false)
  })
  it('counts only the readers that run in this environment (11-18: head-release reads in PROD alone; STG drew its arc anyway)', () => {
    const c: BusContract = { ...contract, clients: contract.clients.map(x => (x.name === 'headrelease' ? { ...x, envs: ['prod'] } : x)) }
    const stg = routesOf(c, 'stg')
    expect(stg.some(r => r.reader === 'headrelease')).toBe(false)
    expect(routesOf(c, 'prod').some(r => r.reader === 'headrelease')).toBe(true)
    // On STG git.main.advanced is then taken by nobody: its domain wears a halo.
    expect(halosOf(stg, domainOfType, listening).get('verification')?.topics).toContain('git.main.advanced')
  })
  it('gives a halo to a domain sending what nobody takes; a display-only topic makes none', () => {
    const h = halosOf(routesOf(contract), domainOfType, listening)
    expect([...h.keys()]).toEqual(['compute'])
    expect(h.get('compute')).toEqual({ count: 7, topics: ['compute.node.changed'] })
  })
})

describe('arc curve', () => {
  it('rises above the line between its ends, and loops over a domain that hears itself', () => {
    const a = { x: 0, y: 0, z: 0 }
    const b = { x: 100, y: 0, z: 0 }
    const c = arcCurve(a, b)
    expect(cubicAt(c, 0)).toEqual(a)
    expect(cubicAt(c, 1)).toEqual(b)
    expect(cubicAt(c, 0.5).y).toBeGreaterThan(20)
    const loop = arcCurve(a, a)
    expect(cubicAt(loop, 0.5).y).toBeGreaterThan(30)
    expect(median([3, 1, 2])).toBe(2)
    expect(median([])).toBeUndefined()
  })
})
