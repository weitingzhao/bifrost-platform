import { describe, expect, it } from 'vitest'
import { cableRun, facilitiesOf, launchOf, pointAt, runLengths } from '../launchSite'

// Origin: 2026-11-17, the Owner via the coordinator: the Ops home is a launch
// site; each domain a facility that opens its page; relations run as cable
// trays on the ground, square, with the junction box where a pulse waits in
// transit; a release that succeeds is a launch.

describe('facilities', () => {
  it('puts each domain with a known facility on the plan, and lists the rest', () => {
    const { facilities, missing } = facilitiesOf(
      [{ id: 'delivery', facility: { kind: 'launch-pad', page: '#releases' } }, { id: 'owner', facility: { kind: 'mission-control', page: '#command' } },
        { id: 'loose' }, { id: 'odd', facility: { kind: 'swimming-pool', page: '#x' } }],
      [], new Map([['release.deploy.finished', 'delivery']]),
      { hours: 24, topics: [{ type: 'release.deploy.finished', published: 4, readers: [], heard: 4, followed: 0 }] },
    )
    expect(facilities.map(f => [f.domain, f.kind, f.page, f.state])).toEqual([['delivery', 'launch-pad', '#releases', 'unheard'], ['owner', 'mission-control', '#command', 'quiet']])
    expect(missing).toEqual(['loose', 'odd'])
  })
})

describe('cable runs', () => {
  it('turns only at right angles, with the junction at the turn', () => {
    const run = cableRun({ x: 0, z: 0 }, { x: 50, z: 80 }, 'arc:a>b')
    for (let i = 1; i < run.points.length; i++) {
      const p = run.points[i - 1], q = run.points[i]
      expect(p.x === q.x || p.z === q.z).toBe(true)
    }
    const j = run.points[run.junction]
    expect(j.x).toBe(run.points[run.junction + 1].x)
    expect(j.z).toBe(run.points[run.junction - 1].z)
  })
  it('measures a run and finds points along it', () => {
    const pts = [{ x: 0, z: 0 }, { x: 10, z: 0 }, { x: 10, z: 10 }]
    const l = runLengths(pts)
    expect(l).toEqual([0, 10, 20])
    expect(pointAt(pts, l, 15)).toEqual({ x: 10, z: 5 })
    expect(pointAt(pts, l, 99)).toEqual({ x: 10, z: 10 })
  })
})

describe('launches', () => {
  it('sends the platform up as the vehicle, a tenant as a payload, and nothing that failed', () => {
    expect(launchOf('release.deploy.finished', 'prod/platform@abc', 'succeeded')).toEqual({ what: 'rocket', env: 'prod', app: 'platform' })
    expect(launchOf('release.deploy.finished', 'prod/trade-api@abc', 'succeeded')?.what).toBe('payload')
    expect(launchOf('release.deploy.finished', 'prod/platform@abc', 'failed')).toBeNull()
    expect(launchOf('ci.run.finished', 'x', 'succeeded')).toBeNull()
  })
})

// 2026-11-17, the CI line: a facility's page is a route typed by hand; when a
// page is renamed or removed it would point at nothing, unseen (law 30's
// drift). Every page the domains' table names is a route the Console has;
// the positive control is a route it has not.
describe('facility pages', () => {
  it('names only routes the Console has', async () => {
    const { readFileSync } = await import('node:fs')
    const { resolve } = await import('node:path')
    const { SHELL_ROUTE_IDS } = await import('@/lib/shell/consoleRoutes')
    const table = JSON.parse(readFileSync(resolve(__dirname, '../../../../../../config/domain-ownership.json'), 'utf8')) as { domains: { id: string; facility?: { page: string } }[] }
    const routeOf = (page: string) => page.replace(/^#/, '').split('?')[0]
    const known = (page: string) => page.startsWith('#') && (SHELL_ROUTE_IDS as readonly string[]).includes(routeOf(page))
    const pages = table.domains.filter(d => d.facility).map(d => [d.id, d.facility!.page] as const)
    expect(pages.length).toBeGreaterThan(0)
    for (const [id, page] of pages) expect(`${id} ${page} ${known(page)}`).toBe(`${id} ${page} true`)
    expect(known('#foo')).toBe(false)
  })
})
