#!/usr/bin/env node
/**
 * Fleet snapshot unit tests.
 * Usage: npx tsx scripts/fleet-snapshot-test.ts
 */
import assert from 'node:assert/strict'
import type {
  ClusterSummary,
  MatrixResponse,
  SelfHealthResponse,
  StgSmokeResponse,
  Target,
} from '../src/api/types'
import {
  DELIVER_STG_RECOVER_SCOPE,
} from '../src/lib/agent/agentScopes'
import { PROD_ENV_FIX_SCOPE } from '../src/lib/agent/prodEnvironmentFixPrompt'
import { buildFleetSnapshot } from '../src/lib/control-room/buildFleetSnapshot'
import {
  getCell,
  normalizeViewerEnv,
  operateQueueClearLabel,
  resolveCellGate,
  resolveFleetVerdict,
  viewerEnvBadgeLabel,
  type FleetCell,
} from '../src/lib/control-room/fleetSnapshot'
import {
  cellAllowsAgentFix,
  lookupFleetFixRoute,
  pickFleetFixCell,
  resolveCellFixScope,
} from '../src/lib/control-room/fleetCellFix'

function target(id: string, ok: boolean): Target {
  return {
    id,
    category: 'trade_http',
    reachability: ok ? 'ok' : 'fail',
    auth: 'skipped',
    authorization_level: 'L0',
    detail: ok ? 'ok' : 'down',
  }
}

function matrix(env: string, ok = true): MatrixResponse {
  return {
    environment: env,
    label: env,
    generated_at: '2026-07-18T00:00:00Z',
    principal: { name: 't', level: 'L0' },
    targets: [target(`${env}-api`, ok)],
  }
}

function selfHealth(
  envs: Array<'dev' | 'stg' | 'prod'>,
  status: 'ok' | 'fail' = 'ok',
  viewerEnv = 'dev',
): SelfHealthResponse {
  return {
    generated_at: '2026-07-18T00:00:00Z',
    overall: status,
    viewer_env: viewerEnv,
    probes: envs.map(env => ({
      id: `platform-api-${env}`,
      category: 'api',
      env,
      status,
      detail: 'HTTP 200',
      latency_ms: 1,
    })),
  }
}

const clusterOk: ClusterSummary = {
  cluster_id: 'c',
  label: 'c',
  distribution: 'k3s',
  api_server: '',
  kubeconfig_path: '',
  reachability: 'ok',
  detail: 'ok',
  nodes_ready: 3,
  nodes_total: 3,
  failing_pods: 0,
  running_pods: 10,
  pending_pods: 0,
  generated_at: '2026-07-18T00:00:00Z',
}

const stgSmokeOk = {
  generated_at: '2026-07-18T00:00:00Z',
  targets: [
    { id: 'fe', label: 'FE', reachability: 'ok', detail: 'ok' },
    { id: 'gw', label: 'GW', reachability: 'ok', detail: 'ok' },
  ],
} as StgSmokeResponse

