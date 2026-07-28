/**
 * Daily Ops Fleet Desk — role × environment board + GO|HOLD|NO-GO verdict.
 * Pure probe-lattice builders; UI and scripts should call
 * `buildFleetSnapshot` from `./buildFleetSnapshot` (core + Checklist union).
 */
import type { AgentBridgeResponse } from '@/api/agentTypes'
import type { ClusterSummary } from '@/api/clusterTypes'
import type { IbGatewayStatusResponse } from '@/api/satelliteBusTypes'
import type { MatrixResponse, SelfHealthResponse } from '@/api/matrixTypes'
import type { RemediationHealthResponse } from '@/api/remediationTypes'
import type { StgSmokeResponse, SupplyChainResponse } from '@/api/deliveryTypes'
import {
  DELIVER_STG_RECOVER_SCOPE,
  PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
} from '@/lib/agent/agentScopes'
import { PROD_ENV_FIX_SCOPE } from '@/lib/agent/prodEnvironmentFixPrompt'
import { tradeReadinessTargets } from '@/lib/control-room/matrixSummary'
import {
  agentSignal,
  controlSignal,
  infraSignal,
  tradeEnvSignal,
  worst,
  type ModuleState,
  type Signal,
} from '@/lib/control-room/missionSignals'

export type FleetRole = 'rocket' | 'satellite' | 'engineer' | 'ground' | 'vendor'
/** Board columns — deploy environments only. Mac seat is Engineer, not a column. */
export type FleetEnvColumn = 'dev' | 'stg' | 'prod'
export type FleetViewerEnv = 'dev' | 'stg' | 'prod' | 'dev-local'

/** Cell health — unavailable is honest path-missing, never silent green. */
export type FleetCellSignal = Signal | 'unavailable'

/**
 * Standard taxonomy — board shows group rollups; Detail lists members.
 *
 * | Group        | Roles                         |
 * |--------------|-------------------------------|
 * | control      | Rocket — platform-api/console |
 * | gitops       | Rocket — Argo apps            |
 * | release      | (unused on Fleet Rocket; Launch Pad / Promote) |
 * | edge         | Satellite — nginx             |
 * | api          | Satellite — trade APIs        |
 * | datastore    | Satellite — postgres/redis    |
 * | automation   | Engineer — runners / git      |
 * | seat         | Engineer — Mac probe-bridge   |
 * | cluster      | Ground — API / nodes / pods   |
 * | feed         | Vendor — Massive / IB         |
 * | tooling      | Vendor — Hermes               |
 * | path         | Structural unavailable        |
 */
export type FleetStandardGroup =
  | 'control'
  | 'gitops'
  | 'release'
  | 'edge'
  | 'api'
  | 'datastore'
  | 'automation'
  | 'seat'
  | 'cluster'
  | 'feed'
  | 'tooling'
  | 'path'

export const FLEET_STANDARD_GROUP_LABEL: Record<FleetStandardGroup, string> = {
  control: 'Control',
  gitops: 'GitOps',
  release: 'Release',
  edge: 'Edge',
  api: 'APIs',
  datastore: 'Data',
  automation: 'Automation',
  seat: 'Mac seat',
  cluster: 'Cluster',
  feed: 'Feeds',
  tooling: 'Tooling',
  path: 'Path',
}

export const FLEET_STANDARD_GROUP_ORDER: FleetStandardGroup[] = [
  'control',
  'gitops',
  'release',
  'edge',
  'api',
  'datastore',
  'automation',
  'seat',
  'cluster',
  'feed',
  'tooling',
  'path',
]

/** Origin of a fleet standard — probe from matrix/bridge, or checklist virtual projection. */
export type FleetStandardSource = 'probe' | 'checklist'

/**
 * One acceptance standard for a fleet cell.
 * Any required standard that is not green (ok) → cell NO-GO.
 */
export type FleetStandard = {
  id: string
  label: string
  signal: FleetCellSignal
  /** Taxonomy group for rollup + Detail sections */
  group: FleetStandardGroup
  /** Human reason — shown in Detail panel */
  reason: string
  /** When false, informational only (e.g. Mac seat from Prod viewer). Default true. */
  required?: boolean
  /** Default `probe`. Checklist-only dimensions use `checklist` virtual chips. */
  source?: FleetStandardSource
}

export type FleetGroupRollup = {
  group: FleetStandardGroup
  label: string
  ok: number
  total: number
  signal: FleetCellSignal
}

/** Cell gate — binary for scored cells; N/A for structural unavailable. */
export type FleetCellGate = 'GO' | 'NO-GO' | 'N/A'

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
  /** Kept for Agent Fix prompts — not shown on Fleet board */
  probePath: string
  /** Acceptance standards — any non-green required standard ⇒ NO-GO */
  standards: FleetStandard[]
  /** Remediation scope when Agent Fix is allowed */
  fixScope: string | null
  agentFixEnabled: boolean
  agentFixDisabledReason?: string
  /** When Engineer CRITICAL — Primary CTA redirects here */
  escalateTabId?: string
  /**
   * When false, cell is display-only for GO|NO-GO (structural unavailable).
   * Defaults to signal !== 'unavailable' when omitted.
   */
  countsTowardVerdict?: boolean
}

export function std(
  id: string,
  label: string,
  signal: FleetCellSignal,
  reason: string,
  group: FleetStandardGroup,
  required = true,
  source: FleetStandardSource = 'probe',
): FleetStandard {
  return { id, label, signal, reason, group, required, source }
}

