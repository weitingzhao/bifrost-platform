/**
 * Fleet snapshot pure-logic unit tests.
 * Mirrors scripts/fleet-snapshot-test.ts but ported to Vitest with additional
 * targeted coverage for individual exported helpers.
 */
import { describe, expect, it } from 'vitest'
import type { ClusterSummary } from '@/api/clusterTypes'
import type { MatrixResponse, SelfHealthResponse, Target } from '@/api/matrixTypes'
import type { StgSmokeResponse } from '@/api/deliveryTypes'
import { DELIVER_STG_RECOVER_SCOPE } from '@/lib/agent/agentScopes'
import { PROD_ENV_FIX_SCOPE } from '@/lib/agent/prodEnvironmentFixPrompt'
import { buildFleetSnapshot } from '@/lib/control-room/buildFleetSnapshot'
import {
  cellCountsTowardVerdict,
  cellKey,
  fleetCellNavigateTab,
  fleetRoleNavigateTab,
  getCell,
  groupStandards,
  normalizeViewerEnv,
  operateQueueClearLabel,
  pickWorstCell,
  resolveCellGate,
  resolveFleetVerdict,
  resolveIbClientStandard,
  rollupStandards,
  severityRank,
  signalFromStandards,
  std,
  viewerEnvBadgeLabel,
  type FleetCell,
  type FleetStandard,
} from '@/lib/control-room/fleetSnapshot'
import {
  cellAllowsAgentFix,
  lookupFleetFixRoute,
  pickFleetFixCell,
  resolveCellFixScope,
} from '@/lib/control-room/fleetCellFix'

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

const stgSmokeOk: StgSmokeResponse = {
  cluster_id: 'c',
  reachability: 'ok',
  detail: 'ok',
  generated_at: '2026-07-18T00:00:00Z',
  targets: [
    { id: 'fe', url: 'https://fe.example', reachability: 'ok', detail: 'ok' },
    { id: 'gw', url: 'https://gw.example', reachability: 'ok', detail: 'ok' },
  ],
}

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
    transport: 'stdio' as const,
    script_path: '',
  },
  nightly_report: { available: false },
}

describe('std', () => {
  it('defaults required=true and source=probe', () => {
    const s = std('id-1', 'Label', 'ok', 'reason', 'api')
    expect(s.required).toBe(true)
    expect(s.source).toBe('probe')
  })

  it('honors explicit required/source overrides', () => {
    const s = std('id-2', 'Label', 'degraded', 'reason', 'feed', false, 'checklist')
    expect(s.required).toBe(false)
    expect(s.source).toBe('checklist')
  })
})

describe('signalFromStandards', () => {
  it('returns unknown when there are no required standards', () => {
    const standards: FleetStandard[] = [std('a', 'A', 'ok', 'r', 'api', false)]
    expect(signalFromStandards(standards)).toBe('unknown')
  })

  it('returns ok only when every required standard is ok', () => {
    const standards: FleetStandard[] = [
      std('a', 'A', 'ok', 'r', 'api'),
      std('b', 'B', 'ok', 'r', 'api'),
    ]
    expect(signalFromStandards(standards)).toBe('ok')
  })

  it('prioritizes fail over degraded and unavailable', () => {
    const standards: FleetStandard[] = [
      std('a', 'A', 'degraded', 'r', 'api'),
      std('b', 'B', 'fail', 'r', 'api'),
      std('c', 'C', 'unavailable', 'r', 'api'),
    ]
    expect(signalFromStandards(standards)).toBe('fail')
  })

  it('prioritizes degraded over unavailable when no fail present', () => {
    const standards: FleetStandard[] = [
      std('a', 'A', 'degraded', 'r', 'api'),
      std('b', 'B', 'unavailable', 'r', 'api'),
    ]
    expect(signalFromStandards(standards)).toBe('degraded')
  })
})

