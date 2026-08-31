/**
 * Fleet navigation / viewer-env helpers.
 */
import {
  type FleetEnvColumn,
  type FleetRole,
  type FleetViewerEnv,
  type FleetCell,
} from '@/lib/control-room/fleetSnapshot/types'

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
      return 'queue'
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