/** Required standards must all be green (ok) for GO. */
export function resolveCellGate(cell: FleetCell): FleetCellGate {
  if (!cellCountsTowardVerdict(cell)) return 'N/A'
  const required = cell.standards.filter(s => s.required !== false)
  if (required.length === 0) {
    return cell.signal === 'ok' ? 'GO' : 'NO-GO'
  }
  return required.every(s => s.signal === 'ok') ? 'GO' : 'NO-GO'
}

export function signalFromStandards(standards: FleetStandard[]): FleetCellSignal {
  const required = standards.filter(s => s.required !== false)
  if (required.length === 0) return 'unknown'
  if (required.every(s => s.signal === 'ok')) return 'ok'
  if (required.some(s => s.signal === 'fail')) return 'fail'
  if (required.some(s => s.signal === 'degraded')) return 'degraded'
  if (required.some(s => s.signal === 'unavailable')) return 'unavailable'
  return 'unknown'
}

/** Compact board: one row per group (ok/total + worst signal). */
export function rollupStandards(standards: FleetStandard[]): FleetGroupRollup[] {
  const map = new Map<FleetStandardGroup, FleetStandard[]>()
  for (const s of standards) {
    const list = map.get(s.group) ?? []
    list.push(s)
    map.set(s.group, list)
  }
  const out: FleetGroupRollup[] = []
  for (const group of FLEET_STANDARD_GROUP_ORDER) {
    const members = map.get(group)
    if (members == null || members.length === 0) continue
    const required = members.filter(m => m.required !== false)
    // Optional-only groups (e.g. Rocket RELEASE on DEV/PROD = N/A) still roll up for display.
    const scored = required.length > 0 ? required : members
    const ok = scored.filter(m => m.signal === 'ok').length
    const signal: FleetCellSignal =
      required.length > 0
        ? signalFromStandards(required)
        : scored.every(m => m.signal === 'ok')
          ? 'ok'
          : scored.some(m => m.signal === 'fail')
            ? 'fail'
            : scored.some(m => m.signal === 'degraded')
              ? 'degraded'
              : scored.some(m => m.signal === 'unavailable')
                ? 'unavailable'
                : 'unknown'
    out.push({
      group,
      label: FLEET_STANDARD_GROUP_LABEL[group],
      ok,
      total: scored.length,
      signal,
    })
  }
  return out
}

/** Group standards for Detail panel sections. */
export function groupStandards(
  standards: FleetStandard[],
): Array<{ group: FleetStandardGroup; label: string; items: FleetStandard[] }> {
  const map = new Map<FleetStandardGroup, FleetStandard[]>()
  for (const s of standards) {
    const list = map.get(s.group) ?? []
    list.push(s)
    map.set(s.group, list)
  }
  return FLEET_STANDARD_GROUP_ORDER.filter(g => map.has(g)).map(group => ({
    group,
    label: FLEET_STANDARD_GROUP_LABEL[group],
    items: map.get(group)!,
  }))
}

function labelProbeId(id: string): string {
  return id
    .replace(/^platform-api-/, 'platform-api · ')
    .replace(/^platform-console-/, 'console · ')
    .replace(/^argo-/, 'argo · ')
    .replace(/-/g, ' ')
}

function rocketProbeGroup(category: string, id: string): FleetStandardGroup {
  const c = category.toLowerCase()
  const i = id.toLowerCase()
  if (c === 'argo' || c === 'gitops' || i.includes('argo')) return 'gitops'
  return 'control'
}

/**
 * Rocket self-health standards — scoped to column env.
 * - stg/prod: only probes tagged that env (+ argo apps for that env when id matches)
 * - local (viewer DEV seat): roll up Control + GitOps from seat probes (may include remote URLs)
 */
function standardsFromSelfProbes(
  self: SelfHealthResponse | undefined,
  scope: 'local' | 'dev' | 'stg' | 'prod',
): FleetStandard[] {
  if (!self) {
    return [std('self-health', 'Platform self-health', 'unknown', 'Probing…', 'control')]
  }

  let probes = self.probes
  if (scope === 'dev' || scope === 'stg' || scope === 'prod') {
    probes = self.probes.filter(p => {
      if (p.env === scope) return true
      // Argo apps often tagged by id suffix
      if (rocketProbeGroup(p.category, p.id) === 'gitops') {
        return p.id.toLowerCase().includes(scope) || p.env === scope
      }
      return false
    })
  }
  // scope === 'local': all seat probes (local platform-api view), still grouped Control/GitOps

  if (probes.length === 0) {
    return [
      std(
        `self-health-${scope}`,
        scope === 'local' ? 'Platform self-health' : `Platform self-health (${scope})`,
        'unknown',
        `No probes for scope=${scope}`,
        'control',
      ),
    ]
  }

  return probes.map(p =>
    std(
      p.id,
      labelProbeId(p.id),
      p.status as FleetCellSignal,
      p.detail || p.status,
      rocketProbeGroup(p.category, p.id),
    ),
  )
}

function satelliteTargetGroup(id: string, category: string): FleetStandardGroup {
  const i = id.toLowerCase()
  const c = category.toLowerCase()
  if (i.includes('nginx') || c.includes('edge') || i.includes('spa')) return 'edge'
  if (
    i.includes('postgres') ||
    i.includes('redis') ||
    c === 'datastore' ||
    c.includes('data')
  ) {
    return 'datastore'
  }
  return 'api'
}

