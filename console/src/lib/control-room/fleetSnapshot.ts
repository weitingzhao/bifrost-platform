/**
 * Daily Ops Fleet Desk — role × environment board + GO|HOLD|NO-GO verdict.
 * Pure functions; UI and hooks consume buildFleetSnapshot().
 */
import type {
  AgentBridgeResponse,
  ClusterSummary,
  MatrixResponse,
  RemediationHealthResponse,
  SelfHealthResponse,
  StgSmokeResponse,
  SupplyChainResponse,
} from '@/api/types'
import {
  DELIVER_STG_RECOVER_SCOPE,
  PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
} from '@/lib/agent/agentScopes'
import { PROD_ENV_FIX_SCOPE } from '@/lib/agent/prodEnvironmentFixPrompt'
import {
  agentSignal,
  controlSignal,
  infraSignal,
  releaseSignal,
  tradeEnvSignal,
  worst,
  type ModuleState,
  type Signal,
} from '@/lib/control-room/missionSignals'

export type FleetRole = 'rocket' | 'satellite' | 'engineer' | 'ground' | 'vendor'
export type FleetEnvColumn = 'dev' | 'stg' | 'prod' | 'dev-local'
export type FleetViewerEnv = 'dev' | 'stg' | 'prod' | 'dev-local'

/** Cell health — unavailable is honest path-missing, never silent green. */
export type FleetCellSignal = Signal | 'unavailable'

export type FleetVerdictKind = 'GO' | 'HOLD' | 'NO-GO'

export type FleetPrimaryCta = {
  label: string
  /** Console tab for navigation CTAs */
  tabId?: string
  /** When set, Primary CTA should dispatch Agent Fix for this cell */
  cellKey?: string
  kind: 'agent-fix' | 'navigate' | 'none'
}

export type FleetCell = {
  key: string
  role: FleetRole
  /** Column env, or null when the cell spans multiple columns */
  env: FleetEnvColumn | null
  span: boolean
  signal: FleetCellSignal
  value: string
  detail: string
  probePath: string
  /** Remediation scope when Agent Fix is allowed */
  fixScope: string | null
  agentFixEnabled: boolean
  agentFixDisabledReason?: string
  /** When Engineer CRITICAL — Primary CTA redirects here */
  escalateTabId?: string
  /**
   * When false, cell is display-only for GO|HOLD|NO-GO (structural unavailable).
   * Defaults to signal !== 'unavailable' when omitted.
   */
  countsTowardVerdict?: boolean
}

/** Structural / path-missing unavailable cells never enter FAIL/HOLD scoring. */
export function cellCountsTowardVerdict(cell: FleetCell): boolean {
  if (cell.countsTowardVerdict != null) return cell.countsTowardVerdict
  return cell.signal !== 'unavailable'
}

export type FleetVerdict = {
  kind: FleetVerdictKind
  topReason: string
  primaryCta: FleetPrimaryCta
  worstCell: FleetCell | null
}

export type FleetSnapshot = {
  viewerEnv: FleetViewerEnv
  columns: FleetEnvColumn[]
  roles: FleetRole[]
  cells: FleetCell[]
  verdict: FleetVerdict
  /** True when every scored cell is ok (unavailable excluded from scoring) */
  fleetNominal: boolean
  /** Operate-queue UI: Clear must not equal fleet clear when this is false */
  fleetClear: boolean
}

export const FLEET_ROLES: FleetRole[] = ['rocket', 'satellite', 'engineer', 'ground', 'vendor']
export const FLEET_COLUMNS: FleetEnvColumn[] = ['dev', 'stg', 'prod', 'dev-local']

export function normalizeViewerEnv(raw: string | undefined | null): FleetViewerEnv {
  const v = (raw ?? '').trim().toLowerCase()
  if (v === 'dev' || v === 'stg' || v === 'prod' || v === 'dev-local') return v
  return 'dev'
}