describe('resolveCellGate', () => {
  function cell(overrides: Partial<FleetCell> = {}): FleetCell {
    return {
      key: 'satellite:dev',
      role: 'satellite',
      env: 'dev',
      span: false,
      signal: 'ok',
      value: '1/1',
      detail: '',
      probePath: '',
      standards: [std('a', 'A', 'ok', 'r', 'api')],
      fixScope: null,
      agentFixEnabled: false,
      ...overrides,
    }
  }

  it('returns N/A for cells that do not count toward the verdict', () => {
    expect(resolveCellGate(cell({ countsTowardVerdict: false }))).toBe('N/A')
  })

  it('returns GO when all required standards are ok', () => {
    expect(resolveCellGate(cell())).toBe('GO')
  })

  it('returns NO-GO when a required standard is not ok', () => {
    expect(resolveCellGate(cell({ standards: [std('a', 'A', 'fail', 'r', 'api')] }))).toBe(
      'NO-GO',
    )
  })

  it('falls back to cell.signal when there are no standards at all', () => {
    expect(resolveCellGate(cell({ standards: [], signal: 'ok' }))).toBe('GO')
    expect(resolveCellGate(cell({ standards: [], signal: 'fail' }))).toBe('NO-GO')
  })

  it('ignores optional (required=false) standards', () => {
    const c = cell({ standards: [std('a', 'A', 'fail', 'r', 'api', false)] })
    expect(resolveCellGate(c)).toBe('GO')
  })
})

describe('rollupStandards', () => {
  it('groups by taxonomy group in FLEET_STANDARD_GROUP_ORDER order', () => {
    const standards: FleetStandard[] = [
      std('edge-1', 'Edge', 'ok', 'r', 'edge'),
      std('api-1', 'Api', 'ok', 'r', 'api'),
      std('api-2', 'Api2', 'fail', 'r', 'api'),
    ]
    const rollup = rollupStandards(standards)
    const groups = rollup.map(r => r.group)
    expect(groups.indexOf('edge')).toBeLessThan(groups.indexOf('api'))
    const apiRollup = rollup.find(r => r.group === 'api')
    expect(apiRollup).toMatchObject({ ok: 1, total: 2, signal: 'fail' })
  })

  it('omits groups with no members', () => {
    const rollup = rollupStandards([std('a', 'A', 'ok', 'r', 'cluster')])
    expect(rollup).toHaveLength(1)
    expect(rollup[0].group).toBe('cluster')
  })

  it('rolls up optional-only groups using all members (not just required)', () => {
    const rollup = rollupStandards([std('a', 'A', 'ok', 'r', 'release', false)])
    expect(rollup[0]).toMatchObject({ ok: 1, total: 1, signal: 'ok' })
  })
})

describe('groupStandards', () => {
  it('preserves canonical group order and buckets members', () => {
    const standards: FleetStandard[] = [
      std('feed-1', 'Feed', 'ok', 'r', 'feed'),
      std('control-1', 'Control', 'ok', 'r', 'control'),
    ]
    const grouped = groupStandards(standards)
    expect(grouped.map(g => g.group)).toEqual(['control', 'feed'])
    expect(grouped.find(g => g.group === 'feed')?.items).toHaveLength(1)
  })
})

describe('normalizeViewerEnv / viewerEnvBadgeLabel', () => {
  it('normalizes known viewer envs and defaults unknown values to dev', () => {
    expect(normalizeViewerEnv('PROD')).toBe('prod')
    expect(normalizeViewerEnv('dev-local')).toBe('dev-local')
    expect(normalizeViewerEnv('nope')).toBe('dev')
    expect(normalizeViewerEnv(undefined)).toBe('dev')
    expect(normalizeViewerEnv(null)).toBe('dev')
  })

  it('renders badge labels per viewer env', () => {
    expect(viewerEnvBadgeLabel('stg')).toBe('STG')
    expect(viewerEnvBadgeLabel('prod')).toBe('PROD')
    expect(viewerEnvBadgeLabel('dev-local')).toBe('DEV-LOCAL')
    expect(viewerEnvBadgeLabel('dev')).toBe('DEV')
  })
})