function standardsFromMatrix(matrix: MatrixResponse): FleetStandard[] {
  const targets = tradeReadinessTargets(matrix.targets)
  if (targets.length === 0) {
    return [std('matrix', 'Trade readiness targets', 'unknown', 'No scored targets', 'api')]
  }
  return targets.map(t =>
    std(
      t.id,
      t.id,
      t.reachability as FleetCellSignal,
      t.detail || t.reachability,
      satelliteTargetGroup(t.id, t.category),
    ),
  )
}

/** Single rollup standard for STG smoke (avoid listing every URL on the board). */
function stgSmokeStandard(stg: StgSmokeResponse): FleetStandard {
  const ok = stg.targets.filter(t => t.reachability === 'ok').length
  const total = stg.targets.length
  const anyFail = stg.targets.some(t => t.reachability === 'fail')
  const anyDeg = stg.targets.some(t => t.reachability === 'degraded')
  const signal: FleetCellSignal = anyFail
    ? 'fail'
    : anyDeg
      ? 'degraded'
      : ok === total && total > 0
        ? 'ok'
        : 'degraded'
  return std(
    'stg-smoke',
    `STG smoke ${ok}/${total}`,
    signal,
    stg.targets.map(t => `${t.id}:${t.reachability}`).join(' · ') || 'No smoke targets',
    'release',
  )
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
export const FLEET_COLUMNS: FleetEnvColumn[] = ['dev', 'stg', 'prod']

/** Deep-link target for a fleet cell / role row. */
export function fleetCellNavigateTab(cell: Pick<FleetCell, 'role' | 'escalateTabId'>): string {
  if (cell.escalateTabId) return cell.escalateTabId
  return fleetRoleNavigateTab(cell.role)
}

export function fleetRoleNavigateTab(role: FleetRole): string {
  switch (role) {
    case 'rocket':
      return 'cluster'
    case 'satellite':
      return 'satellite-bus'
    case 'engineer':
      return 'agent-desk'
    case 'ground':
      return 'operator-plane'
    case 'vendor':
      return 'satellite-bus'
  }
}

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
 * Rocket cell for a column — Control + GitOps only.
 * Deliver / STG smoke live on Launch Pad + Promote, not on Fleet Rocket
 * (Owner: Rocket board is platform health, not the release pipeline lane).
 */
export function buildRocketCell(input: {
  env: FleetEnvColumn
  viewerEnv: FleetViewerEnv
  self?: SelfHealthResponse
  supply?: SupplyChainResponse
  stg?: StgSmokeResponse
}): FleetCell {
  const { env, viewerEnv, self } = input
  const key = cellKey('rocket', env)

  if (env === 'stg') {
    const stgHealth = selfHealthEnvSignal(self, 'stg')
    const unreachable =
      viewerEnv !== 'dev' &&
      stgHealth.signal === 'unknown' &&
      (self == null || self.probes.filter(p => p.env === 'stg').length === 0)
    if (unreachable) {
      return unavailableCell(
        key,
        'rocket',
        env,
        'STG platform pull probes not reachable from this viewer seat',
        'Probe path unavailable from this viewer',
      )
    }
    const standards = standardsFromSelfProbes(self, 'stg')
    const derived = signalFromStandards(standards)
    return rocketCellFromState(
      key,
      env,
      {
        ...stgHealth,
        signal: derived === 'unavailable' ? stgHealth.signal : (derived as Signal),
      },
      PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
      standards,
    )
  }

  if (env === 'prod') {
    const standards = standardsFromSelfProbes(self, 'prod')
    const prodHealth = selfHealthEnvSignal(self, 'prod')
    const derived = signalFromStandards(standards)
    return rocketCellFromState(
      key,
      env,
      {
        ...prodHealth,
        signal: derived === 'unavailable' ? prodHealth.signal : (derived as Signal),
      },
      PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
      standards,
    )
  }

  // env === 'dev'
  if (viewerEnv === 'dev' || viewerEnv === 'dev-local') {
    const control = controlSignal(self)
    // Local seat: Control + GitOps from this platform-api view (not a dump of STG/PROD as "DEV")
    const standards = standardsFromSelfProbes(self, 'local')
    const derived = signalFromStandards(standards)
    return rocketCellFromState(
      key,
      env,
      {
        signal: derived === 'unavailable' ? control.signal : (derived as Signal),
        value: control.value,
        detail: control.detail,
      },
      PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
      standards,
    )
  }

  // Viewer on prod/stg — Rocket DEV via cluster pull
  const devPull = selfHealthEnvSignal(self, 'dev')
  if (devPull.signal === 'unknown' && (self == null || self.probes.filter(p => p.env === 'dev').length === 0)) {
    return unavailableCell(
      key,
      'rocket',
      env,
      'DEV platform pull probes not configured / unreachable from this viewer seat',
      'Probe path unavailable from this viewer',
    )
  }
  const standards = standardsFromSelfProbes(self, 'dev')
  const derived = signalFromStandards(standards)
  return rocketCellFromState(
    key,
    env,
    {
      ...devPull,
      signal: derived === 'unavailable' ? devPull.signal : (derived as Signal),
    },
    PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
    standards,
  )
}

function unavailableCell(
  key: string,
  role: FleetRole,
  env: FleetEnvColumn,
  detail: string,
  disabledReason: string,
): FleetCell {
  return {
    key,
    role,
    env,
    span: false,
    signal: 'unavailable',
    value: '—',
    detail,
    probePath: '',
    standards: [std('probe-path', 'Probe path reachable', 'unavailable', detail, 'path', false)],
    fixScope: null,
    agentFixEnabled: false,
    agentFixDisabledReason: disabledReason,
    countsTowardVerdict: false,
  }
}

function rocketCellFromState(
  key: string,
  env: FleetEnvColumn,
  state: ModuleState,
  fixScope: string,
  standards: FleetStandard[],
): FleetCell {
  const signal = signalFromStandards(standards)
  const cellSignal: FleetCellSignal =
    signal === 'unavailable' ? moduleToCellSignal(state) : signal
  const ok = cellSignal === 'ok'
  return {
    key,
    role: 'rocket',
    env,
    span: false,
    signal: cellSignal,
    value: state.value,
    detail: state.detail,
    probePath: '',
    standards,
    fixScope: ok ? null : fixScope,
    agentFixEnabled: !ok && cellSignal !== 'unknown',
    agentFixDisabledReason: ok
      ? undefined
      : cellSignal === 'unknown'
        ? 'Still probing'
        : undefined,
  }
}

export function buildSatelliteCell(input: {
  env: FleetEnvColumn
  matrices: MatrixResponse[]
  stg?: StgSmokeResponse
}): FleetCell {
  const { env, matrices, stg } = input
  const key = cellKey('satellite', env)

  if (env === 'stg') {
    const matrix = matrices.find(m => m.environment === 'stg')
    if (matrix) {
      return satelliteCellFromState(key, env, tradeEnvSignal(matrix), standardsFromMatrix(matrix))
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
        [stgSmokeStandard(stg)],
      )
    }
    return unavailableCell(
      key,
      'satellite',
      env,
      'No STG matrix or smoke probe payload',
      'STG probe path missing',
    )
  }

  const matrix = matrices.find(m => m.environment === env)
  if (!matrix) {
    return unavailableCell(
      key,
      'satellite',
      env,
      `No matrix for environment=${env}`,
      'Matrix missing for this environment',
    )
  }
  return satelliteCellFromState(key, env, tradeEnvSignal(matrix), standardsFromMatrix(matrix))
}

