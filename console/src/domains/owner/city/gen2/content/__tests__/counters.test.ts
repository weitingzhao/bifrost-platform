// 2026-11-23, 箭10: a software building's rooms are its Go packages, each a counter with a kiosk per route (the
// engine's counter room, bifrost-ui 0.41.0; stage 1 read on PROD: cockpit/ui-engine/counter-stage1.md, e.g.
// verification's 12 routes are served in assembly's server package). These cases pin how City puts the read
// routes into rooms: by the handler's package, the promising domain named when it is not the package's owner,
// readings matched exactly, and nothing read as zero that was not measured.
import { describe, expect, it } from 'vitest'
import type { DomainHoldings } from '../domainOwns'
import { buildingFlow, counterQueriesFor, counterRooms, offContractOf, readingsOf, roomIdOf, routesByPackage } from '../counters'
import { rulesBroken } from '@bifrost/ui/city/rules'
import { domainTree } from '../domainPlaces'

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
      ['pkg.lineage', 'room', true], ['pkg.empty', 'room', true],
    ])
    const many = Array.from({ length: 20 }, (_, i) => `pkg/p${i}`)
    const rooms = counterRooms(many, routes, 'online')
    expect(rooms.length).toBeLessThanOrEqual(16)
    expect(rooms.at(-1)).toMatchObject({ id: 'other', label: `Other · ${20 - (rooms.length - 1)}` })
  })

  // 2026-11-23, bifrost-ui 0.48.0 (fc7cf4f): outside, a building shows one reading, the cars at its door.
  it('gives a building its requests a minute, summed by the engine, read from the requests rule; not read is null', () => {
    const routes = routesByPackage(holdings, readingsOf([pt('GET', '/api/v1/lineage', 0.6), pt('GET', '/api/v1/ci/domains', 3)], []))
    expect(buildingFlow(['pkg/lineage', 'pkg/server'], routes, 'rule')).toEqual({ perMin: 3.6, source: 'rule' })
    expect(buildingFlow(['pkg/lineage'], routesByPackage(holdings, readingsOf(null, null)), 'rule').perMin).toBeNull()
  })
})

// 2026-11-23, bifrost-ui 0.52.0 (4c79092): a fifth of the platform's requests (16:23Z, /health first) hit no contract
// route, so no building can hold them; the engine lists them on the city's level. City reads them from the same
// requests reading the counters use, never a second one, and says why when they cannot be told apart.
describe('off-contract requests', () => {
  it('lists the pairs no contract names, busiest first, and sums the matched part as the counters do', () => {
    const r = offContractOf(holdings, readingsOf([pt('GET', '/api/v1/lineage', 0.6), pt('GET', '/health', 40), pt('POST', '/api/v1/x', 2)], []))
    expect(r).toMatchObject({ contractPerMin: 0.6, routes: [{ method: 'GET', path: '/health', perMin: 40 }, { method: 'POST', path: '/api/v1/x', perMin: 2 }] })
  })
  it('says why instead of none: readings not read, or no contract read', () => {
    expect(offContractOf(holdings, readingsOf(null, null))).toMatchObject({ why: expect.stringContaining('not read') })
    expect(offContractOf({}, readingsOf([pt('GET', '/health', 40)], []))).toMatchObject({ why: expect.stringContaining('contract') })
  })
})

// 2026-11-23 23:16Z, PROD: once the map's buildings were read, the engine refused the whole City ("an id must be
// non-empty, without / # ? or spaces"): a room's id was its package path. The counters' tests never ran the engine's
// own rules over a tree. This case builds the City from package paths as the map serves them and runs those rules.
describe('the City with counters passes the engine\'s rules', () => {
  it('turns a package path into a room id the engine takes', () => {
    expect(roomIdOf('internal/lineage/assembly')).toBe('internal.lineage.assembly')
    const h = { lineage: { id: 'lineage', packages: [], groups: [], paths: [], workloads: [],
      buildings: [{ id: 'lineage', packages: ['internal/lineage', 'internal/lineage/assembly'] }],
      contract: { request_response: [{ method: 'GET', path: '/api/v1/lineage', handler: 'internal/lineage.HandleGet' }] } } }
    const routes = routesByPackage(h, readingsOf([], []))
    const status = { version: 1, at: '', commit: '', domains: [{ id: 'lineage', overall: 'good' }] } as never
    const tree = domainTree(status, null, '', h, routes)
    expect(tree.children[0].children[0].children.map(r => r.id)).toEqual(['internal.lineage', 'internal.lineage.assembly'])
    expect(rulesBroken({ tree })).toEqual([])
  })
})

// 2026-11-23, 箭10: on STG the counters, the cars at the doors and the off-contract card showed PROD's traffic: the
// queries named namespace="bifrost-platform-prod" for every environment.
describe('the counters read the platform of their own environment', () => {
  it('names the environment\'s platform namespace, and reads none where there is no platform', () => {
    expect(counterQueriesFor('stg')).toMatchObject({ namespace: 'bifrost-platform-stg', perMin: expect.stringContaining('namespace="bifrost-platform-stg"') })
    expect(counterQueriesFor('prod')?.failPerMin).toContain('namespace="bifrost-platform-prod"')
    expect(counterQueriesFor('dev')).toBeNull()
    expect(counterQueriesFor(null)).toBeNull()
  })
})