describe('cellKey / fleetCellNavigateTab / fleetRoleNavigateTab', () => {
  it('builds a stable role:env key', () => {
    expect(cellKey('rocket', 'dev')).toBe('rocket:dev')
    expect(cellKey('engineer', 'span')).toBe('engineer:span')
  })

  it('maps each role to its default navigate tab', () => {
    expect(fleetRoleNavigateTab('rocket')).toBe('cluster')
    expect(fleetRoleNavigateTab('satellite')).toBe('satellite-bus')
    expect(fleetRoleNavigateTab('engineer')).toBe('agent-desk')
    expect(fleetRoleNavigateTab('ground')).toBe('operator-plane')
    expect(fleetRoleNavigateTab('vendor')).toBe('satellite-bus')
  })

  it('prefers a cell escalateTabId over the role default', () => {
    expect(fleetCellNavigateTab({ role: 'engineer', escalateTabId: 'operator-plane' })).toBe(
      'operator-plane',
    )
    expect(fleetCellNavigateTab({ role: 'rocket' })).toBe('cluster')
  })
})

describe('severityRank / pickWorstCell', () => {
  function cell(signal: FleetCell['signal'], key: string): FleetCell {
    return {
      key,
      role: 'satellite',
      env: 'dev',
      span: false,
      signal,
      value: '',
      detail: '',
      probePath: '',
      standards: [],
      fixScope: null,
      agentFixEnabled: false,
    }
  }

  it('ranks fail highest, then degraded, unavailable, unknown, ok lowest', () => {
    expect(severityRank('fail')).toBeGreaterThan(severityRank('degraded'))
    expect(severityRank('degraded')).toBeGreaterThan(severityRank('unavailable'))
    expect(severityRank('unavailable')).toBeGreaterThan(severityRank('unknown'))
    expect(severityRank('unknown')).toBeGreaterThan(severityRank('ok'))
  })

  it('picks the most severe cell', () => {
    const cells = [cell('ok', 'a'), cell('degraded', 'b'), cell('fail', 'c')]
    expect(pickWorstCell(cells)?.key).toBe('c')
  })

  it('returns null for an empty cell list', () => {
    expect(pickWorstCell([])).toBeNull()
  })
})

describe('cellCountsTowardVerdict', () => {
  function cell(overrides: Partial<FleetCell>): FleetCell {
    return {
      key: 'k',
      role: 'satellite',
      env: 'dev',
      span: false,
      signal: 'ok',
      value: '',
      detail: '',
      probePath: '',
      standards: [],
      fixScope: null,
      agentFixEnabled: false,
      ...overrides,
    }
  }

  it('defaults to true unless signal is unavailable', () => {
    expect(cellCountsTowardVerdict(cell({ signal: 'ok' }))).toBe(true)
    expect(cellCountsTowardVerdict(cell({ signal: 'unavailable' }))).toBe(false)
  })

  it('respects an explicit countsTowardVerdict override', () => {
    expect(cellCountsTowardVerdict(cell({ signal: 'unavailable', countsTowardVerdict: true }))).toBe(
      true,
    )
    expect(cellCountsTowardVerdict(cell({ signal: 'ok', countsTowardVerdict: false }))).toBe(false)
  })
})

describe('resolveIbClientStandard', () => {
  it('is required=true and unknown when no IB Gateway payload and no matrix targets exist', () => {
    const s = resolveIbClientStandard(undefined, [])
    expect(s.id).toBe('ib-feed')
    expect(s.signal).toBe('unknown')
    expect(s.required).toBe(true)
  })

  it('falls back to the worst matrix IB target when no plugin payload is present', () => {
    const s = resolveIbClientStandard(undefined, [
      { id: 'ib-ok', reachability: 'ok' },
      { id: 'ib-fail', reachability: 'fail', detail: 'down' },
    ])
    expect(s.signal).toBe('fail')
    expect(s.reason).toBe('down')
  })

  it('prefers the IB Gateway plugin payload when present', () => {
    const s = resolveIbClientStandard({ reachability: 'ok', reachable: true, summary: 'ready' })
    expect(s.signal).toBe('ok')
    expect(s.reason).toBe('ready')
  })
})