function satelliteFixScopeForEnv(env: FleetEnvColumn): string {
  if (env === 'stg') return DELIVER_STG_RECOVER_SCOPE
  return PROD_ENV_FIX_SCOPE
}

function satelliteCellFromState(
  key: string,
  env: FleetEnvColumn,
  state: ModuleState,
  standards: FleetStandard[],
): FleetCell {
  const derived = signalFromStandards(standards)
  const cellSignal: FleetCellSignal =
    derived === 'unavailable' ? moduleToCellSignal(state) : derived
  const ok = cellSignal === 'ok'
  const fixScope = satelliteFixScopeForEnv(env)
  return {
    key,
    role: 'satellite',
    env,
    span: false,
    signal: cellSignal,
    value: state.value,
    detail: state.detail,
    probePath: '',
    standards,
    fixScope: ok ? null : fixScope,
    agentFixEnabled: !ok && cellSignal !== 'unknown',
    agentFixDisabledReason: cellSignal === 'unknown' ? 'Still probing' : undefined,
    countsTowardVerdict: true,
  }
}

/**
 * Engineer = AI Agent plane + Mac seat (probe-bridge / thin-client).
 * Mac is not a board column — it is the Engineer's physical workstation.
 * Prod/STG viewers cannot reach Mac 127.0.0.1 — Mac seat is informational only there.
 */
