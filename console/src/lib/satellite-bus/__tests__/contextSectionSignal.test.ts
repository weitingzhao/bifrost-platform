import { describe, expect, it } from 'vitest'
import type { SatelliteBusDeepResponse } from '@/api/satelliteBusTypes'
import type { PayloadReadinessRow } from '@/lib/control-room/payloadReadiness'
import {
  evidenceContextSignal,
  sharedContextSignal,
  socketMatrixContextSignal,
} from '@/lib/satellite-bus/contextSectionSignal'
import type { SocketHealthMatrixRow, SocketHealthRow } from '@/lib/satellite/socketHealthSemantics'

function rocket(reach: SocketHealthRow['reach'] = 'ok'): SocketHealthRow {
  return {
    id: 'platform_ib_gateway',
    label: 'Platform IB Gateway',
    layer: 'rocket',
    required: 'required',
    reach,
    reachLabel: reach,
    detail: 'test',
  }
}

function payloadRow(
  overrides: Partial<PayloadReadinessRow> & Pick<PayloadReadinessRow, 'id' | 'label'>,
): PayloadReadinessRow {
  const ok = { signal: 'ok' as const, detail: 'ok' }
  return {
    role: 'test',
    fleetRole: 'satellite',
    mapMode: 'runtime-map',
    envDiverges: false,
    dev: ok,
    stg: ok,
    prod: ok,
    ...overrides,
  }
}

function matrixRow(
  overrides: Partial<SocketHealthMatrixRow> & Pick<SocketHealthMatrixRow, 'id' | 'label'>,
): SocketHealthMatrixRow {
  const ok = {
    reach: 'ok' as const,
    reachLabel: 'ok',
    required: 'required' as const,
    detail: 'ok',
  }
  return {
    envDiverges: false,
    dev: ok,
    stg: ok,
    prod: ok,
    local: ok,
    ...overrides,
  }
}

describe('sharedContextSignal', () => {
  it('is OK when gateway and payload rows are healthy', () => {
    const s = sharedContextSignal(rocket('ok'), [payloadRow({ id: 'daemon', label: 'Daemon' })])
    expect(s.reach).toBe('ok')
    expect(s.label).toBe('OK')
    expect(s.detail).toBeUndefined()
  })

  it('surfaces payload env diverge as FAIL when a cell fails', () => {
    const s = sharedContextSignal(rocket('ok'), [
      payloadRow({
        id: 'daemon',
        label: 'Daemon',
        envDiverges: true,
        prod: { signal: 'fail', detail: 'down' },
      }),
    ])
    expect(s.reach).toBe('fail')
    expect(s.label).toBe('FAIL')
    expect(s.detail).toMatch(/Daemon|diverge/)
  })

  it('unknown rocket reach → degraded UNPROBED (no gray)', () => {
    const s = sharedContextSignal(rocket('unknown'), [payloadRow({ id: 'daemon', label: 'Daemon' })])
    expect(s.reach).toBe('degraded')
    expect(s.label).toBe('UNPROBED')
  })

  it('unknown IB edge alone (shared vendor not projected) → UNPROBED', () => {
    const probing = { signal: 'unknown' as const, detail: 'No Fleet cell vendor:dev' }
    const s = sharedContextSignal(rocket('ok'), [
      payloadRow({
        id: 'ib',
        label: 'IB edge',
        fleetRole: 'vendor',
        mapMode: 'fleet-vendor',
        dev: probing,
        stg: probing,
        prod: probing,
      }),
    ])
    expect(s.reach).toBe('degraded')
    expect(s.label).toBe('UNPROBED')
  })

  it('OK gateway + OK IB edge → Shared OK', () => {
    const s = sharedContextSignal(rocket('ok'), [
      payloadRow({
        id: 'ib',
        label: 'IB edge',
        fleetRole: 'vendor',
        mapMode: 'fleet-vendor',
      }),
    ])
    expect(s.reach).toBe('ok')
    expect(s.label).toBe('OK')
  })
})

