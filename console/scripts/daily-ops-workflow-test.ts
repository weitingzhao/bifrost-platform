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
import {
  fixPathLabel,
  fixTargetNextStep,
  resolveAmbientJobFixTarget,
  type DailyOpsBlocker,
} from '../src/lib/control-room/dailyOpsPrimaryBlocker'
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
    ibGateway: { reachability: 'ok', reachable: true, summary: 'IB Gateway ready' },
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
    ibGateway: { reachability: 'ok', reachable: true, summary: 'IB Gateway ready' },
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
  assert.equal(r.primaryAction.tabId, 'queue')
})

check('agentPending → remediate View agent', () => {
  const fleet = noGoSatelliteDev()
  const r = resolveDailyOpsWorkflow({ fleet, agentPending: true, queueOpen: 0 })
  assert.equal(r.activePhase, 'remediate')
  assert.equal(r.primaryAction.kind, 'view-agent')
  assert.match(r.primaryAction.label, /View agent/i)
  assert.equal(r.primaryAction.tabId, 'queue')
  // Fix target binding: keep primary blocker while Agent is in flight
  assert.ok(r.primaryBlocker)
})

check('Fix target helpers: Manual Mac seat + misaligned git job', () => {
  const mac: DailyOpsBlocker = {
    itemId: 'mac-probe-bridge',
    stepId: 'engineer-seat',
    stepOrder: 3,
    label: 'Mac seat · probe-bridge',
    group: 'seat',
    signal: 'fail',
    fixCapability: 'manual',
    fixScope: null,
    manualAction: 'Physical: verify Mac is powered on',
    critical: false,
    cellKey: 'engineer:span',
    standardId: 'mac-seat',
    reason: 'not_configured',
  }
  assert.equal(fixPathLabel(mac), 'Manual')
  assert.equal(fixTargetNextStep(mac, 'Mac seat: verify power & bridge →'), 'Mac seat: verify power & bridge')
  assert.equal(fixTargetNextStep(mac, 'View agent'), 'Mac seat: verify power & bridge')

  const job = resolveAmbientJobFixTarget({
    checklistItemId: 'git-bridge',
    primaryBlocker: mac,
    scopeFallbackLabel: 'Operator plane',
  })
  assert.ok(job)
  assert.equal(job.label, 'Git bridge healthy + clean')
  assert.equal(job.pathLabel, 'Semi/Auto')
  assert.equal(job.alignsWithPrimary, false)
})

check('Fix target helpers: job aligns with primary git blocker', () => {
  const git: DailyOpsBlocker = {
    itemId: 'git-bridge',
    stepId: 'engineer-seat',
    stepOrder: 3,
    label: 'Git bridge healthy + clean',
    group: 'automation',
    signal: 'degraded',
    fixCapability: 'semi_auto',
    fixScope: 'git-dirty-remediate',
    critical: false,
    cellKey: 'engineer:span',
    standardId: 'git-bridge',
    reason: 'dirty',
  }
  assert.equal(fixPathLabel(git), 'Semi/Auto')
  const job = resolveAmbientJobFixTarget({
    checklistItemId: 'git-bridge',
    jobScope: 'git-dirty-remediate',
    primaryBlocker: git,
  })
  assert.ok(job)
  assert.equal(job.alignsWithPrimary, true)
})

check('git dirty primary → Propose commit (not AI Fix · Operator Plan)', () => {
  const fleet = clearFleet()
  const eng = fleet.cells.find(c => c.role === 'engineer')
  assert.ok(eng)
  eng.signal = 'degraded'
  eng.standards = eng.standards.map(s => {
    if (s.id === 'git-bridge') {
      return { ...s, signal: 'degraded' as const, reason: 'Git bridge 1 dirty repo(s)' }
    }
    if (s.id === 'mac-seat') {
      return { ...s, signal: 'ok' as const }
    }
    return { ...s, signal: 'ok' as const }
  })
  eng.escalateTabId = 'operator-plane'
  eng.agentFixEnabled = false
  eng.detail = 'Git dirty'
  fleet.fleetClear = false
  fleet.verdict = {
    kind: 'NO-GO',
    topReason: eng.detail,
    primaryCta: {
      label: 'Open Operator Plane',
      tabId: 'operator-plane',
      cellKey: eng.key,
    },
    worstCell: eng,
  }
  const r = resolveDailyOpsWorkflow({ fleet, queueOpen: 0 })
  assert.equal(r.activePhase, 'remediate')
  assert.equal(r.primaryAction.kind, 'propose-commit')
  assert.equal(r.primaryAction.label, 'Propose commit')
  assert.ok(r.blockers.some(b => /Review dirty repos/i.test(b)))
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

check('Engineer CRITICAL Mac seat FAIL → manual-next (not AI Fix)', () => {
  const fleet = clearFleet()
  const eng = fleet.cells.find(c => c.role === 'engineer')
  assert.ok(eng)
  eng.signal = 'fail'
  eng.standards = eng.standards.map(s => {
    if (s.id === 'mac-seat') {
      return { ...s, signal: 'fail' as const, reason: 'Mac seat · probe-bridge not_configured' }
    }
    if (s.id === 'git-bridge') {
      return { ...s, signal: 'degraded' as const, reason: 'Git bridge 1 dirty repo(s)' }
    }
    return { ...s, signal: 'ok' as const }
  })
  eng.escalateTabId = 'operator-plane'
  eng.agentFixEnabled = false
  eng.detail = 'Mac seat CRITICAL'
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
  assert.equal(r.primaryAction.kind, 'manual-next')
  assert.match(r.primaryAction.label, /Mac seat/i)
  assert.notEqual(r.primaryAction.kind, 'operator-plan')
  assert.ok(r.blockers.some(b => b.startsWith('Next:')))
  // Mixed: secondary Propose commit for git dirty sibling (not magic AI Fix)
  assert.ok(r.primaryAction.secondary)
  assert.equal(r.primaryAction.secondary?.kind, 'propose-commit')
  assert.match(r.primaryAction.secondary?.label ?? '', /Also: Propose commit/i)
  // Execution → Now Fix target must share this primary blocker
  assert.ok(r.primaryBlocker)
  assert.equal(r.primaryBlocker?.itemId, 'mac-probe-bridge')
  assert.equal(fixPathLabel(r.primaryBlocker!), 'Manual')
  assert.equal(fixTargetNextStep(r.primaryBlocker!, r.primaryAction.label), r.primaryAction.label)
  const steps = dailyOpsStepStatuses(r)
  assert.equal(steps.remediate, 'active')
})

check('Engineer CRITICAL AI-fixable only → Operator Plan AI Fix', () => {
  const fleet = clearFleet()
  // Force engineer cell to fail for escalate path
  const eng = fleet.cells.find(c => c.role === 'engineer')
  assert.ok(eng)
  eng.signal = 'fail'
  eng.standards = eng.standards.map(s => {
    if (s.id === 'mac-seat') {
      return { ...s, signal: 'ok' as const, required: false }
    }
    // runners / git fail — semi_auto with fixScope
    return { ...s, signal: 'fail' as const }
  })
  eng.escalateTabId = 'operator-plane'
  eng.agentFixEnabled = false
  eng.detail = 'Runners CRITICAL'
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
  assert.ok(r.blockers.some(b => /Next:|Engineer|Operator Plan/i.test(b)))
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