export function buildEngineerCell(input: {
  runner?: RemediationHealthResponse
  bridge?: AgentBridgeResponse
  viewerEnv?: FleetViewerEnv
  groundBridgeReady?: boolean
}): FleetCell {
  const state = agentSignal(input.runner, input.bridge)
  const viewerEnv = normalizeViewerEnv(input.viewerEnv)
  const viewerRemote = viewerEnv === 'prod' || viewerEnv === 'stg'
  const probeBridge = input.bridge?.satellite_probe_bridge
  const bridge = input.bridge
  const runners = bridge?.runners ?? []

  let runnerSig: Signal
  let runnerReason: string
  if (runners.length >= 2) {
    const upCount = runners.filter(r => r.status === 'ok').length
    if (upCount === runners.length) {
      runnerSig = 'ok'
      runnerReason = `Runners ${upCount}/${runners.length} (HA)`
    } else if (upCount === 0) {
      runnerSig = 'fail'
      runnerReason = 'All runners down'
    } else {
      runnerSig = 'degraded'
      runnerReason = `Runner failover active (${upCount}/${runners.length} up)`
    }
  } else if (runners.length === 1) {
    runnerSig = runners[0].status === 'ok' ? 'ok' : 'fail'
    runnerReason = runnerSig === 'ok' ? 'Runner up (no standby)' : 'Runner down'
  } else {
    runnerSig = input.runner == null ? 'unknown' : input.runner.status === 'ok' ? 'ok' : 'fail'
    runnerReason =
      runnerSig === 'ok' ? 'Runner up' : runnerSig === 'unknown' ? 'Runner status unknown' : 'Runner down'
  }

  const gb = bridge?.git_bridge
  const dirty = gb?.dirty_repos ?? 0
  const gitSig: Signal =
    gb == null ? 'unknown' : gb.status !== 'ok' ? 'fail' : dirty > 0 ? 'degraded' : 'ok'
  const gitReason =
    gb == null
      ? 'Git bridge status unknown'
      : gb.status !== 'ok'
        ? 'Git bridge down'
        : dirty > 0
          ? `Git bridge ${dirty} dirty repo(s)`
          : 'Git bridge clean'

  let macSig: Signal = 'unknown'
  let macReason = 'Mac seat: probing'
  if (viewerRemote) {
    macSig =
      probeBridge == null
        ? 'ok'
        : probeBridge.status === 'ok'
          ? 'ok'
          : (probeBridge.status as Signal) === 'degraded'
            ? 'degraded'
            : 'fail'
    macReason =
      probeBridge == null
        ? 'Mac seat N/A from this viewer (info only)'
        : `Mac seat · probe-bridge ${probeBridge.status} (info only from remote)`
  } else if (probeBridge != null) {
    macSig =
      probeBridge.status === 'ok' ? 'ok' : probeBridge.status === 'degraded' ? 'degraded' : 'fail'
    macReason =
      probeBridge.status === 'ok'
        ? 'Mac seat · probe-bridge ok'
        : `Mac seat · probe-bridge ${probeBridge.status}${
            probeBridge.error ? `: ${probeBridge.error}` : ''
          }`
  } else if (input.groundBridgeReady === false) {
    macSig = 'degraded'
    macReason = 'Mac seat · probe-bridge not ready'
  }

  const standards: FleetStandard[] = [
    std('runners', 'Agent runners (HA)', runnerSig, runnerReason, 'automation'),
    std('git-bridge', 'Git bridge clean', gitSig, gitReason, 'automation'),
    std('mac-seat', 'Mac seat · probe-bridge', macSig, macReason, 'seat', !viewerRemote),
  ]
  const signal = signalFromStandards(standards)
  const critical = signal === 'fail'
  const value =
    signal === 'ok'
      ? state.value
      : signal === 'fail'
        ? 'down'
        : signal === 'degraded'
          ? 'drift'
          : state.value

  // Agent Fix only when at least one runner can execute (bridge-down is auto-fixable via bdev).
  const runnersCanAct = runnerSig === 'ok' || runnerSig === 'degraded'
  const canAgentFix = runnersCanAct && signal !== 'ok' && signal !== 'unknown'

  return {
    key: cellKey('engineer', 'span'),
    role: 'engineer',
    env: null,
    span: true,
    signal,
    value,
    detail: standards.map(s => s.reason).join(' · '),
    probePath: '',
    standards,
    fixScope: canAgentFix ? 'operator-plane-remediate' : null,
    agentFixEnabled: canAgentFix,
    agentFixDisabledReason: !runnersCanAct
      ? 'Runners down — recover remediation runners on Operator Plane before Agent Fix'
      : signal === 'ok'
        ? undefined
        : signal === 'unknown'
          ? 'Still probing'
          : undefined,
    escalateTabId: critical || signal === 'degraded' ? 'operator-plane' : undefined,
    countsTowardVerdict: true,
  }
}

/** Ground = cluster / Operator Plane infrastructure — Mac seat belongs to Engineer. */
export function buildGroundCell(input: {
  cluster?: ClusterSummary
}): FleetCell {
  const cluster = input.cluster
  const infra = infraSignal(cluster)

  const apiSig: Signal = cluster == null ? 'unknown' : (cluster.reachability as Signal)
  const nodesOk =
    cluster != null &&
    cluster.reachability === 'ok' &&
    cluster.nodes_total > 0 &&
    cluster.nodes_ready >= cluster.nodes_total
  const nodesSig: Signal =
    cluster == null ? 'unknown' : cluster.reachability === 'fail' ? 'fail' : nodesOk ? 'ok' : 'degraded'
  const podsSig: Signal =
    cluster == null ? 'unknown' : cluster.failing_pods > 0 ? 'degraded' : cluster.reachability === 'fail' ? 'fail' : 'ok'

  const standards: FleetStandard[] = [
    std(
      'cluster-api',
      'Cluster API reachable',
      apiSig,
      cluster == null ? 'Cluster: probing' : cluster.detail || cluster.reachability,
      'cluster',
    ),
    std(
      'nodes-ready',
      'All nodes Ready',
      nodesSig,
      cluster == null
        ? 'Nodes: probing'
        : `${cluster.nodes_ready}/${cluster.nodes_total} nodes Ready${
            (cluster.elastic_standby ?? 0) > 0 ? ` (+${cluster.elastic_standby} standby)` : ''
          }`,
      'cluster',
    ),
    std(
      'failing-pods',
      'No failing pods',
      podsSig,
      cluster == null
        ? 'Pods: probing'
        : cluster.failing_pods > 0
          ? `${cluster.failing_pods} failing pods`
          : 'No failing pods',
      'cluster',
    ),
  ]
  const signal = signalFromStandards(standards)
  const value =
    signal === 'ok' ? 'ready' : signal === 'fail' ? 'down' : signal === 'degraded' ? 'drift' : infra.value

  return {
    key: cellKey('ground', 'span'),
    role: 'ground',
    env: null,
    span: true,
    signal,
    value,
    detail: standards.map(s => s.reason).join(' · '),
    probePath: '',
    standards,
    fixScope: signal === 'ok' ? null : PROD_ENV_FIX_SCOPE,
    agentFixEnabled: signal !== 'ok' && signal !== 'unknown',
    agentFixDisabledReason: signal === 'unknown' ? 'Still probing' : undefined,
    escalateTabId: signal === 'fail' || signal === 'degraded' ? 'operator-plane' : undefined,
    countsTowardVerdict: true,
  }
}