export function viewerEnvBadgeLabel(env: FleetViewerEnv): 'DEV' | 'STG' | 'PROD' | 'DEV-LOCAL' {
  switch (env) {
    case 'stg':
      return 'STG'
    case 'prod':
      return 'PROD'
    case 'dev-local':
      return 'DEV-LOCAL'
    default:
      return 'DEV'
  }
}

export function cellKey(role: FleetRole, env: FleetEnvColumn | 'span'): string {
  return `${role}:${env}`
}

function moduleToCellSignal(state: ModuleState): FleetCellSignal {
  return state.signal
}

function selfHealthEnvSignal(
  self: SelfHealthResponse | undefined,
  env: 'dev' | 'stg' | 'prod',
): ModuleState {
  if (!self) return { signal: 'unknown', value: '…', detail: 'Self-health: probing' }
  const probes = self.probes.filter(p => p.env === env)
  if (probes.length === 0) {
    return {
      signal: 'unknown',
      value: 'n/a',
      detail: `No self-health probes tagged env=${env}`,
    }
  }
  const statuses = probes.map(p => p.status as Signal)
  const signal = worst(...statuses)
  const ok = probes.filter(p => p.status === 'ok').length
  return {
    signal,
    value: `${ok}/${probes.length}`,
    detail: `Platform self-health ${env}: ${ok}/${probes.length} probes OK`,
  }
}

/**
 * Rocket cell for a column — Wave 0.4:
 * - viewer=dev: DEV uses local controlSignal (self) + reachable platform/dev probes
 * - viewer=prod|stg: DEV/STG from cluster pull (self-health env probes); unreachable → unavailable
 * - prod column always from prod self-health / release overlay on stg
 */
export function buildRocketCell(input: {
  env: FleetEnvColumn
  viewerEnv: FleetViewerEnv
  self?: SelfHealthResponse
  supply?: SupplyChainResponse
  stg?: StgSmokeResponse
}): FleetCell {
  const { env, viewerEnv, self, supply, stg } = input
  const key = cellKey('rocket', env)

  if (env === 'dev-local') {
    return {
      key,
      role: 'rocket',
      env,
      span: false,
      signal: 'unavailable',
      value: 'n/a',
      detail: 'Rocket has no Mac thin-client seat — platform-api runs on cluster or local Console host',
      probePath: 'n/a (no rocket seat on dev-local)',
      fixScope: null,
      agentFixEnabled: false,
      agentFixDisabledReason: 'No Rocket probe path for dev-local',
    }
  }

  if (env === 'stg') {
    const release = releaseSignal(supply, stg)
    const stgHealth = selfHealthEnvSignal(self, 'stg')
    const combined = worst(release.signal, stgHealth.signal)
    const unreachable =
      viewerEnv !== 'dev' &&
      stgHealth.signal === 'unknown' &&
      (self == null || self.probes.filter(p => p.env === 'stg').length === 0)
    if (unreachable) {
      return {
        key,
        role: 'rocket',
        env,
        span: false,
        signal: 'unavailable',
        value: '—',
        detail: 'STG platform pull probes not reachable from this viewer seat',
        probePath: 'GET /api/v1/self-health?env=stg · supply-chain · stg-smoke',
        fixScope: null,
        agentFixEnabled: false,
        agentFixDisabledReason: 'Probe path unavailable from this viewer',
      }
    }
    const state: ModuleState = {
      signal: combined,
      value: release.value !== '…' ? release.value : stgHealth.value,
      detail: [stgHealth.detail, release.detail].filter(Boolean).join(' · '),
    }
    return rocketCellFromState(key, env, state, DELIVER_STG_RECOVER_SCOPE, 'GET /api/v1/self-health (stg) + supply-chain + stg-smoke')
  }

  if (env === 'prod') {
    const prodHealth = selfHealthEnvSignal(self, 'prod')
    return rocketCellFromState(
      key,
      env,
      prodHealth,
      PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
      'GET /api/v1/self-health (prod) · Argo bifrost-platform-prod',
    )
  }

  // env === 'dev'
  if (viewerEnv === 'dev' || viewerEnv === 'dev-local') {
    const control = controlSignal(self)
    const devTagged = selfHealthEnvSignal(self, 'dev')
    const hasDevTagged = devTagged.signal !== 'unknown'
    // Prefer overall control when viewer is on the same host as platform-api
    const state: ModuleState = hasDevTagged
      ? {
          signal: worst(control.signal, devTagged.signal),
          value: control.value,
          detail: `${control.detail} · ${devTagged.detail}`,
        }
      : control
    return rocketCellFromState(
      key,
      env,
      state,
      PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
      'GET /api/v1/self-health (viewer-local) · platform-api / console',
    )
  }

  // Viewer on prod/stg — Rocket DEV via cluster pull
  const devPull = selfHealthEnvSignal(self, 'dev')
  if (devPull.signal === 'unknown' && (self == null || self.probes.filter(p => p.env === 'dev').length === 0)) {
    return {
      key,
      role: 'rocket',
      env,
      span: false,
      signal: 'unavailable',
      value: '—',
      detail: 'DEV platform pull probes not configured / unreachable from this viewer seat',
      probePath: 'GET /api/v1/self-health (dev) — cluster pull',
      fixScope: null,
      agentFixEnabled: false,
      agentFixDisabledReason: 'Probe path unavailable from this viewer',
    }
  }
  return rocketCellFromState(
    key,
    env,
    devPull,
    PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
    'GET /api/v1/self-health (dev) — cluster pull',
  )
}