describe('operateQueueClearLabel', () => {
  it('shows open count when queue has items', () => {
    expect(operateQueueClearLabel(2, false)).toBe('2 open')
    expect(operateQueueClearLabel(1, true)).toBe('1 open')
  })

  it('demotes Clear to a caveat label when fleet is not clear', () => {
    expect(operateQueueClearLabel(0, false)).toBe('Queue clear · fleet not clear')
  })

  it('shows plain Clear when queue is empty and fleet is clear', () => {
    expect(operateQueueClearLabel(0, true)).toBe('Clear')
  })
})

describe('resolveFleetVerdict', () => {
  it('is NO-GO with no reason when there are no scored cells', () => {
    const v = resolveFleetVerdict([])
    expect(v.kind).toBe('NO-GO')
    expect(v.worstCell).toBeNull()
  })

  it('is GO when every scored cell gate is GO', () => {
    const cells: FleetCell[] = [
      {
        key: 'satellite:dev',
        role: 'satellite',
        env: 'dev',
        span: false,
        signal: 'ok',
        value: 'ok',
        detail: '',
        probePath: '',
        standards: [std('a', 'A', 'ok', 'r', 'api')],
        fixScope: null,
        agentFixEnabled: false,
      },
    ]
    expect(resolveFleetVerdict(cells).kind).toBe('GO')
  })

  it('routes Engineer CRITICAL to Operator Plane instead of Agent Fix', () => {
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
        standards: [std('runners', 'Agent runners (HA)', 'fail', 'Runners down', 'automation')],
        fixScope: null,
        agentFixEnabled: false,
        agentFixDisabledReason: 'Engineer CRITICAL',
        escalateTabId: 'operator-plane',
        countsTowardVerdict: true,
      },
    ]
    const v = resolveFleetVerdict(cells)
    expect(v.kind).toBe('NO-GO')
    expect(v.primaryCta).toMatchObject({ tabId: 'operator-plane', kind: 'navigate' })
  })
})

