// 2026-11-23, 箭10: a software building's rooms are its Go packages, each a counter with a kiosk per route (the
// engine's counter room, bifrost-ui 0.41.0; stage 1 read on PROD: cockpit/ui-engine/counter-stage1.md, e.g.
// verification's 12 routes are served in assembly's server package). These cases pin how City puts the read
// routes into rooms: by the handler's package, the promising domain named when it is not the package's owner,
// readings matched exactly, and nothing read as zero that was not measured.
import { describe, expect, it } from 'vitest'
import type { DomainHoldings } from '../domainOwns'
import { counterRooms, readingsOf, routesByPackage } from '../counters'

const holding = (id: string, buildings: { id: string; packages: string[] }[], routes: { method: string; path: string; handler: string }[] = []): DomainHoldings =>
  ({ id, packages: [], groups: [], paths: [], workloads: [], buildings, contract: { request_response: routes } })

const holdings = {
  lineage: holding('lineage', [{ id: 'lineage', packages: ['pkg/lineage'] }], [
    { method: 'GET', path: '/api/v1/lineage', handler: 'pkg/lineage.HandleGet' },
    { method: 'GET', path: '/api/v1/lineage/branches', handler: 'pkg/lineage.HandleBranches' },
  ]),
  verification: holding('verification', [], [{ method: 'GET', path: '/api/v1/ci/domains', handler: 'pkg/server.ciDomains' }]),
  assembly: holding('assembly', [{ id: 'server', packages: ['pkg/server'] }]),
  foundation: holding('foundation', [], [{ method: 'GET', path: '/api/v1/proxy/*', handler: 'pkg/server.proxy' }]),
}
const pt = (method: string, handler: string, value: number) => ({ labels: { method, handler }, value })

describe('counter rooms', () => {
  it('puts each route in its handler package, names the promising domain, and matches readings exactly', () => {
    const reads = readingsOf([pt('GET', '/api/v1/lineage', 0.6), pt('GET', '/api/v1/ci/domains', 3)], [pt('GET', '/api/v1/ci/domains', 0.1)])
    const routes = routesByPackage(holdings, reads)
    expect(routes.get('pkg/lineage')).toEqual([
      { method: 'GET', path: '/api/v1/lineage', perMin: 0.6, failPerMin: 0 },
      { method: 'GET', path: '/api/v1/lineage/branches', perMin: null, failPerMin: null },
    ])
    expect(routes.get('pkg/server')).toContainEqual({ method: 'GET', path: '/api/v1/ci/domains', perMin: 3, failPerMin: 0.1, promisedBy: 'verification' })
  })

  it('shutters a wildcard route and every route when the readings were not read, never reading them as zero', () => {
    const notRead = routesByPackage(holdings, readingsOf(null, null))
    expect(notRead.get('pkg/lineage')!.every(r => r.perMin === null && r.unreadable)).toBe(true)
    const proxy = routesByPackage(holdings, readingsOf([pt('GET', '/api/v1/proxy/*', 9)], [])).get('pkg/server')!.find(r => r.path.endsWith('*'))!
    expect(proxy).toMatchObject({ perMin: null, unreadable: expect.stringContaining('wildcard') })
  })

  it('gives every package a room, and folds what does not fit a level into Other', () => {
    const routes = routesByPackage(holdings, readingsOf([], []))
    expect(counterRooms(['pkg/lineage', 'pkg/empty'], routes, 'online').map(r => [r.id, r.kind, !!r.picture])).toEqual([
      ['pkg/lineage', 'room', true], ['pkg/empty', 'room', true],
    ])
    const many = Array.from({ length: 20 }, (_, i) => `pkg/p${i}`)
    const rooms = counterRooms(many, routes, 'online')
    expect(rooms.length).toBeLessThanOrEqual(16)
    expect(rooms.at(-1)).toMatchObject({ id: 'other', label: `Other · ${20 - (rooms.length - 1)}` })
  })
})