function rocketCellFromState(
  key: string,
  env: FleetEnvColumn,
  state: ModuleState,
  fixScope: string,
  probePath: string,
): FleetCell {
  const ok = state.signal === 'ok'
  return {
    key,
    role: 'rocket',
    env,
    span: false,
    signal: moduleToCellSignal(state),
    value: state.value,
    detail: state.detail,
    probePath,
    fixScope: ok ? null : fixScope,
    agentFixEnabled: !ok && state.signal !== 'unknown',
    agentFixDisabledReason: ok
      ? undefined
      : state.signal === 'unknown'
        ? 'Still probing'
        : undefined,
  }
}

export function buildSatelliteCell(input: {
  env: FleetEnvColumn
  matrices: MatrixResponse[]
  stg?: StgSmokeResponse
  bridge?: AgentBridgeResponse
  /** When false, Prod→Mac / bridge seats stay unavailable (Wave 5.3) */
  groundBridgeReady?: boolean
}): FleetCell {
  const { env, matrices, stg, bridge, groundBridgeReady = false } = input
  const key = cellKey('satellite', env)

  if (env === 'dev-local') {
    const probeBridge = bridge?.satellite_probe_bridge
    if (!groundBridgeReady || probeBridge == null || probeBridge.status !== 'ok') {
      return {
        key,
        role: 'satellite',
        env,
        span: false,
        signal: 'unavailable',
        value: '—',
        detail:
          'Mac thin-client trade probes require satellite-probe-bridge; Prod/cluster cannot reach 127.0.0.1 on the notebook',
        probePath: 'bridge GET · environments.yaml probe_mode=bridge · trade_bridge_url',
        fixScope: null,
        agentFixEnabled: false,
        agentFixDisabledReason: 'Ground bridge not ready for this seat',
      }
    }
    const matrix = matrices.find(m => m.environment === 'dev-local' || m.environment === 'dev')
    const state = tradeEnvSignal(matrix)
    return satelliteCellFromState(key, env, state, 'bridge · matrix dev-local/dev')
  }

  if (env === 'stg') {
    const matrix = matrices.find(m => m.environment === 'stg')
    if (matrix) {
      return satelliteCellFromState(key, env, tradeEnvSignal(matrix), 'GET /api/v1/matrix?env=stg')
    }
    // Fallback: STG smoke when matrix omitted (legacy gap — must not drop STG)
    if (stg && stg.targets.length > 0) {
      const ok = stg.targets.filter(t => t.reachability === 'ok').length
      const total = stg.targets.length
      const anyFail = stg.targets.some(t => t.reachability === 'fail')
      const anyDeg = stg.targets.some(t => t.reachability === 'degraded')
      const signal: Signal = anyFail ? 'fail' : anyDeg ? 'degraded' : ok === total ? 'ok' : 'degraded'
      return satelliteCellFromState(
        key,
        env,
        { signal, value: `${ok}/${total}`, detail: `STG smoke ${ok}/${total} targets` },
        'GET /api/v1/stg-smoke (matrix stg missing)',
      )
    }
    return {
      key,
      role: 'satellite',
      env,
      span: false,
      signal: 'unavailable',
      value: '—',
      detail: 'No STG matrix or smoke probe payload',
      probePath: 'GET /api/v1/matrix?env=stg · GET /api/v1/stg-smoke',
      fixScope: null,
      agentFixEnabled: false,
      agentFixDisabledReason: 'STG probe path missing',
    }
  }

  const matrix = matrices.find(m => m.environment === env)
  if (!matrix) {
    return {
      key,
      role: 'satellite',
      env,
      span: false,
      signal: 'unavailable',
      value: '—',
      detail: `No matrix for environment=${env}`,
      probePath: `GET /api/v1/matrix?env=${env}`,
      fixScope: null,
      agentFixEnabled: false,
      agentFixDisabledReason: 'Matrix missing for this environment',
    }
  }
  return satelliteCellFromState(key, env, tradeEnvSignal(matrix), `GET /api/v1/matrix?env=${env}`)
}

