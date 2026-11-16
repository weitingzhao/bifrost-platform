// 2026-11-16 extraction: a timeout may use plugin health; a real bus failure must not.
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { buildTenantBusObservation } from '../busObservation'
const probe = vi.hoisted(() => ({ health: 'unavailable', topReason: 'context deadline exceeded' }))
vi.mock('@/lib/satellite-bus/satelliteBusViewModel', () => ({ buildSatelliteBusViewModel: () => probe }))
type Args = Parameters<typeof buildTenantBusObservation>
const bus = { environment: 'stg' } as Args[0]
const plugin = { reachability: 'ok', summary: 'reachable' } as Args[3]
beforeEach(() => { probe.health = 'unavailable'; probe.topReason = 'context deadline exceeded' })
describe('tenant observation adapter', () => {
  it('keeps missing bus data unobserved even when the plugin is reachable', () => {
    expect(buildTenantBusObservation(undefined, undefined, 'stg', plugin)).toBeNull()
  })
  it('uses plugin confirmation only for the existing timeout states', () => {
    for (const health of ['unavailable', 'unknown', 'degraded']) {
      probe.health = health
      expect(buildTenantBusObservation(bus, undefined, 'stg', plugin)).toEqual({ health: 'healthy', topReason: 'Monitor probe timed out; Platform IB Gateway plugin ok — reachable' })
    }
  })
  it('preserves real failures and timeouts without plugin confirmation', () => {
    expect(buildTenantBusObservation(bus, undefined, 'stg', undefined)).toEqual(probe)
    probe.topReason = 'Redis consumer disconnected'
    expect(buildTenantBusObservation(bus, undefined, 'stg', plugin)).toEqual(probe)
  })
})
