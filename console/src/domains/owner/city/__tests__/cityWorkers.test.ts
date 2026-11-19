import { describe, expect, it } from 'vitest'
import { targetOfCharter, workersGlance, workersOf } from '../cityWorkers'
import { addRoad, doorOf, emptyDoc, fromTiled, targetPart, toTiled, trim, walkPath } from '../builderDoc'
import { bayReading, skyReading, type ReleaseContext } from '../cityReadings'
import { plannedCity } from '../defaultLayout'
import { pluginsOf, tenantsOf } from '../catalog'
import { nextZoneName, zoneTint } from '../zones'
import { campuses, campusPlan, campusSize } from '../cityCampus'
import { coreTier, diskTier, machines, memTier, osOf, variantOf } from '../cityMachines'
import { layersOf, spriteOf } from '../catalog'

// Origin: 2026-11-18, the Owner: seats are the workers; they live in dorms
// and their real work shows. A worker stands at the building of the domain
// its charter names (charters/README.md: the file name is the domain).

const st = (charter: string, minutes: number, waiting_on?: string) => ({ seat: `thread:local_${charter}`, message_id: 'm', at: new Date(1_000_000_000_000 - minutes * 60_000).toISOString(), charter, goal: '', doing: 'a thing', percent: 40, percent_basis: 'steps', next: 'n', waiting_on })
const NOW = 1_000_000_000_000

describe('workers', () => {
  it('go to the building of the domain their charter names, home when it names none we draw', () => {
    expect(targetOfCharter('3-4-verify-release-ci')).toMatchObject({ kind: 'hif' })
    expect(targetOfCharter('charters/7-tenant.md')).toMatchObject({ kind: 'satellites' })
    expect(targetOfCharter('2-lineage').kind).toBe('archive')
    expect(targetOfCharter('6-compute').kind).toBe('power')
    expect(targetOfCharter('10-cockpit').kind).toBe('control')
    expect(targetOfCharter('8-2-comm-desk').kind).toBe('dorm')
    expect(targetOfCharter(undefined).kind).toBe('dorm')
  })
  it('wait when the status waits on something, go home when it is older than 120 minutes', () => {
    const ws = workersOf([st('6-compute', 5), st('8-2-comm-desk', 10, 'the Owner'), st('2-lineage', 400)], NOW)
    expect(ws.map(w => w.state)).toEqual(['working', 'waiting', 'home'])
    expect(ws[2].target.kind).toBe('dorm')
    expect(ws[1].bubble).toBe('waiting: the Owner')
    expect(workersGlance(ws).text).toBe('Seats: 1 working, 1 waiting, 1 home')
    expect(workersGlance(undefined).text).toBe('Seats: unknown')
  })
  it('walk along the roads from the dorm to every building of the plan', () => {
    const d = plannedCity(), home = targetPart(d, { kind: 'dorm', binds: 'domain:coordination' })!
    for (const kind of ['hif', 'satellites', 'archive', 'power', 'control']) {
      const p = targetPart(d, { kind })!
      expect(walkPath(d, doorOf(home), doorOf(p)).length, kind).toBeGreaterThan(0)
    }
  })
})

describe('the zoned plan', () => {
  // 2026-11-18, the Owner: every entity stays, the plan is compact enough to take in at a glance.
  it('is compact: every building inside a district, the pad by the sea, no empty band', () => {
    const d = plannedCity()
    expect(d.zones?.map(z => z.name)).toEqual(['Satellite campuses', 'Launch site', 'Suppliers', 'Town', 'Power yard', 'Off the grid'])
    for (const p of d.parts) expect(d.zones!.some(z => p.x >= z.x && p.y >= z.y && p.x + p.size <= z.x + z.w && p.y + p.size <= z.y + z.h), p.binds || p.kind).toBe(true)
    expect(d.parts.find(p => p.kind === 'pad')!.y).toBe(0)
    expect(d.width * d.height).toBeLessThan(30 * 30)
    expect(trim(d)).toBe(false)
  })
  it('reads the plugins from the repositories the lanes clone', () => {
    expect(pluginsOf([{ pipeline: 'bifrost-build-market-data', repos: ['bifrost-platform-plugin-market-data', 'bifrost-trade-infra'] }, { pipeline: 'bifrost-build-ib-gateway', repos: ['bifrost-platform-plugin'] }]))
      .toEqual(['bifrost-platform-plugin', 'bifrost-platform-plugin-market-data'])
  })
})

