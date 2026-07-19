#!/usr/bin/env node
/**
 * Daily Ops workflow resolver unit tests.
 * Usage: npx tsx scripts/daily-ops-workflow-test.ts
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
  dailyOpsStepStatuses,
  resolveDailyOpsWorkflow,
} from '../src/lib/control-room/dailyOpsWorkflow'
import { buildFleetSnapshot } from '../src/lib/control-room/buildFleetSnapshot'
import { pickFleetFixCell } from '../src/lib/control-room/fleetCellFix'

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

const bridgeOk = {
  generated_at: 't',
  remediation_runner: { url: 'http://127.0.0.1:8781', status: 'ok' as const },
  git_bridge: { status: 'ok' as const, dirty_repos: 0 },
  satellite_probe_bridge: { status: 'ok' as const },
  hermes_mcp: { status: 'ok' as const },
  nous_hermes: {
    status: 'ok' as const,
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
}

let passed = 0
function check(name: string, fn: () => void) {
  try {
    fn()
    passed += 1
    console.log(`ok  ${name}`)
  } catch (e) {
    console.error(`FAIL  ${name}`)
    throw e
  }
}

function clearFleet() {
  return buildFleetSnapshot({
    viewerEnv: 'dev',
    matrices: [matrix('dev'), matrix('stg'), matrix('prod')],
    self: selfHealth(['dev', 'stg', 'prod']),
    stg: stgSmokeOk,
    supply: supplyOk,
    cluster: clusterOk,
    groundBridgeReady: true,
    runner: { status: 'ok' },
    bridge: bridgeOk,
  })
}

function noGoSatelliteDev() {
  return buildFleetSnapshot({
    viewerEnv: 'dev',
    matrices: [matrix('dev', false), matrix('stg'), matrix('prod')],
    self: selfHealth(['dev', 'stg', 'prod']),
    stg: stgSmokeOk,
    supply: supplyOk,
    cluster: clusterOk,
    groundBridgeReady: true,
    runner: { status: 'ok' },
    bridge: bridgeOk,
  })
}

check('fleetClear + queue 0 → clear + Run daily check', () => {
  const r = resolveDailyOpsWorkflow({ fleet: clearFleet(), queueOpen: 0 })
  assert.equal(r.activePhase, 'clear')
  assert.equal(r.primaryAction.kind, 'run-check')
  assert.equal(r.primaryAction.label, 'Run daily check')
  const steps = dailyOpsStepStatuses(r)
  assert.equal(steps.discover, 'done')
  assert.equal(steps.clear, 'done')
})

check('fleetClear + queue open → clear queue', () => {
  const r = resolveDailyOpsWorkflow({ fleet: clearFleet(), queueOpen: 2 })
  assert.equal(r.activePhase, 'clear')
  assert.equal(r.primaryAction.kind, 'clear-queue')
  assert.equal(r.primaryAction.tabId, 'control-room')
})

check('agentPending → remediate View agent', () => {
  const fleet = noGoSatelliteDev()
  const r = resolveDailyOpsWorkflow({ fleet, agentPending: true, queueOpen: 0 })
  assert.equal(r.activePhase, 'remediate')
  assert.equal(r.primaryAction.kind, 'view-agent')
  assert.match(r.primaryAction.label, /View agent/i)
  assert.equal(r.primaryAction.tabId, 'agent-desk')
})

check('agentJustSucceeded + !fleetClear → verify', () => {
  const fleet = noGoSatelliteDev()
  const r = resolveDailyOpsWorkflow({
    fleet,
    agentJustSucceeded: true,
    queueOpen: 0,
  })
  assert.equal(r.activePhase, 'verify')
  assert.equal(r.primaryAction.kind, 'verify')
})

check('NO-GO fixable → remediate agent-fix', () => {
  const fleet = noGoSatelliteDev()
  assert.equal(fleet.fleetClear, false)
  const fix = pickFleetFixCell(fleet)
  assert.ok(fix)
  const r = resolveDailyOpsWorkflow({ fleet, queueOpen: 0 })
  assert.equal(r.activePhase, 'remediate')
  assert.equal(r.primaryAction.kind, 'agent-fix')
  assert.equal(r.targetCellKey, fix.key)
  assert.equal(r.primaryAction.cellKey, fix.key)
})

check('agentPending wins over agentJustSucceeded', () => {
  const fleet = noGoSatelliteDev()
  const r = resolveDailyOpsWorkflow({
    fleet,
    agentPending: true,
    agentJustSucceeded: true,
    queueOpen: 0,
  })
  assert.equal(r.activePhase, 'remediate')
  assert.equal(r.primaryAction.kind, 'view-agent')
})

check('D10 blocker present on remediate path', () => {
  const r = resolveDailyOpsWorkflow({ fleet: noGoSatelliteDev(), queueOpen: 0 })
  assert.ok(r.blockers.some(b => b.includes('D10')))
})

check('Engineer CRITICAL → remediate inline Operator Plan AI Fix', () => {
  const fleet = clearFleet()
  // Force engineer cell to fail for escalate path
  const eng = fleet.cells.find(c => c.role === 'engineer')
  assert.ok(eng)
  eng.signal = 'fail'
  eng.standards = eng.standards.map(s =>
    s.required === false ? s : { ...s, signal: 'fail' as const },
  )
  eng.escalateTabId = 'operator-plane'
  eng.agentFixEnabled = false
  eng.detail = 'Mac seat CRITICAL'
  // Recompute verdict-like inputs via resolveDailyOpsWorkflow (uses pickFleetFixCell + engineer escalate)
  // Mutate snapshot verdict worstCell for engineer escalate helper
  fleet.fleetClear = false
  fleet.verdict = {
    kind: 'NO-GO',
    topReason: eng.detail,
    primaryCta: {
      label: 'Open Operator Plane',
      tabId: 'operator-plane',
      cellKey: eng.key,
      kind: 'navigate',
    },
    worstCell: eng,
  }
  const r = resolveDailyOpsWorkflow({ fleet, queueOpen: 0 })
  assert.equal(r.activePhase, 'remediate')
  assert.equal(r.primaryAction.kind, 'operator-plan')
  assert.equal(r.primaryAction.tabId, 'operator-plane')
  assert.equal(r.primaryAction.label, 'AI Fix · Operator Plan')
  assert.ok(r.blockers.some(b => /Engineer|Operator Plan/i.test(b)))
  const steps = dailyOpsStepStatuses(r)
  assert.equal(steps.remediate, 'active')
})

check('circle stepper: agent-fix remediate is active not blocked', () => {
  const fleet = noGoSatelliteDev()
  const r = resolveDailyOpsWorkflow({ fleet, queueOpen: 0 })
  const steps = dailyOpsStepStatuses(r)
  assert.equal(steps.discover, 'done')
  assert.equal(steps.remediate, 'active')
  assert.equal(steps.verify, 'planned')
})

check('Discover (no fixable cell) → AI Check primary', () => {
  const fleet = clearFleet()
  // NO-GO without Agent Fix path: degrade a non-fixable observe-only style cell via verdict only
  const eng = fleet.cells.find(c => c.role === 'engineer')
  assert.ok(eng)
  fleet.fleetClear = false
  fleet.verdict = {
    kind: 'HOLD',
    topReason: 'Checklist gaps — run AI Check',
    primaryCta: {
      label: 'Open Operator Plane',
      tabId: 'operator-plane',
      cellKey: eng.key,
      kind: 'navigate',
    },
    worstCell: eng,
  }
  // Engineer escalate requires fail + escalate route; keep eng GO so we stay on Discover
  eng.signal = 'ok'
  eng.agentFixEnabled = false
  const r = resolveDailyOpsWorkflow({ fleet, queueOpen: 0 })
  assert.equal(r.activePhase, 'discover')
  assert.equal(r.primaryAction.kind, 'ai-check')
  assert.equal(r.primaryAction.label, 'AI Check')
  // Navigate CTA must not steal the strip primary
  assert.notEqual(r.primaryAction.kind, 'navigate')
})

console.log(`\n${passed} checks passed`)
