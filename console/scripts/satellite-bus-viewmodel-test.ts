#!/usr/bin/env node
/**
 * Satellite Bus view-model unit tests (deterministic, no test framework).
 * Usage: npx tsx scripts/satellite-bus-viewmodel-test.ts
 */
import assert from 'node:assert/strict'
import type {
  Reachability,
  SatelliteBusDeepResponse,
  SatelliteBusIngestService,
  SatelliteBusSocketComponent,
} from '../src/api/types'
import {
  buildSatelliteBusViewModel,
  type SatelliteBusViewModelInput,
} from '../src/lib/satellite-bus/satelliteBusViewModel'

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
  massive?: SatelliteBusSocketComponent
  platform_ib_gateway?: SatelliteBusSocketComponent
  ingest?: SatelliteBusIngestService[]
  daemonAlive?: boolean
  daemonPolicyOff?: boolean
  celeryOk?: boolean
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
      id: 'massive_ws',
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
        massive: o.massive ?? component('ok'),
        ib_ingestor: o.ib_ingestor ?? component('ok', 'connected @ redis-ib'),
        ib_account_agent: o.ib_account_agent ?? component('ok', 'connected @ redis-ib'),
        ib_operator: o.ib_operator ?? component('ok', 'rpc consumer ready'),
        platform_ib_gateway: o.platform_ib_gateway ?? gatewayUp(),
      },
      celery: {
        broker_connected: o.celeryOk ?? true,
        workers: ['celery@w1'],
        worker_ib_connected: false,
        reachability: (o.celeryOk ?? true) ? 'ok' : 'fail',
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

let passed = 0
function check(name: string, fn: () => void) {
  try {
    fn()
    passed += 1
    console.log(`ok — ${name}`)
  } catch (e) {
    console.error(`FAIL — ${name}`)
    throw e
  }
}

check('a) STG policy-off + required path OK => HEALTHY (expected-off is neutral)', () => {
  const vm = buildSatelliteBusViewModel(
    input('stg', { stg: bus('stg', { daemonPolicyOff: true }), dev: bus('dev'), prod: bus('prod') }),
  )
  assert.equal(vm.health, 'healthy')
  assert.equal(vm.healthLabel, 'HEALTHY')
  assert.match(vm.topReason, /expected off/i)
  assert.equal(vm.attention.length, 0)
  const daemonRow = vm.runtimeConsumers.find(r => r.id === 'trading_engine')
  assert.ok(daemonRow)
  assert.equal(daemonRow.stateLabel, 'EXPECTED OFF')
  assert.equal(daemonRow.health, 'expected-off')
  const massiveRow = vm.dataPathConsumers.find(r => r.id === 'massive')
  assert.ok(massiveRow)
  assert.equal(massiveRow.stateLabel, 'EXPECTED OFF')
  // D10: no visible copy suggests enabling live trading / starting the daemon.
  const serialized = JSON.stringify(vm)
  assert.doesNotMatch(serialized, /NO-GO|policy-off fail|start the daemon|enable live/i)
})

check('b) selected required path fail => UNAVAILABLE', () => {
  const vm = buildSatelliteBusViewModel(
    input('stg', {
      stg: bus('stg', { daemonPolicyOff: true, ib_ingestor: component('fail', 'consumer disconnected') }),
    }),
  )
  assert.equal(vm.health, 'unavailable')
  assert.match(vm.topReason, /IB Ingestor/)
  const issue = vm.attention.find(i => i.id === 'stg:ib_ingestor')
  assert.ok(issue)
  assert.equal(issue.severity, 'critical')
  assert.equal(issue.scope, 'selected')
})

check('c) selected required consumer partial => DEGRADED', () => {
  const vm = buildSatelliteBusViewModel(
    input('stg', {
      stg: bus('stg', { daemonPolicyOff: true, ib_operator: component('degraded', 'rpc slow') }),
    }),
  )
  assert.equal(vm.health, 'degraded')
  assert.match(vm.topReason, /IB Operator/)
})

check('c2) runtime consumer partial (APIs) => DEGRADED, not UNAVAILABLE', () => {
  const vm = buildSatelliteBusViewModel(
    input('stg', { stg: bus('stg', { daemonPolicyOff: true }) }, { ok: 5, total: 9 }),
  )
  assert.equal(vm.health, 'degraded')
  assert.match(vm.topReason, /Trade APIs/)
})

check('d) PROD fail while STG selected => STG verdict unchanged + cross-env issue', () => {
  const vm = buildSatelliteBusViewModel(
    input('stg', {
      stg: bus('stg', { daemonPolicyOff: true }),
      prod: bus('prod', { ib_ingestor: component('fail', 'prod consumer down') }),
    }),
  )
  assert.equal(vm.health, 'healthy')
  assert.equal(vm.attention.length, 0)
  const cross = vm.crossEnvIssues.find(i => i.id === 'prod:ib_ingestor')
  assert.ok(cross)
  assert.equal(cross.scope, 'cross-env')
  assert.match(cross.title, /Prod/)
})

check('e) stale/missing required probe => UNKNOWN', () => {
  const missing = buildSatelliteBusViewModel(input('stg', { dev: bus('dev') }))
  assert.equal(missing.health, 'unknown')
  assert.match(missing.topReason, /No bus probe/i)

  const stale = buildSatelliteBusViewModel(
    input('stg', {
      stg: bus('stg', { daemonPolicyOff: true, ib_account_agent: component('unknown', 'Not probed') }),
    }),
  )
  assert.equal(stale.health, 'unknown')
  assert.match(stale.topReason, /missing\/stale/i)
})

check('f) shared gateway down => selected UNAVAILABLE', () => {
  const gw = gatewayDown()
  const vm = buildSatelliteBusViewModel(
    input('stg', {
      stg: bus('stg', { daemonPolicyOff: true, platform_ib_gateway: gw }),
      dev: bus('dev', { platform_ib_gateway: gw }),
      prod: bus('prod', { platform_ib_gateway: gw }),
    }),
  )
  assert.equal(vm.health, 'unavailable')
  assert.match(vm.topReason, /Platform IB Gateway/i)
  const shared = vm.attention.find(i => i.scope === 'shared')
  assert.ok(shared)
  assert.equal(shared.severity, 'critical')
})

check('f2) shared gateway up in one env wins (cluster-shared resource)', () => {
  const vm = buildSatelliteBusViewModel(
    input('stg', {
      stg: bus('stg', { daemonPolicyOff: true, platform_ib_gateway: gatewayDown() }),
      dev: bus('dev', { platform_ib_gateway: gatewayUp() }),
    }),
  )
  // Gateway is shared — dev sees it healthy, so the shared node is healthy.
  assert.equal(vm.path.find(n => n.id === 'gateway')?.health, 'ok')
  assert.equal(vm.health, 'healthy')
})

check('g) unrelated Ground/ops degraded => no verdict poisoning', () => {
  const vm = buildSatelliteBusViewModel(
    input('stg', { stg: bus('stg', { daemonPolicyOff: true, opsFail: true }) }),
  )
  assert.equal(vm.health, 'healthy')
  assert.equal(vm.attention.length, 0)
})

check('h) K8s workload readiness is evidence-only — cannot enter the verdict', () => {
  // Boundary guard: the view-model input deliberately has no workloads field,
  // so K8s workload evidence cannot influence Bus Health by construction.
  type WorkloadsExcluded = 'workloads' extends keyof SatelliteBusViewModelInput ? never : true
  const workloadsExcluded: WorkloadsExcluded = true
  assert.equal(workloadsExcluded, true)
  // And no verdict copy attributes Bus Health to K8s workload readiness.
  const vm = buildSatelliteBusViewModel(input('stg', { stg: bus('stg', { daemonPolicyOff: true }) }))
  assert.doesNotMatch(JSON.stringify(vm), /workload|k8s/i)
})

check('daemon unexpected down (not policy-off) => DEGRADED runtime issue, not UNAVAILABLE', () => {
  const vm = buildSatelliteBusViewModel(
    input('dev', { dev: bus('dev', { daemonAlive: false, accountSyncOk: false }) }),
  )
  assert.equal(vm.health, 'degraded')
  const daemonRow = vm.runtimeConsumers.find(r => r.id === 'trading_engine')
  assert.ok(daemonRow)
  assert.equal(daemonRow.stateLabel, 'UNEXPECTED DOWN')
  assert.ok(vm.attention.some(i => i.id === 'dev:runtime:trading_engine'))
})

check('data path node order: gateway → redis-ib → consumers → selected namespace', () => {
  const vm = buildSatelliteBusViewModel(input('prod', { prod: bus('prod') }))
  assert.deepEqual(
    vm.path.map(n => n.id),
    ['gateway', 'redis-ib', 'consumers', 'namespace'],
  )
  assert.equal(vm.path[0].scopeLabel, 'SHARED')
  assert.equal(vm.path[2].scopeLabel, 'ALL ENVS')
  assert.equal(vm.path[3].scopeLabel, 'SELECTED')
  assert.equal(vm.namespace, 'bifrost-prod')
})

check('redis-ib derived fail when gateway up but all required consumers fail', () => {
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
  assert.equal(vm.health, 'unavailable')
  const redis = vm.path.find(n => n.id === 'redis-ib')
  assert.ok(redis)
  assert.equal(redis.health, 'fail')
})

console.log(`\n${passed} checks passed`)