function satelliteFixScopeForEnv(env: FleetEnvColumn): string {
  if (env === 'stg') return DELIVER_STG_RECOVER_SCOPE
  return PROD_ENV_FIX_SCOPE
}

function satelliteCellFromState(
  key: string,
  env: FleetEnvColumn,
  state: ModuleState,
  probePath: string,
): FleetCell {
  const ok = state.signal === 'ok'
  const fixScope = satelliteFixScopeForEnv(env)
  return {
    key,
    role: 'satellite',
    env,
    span: false,
    signal: moduleToCellSignal(state),
    value: state.value,
    detail: state.detail,
    probePath,
    fixScope: ok ? null : fixScope,
    agentFixEnabled: !ok && state.signal !== 'unknown',
    agentFixDisabledReason: state.signal === 'unknown' ? 'Still probing' : undefined,
    countsTowardVerdict: true,
  }
}

export function buildEngineerCell(input: {
  runner?: RemediationHealthResponse
  bridge?: AgentBridgeResponse
}): FleetCell {
  const state = agentSignal(input.runner, input.bridge)
  const critical = state.signal === 'fail'
  return {
    key: cellKey('engineer', 'span'),
    role: 'engineer',
    env: null,
    span: true,
    signal: moduleToCellSignal(state),
    value: state.value,
    detail: state.detail,
    probePath: 'GET /api/v1/agent/bridge · remediation health',
    fixScope: null,
    agentFixEnabled: false,
    agentFixDisabledReason: critical
      ? 'Engineer CRITICAL — use Operator Plane / Ground (Agent Fix disabled)'
      : state.signal === 'ok'
        ? undefined
        : 'Engineer plane uses Operator Plane remediation, not cell Agent Fix',
    escalateTabId: critical ? 'operator-plane' : undefined,
  }
}