describe('buildFleetSnapshot integration', () => {
  it('exposes STG satellite from smoke when matrix is missing', () => {
    const snap = buildFleetSnapshot({
      viewerEnv: 'dev',
      matrices: [matrix('dev'), matrix('prod')],
      stg: stgSmokeOk,
      self: selfHealth(['stg', 'prod']),
      cluster: clusterOk,
    })
    const stgSat = getCell(snap, 'satellite', 'stg')
    expect(stgSat).toBeDefined()
    expect(stgSat?.signal).toBe('ok')
    expect(stgSat?.standards.length).toBeGreaterThan(0)
  })

  it('marks STG unavailable when both matrix and smoke are missing', () => {
    const snap = buildFleetSnapshot({
      viewerEnv: 'dev',
      matrices: [matrix('dev'), matrix('prod')],
      self: selfHealth(['prod']),
    })
    const stgSat = getCell(snap, 'satellite', 'stg')
    expect(stgSat?.signal).toBe('unavailable')
  })

  it('does not mark Rocket DEV unavailable when viewer is on dev', () => {
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
    expect(getCell(snap, 'rocket', 'dev')?.signal).not.toBe('unavailable')
  })

  it('marks Rocket DEV unavailable for a prod viewer without dev probes', () => {
    const snap = buildFleetSnapshot({
      viewerEnv: 'prod',
      matrices: [matrix('dev'), matrix('stg'), matrix('prod')],
      self: selfHealth(['prod', 'stg'], 'ok', 'prod'),
    })
    expect(getCell(snap, 'rocket', 'dev')?.signal).toBe('unavailable')
  })

  it('produces DEV/STG/PROD columns only — no Mac column', () => {
    const snap = buildFleetSnapshot({
      viewerEnv: 'prod',
      matrices: [matrix('dev'), matrix('stg'), matrix('prod')],
      self: selfHealth(['prod', 'stg'], 'ok', 'prod'),
      groundBridgeReady: false,
    })
    expect(snap.columns).toEqual(['dev', 'stg', 'prod'])
    expect(getCell(snap, 'satellite', 'dev-local' as never)).toBeUndefined()
    expect(getCell(snap, 'rocket', 'dev-local' as never)).toBeUndefined()
  })

  it('degrades Engineer when the local Mac probe-bridge fails, without affecting Ground', () => {
    const snap = buildFleetSnapshot({
      viewerEnv: 'dev',
      matrices: [matrix('dev'), matrix('stg'), matrix('prod')],
      self: selfHealth(['dev', 'stg', 'prod']),
      stg: stgSmokeOk,
      supply: supplyOk,
      cluster: clusterOk,
      groundBridgeReady: false,
      runner: { status: 'ok' },
      bridge: { ...bridgeOk, satellite_probe_bridge: { status: 'fail', error: 'not_configured' } },
    })
    const eng = getCell(snap, 'engineer', 'span')
    expect(eng?.signal).toBe('fail')
    expect(eng?.detail).toMatch(/Mac seat/)
    expect(getCell(snap, 'ground', 'span')?.signal).toBe('ok')
  })

  it('is GO with fleetClear when every module is green (incl. Mac seat + IB Gateway)', () => {
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
      bridge: bridgeOk,
    })
    expect(snap.verdict.kind).toBe('GO')
    expect(snap.fleetClear).toBe(true)
    expect(snap.fleetNominal).toBe(true)
    const vendor = getCell(snap, 'vendor', 'span')!
    expect(resolveCellGate(vendor)).toBe('GO')
  })

  it('is GO for a prod viewer even though Rocket DEV is structurally unavailable (info-only)', () => {
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
      bridge: { ...bridgeOk, satellite_probe_bridge: { status: 'fail', error: 'unreachable from prod' } },
    })
    const rocketDev = getCell(snap, 'rocket', 'dev')
    expect(rocketDev?.signal).toBe('unavailable')
    expect(rocketDev?.countsTowardVerdict).toBe(false)
    expect(getCell(snap, 'engineer', 'span')?.signal).toBe('ok')
    expect(snap.verdict.kind).toBe('GO')
    expect(snap.fleetClear).toBe(true)
  })

  it('is NO-GO for Vendor when IB Gateway plugin is missing or failing', () => {
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
      bridge: bridgeOk,
    })
    const vendor = getCell(snap, 'vendor', 'span')!
    expect(resolveCellGate(vendor)).toBe('NO-GO')
    expect(vendor.detail).toMatch(/IB/i)
    expect(snap.verdict.kind).toBe('NO-GO')
    expect(vendor.agentFixEnabled).toBe(false)
  })

  it('fails Vendor IB when the live heartbeat is stale despite an optimistic connected flag', () => {
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
        ingestor_health: { connected: 'True', client_id: '70', last_msg_ts: String(now / 1000 - 200) },
        account_health: { host_connected: 'True', host_client_id: '70', last_msg_ts: String(now / 1000 - 200) },
        sample_tick_nvda: JSON.stringify({ bid: -1, ask: -1, last: 201, ts: now / 1000 - 200 }),
        account_snapshot: JSON.stringify({
          host_connected: true,
          secondary_connected: true,
          accounts_snapshot: [{ account_id: 'U1' }],
          updated_at: now / 1000 - 200,
        }),
      },
      bridge: bridgeOk,
    })
    const vendor = getCell(snap, 'vendor', 'span')!
    const ib = vendor.standards.find(s => s.id === 'ib-feed')!
    expect(ib.signal).toBe('fail')
    expect(ib.reason).toMatch(/stale|ghost|empty/i)
    expect(resolveCellGate(vendor)).toBe('NO-GO')
  })

  it('fails Vendor IB on a ghost session (connected flag true, but empty accounts_snapshot)', () => {
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
        ingestor_health: { connected: 'True', client_id: '70', last_msg_ts: String(now / 1000) },
        account_health: {
          host_connected: 'True',
          host_client_id: '70',
          secondary_connected: 'True',
          secondary_client_id: '72',
          last_msg_ts: String(now / 1000),
        },
        sample_tick_nvda: JSON.stringify({ bid: -1, ask: -1, last: 201.26, ts: now / 1000 }),
        account_snapshot: JSON.stringify({
          host_connected: true,
          secondary_connected: true,
          accounts_snapshot: [],
          updated_at: now / 1000,
        }),
      },
      bridge: bridgeOk,
    })
    const vendor = getCell(snap, 'vendor', 'span')!
    const ib = vendor.standards.find(s => s.id === 'ib-feed')!
    expect(ib.signal).toBe('fail')
    expect(ib.reason).toMatch(/ghost|empty/i)
    expect(resolveCellGate(vendor)).toBe('NO-GO')
    expect(vendor.agentFixEnabled).toBe(false)
  })

  it('is NO-GO when a satellite target fails, and NO-GO (not green) when degraded', () => {
    const failSnap = buildFleetSnapshot({
      viewerEnv: 'dev',
      matrices: [matrix('dev', false), matrix('stg'), matrix('prod')],
      self: selfHealth(['dev', 'stg', 'prod']),
      stg: stgSmokeOk,
      cluster: clusterOk,
    })
    expect(failSnap.verdict.kind).toBe('NO-GO')
    expect(failSnap.fleetClear).toBe(false)

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
        standards: [std('svc', 'service', 'degraded', 'partial', 'api')],
        fixScope: PROD_ENV_FIX_SCOPE,
        agentFixEnabled: true,
        countsTowardVerdict: true,
      },
    ]
    expect(resolveFleetVerdict(degCells).kind).toBe('NO-GO')
  })
})

