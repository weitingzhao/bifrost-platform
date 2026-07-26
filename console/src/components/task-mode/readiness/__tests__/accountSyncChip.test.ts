import { describe, expect, it } from 'vitest'
import type { SatelliteBusDeepResponse } from '@/api/satelliteBusTypes'
import {
  accountSyncChipFromBus,
  pickPrimaryFailingChip,
} from '@/components/task-mode/readiness/utils'
import { readinessChipFixActions } from '@/lib/task-mode/readinessChipActions'

function bus(overrides: {
  daemonPolicyOff?: boolean
  daemonAlive?: boolean
  accountSyncOk?: boolean
}): SatelliteBusDeepResponse {
  const daemonAlive = overrides.daemonAlive ?? overrides.daemonPolicyOff !== true
  const accountSyncOk = overrides.accountSyncOk ?? overrides.daemonPolicyOff !== true
  return {
    environment: 'prod',
    label: 'prod',
    generated_at: '2026-07-26T00:00:00Z',
    reachability: 'ok',
    detail: 'ok',
    monitor: {
      reachability: 'ok',
      detail: 'ok',
      health: { reachability: 'ok', status_lamp: 'green', block_reasons: [] },
      daemon: {
        reachability: daemonAlive ? 'ok' : 'fail',
        self_check: daemonAlive ? 'ok' : 'blocked',
        heartbeat: { daemon_alive: daemonAlive },
        block_reasons: daemonAlive ? [] : ['heartbeat_stale'],
      },
      socket: {
        massive: { reachability: 'ok', detail: 'ok' },
        ib_ingestor: { reachability: 'ok', detail: 'ok' },
        ib_account_agent: { reachability: 'ok', detail: 'ok' },
        ib_operator: { reachability: 'ok', detail: 'ok' },
        platform_ib_gateway: { reachability: 'ok', detail: 'ok' },
      },
      celery: {
        broker_connected: true,
        workers: ['w1'],
        worker_ib_connected: false,
        reachability: 'ok',
      },
      account_sync: {
        daemon_alive: accountSyncOk,
        reachability: accountSyncOk ? 'ok' : 'fail',
      },
    },
    ops: { reachability: 'ok', detail: 'ok', k8s_reachable: true },
    ingest: {
      services: overrides.daemonPolicyOff
        ? [
            {
              id: 'trading_engine',
              runtime_status: 'policy-off',
              display_active: 'daemon scale 0 (env policy)',
              reachability: 'ok',
              detail: 'policy-off',
            },
          ]
        : [],
      reachability: 'ok',
      detail: 'ok',
    },
  }
}

describe('accountSyncChipFromBus', () => {
  it('marks pair asymmetric when daemon up and sync down', () => {
    const chip = accountSyncChipFromBus(bus({ daemonAlive: true, accountSyncOk: false }), 'prod')
    expect(chip.label).toBe('Account sync')
    expect(chip.signal).toBe('degraded')
    expect(chip.detail).toMatch(/pair asymmetric/i)
  })

  it('treats STG policy-off + idle sync as ok (not a fault)', () => {
    const chip = accountSyncChipFromBus(
      bus({ daemonPolicyOff: true, accountSyncOk: false }),
      'stg',
    )
    expect(chip.signal).toBe('ok')
    expect(chip.detail).toMatch(/expected off/i)
  })

  it('is ok when both sides are healthy', () => {
    const chip = accountSyncChipFromBus(bus({ daemonAlive: true, accountSyncOk: true }), 'prod')
    expect(chip.signal).toBe('ok')
  })
})

describe('pickPrimaryFailingChip', () => {
  it('prefers Account sync over earlier failing chips', () => {
    const primary = pickPrimaryFailingChip([
      { label: 'Trade · K8s PROD', signal: 'fail' as const },
      { label: 'Account sync', signal: 'degraded' as const },
    ])
    expect(primary?.label).toBe('Account sync')
  })
})

describe('readinessChipFixActions account sync', () => {
  it('offers rollout-restart for Account sync chip', () => {
    const actions = readinessChipFixActions('Account sync', 'degraded', {
      modeId: 'mission-launch',
      env: 'prod',
    })
    expect(actions.some(a => a.tabId === 'satellite-bus' && a.busFocus === 'monitor')).toBe(true)
    const restart = actions.find(a => a.kind === 'actuate' && a.label === 'Restart account-sync')
    expect(restart?.actuation).toEqual({
      kind: 'rollout-restart',
      namespace: 'bifrost-prod',
      deployment: 'account-sync',
    })
  })

  it('never offers daemon scale-up actuate', () => {
    const actions = readinessChipFixActions('Trading daemon', 'fail', {
      modeId: 'mission-launch',
      env: 'stg',
    })
    expect(actions.every(a => a.kind !== 'actuate' || a.actuation?.kind !== 'rollout-restart' || a.actuation.deployment !== 'daemon')).toBe(true)
    expect(actions.every(a => a.label.toLowerCase().includes('scale') === false)).toBe(true)
  })
})