describe('the payload works', () => {
  const lanes = [
    { pipeline: 'bifrost-deliver-prod', repos: ['bifrost-trade-api', 'bifrost-trade-core'] },
    { pipeline: 'bifrost-deliver-research', repos: ['bifrost-research'] },
    { pipeline: 'bifrost-build-research-dagster', repos: ['bifrost-research'] },
    { pipeline: 'bifrost-build-market-data', repos: ['bifrost-platform-plugin-market-data'] },
  ]
  const ctx = (repo: string, runs: { name: string; pipeline: string }[]): ReleaseContext => ({ repo, facts: { runs_in_flight: { value: runs }, prod: { value: { commit: 'abcdef1234', run: 'r', completed_at: new Date(NOW - 3600_000).toISOString() } } } })
  it('has one bay a tenant; the plugins are vendors, not satellites in orbit', () => {
    expect(tenantsOf(lanes).map(t => t.name)).toEqual(['Research', 'Trade'])
    const contexts = new Map([['bifrost-trade-api', ctx('bifrost-trade-api', [])], ['bifrost-research', ctx('bifrost-research', [])], ['bifrost-platform-plugin-market-data', ctx('bifrost-platform-plugin-market-data', [])]])
    expect(skyReading(contexts, lanes, NOW).satellites.map(s => s.name).sort()).toEqual(['Research', 'Trade'])
  })
  it('a bay builds while a run of its tenant is in flight, is ready otherwise, unknown when not read', () => {
    const busy = new Map([['bifrost-research', ctx('bifrost-research', [{ name: 'd-1', pipeline: 'bifrost-build-research-dagster' }])]])
    expect(bayReading(busy, lanes, 'Research', NOW)).toMatchObject({ word: 'building', active: true, count: 1 })
    expect(bayReading(new Map([['bifrost-research', ctx('bifrost-research', [])]]), lanes, 'Research', NOW).word).toBe('ready')
    expect(bayReading(new Map([['bifrost-research', null]]), lanes, 'Research', NOW).word).toBe('unknown')
  })
  it('keeps the districts through a saved Tiled map', () => {
    expect(fromTiled(JSON.parse(JSON.stringify(toTiled(plannedCity()))))?.zones).toEqual(plannedCity().zones)
  })
})