export function buildGroundCell(input: {
  cluster?: ClusterSummary
  viewerEnv: FleetViewerEnv
  groundBridgeReady?: boolean
  bridge?: AgentBridgeResponse
}): FleetCell {
  const infra = infraSignal(input.cluster)
  const probeBridge = input.bridge?.satellite_probe_bridge
  const viewerRemote = input.viewerEnv === 'prod' || input.viewerEnv === 'stg'

  let bridgeSig: Signal = 'unknown'
  let bridgeDetail = 'Satellite probe bridge: probing'
  if (viewerRemote) {
    // Prod/STG cannot reach Mac 127.0.0.1 — bridge outcome is informational only
    bridgeSig = 'ok'
    bridgeDetail =
      probeBridge == null
        ? 'Mac bridge N/A from this viewer seat (Wave 5)'
        : probeBridge.status === 'ok'
          ? 'Satellite probe bridge ok'
          : `Mac bridge ${probeBridge.status} (excluded from Ground seat scoring)`
  } else if (probeBridge != null) {
    bridgeSig =
      probeBridge.status === 'ok' ? 'ok' : probeBridge.status === 'degraded' ? 'degraded' : 'fail'
    bridgeDetail =
      probeBridge.status === 'ok'
        ? 'Satellite probe bridge ok'
        : `Satellite probe bridge ${probeBridge.status}${probeBridge.error ? `: ${probeBridge.error}` : ''}`
  } else if (input.groundBridgeReady === false) {
    bridgeSig = 'degraded'
    bridgeDetail = 'Ground bridge not ready for Mac thin-client seat'
  }

  // Operator-plane / bridge first; cluster infra supports the seat
  const signal = worst(bridgeSig, infra.signal)
  const value =
    signal === 'ok' ? 'ready' : signal === 'fail' ? 'down' : signal === 'degraded' ? 'drift' : infra.value
  const detail = ['Operator plane', bridgeDetail, infra.detail].filter(Boolean).join(' · ')

  return {
    key: cellKey('ground', 'span'),
    role: 'ground',
    env: null,
    span: true,
    signal,
    value,
    detail,
    probePath:
      'GET /api/v1/agent/bridge (satellite_probe_bridge) · GET /api/v1/cluster · Operator Plane',
    fixScope: signal === 'ok' ? null : PROD_ENV_FIX_SCOPE,
    agentFixEnabled: signal !== 'ok' && signal !== 'unknown',
    agentFixDisabledReason: signal === 'unknown' ? 'Still probing' : undefined,
    escalateTabId: signal === 'fail' || signal === 'degraded' ? 'operator-plane' : undefined,
    countsTowardVerdict: true,
  }
}

function vendorIbMassiveFromMatrices(matrices: MatrixResponse[]): {
  signal: Signal
  value: string
  detail: string
} | null {
  const targets = matrices.flatMap(m =>
    m.targets.filter(
      t =>
        t.auth !== 'blocked' &&
        (t.id === 'api-massive' ||
          t.id.includes('massive') ||
          (t.id.includes('ib') && t.category !== 'trade_write')),
    ),
  )
  if (targets.length === 0) return null
  const ok = targets.filter(t => t.reachability === 'ok').length
  const anyFail = targets.some(t => t.reachability === 'fail')
  const anyDeg = targets.some(t => t.reachability === 'degraded')
  const signal: Signal = anyFail ? 'fail' : anyDeg ? 'degraded' : ok === targets.length ? 'ok' : 'degraded'
  const massive = targets.filter(t => t.id.includes('massive'))
  const ib = targets.filter(t => t.id.includes('ib'))
  const parts = [
    massive.length > 0
      ? `Massive ${massive.filter(t => t.reachability === 'ok').length}/${massive.length}`
      : null,
    ib.length > 0
      ? `IB ${ib.filter(t => t.reachability === 'ok').length}/${ib.length}`
      : massive.length > 0
        ? 'IB (matrix write path blocked — see Satellite Bus)'
        : null,
  ].filter(Boolean)
  return {
    signal,
    value: `${ok}/${targets.length}`,
    detail: parts.join(' · ') || `${ok}/${targets.length} vendor probes`,
  }
}

