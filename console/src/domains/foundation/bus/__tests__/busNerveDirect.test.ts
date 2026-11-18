import { describe, expect, it } from 'vitest'
import { arcCurve, arcsOf, cubicAt, halosOf, layerPositions, median, routesOf, tierRings } from '../busNerveDirect'
import type { BusContract } from '../busNextgen'
import type { BusListening } from '../busNerve'

// Origin: 2026-11-17, the Owner via the coordinator: everything ran into the
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
  it('puts each tier on its own floor, in name order, the same every time', () => {
    const p = layerPositions([{ id: 'owner', tier: 3 }, { id: 'foundation', tier: 0 }, { id: 'compute', tier: 0 }, { id: 'coordination', tier: 1 }])
    expect(p.get('foundation')!.y).toBe(p.get('compute')!.y)
    expect(p.get('coordination')!.y).toBeGreaterThan(p.get('foundation')!.y)
    expect(p.get('owner')!.y).toBeGreaterThan(p.get('coordination')!.y)
    // A lone domain on its floor stands in the middle.
    expect(Math.hypot(p.get('owner')!.x, p.get('owner')!.z)).toBe(0)
    expect(layerPositions([{ id: 'compute', tier: 0 }, { id: 'foundation', tier: 0 }]).get('compute')).toEqual(p.get('compute'))
    expect(tierRings(p).length).toBe(3)
  })
  it('stands a domain with no declared tier above the declared ones, apart', () => {
    const p = layerPositions([{ id: 'a', tier: 0 }, { id: 'b', tier: 2 }, { id: 'new' }])
    expect(p.get('new')!.y).toBeGreaterThan(p.get('b')!.y)
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
