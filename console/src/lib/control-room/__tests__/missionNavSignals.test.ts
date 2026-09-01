import { describe, expect, it } from 'vitest'
import { resolveSatelliteRocketNavLamp } from '@/lib/control-room/missionNavSignals'
import type { MissionSnapshot } from '@/lib/control-room/missionSignals'

function snap(partial: Partial<MissionSnapshot> = {}): MissionSnapshot {
  const ok = { signal: 'ok' as const, value: '3/3', detail: 'ok' }
  return {
    infra: { signal: 'ok', value: '3/3', detail: 'Cluster: 3/3 nodes Ready' },
    release: ok,
    control: { signal: 'ok', value: '6/6', detail: 'Platform self-health 6/6 probes OK' },
    agent: ok,
    tradeDev: ok,
    tradeStg: ok,
    tradeProd: ok,
    rocketOverall: 'ok',
    payloadOverall: 'ok',
    missionOverall: 'ok',
    ...partial,
  }
}

describe('resolveSatelliteRocketNavLamp', () => {
  it('tints Cluster from infra and Rocket Health from control', () => {
    const s = snap({
      infra: { signal: 'degraded', value: '1 pods', detail: '1 failing pods' },
      control: { signal: 'fail', value: '0/6', detail: 'Platform self-health 0/6' },
    })
    expect(resolveSatelliteRocketNavLamp('cluster', {
      snapshotLoading: false,
      snapshot: s,
      busLoading: false,
      bus: null,
    })).toEqual({ signal: 'degraded', title: '1 failing pods' })
    expect(resolveSatelliteRocketNavLamp('rocket-health', {
      snapshotLoading: false,
      snapshot: s,
      busLoading: false,
      bus: null,
    })).toEqual({ signal: 'fail', title: 'Platform self-health 0/6' })
  })

  it('tints Satellite Health from payload overall', () => {
    const s = snap({
      payloadOverall: 'degraded',
      tradeProd: { signal: 'degraded', value: '7/9', detail: '7/9 services reachable' },
    })
    const lamp = resolveSatelliteRocketNavLamp('satellite-health', {
      snapshotLoading: false,
      snapshot: s,
      busLoading: false,
      bus: null,
    })
    expect(lamp?.signal).toBe('degraded')
    expect(lamp?.title).toContain('CAUTION')
    expect(lamp?.title).toContain('PROD 7/9')
  })

  it('tints Bus Status from the bus rollup', () => {
    const lamp = resolveSatelliteRocketNavLamp('satellite-bus', {
      snapshotLoading: false,
      snapshot: snap(),
      busLoading: false,
      bus: { signal: 'fail', title: 'Bus Status: DEV HEALTHY · STG UNAVAILABLE · PROD HEALTHY' },
    })
    expect(lamp?.signal).toBe('fail')
    expect(lamp?.title).toContain('STG UNAVAILABLE')
  })

  it('returns unknown while snapshot is loading', () => {
    expect(
      resolveSatelliteRocketNavLamp('cluster', {
        snapshotLoading: true,
        snapshot: snap(),
        busLoading: false,
        bus: null,
      })?.signal,
    ).toBe('unknown')
  })

  it('ignores unrelated nav items', () => {
    expect(
      resolveSatelliteRocketNavLamp('research-engine', {
        snapshotLoading: false,
        snapshot: snap(),
        busLoading: false,
        bus: null,
      }),
    ).toBeNull()
  })
})
