/**
 * (role, env) → fixScope / prompt routing for Daily Ops Fleet Desk.
 * Prefer explicit cell mapping over silent pickFixScope across unrelated cells.
 */
import type { FleetCell, FleetCellSignal, FleetEnvColumn, FleetRole, FleetSnapshot } from '@/lib/control-room/fleetSnapshot'
import { cellKey } from '@/lib/control-room/fleetSnapshot'
import {
  DELIVER_STG_RECOVER_SCOPE,
  GITOPS_CONFIG_REPAIR_SCOPE,
  PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
} from '@/lib/agent/agentScopes'
import { PROD_ENV_FIX_SCOPE } from '@/lib/agent/prodEnvironmentFixPrompt'

export type FleetFixRoute = {
  role: FleetRole
  env: FleetEnvColumn | 'span'
  fixScope: string | null
  agentFixAllowed: boolean
  /** When Agent Fix is blocked, optional navigate tab */
  navigateTabId?: string
  disabledReason?: string
}

const ROUTE_TABLE: FleetFixRoute[] = [
  {
    role: 'rocket',
    env: 'dev',
    fixScope: PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
    agentFixAllowed: true,
  },
  {
    role: 'rocket',
    env: 'stg',
    fixScope: DELIVER_STG_RECOVER_SCOPE,
    agentFixAllowed: true,
  },
  {
    role: 'rocket',
    env: 'prod',
    fixScope: PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
    agentFixAllowed: true,
  },
  {
    role: 'rocket',
    env: 'dev-local',
    fixScope: null,
    agentFixAllowed: false,
    disabledReason: 'No Rocket seat on Mac thin-client',
  },
  {
    role: 'satellite',
    env: 'dev',
    fixScope: PROD_ENV_FIX_SCOPE,
    agentFixAllowed: true,
  },
  {
    role: 'satellite',
    env: 'stg',
    fixScope: DELIVER_STG_RECOVER_SCOPE,
    agentFixAllowed: true,
  },
  {
    role: 'satellite',
    env: 'prod',
    fixScope: PROD_ENV_FIX_SCOPE,
    agentFixAllowed: true,
  },
  {
    role: 'satellite',
    env: 'dev-local',
    fixScope: null,
    agentFixAllowed: false,
    navigateTabId: 'operator-plane',
    disabledReason: 'Requires Ground bridge — not Agent Fix',
  },
  {
    role: 'engineer',
    env: 'span',
    fixScope: null,
    agentFixAllowed: false,
    navigateTabId: 'operator-plane',
    disabledReason: 'Engineer CRITICAL → Operator Plane / Ground',
  },
  {
    role: 'ground',
    env: 'span',
    fixScope: PROD_ENV_FIX_SCOPE,
    agentFixAllowed: true,
    navigateTabId: 'operator-plane',
  },
  {
    role: 'vendor',
    env: 'span',
    fixScope: GITOPS_CONFIG_REPAIR_SCOPE,
    agentFixAllowed: true,
  },
]

export function fleetFixRoutes(): readonly FleetFixRoute[] {
  return ROUTE_TABLE
}

export function lookupFleetFixRoute(
  role: FleetRole,
  env: FleetEnvColumn | 'span',
): FleetFixRoute | undefined {
  return ROUTE_TABLE.find(r => r.role === role && r.env === env)
}

export function resolveCellFixScope(cell: FleetCell): string | null {
  if (cell.signal === 'ok' || cell.signal === 'unavailable') return null
  const env = cell.span ? 'span' : (cell.env ?? 'span')
  const route = lookupFleetFixRoute(cell.role, env)
  if (route == null) return cell.fixScope
  if (!route.agentFixAllowed) return null
  return route.fixScope ?? cell.fixScope
}

export function cellAllowsAgentFix(cell: FleetCell): boolean {
  if (cell.signal === 'ok' || cell.signal === 'unavailable' || cell.signal === 'unknown') {
    return false
  }
  if (cell.role === 'engineer') return false
  const env = cell.span ? 'span' : (cell.env ?? 'span')
  const route = lookupFleetFixRoute(cell.role, env)
  if (route != null && !route.agentFixAllowed) return false
  return cell.agentFixEnabled
}

export function buildFleetCellFixPrompt(cell: FleetCell, snap: FleetSnapshot): string {
  const scope = resolveCellFixScope(cell) ?? 'cluster_issues_full_auto'
  return [
    `Daily Ops Fleet Desk — Agent Fix for cell ${cell.key}.`,
    '',
    `Viewer env: ${snap.viewerEnv}`,
    `Role: ${cell.role} · Env: ${cell.env ?? 'span'} · Signal: ${cell.signal}`,
    `Probe path: ${cell.probePath}`,
    `Detail: ${cell.detail}`,
    '',
    `Playbook / scope: ${scope}`,
    '',
    '## Workflow',
    '1. verify_mission_snapshot (MCP) focusing this role × environment cell.',
    '2. Remediate only this cell — do not silently retarget other fleet cells.',
    '3. D10 remains BLOCKED — no live trade enable / daemon scale.',
    '',
    'Before closing: verify_mission_snapshot + post_fix_verification.passed must be true.',
  ].join('\n')
}

/** Pick the single worst fixable cell — never silently swap to another role's scope. */
export function pickFleetFixCell(snap: FleetSnapshot): FleetCell | null {
  const candidates = snap.cells.filter(c => cellAllowsAgentFix(c))
  if (candidates.length === 0) return null
  let best = candidates[0]
  const rank = (s: FleetCellSignal) =>
    s === 'fail' ? 3 : s === 'degraded' ? 2 : s === 'unknown' ? 1 : 0
  for (const c of candidates) {
    if (rank(c.signal) > rank(best.signal)) best = c
  }
  return best
}

export function fleetCellKey(role: FleetRole, env: FleetEnvColumn | 'span'): string {
  return cellKey(role, env)
}