function isMassiveVendorTarget(t: { id: string; auth?: string }): boolean {
  if (t.auth === 'blocked') return false
  const id = t.id.toLowerCase()
  return id === 'api-massive' || id.includes('massive') || id.includes('polygon')
}

function isIbVendorTarget(t: { id: string; auth?: string; category?: string }): boolean {
  if (t.auth === 'blocked') return false
  if (t.category === 'trade_write') return false
  const id = t.id.toLowerCase()
  // Exclude shared placeholders claimed by Massive (e.g. massive-ib)
  if (id.includes('massive') || id.includes('polygon')) return false
  return id.includes('ib') || id.includes('ibkr')
}

function vendorTargets(matrices: MatrixResponse[]) {
  return matrices.flatMap(m =>
    m.targets.filter(t => isMassiveVendorTarget(t) || isIbVendorTarget(t)),
  )
}

export function buildVendorCell(input: {
  bridge?: AgentBridgeResponse
  matrices?: MatrixResponse[]
  /** Platform IB Gateway plugin status — required for Vendor GO. */
  ibGateway?: IbGatewayStatusResponse
}): FleetCell {
  const hermes = input.bridge?.nous_hermes ?? input.bridge?.hermes_mcp
  const targets = vendorTargets(input.matrices ?? [])
  const massiveTargets = targets.filter(t => isMassiveVendorTarget(t))
  const ibTargets = targets.filter(t => isIbVendorTarget(t))

  const standards: FleetStandard[] = []
  for (const t of massiveTargets) {
    standards.push(
      std(t.id, t.id, t.reachability as FleetCellSignal, t.detail || t.reachability, 'feed'),
    )
  }
  if (massiveTargets.length === 0) {
    // Stable id for Checklist match (massive|polygon). Informational — do not alone NO-GO.
    standards.push(
      std(
        'massive-polygon',
        'Massive / Polygon feed',
        'unknown',
        'Massive/Polygon matrix targets not present',
        'feed',
        false,
      ),
    )
  }
  // IB Client / Gateway — required for Vendor GO (plugin status). D10: observe/manual only, no Agent Fix.
  const ibProbe = resolveIbClientStandard(input.ibGateway, ibTargets)
  standards.push(ibProbe)

  const hermesSig: Signal =
    hermes == null
      ? 'unknown'
      : hermes.status === 'ok'
        ? 'ok'
        : hermes.status === 'degraded'
          ? 'degraded'
          : 'fail'
  standards.push(
    std(
      'hermes',
      'Hermes ready',
      hermesSig,
      hermes == null ? 'Hermes status unknown' : `Hermes ${hermes.status}`,
      'tooling',
    ),
  )

  // Git bridge is scored on Engineer (automation) — do not mirror on Vendor (closes Board→Checklist gap).

  const signal = signalFromStandards(standards)
  const required = standards.filter(s => s.required !== false)
  const okRequired = required.filter(s => s.signal === 'ok').length
  const value =
    required.length > 0
      ? `${okRequired}/${required.length}`
      : signal === 'ok'
        ? 'ready'
        : signal === 'fail'
          ? 'down'
          : signal === 'degraded'
            ? 'drift'
            : '…'

  const ibBlocking = ibProbe.signal !== 'ok'
  const otherFeedFail = standards.some(
    s =>
      s.required !== false &&
      s.id !== 'ib-feed' &&
      s.group === 'feed' &&
      s.signal !== 'ok' &&
      s.signal !== 'unknown',
  )
  // Massive/Hermes may use Agent Fix; IB Client stays D10 observe (Plugin Gallery / TWS).
  const agentFixEnabled =
    !ibBlocking && (signal === 'fail' || signal === 'degraded') && otherFeedFail

  return {
    key: cellKey('vendor', 'span'),
    role: 'vendor',
    env: null,
    span: true,
    signal,
    value,
    detail: standards
      .filter(s => s.required !== false)
      .map(s => s.reason)
      .join(' · '),
    probePath: '',
    standards,
    fixScope:
      signal === 'ok'
        ? null
        : ibBlocking && !otherFeedFail
          ? null
          : targets.length > 0
            ? PROD_ENV_FIX_SCOPE
            : GITOPS_HINT_SCOPE,
    agentFixEnabled,
    agentFixDisabledReason: ibBlocking
      ? 'IB Client required — observe only (D10). If TWS is already running: Reconnect Gateway (rollout restart data/ib-gateway), then Re-probe.'
      : signal === 'unknown'
        ? 'Still probing'
        : undefined,
    escalateTabId: ibBlocking ? 'plugin-gallery' : undefined,
    countsTowardVerdict: true,
  }
}