// Origin: 2026-11-18, the Owner: he draws the districts himself; positions are only how things are shown.
describe('districts the Owner draws', () => {
  it('a new one takes the first usual name not on the land, its tint following the name', () => {
    expect(nextZoneName([])).toBe('Launch site')
    expect(nextZoneName(plannedCity().zones!)).toBe('District 7')
    expect(zoneTint('Town')).toBe('#f1e8d6')
    expect(zoneTint('Research campus')).toMatch(/^#[0-9a-f]{6}$/)
    expect(zoneTint('Research campus')).toBe(zoneTint('Research campus'))
  })
})

// Origin: 2026-11-18, the Owner: a satellite like Trade is a complex system, many factories joined to CI/CD.
describe('a tenant campus', () => {
  const lanes = [
    { pipeline: 'bifrost-ci-python', repos: ['bifrost-trade-core', 'bifrost-trade-infra'] },
    { pipeline: 'bifrost-deliver-prod', repos: ['bifrost-trade-api', 'bifrost-trade-core', 'bifrost-trade-frontend', 'bifrost-trade-infra', 'bifrost-trade-worker', 'bifrost-ui'] },
    { pipeline: 'bifrost-ci-finance', repos: ['bifrost-finance'] },
  ]
  const run = new Map([['bifrost-trade-core', { repo: 'bifrost-trade-core', facts: { runs_in_flight: { value: [{ name: 'py-1', pipeline: 'bifrost-ci-python' }] } } }]])
  const deps = [{ labels: { namespace: 'bifrost-prod', deployment: 'api-market' }, value: 1 }, { labels: { namespace: 'bifrost-prod', deployment: 'daemon' }, value: 2 }, { labels: { namespace: 'research', deployment: 'research-api' }, value: 1 }]
  it('has a factory per repository of its own, smoking while a run of it is in flight', () => {
    const t = campuses(lanes, run, deps).get('Trade')!
    expect(t.factories.map(f => [f.name, f.busy])).toEqual([['api', false], ['core', true], ['frontend', false], ['worker', false]])
    expect(campuses(lanes, new Map(), deps).get('Finance')!.factories.map(f => f.name)).toEqual(['finance'])
  })
  it('has its modules only where the tenant\'s namespaces are known, unknown otherwise', () => {
    expect(campuses(lanes, run, deps).get('Trade')!.modules).toBeNull()
    expect(campuses(lanes, run, deps, t => t === 'Trade' ? ['bifrost-prod'] : undefined).get('Trade')!.modules).toEqual([{ name: 'api-market', replicas: 1 }, { name: 'daemon', replicas: 2 }])
  })
  it('is big with three factories or more than four modules, and keeps everything inside', () => {
    const t = campuses(lanes, run, deps).get('Trade')!, f = campuses(lanes, run, deps).get('Finance')!
    expect(campusSize(t)).toBe(8); expect(campusSize(f)).toBe(4)
    const p = campusPlan({ ...t, modules: Array.from({ length: 30 }, (_, i) => ({ name: `m${i}`, replicas: 1 })) }, 8)
    expect(p.modules.every(m => m.x < 8 && m.y < 8)).toBe(true)
    expect(p.more).toBe(30 - p.modules.length)
  })
})

describe('trimming the land', () => {
  it('cuts the empty border to one cell and moves nothing relative to anything else', () => {
    const d = emptyDoc(32)
    d.parts.push({ id: 'a', kind: 'dorm', x: 10, y: 12, size: 2, binds: '' })
    addRoad(d, 14, 12)
    d.zones = [{ name: 'Town', x: 9, y: 11, w: 8, h: 4, tint: '#f1e8d6' }]
    expect(trim(d)).toBe(true)
    expect([d.width, d.height]).toEqual([7, 4])
    expect(d.parts[0]).toMatchObject({ x: 1, y: 1 })
    expect(d.roads.has('5,1')).toBe(true)
    expect(d.zones[0]).toMatchObject({ x: 0, y: 0, w: 7, h: 4 })
    expect(trim(d)).toBe(false)
    expect(trim(emptyDoc(16))).toBe(false)
  })
})

// Origin: 2026-11-18, the Owner polishing the power yard: Mac, Windows and Linux look different; memory and disk show as sizes; tags cover the picture.
describe('the machines', () => {
  const G = 2 ** 30
  const cap = (node: string, cpu: number, mem: number, disk: number) => [
    { labels: { node, resource: 'cpu' }, value: cpu }, { labels: { node, resource: 'memory' }, value: mem }, { labels: { node, resource: 'ephemeral_storage' }, value: disk }]
  it('sizes cores, memory and disk in four fixed tiers, unknown when not read', () => {
    expect([memTier(16 * G), memTier(22.8 * G), memTier(64 * G), memTier(125 * G), memTier(null)]).toEqual([0, 1, 2, 3, -1])
    expect([diskTier(500e9), diskTier(1.2e12), diskTier(1.97e12), diskTier(8e12), diskTier(null)]).toEqual([0, 1, 2, 3, -1])
  })
  it('tells the system from the OS image, and the machines outside the cluster from the topology', () => {
    expect([osOf('Ubuntu 24.04.5 LTS'), osOf('Windows Server 2022'), osOf('Darwin 25'), osOf(''), osOf('Plan 9')]).toEqual(['linux', 'win', 'mac', 'unknown', 'unknown'])
    const ms = machines([{ labels: { node: 'ubt-k3s-02', os_image: 'Ubuntu 24.04.5 LTS' }, value: 1 }], cap('ubt-k3s-02', 16, 16 * G, 500e9), [
      { id: 'mini-pc-a', host: '192.168.10.70', group: 'linux', in_k3s_cluster: true },
      { id: 'mac-mini-1', label: 'Mac Mini', group: 'mac', in_k3s_cluster: false, status: 'ok' },
      { id: 'mac-mini-2', label: 'Mac Mini', group: 'mac', in_k3s_cluster: false },
      { id: 'win11-host', label: 'Win11 Host', group: 'external', in_k3s_cluster: false },
    ])
    expect(ms.map(m => [m.id, m.name, m.os, m.offGrid])).toEqual([['ubt-k3s-02', 'node 02', 'linux', false], ['mac-mini-1', 'Mac Mini', 'mac', true], ['mac-mini-2', 'mac-mini-2', 'mac', true], ['win11-host', 'Win11 Host', 'win', true]])
    expect(ms.map(variantOf)).toEqual(['linux', 'mac', 'mac', 'win'])
  })
  it('draws a set by its system; its size shows in its bars, not in the sprite (the Owner: bars beside read better than tanks inside)', () => {
    expect(layersOf('genset', 'node:x', undefined, 'mac').map(l => l.tex)).toEqual(['genset:mac::'])
    expect(layersOf('power', 'node:gpu-server', undefined, 'linux').map(l => l.tex)).toEqual(['power'])
    // 2026-11-19, the Owner: a machine room's number is painted on it, so it is part of its texture.
    expect(layersOf('genset', 'node:x', undefined, 'linux', '01', 'node 01').map(l => l.tex)).toEqual(['genset:linux:01:node%2001'])
    expect(layersOf('power', 'node:gpu-server', undefined, 'linux', 'GPU').map(l => l.tex)).toEqual(['power:GPU:'])
    expect(spriteOf('genset:linux:A%26B:Mac%20Mini').svg).toContain('A&amp;B')
    // 2026-11-19, the Owner: its name on its wall, its system's mark on its side.
    expect(spriteOf('genset:mac::Mac%20Mini').svg).toContain('Mac Mini')
    expect(spriteOf('genset:win::x').svg).toContain('#2f78d6')
    expect([coreTier(8), coreTier(16), coreTier(32), coreTier(64), coreTier(null)]).toEqual([0, 1, 2, 3, -1])
  })
})

// Origin: 2026-11-18, the Owner: a number on each unit (01, 02…), set by him like a position; only how it is shown.
describe('the numbers on the buildings', () => {
  it('are kept through a saved map, trimmed and capped, and absent when not set', () => {
    const d = plannedCity()
    expect(d.parts.find(p => p.binds === 'node:ubt-k3s-02')?.label).toBe('02')
    expect(d.parts.find(p => p.binds === 'tenant:Trade')?.label).toBeUndefined()
    d.parts[0].label = '  TRADE-A-LONG  '
    const back = fromTiled(JSON.parse(JSON.stringify(toTiled(d))))!
    expect(back.parts[0].label).toBe('TRADE-')
    expect(back.parts.find(p => p.binds === 'node:mac-mini-1')?.label).toBe('M1')
    expect('label' in back.parts.find(p => p.binds === 'law')!).toBe(false)
  })
})
