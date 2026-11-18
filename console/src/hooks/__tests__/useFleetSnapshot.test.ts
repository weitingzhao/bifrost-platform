import { describe, expect, it } from 'vitest'
import { resolveDaemonIbObserve } from '@/hooks/useFleetSnapshot'
import type { SatelliteBusDeepResponse } from '@/api/satelliteBusTypes'

describe('resolveDaemonIbObserve', () => {
  function bus(blockReasons: string[] | undefined): SatelliteBusDeepResponse {
    return {
      environment: 'prod',
      label: 'Production',
      generated_at: '2026-01-01T00:00:00Z',
      reachability: 'degraded',
      detail: 'test',
      monitor: {
        reachability: 'degraded',
        detail: 'test',
        health: { reachability: 'degraded' },
        daemon: { reachability: 'degraded', block_reasons: blockReasons },
        socket: {
          ib_ingestor: { reachability: 'ok', detail: '' },
          ib_account_agent: { reachability: 'ok', detail: '' },
          ib_operator: { reachability: 'ok', detail: '' },
          platform_ib_gateway: { reachability: 'ok', detail: '' },
        },
        account_sync: { daemon_alive: true, reachability: 'ok' },
      },
      ops: { reachability: 'ok', detail: '' },
      ingest: { services: [], reachability: 'ok', detail: '' },
    }
  }

  it('is true when any env reports ib_not_connected', () => {
    expect(resolveDaemonIbObserve([bus(['ib_not_connected'])])).toBe(true)
    expect(
      resolveDaemonIbObserve([bus([]), bus(['ib_not_connected', 'socket_massive_disconnected'])]),
    ).toBe(true)
  })

  it('is false when no env reports ib_not_connected', () => {
    expect(resolveDaemonIbObserve([])).toBe(false)
    expect(resolveDaemonIbObserve([bus(undefined)])).toBe(false)
    expect(resolveDaemonIbObserve([bus(['socket_massive_disconnected'])])).toBe(false)
  })
})
