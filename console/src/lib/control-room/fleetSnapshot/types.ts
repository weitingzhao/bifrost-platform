/**
 * Daily Ops Fleet Desk — shared types and constants.
 */
import type { Signal } from '@/lib/control-room/missionSignals'

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