describe('fleetCellFix routing', () => {
  it('allows Engineer Agent Fix via operator-plane-remediate when runners can act', () => {
    const route = lookupFleetFixRoute('engineer', 'span')
    expect(route?.agentFixAllowed).toBe(true)
    expect(route?.fixScope).toBe('operator-plane-remediate')
    expect(route?.navigateTabId).toBe('operator-plane')
  })

  it('scopes satellite fix routes per environment without crossing', () => {
    expect(lookupFleetFixRoute('satellite', 'stg')?.fixScope).toBe(DELIVER_STG_RECOVER_SCOPE)
    expect(lookupFleetFixRoute('satellite', 'dev')?.fixScope).toBe(PROD_ENV_FIX_SCOPE)
    expect(lookupFleetFixRoute('satellite', 'prod')?.fixScope).toBe(PROD_ENV_FIX_SCOPE)
  })

  it('picks the worst fixable cell from a snapshot and resolves its scope', () => {
    const snap = buildFleetSnapshot({
      viewerEnv: 'dev',
      matrices: [matrix('dev', false), matrix('stg'), matrix('prod')],
      self: selfHealth(['stg', 'prod']),
      stg: stgSmokeOk,
      cluster: clusterOk,
    })
    const cell = pickFleetFixCell(snap)
    expect(cell?.role).toBe('satellite')
    expect(cell?.env).toBe('dev')
    expect(cellAllowsAgentFix(cell!)).toBe(true)
    expect(resolveCellFixScope(cell!)).toBe(PROD_ENV_FIX_SCOPE)
  })

  it('uses the STG-specific recovery scope for a failing STG satellite cell', () => {
    const stgFail = buildFleetSnapshot({
      viewerEnv: 'dev',
      matrices: [matrix('dev'), matrix('stg', false), matrix('prod')],
      self: selfHealth(['dev', 'stg', 'prod']),
      stg: stgSmokeOk,
      cluster: clusterOk,
    })
    const stgCell = getCell(stgFail, 'satellite', 'stg')
    expect(stgCell?.signal).toBe('fail')
    expect(resolveCellFixScope(stgCell!)).toBe(DELIVER_STG_RECOVER_SCOPE)
    expect(resolveCellFixScope(stgCell!)).not.toBe(PROD_ENV_FIX_SCOPE)
  })
})