describe('socketMatrixContextSignal', () => {
  it('flags diverge-only rows as DRIFT', () => {
    const s = socketMatrixContextSignal([
      matrixRow({
        id: 'trading_daemon',
        label: 'Trading daemon',
        envDiverges: true,
        dev: {
          reach: 'ok',
          reachLabel: 'observe',
          required: 'required',
          detail: 'observe',
        },
      }),
    ])
    expect(s.reach).toBe('degraded')
    expect(s.label).toBe('DRIFT')
    expect(s.detail).toContain('diverged')
  })

  it('uses FAIL when a required cell fails', () => {
    const s = socketMatrixContextSignal([
      matrixRow({
        id: 'ib_ingestor',
        label: 'IB Ingestor',
        envDiverges: true,
        stg: {
          reach: 'fail',
          reachLabel: 'fail',
          required: 'required',
          detail: 'down',
        },
      }),
    ])
    expect(s.reach).toBe('fail')
    expect(s.label).toBe('FAIL')
  })

  it('ignores policy-off expected-off cells', () => {
    const s = socketMatrixContextSignal([
      matrixRow({
        id: 'massive',
        label: 'Massive WS',
        dev: {
          reach: 'fail',
          reachLabel: 'expected off',
          required: 'policy-off',
          detail: 'policy',
        },
        stg: {
          reach: 'fail',
          reachLabel: 'expected off',
          required: 'policy-off',
          detail: 'policy',
        },
        prod: {
          reach: 'fail',
          reachLabel: 'expected off',
          required: 'policy-off',
          detail: 'policy',
        },
        local: {
          reach: 'fail',
          reachLabel: 'expected off',
          required: 'policy-off',
          detail: 'policy',
        },
      }),
    ])
    expect(s.reach).toBe('ok')
  })
})

describe('evidenceContextSignal', () => {
  it('OBSERVE when daemon self_check degraded / yellow lamp', () => {
    const bus = {
      monitor: {
        daemon: {
          reachability: 'degraded',
          self_check: 'degraded',
          lamp: 'yellow',
          block_reasons: ['ib_not_connected'],
        },
        celery: { reachability: 'ok' },
        account_sync: { reachability: 'ok' },
      },
      ops: { reachability: 'ok' },
    } as SatelliteBusDeepResponse
    const s = evidenceContextSignal(bus, [], [])
    expect(s.reach).toBe('degraded')
    expect(s.label).toBe('OBSERVE')
    expect(s.detail).toBeTruthy()
  })

  it('OBSERVE (not FAIL) when daemon gracefully shut down under D10', () => {
    const bus = {
      monitor: {
        daemon: {
          reachability: 'fail',
          self_check: 'blocked',
          lamp: 'red',
          block_reasons: ['heartbeat_stale'],
          heartbeat: {
            daemon_alive: false,
            graceful_shutdown_at: 1785087512.175405,
          },
        },
        celery: { reachability: 'ok' },
        account_sync: { reachability: 'ok' },
      },
      ops: { reachability: 'ok' },
    } as SatelliteBusDeepResponse
    const s = evidenceContextSignal(bus, [], [])
    expect(s.reach).toBe('degraded')
    expect(s.label).toBe('OBSERVE')
    expect(s.detail).toMatch(/D10|expected off|graceful/i)
  })

  it('FAIL when daemon is down without graceful shutdown', () => {
    const bus = {
      monitor: {
        daemon: {
          reachability: 'fail',
          self_check: 'blocked',
          lamp: 'red',
          block_reasons: ['heartbeat_stale'],
          heartbeat: {
            daemon_alive: false,
          },
        },
        celery: { reachability: 'ok' },
        account_sync: { reachability: 'ok' },
      },
      ops: { reachability: 'ok' },
    } as SatelliteBusDeepResponse
    const s = evidenceContextSignal(bus, [], [])
    expect(s.reach).toBe('fail')
    expect(s.label).toBe('FAIL')
  })

  it('OBSERVE when critical process is scaled-to-zero standby', () => {
    const bus = {
      monitor: {
        daemon: { reachability: 'ok' },
        celery: { reachability: 'ok' },
        account_sync: { reachability: 'ok' },
      },
      ops: { reachability: 'ok' },
    } as SatelliteBusDeepResponse
    const s = evidenceContextSignal(
      bus,
      [],
      [
        {
          label: 'GsTrading daemon',
          name: 'daemon',
          namespace: 'bifrost-stg',
          reachability: 'degraded',
          ready: '0/0',
          status: 'scaled to zero (standby)',
        },
      ],
    )
    expect(s.reach).toBe('degraded')
    expect(s.label).toBe('OBSERVE')
  })

  it('UNPROBED (degraded) when bus-deep missing', () => {
    const s = evidenceContextSignal(undefined, [], [])
    expect(s.reach).toBe('degraded')
    expect(s.label).toBe('UNPROBED')
  })
})
