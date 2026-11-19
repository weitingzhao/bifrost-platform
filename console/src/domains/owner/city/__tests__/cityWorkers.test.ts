import { describe, expect, it } from 'vitest'
import { targetOfCharter, workersGlance, workersOf } from '../cityWorkers'
import { doorOf, fromTiled, targetPart, toTiled, walkPath } from '../builderDoc'
import { bayReading, skyReading, type ReleaseContext } from '../cityReadings'
import { plannedCity } from '../defaultLayout'
import { pluginsOf, tenantsOf } from '../catalog'
import { nextZoneName, zoneTint } from '../zones'

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
  it('keeps the launch site by the sea, the payload works, the town and the power yard apart', () => {
    const d = plannedCity(), at = (k: string) => d.parts.filter(p => p.kind === k)
    expect(at('pad')[0].y).toBe(1)
    expect(d.zones?.map(z => z.name)).toEqual(["Payload works", "Launch site", "Town", "Power yard"])
    expect(Math.min(...[...at('genset'), ...at('power')].map(p => p.x))).toBeGreaterThan(14)
    expect(Math.max(...[...at('control'), ...at('court'), ...at('dorm'), ...at('archive')].map(p => p.x + p.size))).toBeLessThanOrEqual(14)
    expect([...at("supplier"), ...at("bay")].every(p => p.y < 9)).toBe(true)
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
    expect(nextZoneName(plannedCity().zones!)).toBe('District 5')
    expect(zoneTint('Town')).toBe('#f1e8d6')
    expect(zoneTint('Research campus')).toMatch(/^#[0-9a-f]{6}$/)
    expect(zoneTint('Research campus')).toBe(zoneTint('Research campus'))
  })
})
