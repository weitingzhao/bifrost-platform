/**
 * Satellite Bus view-model unit tests.
 * Ported from scripts/satellite-bus-viewmodel-test.ts.
 */
import { describe, expect, it } from 'vitest'
import type { Reachability } from '@/api/matrixTypes'
import type {
  SatelliteBusDeepResponse,
  SatelliteBusIngestService,
  SatelliteBusSocketComponent,
} from '@/api/satelliteBusTypes'
import {
  buildSatelliteBusViewModel,
  type SatelliteBusViewModelInput,
} from '@/lib/satellite-bus/satelliteBusViewModel'

function component(
  reachability: Reachability,
  detail = 'ok',
  raw?: Record<string, unknown>,
): SatelliteBusSocketComponent {
  return { reachability, detail, raw }
}

function gatewayUp(): SatelliteBusSocketComponent {
  return component('ok', 'Platform IB Gateway healthy @ redis-ib', {
    lamp: 'green',
    connected: true,
    components: { ingestor: { connected: true }, account: { service_alive: true } },
  })
}

function gatewayDown(): SatelliteBusSocketComponent {
  return component('fail', 'gateway unreachable', {
    lamp: 'red',
    connected: false,
    components: { ingestor: { connected: false }, account: { connected: false } },
  })
}

type BusOverrides = {
  ib_ingestor?: SatelliteBusSocketComponent
  ib_account_agent?: SatelliteBusSocketComponent
  ib_operator?: SatelliteBusSocketComponent
  polygon_ws?: SatelliteBusSocketComponent
  platform_ib_gateway?: SatelliteBusSocketComponent
  ingest?: SatelliteBusIngestService[]
  daemonAlive?: boolean
  daemonPolicyOff?: boolean
  accountSyncOk?: boolean
  opsFail?: boolean
}

function bus(env: 'dev' | 'stg' | 'prod', o: BusOverrides = {}): SatelliteBusDeepResponse {
  const ingest: SatelliteBusIngestService[] = o.ingest ?? [
    ...(o.daemonPolicyOff === true
      ? [
          {
            id: 'trading_engine',
            runtime_status: 'policy-off',
            display_active: 'daemon scale 0 (env policy)',
            reachability: 'ok' as Reachability,
            detail: 'policy-off',
          },
        ]
      : []),
    {
      id: 'polygon_ws',
      runtime_status: 'policy-off',
      display_active: 'ws-disabled (rest-only)',
      reachability: 'ok' as Reachability,
      detail: 'policy-off',
    },
  ]
  const daemonAlive = o.daemonAlive ?? o.daemonPolicyOff !== true
  return {
    environment: env,
    label: env,
    generated_at: '2026-07-21T00:00:00Z',
    reachability: 'ok',
    detail: 'Deep bus semantics from trade monitor/ops APIs',
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
        polygon_ws: o.polygon_ws ?? component('ok'),
        ib_ingestor: o.ib_ingestor ?? component('ok', 'connected @ redis-ib'),
        ib_account_agent: o.ib_account_agent ?? component('ok', 'connected @ redis-ib'),
        ib_operator: o.ib_operator ?? component('ok', 'rpc consumer ready'),
        platform_ib_gateway: o.platform_ib_gateway ?? gatewayUp(),
      },
      account_sync: {
        daemon_alive: o.accountSyncOk ?? o.daemonPolicyOff !== true,
        reachability: (o.accountSyncOk ?? o.daemonPolicyOff !== true) ? 'ok' : 'fail',
      },
    },
    ops: {
      reachability: o.opsFail === true ? 'fail' : 'ok',
      detail: o.opsFail === true ? 'ops executor unreachable' : 'ok',
      k8s_reachable: o.opsFail !== true,
    },
    ingest: { services: ingest, reachability: 'ok', detail: 'ok' },
  }
}

function input(
  selectedEnv: 'dev' | 'stg' | 'prod',
  buses: SatelliteBusViewModelInput['buses'],
  tradeApi = { ok: 9, total: 9 },
): SatelliteBusViewModelInput {
  return { selectedEnv, buses, tradeApi }
}