/** Map IB Gateway plugin status (+ optional matrix IB targets) → required Vendor feed standard. */
export function resolveIbClientStandard(
  ibGateway: IbGatewayStatusResponse | undefined,
  ibTargets: { id: string; reachability: string; detail?: string }[] = [],
): FleetStandard {
  if (ibGateway != null) {
    const assessed = assessIbGatewaySocketQuality(ibGateway)
    return std('ib-feed', 'IB Client / Gateway', assessed.signal, assessed.reason, 'feed', true)
  }
  // Prefer a real matrix IB probe if present (rare — most IB matrix rows are trade_write blocked).
  if (ibTargets.length > 0) {
    const worst = ibTargets.reduce((a, b) =>
      severityRank(b.reachability as FleetCellSignal) > severityRank(a.reachability as FleetCellSignal)
        ? b
        : a,
    )
    const raw = worst.reachability as string
    const sig: FleetCellSignal =
      raw === 'ok' ||
      raw === 'degraded' ||
      raw === 'fail' ||
      raw === 'unavailable' ||
      raw === 'unknown'
        ? raw
        : 'unknown'
    return std(
      'ib-feed',
      'IB Client / Gateway',
      sig,
      worst.detail || `IB matrix ${worst.id}: ${worst.reachability}`,
      'feed',
      true,
    )
  }
  // No plugin payload yet — required unknown so Vendor cannot GO (loading or API down).
  return std(
    'ib-feed',
    'IB Client / Gateway',
    'unknown',
    'IB Gateway status not loaded — Vendor cannot GO without IB Client',
    'feed',
    true,
  )
}

/**
 * Socket-quality gate for Vendor IB: Redis "connected" alone is not enough.
 * Aligns with api/internal/ibgateway assessSocketFeedQuality.
 */
export function assessIbGatewaySocketQuality(
  ib: IbGatewayStatusResponse,
  nowMs: number = Date.now(),
): { signal: FleetCellSignal; reason: string } {
  const mode = (ib.mode ?? '').toLowerCase()
  const baseReach = (ib.reachability ??
    (ib.reachable === true ? 'ok' : ib.reachable === false ? 'fail' : 'unknown')) as FleetCellSignal
  const baseReason =
    ib.summary ?? ib.error ?? ib.deployment?.detail ?? `IB Gateway ${baseReach}`

  if (mode !== 'live') {
    return { signal: normalizeReach(baseReach), reason: baseReason }
  }

  const ing = ib.ingestor_health ?? {}
  const acc = ib.account_health ?? {}
  const connected = String(ing.connected ?? '').toLowerCase() === 'true'
  if (!connected) {
    return {
      signal: 'fail',
      reason: 'IB ingestor not connected — TWS API socket down',
    }
  }
  const cid = String(ing.client_id ?? '').trim()
  const hostCid = String(acc.host_client_id ?? '').trim()
  if ((!cid || cid === '0') && (!hostCid || hostCid === '0')) {
    return {
      signal: 'fail',
      reason: 'connected flag set but no client_id — TWS API session missing',
    }
  }

  // Ghost-session detector (works on weekends when RTH BBO rule does not fire):
  // plugin may keep Redis "connected" while TWS has no live API clients.
  const snapQ = assessAccountSnapshotQuality(ib.account_snapshot, nowMs)
  if (snapQ) return snapQ

  const ingAge = unixAgeSec(ing.last_msg_ts, nowMs)
  if (ingAge != null && ingAge > 90) {
    return {
      signal: 'fail',
      reason: `IB socket heartbeat stale (${Math.round(ingAge)}s) — treat as dead API client`,
    }
  }
  const accAge = unixAgeSec(acc.last_msg_ts, nowMs)
  if (accAge != null && accAge > 90) {
    return {
      signal: 'fail',
      reason: `IB account heartbeat stale (${Math.round(accAge)}s)`,
    }
  }

  const tickRaw = ib.sample_tick_nvda
  if (tickRaw == null || String(tickRaw).trim() === '') {
    const sig = worseSignal(normalizeReach(baseReach), 'degraded')
    return { signal: sig, reason: `${baseReason} · no sample tick (NVDA)` }
  }

  let bid = 0
  let ask = 0
  let last = 0
  let tickTs = 0
  try {
    const tick = typeof tickRaw === 'string' ? JSON.parse(tickRaw) : tickRaw
    bid = Number(tick?.bid ?? 0)
    ask = Number(tick?.ask ?? 0)
    last = Number(tick?.last ?? 0)
    tickTs = Number(tick?.ts ?? 0)
  } catch {
    return {
      signal: worseSignal(normalizeReach(baseReach), 'degraded'),
      reason: `${baseReason} · sample tick unparseable`,
    }
  }
  if (tickTs > 0) {
    const tickAge = unixAgeSec(String(tickTs), nowMs)
    if (tickAge != null && tickAge > 180) {
      return {
        signal: 'fail',
        reason: `sample tick stale (${Math.round(tickAge)}s) — socket not delivering`,
      }
    }
  }
  if (inUSEquityRTH(nowMs) && bid <= 0 && ask <= 0) {
    return {
      signal: 'fail',
      reason: `RTH but no usable BBO (bid/ask≤0)${last > 0 ? ` · last=${last}` : ''} — TWS socket/market-data suspect`,
    }
  }

  return { signal: normalizeReach(baseReach), reason: baseReason }
}