export function buildVendorCell(input: {
  bridge?: AgentBridgeResponse
  matrices?: MatrixResponse[]
}): FleetCell {
  const gb = input.bridge?.git_bridge
  const hermes = input.bridge?.nous_hermes ?? input.bridge?.hermes_mcp
  const primary = vendorIbMassiveFromMatrices(input.matrices ?? [])

  const gitLine =
    gb == null
      ? 'Git bridge ?'
      : gb.status !== 'ok'
        ? 'Git bridge down'
        : `Git bridge ok (${gb.dirty_repos ?? 0} dirty)`
  const hermesLine = hermes == null ? 'Hermes ?' : `Hermes ${hermes.status}`
  const secondary = `${gitLine} · ${hermesLine}`

  let signal: FleetCellSignal = 'unknown'
  let value = '…'
  let detail = 'Vendor IB/Massive: probing'
  let probePath = 'GET /api/v1/matrix (api-massive · IB) · bridge secondary'

  if (primary != null) {
    signal = primary.signal
    value = primary.value
    detail = `${primary.detail} · secondary: ${secondary}`
  } else if (gb != null || hermes != null) {
    // Fallback when matrix vendor probes absent — keep Git/Hermes as soft signal only
    const gitSig: Signal =
      gb == null ? 'unknown' : gb.status !== 'ok' ? 'fail' : (gb.dirty_repos ?? 0) > 0 ? 'degraded' : 'ok'
    const hermesSig: Signal =
      hermes == null
        ? 'unknown'
        : hermes.status === 'ok'
          ? 'ok'
          : hermes.status === 'degraded'
            ? 'degraded'
            : 'fail'
    signal = worst(gitSig, hermesSig)
    value =
      signal === 'ok' ? 'ready' : signal === 'fail' ? 'down' : signal === 'degraded' ? 'drift' : '…'
    detail = `IB/Massive matrix n/a · secondary: ${secondary}`
    probePath = 'GET /api/v1/agent/bridge (git_bridge · hermes) — matrix vendor probes missing'
  }

  return {
    key: cellKey('vendor', 'span'),
    role: 'vendor',
    env: null,
    span: true,
    signal,
    value,
    detail,
    probePath,
    fixScope: signal === 'ok' ? null : primary != null ? PROD_ENV_FIX_SCOPE : GITOPS_HINT_SCOPE,
    agentFixEnabled: signal === 'fail' || signal === 'degraded',
    agentFixDisabledReason: signal === 'unknown' ? 'Still probing' : undefined,
    countsTowardVerdict: true,
  }
}

const GITOPS_HINT_SCOPE = 'gitops-config-repair'

export function severityRank(s: FleetCellSignal): number {
  switch (s) {
    case 'fail':
      return 4
    case 'degraded':
      return 3
    case 'unavailable':
      return 2
    case 'unknown':
      return 1
    default:
      return 0
  }
}

export function pickWorstCell(cells: FleetCell[]): FleetCell | null {
  let worstCell: FleetCell | null = null
  for (const c of cells) {
    if (worstCell == null || severityRank(c.signal) > severityRank(worstCell.signal)) {
      worstCell = c
    }
  }
  return worstCell
}

/**
 * Verdict rules (unavailable excluded from scoring — display only):
 * - NO-GO: any scored fail
 * - HOLD: any scored degraded / unknown (no fail)
 * - GO: all scored cells ok
 * Engineer fail → Primary CTA navigates to Operator Plane / Ground (not Agent Fix)
 */