describe('buildSatelliteBusViewModel', () => {
  it('a) STG policy-off + required path OK => HEALTHY (expected-off is neutral)', () => {
    const vm = buildSatelliteBusViewModel(
      input('stg', { stg: bus('stg', { daemonPolicyOff: true }), dev: bus('dev'), prod: bus('prod') }),
    )
    expect(vm.health).toBe('healthy')
    expect(vm.healthLabel).toBe('HEALTHY')
    expect(vm.topReason).toMatch(/expected off/i)
    expect(vm.attention).toHaveLength(0)
    const daemonRow = vm.runtimeConsumers.find(r => r.id === 'trading_engine')
    expect(daemonRow).toBeTruthy()
    expect(daemonRow!.stateLabel).toBe('EXPECTED OFF')
    expect(daemonRow!.health).toBe('expected-off')
    const polygonRow = vm.dataPathConsumers.find(r => r.id === 'polygon_ws')
    expect(polygonRow).toBeTruthy()
    expect(polygonRow!.stateLabel).toBe('EXPECTED OFF')
    // D10: no visible copy suggests enabling live trading / starting the daemon.
    const serialized = JSON.stringify(vm)
    expect(serialized).not.toMatch(/NO-GO|policy-off fail|start the daemon|enable live/i)
  })

  it('b) selected required path fail => UNAVAILABLE', () => {
    const vm = buildSatelliteBusViewModel(
      input('stg', {
        stg: bus('stg', { daemonPolicyOff: true, ib_ingestor: component('fail', 'consumer disconnected') }),
      }),
    )
    expect(vm.health).toBe('unavailable')
    expect(vm.topReason).toMatch(/IB Ingestor/)
    const issue = vm.attention.find(i => i.id === 'stg:ib_ingestor')
    expect(issue).toBeTruthy()
    expect(issue!.severity).toBe('critical')
    expect(issue!.scope).toBe('selected')
  })

  it('c) selected required consumer partial => DEGRADED', () => {
    const vm = buildSatelliteBusViewModel(
      input('stg', {
        stg: bus('stg', { daemonPolicyOff: true, ib_operator: component('degraded', 'rpc slow') }),
      }),
    )
    expect(vm.health).toBe('degraded')
    expect(vm.topReason).toMatch(/IB Operator/)
  })

  it('c2) runtime consumer partial (APIs) => DEGRADED, not UNAVAILABLE', () => {
    const vm = buildSatelliteBusViewModel(
      input('stg', { stg: bus('stg', { daemonPolicyOff: true }) }, { ok: 5, total: 9 }),
    )
    expect(vm.health).toBe('degraded')
    expect(vm.topReason).toMatch(/Trade APIs/)
  })

  it('d) PROD fail while STG selected => STG verdict unchanged + cross-env issue', () => {
    const vm = buildSatelliteBusViewModel(
      input('stg', {
        stg: bus('stg', { daemonPolicyOff: true }),
        prod: bus('prod', { ib_ingestor: component('fail', 'prod consumer down') }),
      }),
    )
    expect(vm.health).toBe('healthy')
    expect(vm.attention).toHaveLength(0)
    const cross = vm.crossEnvIssues.find(i => i.id === 'prod:ib_ingestor')
    expect(cross).toBeTruthy()
    expect(cross!.scope).toBe('cross-env')
    expect(cross!.title).toMatch(/Prod/)
  })

  it('e) stale/missing required probe => UNKNOWN (health) with UNPROBED label', () => {
    const missing = buildSatelliteBusViewModel(input('stg', { dev: bus('dev') }))
    expect(missing.health).toBe('unknown')
    expect(missing.healthLabel).toBe('UNPROBED')
    expect(missing.topReason).toMatch(/No bus probe/i)

    const stale = buildSatelliteBusViewModel(
      input('stg', {
        stg: bus('stg', { daemonPolicyOff: true, ib_account_agent: component('unknown', 'Not probed') }),
      }),
    )
    expect(stale.health).toBe('unknown')
    expect(stale.healthLabel).toBe('UNPROBED')
    expect(stale.topReason).toMatch(/missing\/stale/i)
  })

  it('f) shared gateway down => selected UNAVAILABLE', () => {
    const gw = gatewayDown()
    const vm = buildSatelliteBusViewModel(
      input('stg', {
        stg: bus('stg', { daemonPolicyOff: true, platform_ib_gateway: gw }),
        dev: bus('dev', { platform_ib_gateway: gw }),
        prod: bus('prod', { platform_ib_gateway: gw }),
      }),
    )
    expect(vm.health).toBe('unavailable')
    expect(vm.topReason).toMatch(/Platform IB Gateway/i)
    const shared = vm.attention.find(i => i.scope === 'shared')
    expect(shared).toBeTruthy()
    expect(shared!.severity).toBe('critical')
  })

  it('f2) shared gateway up in one env wins (cluster-shared resource)', () => {
    const vm = buildSatelliteBusViewModel(
      input('stg', {
        stg: bus('stg', { daemonPolicyOff: true, platform_ib_gateway: gatewayDown() }),
        dev: bus('dev', { platform_ib_gateway: gatewayUp() }),
      }),
    )
    // Gateway is shared — dev sees it healthy, so the shared node is healthy.
    expect(vm.path.find(n => n.id === 'gateway')?.health).toBe('ok')
    expect(vm.health).toBe('healthy')
  })

  it('g) unrelated Ground/ops degraded => no verdict poisoning', () => {
    const vm = buildSatelliteBusViewModel(
      input('stg', { stg: bus('stg', { daemonPolicyOff: true, opsFail: true }) }),
    )
    expect(vm.health).toBe('healthy')
    expect(vm.attention).toHaveLength(0)
  })

  it('h) K8s workload readiness is evidence-only — cannot enter the verdict', () => {
    // Boundary guard: the view-model input deliberately has no workloads field,
    // so K8s workload evidence cannot influence Bus Health by construction.
    type WorkloadsExcluded = 'workloads' extends keyof SatelliteBusViewModelInput ? never : true
    const workloadsExcluded: WorkloadsExcluded = true
    expect(workloadsExcluded).toBe(true)
    // And no verdict copy attributes Bus Health to K8s workload readiness.
    const vm = buildSatelliteBusViewModel(input('stg', { stg: bus('stg', { daemonPolicyOff: true }) }))
    expect(JSON.stringify(vm)).not.toMatch(/workload|k8s/i)
  })

  it('daemon unexpected down (not policy-off) => DEGRADED runtime issue, not UNAVAILABLE', () => {
    const vm = buildSatelliteBusViewModel(
      input('dev', { dev: bus('dev', { daemonAlive: false, accountSyncOk: false }) }),
    )
    expect(vm.health).toBe('degraded')
    const daemonRow = vm.runtimeConsumers.find(r => r.id === 'trading_engine')
    expect(daemonRow).toBeTruthy()
    expect(daemonRow!.stateLabel).toBe('UNEXPECTED DOWN')
    expect(vm.attention.some(i => i.id === 'dev:runtime:trading_engine')).toBe(true)
  })

  it('pair asymmetric: daemon up + account-sync down => DEGRADED with PAIR ASYMMETRIC', () => {
    const vm = buildSatelliteBusViewModel(
      input('prod', { prod: bus('prod', { daemonAlive: true, accountSyncOk: false }) }),
    )
    expect(vm.health).toBe('degraded')
    expect(vm.topReason).toMatch(/account sync|Account sync/i)
    const syncRow = vm.runtimeConsumers.find(r => r.id === 'account-sync')
    expect(syncRow).toBeTruthy()
    expect(syncRow!.stateLabel).toBe('PAIR ASYMMETRIC')
    expect(syncRow!.health).toBe('degraded')
    expect(syncRow!.detail).toMatch(/daemon up · account-sync down \(co-scale pair\)/)
  })

  it('STG policy-off + sync idle stays EXPECTED OFF (not pair asymmetric)', () => {
    const vm = buildSatelliteBusViewModel(
      input('stg', { stg: bus('stg', { daemonPolicyOff: true, accountSyncOk: false }) }),
    )
    expect(vm.health).toBe('healthy')
    const syncRow = vm.runtimeConsumers.find(r => r.id === 'account-sync')
    expect(syncRow!.stateLabel).toBe('EXPECTED OFF')
    expect(syncRow!.health).toBe('expected-off')
  })

  it('data path node order: gateway → redis-ib → consumers → selected namespace', () => {
    const vm = buildSatelliteBusViewModel(input('prod', { prod: bus('prod') }))
    expect(vm.path.map(n => n.id)).toEqual(['gateway', 'redis-ib', 'consumers', 'namespace'])
    expect(vm.path[0]!.scopeLabel).toBe('SHARED')
    expect(vm.path[2]!.scopeLabel).toBe('ALL ENVS')
    expect(vm.path[3]!.scopeLabel).toBe('SELECTED')
    expect(vm.namespace).toBe('bifrost-prod')
  })

  it('redis-ib derived fail when gateway up but all required consumers fail', () => {
    const vm = buildSatelliteBusViewModel(
      input('stg', {
        stg: bus('stg', {
          daemonPolicyOff: true,
          ib_ingestor: component('fail', 'down'),
          ib_account_agent: component('fail', 'down'),
          ib_operator: component('fail', 'down'),
        }),
      }),
    )
    expect(vm.health).toBe('unavailable')
    const redis = vm.path.find(n => n.id === 'redis-ib')
    expect(redis).toBeTruthy()
    expect(redis!.health).toBe('fail')
  })
})