const supplyOk = {
  cluster_id: 'c',
  cicd_namespace: 'cicd',
  stg_namespace: 'bifrost-stg',
  reachability: 'ok' as const,
  detail: 'ok',
  mirror_credentials_configured: true,
  default_revision: 'main',
  tracked_repos: [],
  dockerfile_configmaps: [],
  stg_workloads: [],
  generated_at: '2026-07-18T00:00:00Z',
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

check('normalizeViewerEnv', () => {
  assert.equal(normalizeViewerEnv('PROD'), 'prod')
  assert.equal(normalizeViewerEnv('dev-local'), 'dev-local')
  assert.equal(viewerEnvBadgeLabel('stg'), 'STG')
  assert.equal(normalizeViewerEnv('nope'), 'dev')
})

check('STG satellite from smoke when matrix missing', () => {
  const snap = buildFleetSnapshot({
    viewerEnv: 'dev',
    matrices: [matrix('dev'), matrix('prod')],
    stg: stgSmokeOk,
    self: selfHealth(['stg', 'prod']),
    cluster: clusterOk,
  })
  const stgSat = getCell(snap, 'satellite', 'stg')
  assert.ok(stgSat)
  assert.equal(stgSat.signal, 'ok')
  assert.ok(stgSat.standards.length > 0)
  assert.ok(
    stgSat.standards.some(
      s => s.id === 'stg-smoke' || s.group === 'api' || s.group === 'release' || /fe|gw/i.test(s.label),
    ),
  )
})

check('STG unavailable when matrix and smoke missing', () => {
  const snap = buildFleetSnapshot({
    viewerEnv: 'dev',
    matrices: [matrix('dev'), matrix('prod')],
    self: selfHealth(['prod']),
  })
  const stgSat = getCell(snap, 'satellite', 'stg')
  assert.ok(stgSat)
  assert.equal(stgSat.signal, 'unavailable')
})

check('viewer=dev rocket DEV not unavailable', () => {
  const snap = buildFleetSnapshot({
    viewerEnv: 'dev',
    matrices: [matrix('dev'), matrix('stg'), matrix('prod')],
    self: {
      generated_at: 't',
      overall: 'ok',
      viewer_env: 'dev',
      probes: [{ id: 'a', category: 'api', env: 'prod', status: 'ok', detail: 'x', latency_ms: 1 }],
    },
  })
  const rocketDev = getCell(snap, 'rocket', 'dev')
  assert.ok(rocketDev)
  assert.notEqual(rocketDev.signal, 'unavailable')
})

check('viewer=prod rocket DEV unavailable without dev probes', () => {
  const snap = buildFleetSnapshot({
    viewerEnv: 'prod',
    matrices: [matrix('dev'), matrix('stg'), matrix('prod')],
    self: selfHealth(['prod', 'stg'], 'ok', 'prod'),
  })
  const rocketDev = getCell(snap, 'rocket', 'dev')
  assert.ok(rocketDev)
  assert.equal(rocketDev.signal, 'unavailable')
})

check('board columns are DEV/STG/PROD only — no Mac column', () => {
  const snap = buildFleetSnapshot({
    viewerEnv: 'prod',
    matrices: [matrix('dev'), matrix('stg'), matrix('prod')],
    self: selfHealth(['prod', 'stg'], 'ok', 'prod'),
    groundBridgeReady: false,
  })
  assert.deepEqual(snap.columns, ['dev', 'stg', 'prod'])
  assert.equal(getCell(snap, 'satellite', 'dev-local' as never), undefined)
  assert.equal(getCell(snap, 'rocket', 'dev-local' as never), undefined)
})

check('Engineer folds Mac seat; local bridge fail degrades Engineer', () => {
  const snap = buildFleetSnapshot({
    viewerEnv: 'dev',
    matrices: [matrix('dev'), matrix('stg'), matrix('prod')],
    self: selfHealth(['dev', 'stg', 'prod']),
    stg: stgSmokeOk,
    supply: supplyOk,
    cluster: clusterOk,
    groundBridgeReady: false,
    runner: { status: 'ok' },
    bridge: {
      generated_at: 't',
      remediation_runner: { url: 'http://127.0.0.1:8781', status: 'ok' },
      git_bridge: { status: 'ok', dirty_repos: 0 },
      satellite_probe_bridge: { status: 'fail', error: 'not_configured' },
      hermes_mcp: { status: 'ok' },
      nous_hermes: {
        status: 'ok',
        gateway_running: true,
        active_agents: 0,
        active_sessions: 0,
        mcp_tool_count: 0,
      },
      platform_mcp: {
        server_name: 'p',
        server_version: '1',
        tool_count: 0,
        implemented_count: 0,
        agent_tool_count: 0,
        transport: 'stdio',
        script_path: '',
      },
      nightly_report: { available: false },
    },
  })
  const eng = getCell(snap, 'engineer', 'span')
  assert.ok(eng)
  assert.equal(eng.signal, 'fail')
  assert.match(eng.detail, /Mac seat/)
  // Ground no longer owns Mac bridge
  const ground = getCell(snap, 'ground', 'span')
  assert.ok(ground)
  assert.equal(ground.signal, 'ok')
})

check('Engineer CRITICAL → Operator Plane CTA', () => {
  const cells: FleetCell[] = [
    {
      key: 'engineer:span',
      role: 'engineer',
      env: null,
      span: true,
      signal: 'fail',
      value: 'down',
      detail: 'Runners down',
      probePath: 'bridge',
      standards: [
        {
          id: 'runners',
          label: 'Agent runners (HA)',
          signal: 'fail',
          reason: 'Runners down',
          group: 'automation',
        },
      ],
      fixScope: null,
      agentFixEnabled: false,
      agentFixDisabledReason: 'Engineer CRITICAL',
      escalateTabId: 'operator-plane',
      countsTowardVerdict: true,
    },
  ]
  const v = resolveFleetVerdict(cells)
  assert.equal(v.kind, 'NO-GO')
  assert.equal(v.primaryCta.tabId, 'operator-plane')
  assert.equal(v.primaryCta.kind, 'navigate')
})

check('operateQueueClearLabel demotes Clear', () => {
  assert.equal(operateQueueClearLabel(0, false), 'Queue clear · fleet not clear')
  assert.equal(operateQueueClearLabel(0, true), 'Clear')
  assert.equal(operateQueueClearLabel(2, false), '2 open')
})

check('all green + Mac seat ok → GO + fleetClear', () => {
  const snap = buildFleetSnapshot({
    viewerEnv: 'dev',
    matrices: [matrix('dev'), matrix('stg'), matrix('prod')],
    self: selfHealth(['dev', 'stg', 'prod']),
    stg: stgSmokeOk,
    supply: supplyOk,
    cluster: clusterOk,
    groundBridgeReady: true,
    runner: { status: 'ok' },
    ibGateway: { reachability: 'ok', reachable: true, summary: 'IB Gateway ready' },
    postgresBackup: {
      fresh: true,
      signal: 'ok',
      detail: 'last completed 1h ago',
      max_age_hours: 48,
      backup_count: 1,
      generated_at: '2026-07-18T00:00:00Z',
    },
    bridge: {
      generated_at: 't',
      remediation_runner: { url: 'http://127.0.0.1:8781', status: 'ok' },
      git_bridge: { status: 'ok', dirty_repos: 0 },
      satellite_probe_bridge: { status: 'ok' },
      hermes_mcp: { status: 'ok' },
      nous_hermes: {
        status: 'ok',
        gateway_running: true,
        active_agents: 0,
        active_sessions: 0,
        mcp_tool_count: 0,
      },
      platform_mcp: {
        server_name: 'p',
        server_version: '1',
        tool_count: 0,
        implemented_count: 0,
        agent_tool_count: 0,
        transport: 'stdio',
        script_path: '',
      },
      nightly_report: { available: false },
    },
  })
  assert.deepEqual(snap.columns, ['dev', 'stg', 'prod'])
  const eng = getCell(snap, 'engineer', 'span')
  assert.ok(eng)
  assert.equal(eng.signal, 'ok')
  assert.match(eng.detail, /Mac seat/)
  const vendor = getCell(snap, 'vendor', 'span')
  assert.ok(vendor)
  assert.equal(resolveCellGate(vendor), 'GO')
  assert.equal(snap.verdict.kind, 'GO')
  assert.equal(snap.fleetClear, true)
  assert.equal(snap.fleetNominal, true)
})

check('PROD viewer all green + Rocket DEV structural unavailable → GO (Mac info-only)', () => {
  const snap = buildFleetSnapshot({
    viewerEnv: 'prod',
    matrices: [matrix('dev'), matrix('stg'), matrix('prod')],
    self: selfHealth(['prod', 'stg'], 'ok', 'prod'),
    stg: stgSmokeOk,
    supply: supplyOk,
    cluster: clusterOk,
    groundBridgeReady: false,
    runner: { status: 'ok' },
    ibGateway: { reachability: 'ok', reachable: true, summary: 'IB Gateway ready' },
    postgresBackup: {
      fresh: true,
      signal: 'ok',
      detail: 'last completed 1h ago',
      max_age_hours: 48,
      backup_count: 1,
      generated_at: '2026-07-18T00:00:00Z',
    },
    bridge: {
      generated_at: 't',
      remediation_runner: { url: 'http://127.0.0.1:8781', status: 'ok' },
      git_bridge: { status: 'ok', dirty_repos: 0 },
      satellite_probe_bridge: { status: 'fail', error: 'unreachable from prod' },
      hermes_mcp: { status: 'ok' },
      nous_hermes: {
        status: 'ok',
        gateway_running: true,
        active_agents: 0,
        active_sessions: 0,
        mcp_tool_count: 0,
      },
      platform_mcp: {
        server_name: 'p',
        server_version: '1',
        tool_count: 0,
        implemented_count: 0,
        agent_tool_count: 0,
        transport: 'stdio',
        script_path: '',
      },
      nightly_report: { available: false },
    },
  })
  const rocketDev = getCell(snap, 'rocket', 'dev')
  assert.ok(rocketDev)
  assert.equal(rocketDev.signal, 'unavailable')
  assert.equal(rocketDev.countsTowardVerdict, false)
  // Remote Mac fail must not NO-GO Engineer / fleet
  const eng = getCell(snap, 'engineer', 'span')
  assert.ok(eng)
  assert.equal(eng.signal, 'ok')
  assert.match(eng.detail, /Mac seat/)
  assert.equal(snap.verdict.kind, 'GO')
  assert.equal(snap.fleetClear, true)
})

check('Vendor NO-GO when IB Gateway plugin missing or fail', () => {
  const snap = buildFleetSnapshot({
    viewerEnv: 'dev',
    matrices: [matrix('dev'), matrix('stg'), matrix('prod')],
    self: selfHealth(['dev', 'stg', 'prod']),
    stg: stgSmokeOk,
    supply: supplyOk,
    cluster: clusterOk,
    groundBridgeReady: true,
    runner: { status: 'ok' },
    ibGateway: { reachability: 'fail', reachable: false, error: 'IB Client not running' },
    bridge: {
      generated_at: 't',
      remediation_runner: { url: 'http://127.0.0.1:8781', status: 'ok' },
      git_bridge: { status: 'ok', dirty_repos: 0 },
      satellite_probe_bridge: { status: 'ok' },
      hermes_mcp: { status: 'ok' },
      nous_hermes: {
        status: 'ok',
        gateway_running: true,
        active_agents: 0,
        active_sessions: 0,
        mcp_tool_count: 0,
      },
      platform_mcp: {
        server_name: 'p',
        server_version: '1',
        tool_count: 0,
        implemented_count: 0,
        agent_tool_count: 0,
        transport: 'stdio',
        script_path: '',
      },
      nightly_report: { available: false },
    },
  })
  const vendor = getCell(snap, 'vendor', 'span')!
  assert.equal(resolveCellGate(vendor), 'NO-GO')
  assert.match(vendor.detail, /IB/i)
  assert.equal(snap.verdict.kind, 'NO-GO')
  assert.equal(vendor.agentFixEnabled, false)
})

check('Vendor IB fails when live heartbeat stale despite connected flag', () => {
  const now = Date.now()
  const snap = buildFleetSnapshot({
    viewerEnv: 'dev',
    matrices: [matrix('dev'), matrix('stg'), matrix('prod')],
    self: selfHealth(['dev', 'stg', 'prod']),
    stg: stgSmokeOk,
    supply: supplyOk,
    cluster: clusterOk,
    groundBridgeReady: true,
    runner: { status: 'ok' },
    ibGateway: {
      mode: 'live',
      reachability: 'ok',
      reachable: true,
      summary: 'live · connected (optimistic)',
      ingestor_health: {
        connected: 'True',
        client_id: '70',
        last_msg_ts: String(now / 1000 - 200),
      },
      account_health: {
        host_connected: 'True',
        host_client_id: '70',
        last_msg_ts: String(now / 1000 - 200),
      },
      sample_tick_nvda: JSON.stringify({
        bid: -1,
        ask: -1,
        last: 201,
        ts: now / 1000 - 200,
      }),
      account_snapshot: JSON.stringify({
        host_connected: true,
        secondary_connected: true,
        accounts_snapshot: [{ account_id: 'U1' }],
        updated_at: now / 1000 - 200,
      }),
    },
    bridge: {
      generated_at: 't',
      remediation_runner: { url: 'http://127.0.0.1:8781', status: 'ok' },
      git_bridge: { status: 'ok', dirty_repos: 0 },
      satellite_probe_bridge: { status: 'ok' },
      hermes_mcp: { status: 'ok' },
      nous_hermes: {
        status: 'ok',
        gateway_running: true,
        active_agents: 0,
        active_sessions: 0,
        mcp_tool_count: 0,
      },
      platform_mcp: {
        server_name: 'p',
        server_version: '1',
        tool_count: 0,
        implemented_count: 0,
        agent_tool_count: 0,
        transport: 'stdio',
        script_path: '',
      },
      nightly_report: { available: false },
    },
  })
  const vendor = getCell(snap, 'vendor', 'span')!
  const ib = vendor.standards.find(s => s.id === 'ib-feed')!
  assert.equal(ib.signal, 'fail')
  // Stale snapshot fires before heartbeat — either reason is a fail.
  assert.match(ib.reason, /stale|ghost|empty/i)
  assert.equal(resolveCellGate(vendor), 'NO-GO')
})

check('Vendor IB fails on ghost session (connected + empty accounts_snapshot)', () => {
  const now = Date.now()
  const snap = buildFleetSnapshot({
    viewerEnv: 'dev',
    matrices: [matrix('dev'), matrix('stg'), matrix('prod')],
    self: selfHealth(['dev', 'stg', 'prod']),
    stg: stgSmokeOk,
    supply: supplyOk,
    cluster: clusterOk,
    groundBridgeReady: true,
    runner: { status: 'ok' },
    ibGateway: {
      mode: 'live',
      reachability: 'ok',
      reachable: true,
      summary: 'live · ib-gateway 1/1 · host=true secondary=true · redis-ib ok',
      ingestor_health: {
        connected: 'True',
        client_id: '70',
        last_msg_ts: String(now / 1000),
      },
      account_health: {
        host_connected: 'True',
        host_client_id: '70',
        secondary_connected: 'True',
        secondary_client_id: '72',
        last_msg_ts: String(now / 1000),
      },
      sample_tick_nvda: JSON.stringify({
        bid: -1,
        ask: -1,
        last: 201.26,
        ts: now / 1000,
      }),
      account_snapshot: JSON.stringify({
        host_connected: true,
        secondary_connected: true,
        accounts_snapshot: [],
        updated_at: now / 1000,
      }),
    },
    bridge: {
      generated_at: 't',
      remediation_runner: { url: 'http://127.0.0.1:8781', status: 'ok' },
      git_bridge: { status: 'ok', dirty_repos: 0 },
      satellite_probe_bridge: { status: 'ok' },
      hermes_mcp: { status: 'ok' },
      nous_hermes: {
        status: 'ok',
        gateway_running: true,
        active_agents: 0,
        active_sessions: 0,
        mcp_tool_count: 0,
      },
      platform_mcp: {
        server_name: 'p',
        server_version: '1',
        tool_count: 0,
        implemented_count: 0,
        agent_tool_count: 0,
        transport: 'stdio',
        script_path: '',
      },
      nightly_report: { available: false },
    },
  })
  const vendor = getCell(snap, 'vendor', 'span')!
  const ib = vendor.standards.find(s => s.id === 'ib-feed')!
  assert.equal(ib.signal, 'fail')
  assert.match(ib.reason, /ghost|empty/i)
  assert.equal(resolveCellGate(vendor), 'NO-GO')
  assert.equal(vendor.agentFixEnabled, false)
})

check('fail → NO-GO; degraded → NO-GO (not green)', () => {
  const failSnap = buildFleetSnapshot({
    viewerEnv: 'dev',
    matrices: [matrix('dev', false), matrix('stg'), matrix('prod')],
    self: selfHealth(['dev', 'stg', 'prod']),
    stg: stgSmokeOk,
    cluster: clusterOk,
  })
  assert.equal(failSnap.verdict.kind, 'NO-GO')
  assert.equal(failSnap.fleetClear, false)

  const degCells: FleetCell[] = [
    {
      key: 'satellite:dev',
      role: 'satellite',
      env: 'dev',
      span: false,
      signal: 'degraded',
      value: '1/2',
      detail: 'partial',
      probePath: 'matrix',
      standards: [
        {
          id: 'svc',
          label: 'service',
          signal: 'degraded',
          reason: 'partial',
          group: 'api',
        },
      ],
      fixScope: PROD_ENV_FIX_SCOPE,
      agentFixEnabled: true,
      countsTowardVerdict: true,
    },
  ]
  assert.equal(resolveFleetVerdict(degCells).kind, 'NO-GO')
})

check('fleetCellFix engineer blocked; satellite scopes do not cross', () => {
  const route = lookupFleetFixRoute('engineer', 'span')
  assert.ok(route)
  assert.equal(route.agentFixAllowed, false)
  assert.equal(route.navigateTabId, 'operator-plane')

  assert.equal(lookupFleetFixRoute('satellite', 'stg')?.fixScope, DELIVER_STG_RECOVER_SCOPE)
  assert.equal(lookupFleetFixRoute('satellite', 'dev')?.fixScope, PROD_ENV_FIX_SCOPE)
  assert.equal(lookupFleetFixRoute('satellite', 'prod')?.fixScope, PROD_ENV_FIX_SCOPE)

  const snap = buildFleetSnapshot({
    viewerEnv: 'dev',
    matrices: [matrix('dev', false), matrix('stg'), matrix('prod')],
    self: selfHealth(['stg', 'prod']),
    stg: stgSmokeOk,
    cluster: clusterOk,
  })
  const cell = pickFleetFixCell(snap)
  assert.ok(cell)
  assert.equal(cell.role, 'satellite')
  assert.equal(cell.env, 'dev')
  assert.ok(cellAllowsAgentFix(cell))
  assert.equal(resolveCellFixScope(cell), PROD_ENV_FIX_SCOPE)

  const stgFail = buildFleetSnapshot({
    viewerEnv: 'dev',
    matrices: [matrix('dev'), matrix('stg', false), matrix('prod')],
    self: selfHealth(['dev', 'stg', 'prod']),
    stg: stgSmokeOk,
    cluster: clusterOk,
  })
  const stgCell = getCell(stgFail, 'satellite', 'stg')
  assert.ok(stgCell)
  assert.equal(stgCell.signal, 'fail')
  assert.equal(resolveCellFixScope(stgCell), DELIVER_STG_RECOVER_SCOPE)
  assert.notEqual(resolveCellFixScope(stgCell), PROD_ENV_FIX_SCOPE)
})

console.log(`\n${passed} checks passed`)