/** Empty / missing account snapshot while claiming connected → ghost TWS API client. */
export function assessAccountSnapshotQuality(
  raw: string | undefined,
  nowMs: number,
): { signal: FleetCellSignal; reason: string } | null {
  if (raw == null || String(raw).trim() === '') {
    return {
      signal: 'fail',
      reason: 'no account snapshot on redis-ib — TWS API session not verified',
    }
  }
  try {
    const snap = typeof raw === 'string' ? JSON.parse(raw) : raw
    const updated = Number(snap?.updated_at ?? 0)
    if (Number.isFinite(updated) && updated > 0) {
      const age = unixAgeSec(String(updated), nowMs)
      if (age != null && age > 90) {
        return {
          signal: 'fail',
          reason: `account snapshot stale (${Math.round(age)}s)`,
        }
      }
    }
    const accounts = snap?.accounts_snapshot
    const count = Array.isArray(accounts) ? accounts.length : 0
    if (count === 0) {
      const hostClaim = snap?.host_connected === true || String(snap?.host_connected).toLowerCase() === 'true'
      const secClaim =
        snap?.secondary_connected === true || String(snap?.secondary_connected).toLowerCase() === 'true'
      if (hostClaim || secClaim) {
        return {
          signal: 'fail',
          reason: 'connected but accounts_snapshot empty — ghost TWS API client',
        }
      }
      return {
        signal: 'fail',
        reason: 'account snapshot has no managed accounts',
      }
    }
  } catch {
    return {
      signal: 'degraded',
      reason: 'account snapshot unparseable',
    }
  }
  return null
}

function normalizeReach(s: FleetCellSignal): FleetCellSignal {
  if (s === 'ok' || s === 'degraded' || s === 'fail' || s === 'unavailable' || s === 'unknown') {
    return s
  }
  return 'unknown'
}

function worseSignal(a: FleetCellSignal, b: FleetCellSignal): FleetCellSignal {
  return severityRank(b) > severityRank(a) ? b : a
}

function unixAgeSec(raw: string | undefined, nowMs: number): number | null {
  if (raw == null || String(raw).trim() === '') return null
  let ts = Number(raw)
  if (!Number.isFinite(ts)) return null
  if (ts > 1e12) ts = ts / 1000
  return nowMs / 1000 - ts
}

function inUSEquityRTH(nowMs: number): boolean {
  const fmt = new Intl.DateTimeFormat('en-US', {
    timeZone: 'America/New_York',
    weekday: 'short',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  })
  const parts = Object.fromEntries(fmt.formatToParts(new Date(nowMs)).map(p => [p.type, p.value]))
  const wd = parts.weekday
  if (wd === 'Sat' || wd === 'Sun') return false
  const hour = Number(parts.hour)
  const minute = Number(parts.minute)
  const mins = hour * 60 + minute
  return mins >= 9 * 60 + 30 && mins < 16 * 60
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
 * - GO: every scored cell gate is GO (all required standards green)
 * - NO-GO: any scored cell has a non-green required standard
 * Engineer fail/degraded → Primary CTA navigates to Operator Plane (not Agent Fix)
 */
export function resolveFleetVerdict(cells: FleetCell[]): FleetVerdict {
  const scored = cells.filter(cellCountsTowardVerdict)
  const worstCell = pickWorstCell(scored)
  if (worstCell == null) {
    return {
      kind: 'NO-GO',
      topReason: scored.length === 0 ? 'No scored fleet cells' : 'No fleet cells',
      primaryCta: { label: 'Open Control Room', tabId: 'control-room', kind: 'navigate' },
      worstCell: null,
    }
  }

  const anyNoGo = scored.some(c => resolveCellGate(c) === 'NO-GO')
  if (!anyNoGo) {
    return {
      kind: 'GO',
      topReason: 'All required standards green',
      primaryCta: { label: 'Fleet clear', kind: 'none' },
      worstCell: null,
    }
  }

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

export type BuildFleetSnapshotInput = {
  viewerEnv: FleetViewerEnv
  cluster?: ClusterSummary
  supply?: SupplyChainResponse
  stg?: StgSmokeResponse
  self?: SelfHealthResponse
  runner?: RemediationHealthResponse
  bridge?: AgentBridgeResponse
  matrices: MatrixResponse[]
  /** Mac seat readiness for Engineer row (local viewer only scores bridge) */
  groundBridgeReady?: boolean
  /** IB Gateway plugin — required Vendor feed (IB Client). */
  ibGateway?: IbGatewayStatusResponse
}

/**
 * Probe lattice only — no Checklist virtual chips.
 * Prefer {@link buildFleetSnapshot} (core + union finalize) for UI / scripts.
 */
export function buildFleetSnapshotCore(input: BuildFleetSnapshotInput): FleetSnapshot {
  const viewerEnv = normalizeViewerEnv(input.viewerEnv)
  const columns: FleetEnvColumn[] = [...FLEET_COLUMNS]

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
      }),
    )
  }

  cells.push(
    buildEngineerCell({
      runner: input.runner,
      bridge: input.bridge,
      viewerEnv,
      groundBridgeReady: input.groundBridgeReady,
    }),
  )
  cells.push(buildGroundCell({ cluster: input.cluster }))
  cells.push(
    buildVendorCell({
      bridge: input.bridge,
      matrices: input.matrices,
      ibGateway: input.ibGateway,
    }),
  )

  const annotated = cells.map(c => ({
    ...c,
    countsTowardVerdict: cellCountsTowardVerdict(c),
  }))
  const verdict = resolveFleetVerdict(annotated)
  const scored = annotated.filter(cellCountsTowardVerdict)
  const fleetNominal = scored.length > 0 && scored.every(c => resolveCellGate(c) === 'GO')
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