export function resolveFleetVerdict(cells: FleetCell[]): FleetVerdict {
  const scored = cells.filter(cellCountsTowardVerdict)
  const worstCell = pickWorstCell(scored)
  if (worstCell == null) {
    return {
      kind: 'HOLD',
      topReason: scored.length === 0 ? 'No scored fleet cells' : 'No fleet cells',
      primaryCta: { label: 'Open Control Room', tabId: 'control-room', kind: 'navigate' },
      worstCell: null,
    }
  }

  const hasFail = scored.some(c => c.signal === 'fail')
  const hasHold = scored.some(c => c.signal === 'degraded' || c.signal === 'unknown')

  if (hasFail) {
    if (worstCell.role === 'engineer' && worstCell.escalateTabId) {
      return {
        kind: 'NO-GO',
        topReason: worstCell.detail,
        primaryCta: {
          label: 'Open Operator Plane',
          tabId: worstCell.escalateTabId,
          cellKey: worstCell.key,
          kind: 'navigate',
        },
        worstCell,
      }
    }
    return {
      kind: 'NO-GO',
      topReason: worstCell.detail,
      primaryCta: {
        label: 'Agent Fix',
        cellKey: worstCell.key,
        kind: worstCell.agentFixEnabled ? 'agent-fix' : 'navigate',
        tabId: worstCell.agentFixEnabled ? undefined : 'control-room',
      },
      worstCell,
    }
  }

  if (hasHold) {
    return {
      kind: 'HOLD',
      topReason: worstCell.detail,
      primaryCta: {
        label: 'Agent Fix',
        cellKey: worstCell.key,
        kind: worstCell.agentFixEnabled ? 'agent-fix' : 'navigate',
        tabId: worstCell.agentFixEnabled ? undefined : 'control-room',
      },
      worstCell,
    }
  }

  return {
    kind: 'GO',
    topReason: 'All scored fleet cells nominal',
    primaryCta: { label: 'Fleet clear', kind: 'none' },
    worstCell: null,
  }
}

export function buildFleetSnapshot(input: {
  viewerEnv: FleetViewerEnv
  cluster?: ClusterSummary
  supply?: SupplyChainResponse
  stg?: StgSmokeResponse
  self?: SelfHealthResponse
  runner?: RemediationHealthResponse
  bridge?: AgentBridgeResponse
  matrices: MatrixResponse[]
  /** Wave 5.3 — when false, Mac bridge seats stay unavailable */
  groundBridgeReady?: boolean
  /** Include optional Mac thin-client column (default true) */
  includeDevLocal?: boolean
}): FleetSnapshot {
  const viewerEnv = normalizeViewerEnv(input.viewerEnv)
  const includeDevLocal = input.includeDevLocal !== false
  const columns: FleetEnvColumn[] = includeDevLocal
    ? [...FLEET_COLUMNS]
    : (['dev', 'stg', 'prod'] as FleetEnvColumn[])

  const cells: FleetCell[] = []

  for (const env of columns) {
    cells.push(
      buildRocketCell({
        env,
        viewerEnv,
        self: input.self,
        supply: input.supply,
        stg: input.stg,
      }),
    )
    cells.push(
      buildSatelliteCell({
        env,
        matrices: input.matrices,
        stg: input.stg,
        bridge: input.bridge,
        groundBridgeReady: input.groundBridgeReady,
      }),
    )
  }

  cells.push(buildEngineerCell({ runner: input.runner, bridge: input.bridge }))
  cells.push(
    buildGroundCell({
      cluster: input.cluster,
      viewerEnv,
      groundBridgeReady: input.groundBridgeReady,
      bridge: input.bridge,
    }),
  )
  cells.push(buildVendorCell({ bridge: input.bridge, matrices: input.matrices }))

  const annotated = cells.map(c => ({
    ...c,
    countsTowardVerdict: cellCountsTowardVerdict(c),
  }))
  const verdict = resolveFleetVerdict(annotated)
  const scored = annotated.filter(cellCountsTowardVerdict)
  const fleetNominal = scored.length > 0 && scored.every(c => c.signal === 'ok')
  const fleetClear = fleetNominal

  return {
    viewerEnv,
    columns,
    roles: [...FLEET_ROLES],
    cells: annotated,
    verdict,
    fleetNominal,
    fleetClear,
  }
}

export function getCell(
  snap: FleetSnapshot,
  role: FleetRole,
  env: FleetEnvColumn | 'span',
): FleetCell | undefined {
  const key = cellKey(role, env)
  return snap.cells.find(c => c.key === key)
}

/**
 * Operate summary label when queue is empty but fleet is not clear.
 * fleetClear follows scored verdict (unavailable excluded) — must not hardcode Clear.
 */
export function operateQueueClearLabel(queueOpen: number, fleetClear: boolean): string {
  if (queueOpen > 0) return `${queueOpen} open`
  if (!fleetClear) return 'Queue clear · fleet not clear'
  return 'Clear'
}
