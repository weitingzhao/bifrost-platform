import { describe, expect, it } from 'vitest'
import * as migrated from '../fleetNavigation'
import * as compatibility from '@/lib/control-room/fleetSnapshot/nav'

// Frozen 86f7a7d4 implementation: compare every branch without changing original callers.
/**
 * Fleet navigation / viewer-env helpers.
 */
import {
  type FleetEnvColumn,
  type FleetRole,
  type FleetViewerEnv,
  type FleetCell,
} from '@/lib/control-room/fleetSnapshot/types'

function fleetCellNavigateTab(cell: Pick<FleetCell, 'role' | 'escalateTabId'>): string {
  if (cell.escalateTabId) return cell.escalateTabId
  return fleetRoleNavigateTab(cell.role)
}

function fleetRoleNavigateTab(role: FleetRole): string {
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

function normalizeViewerEnv(raw: string | undefined | null): FleetViewerEnv {
  const v = (raw ?? '').trim().toLowerCase()
  if (v === 'dev' || v === 'stg' || v === 'prod' || v === 'dev-local') return v
  return 'dev'
}

function viewerEnvBadgeLabel(env: FleetViewerEnv): 'DEV' | 'STG' | 'PROD' | 'DEV-LOCAL' {
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

function cellKey(role: FleetRole, env: FleetEnvColumn | 'span'): string {
  return `${role}:${env}`
}

const roles: FleetRole[] = ['rocket', 'satellite', 'engineer', 'ground', 'vendor']
describe('fleet navigation extraction parity', () => {
  it.each(roles)('preserves role, explicit target and cell keys for %s', role => {
    expect(migrated.fleetRoleNavigateTab(role)).toBe(fleetRoleNavigateTab(role))
    for (const escalateTabId of [undefined, '', 'queue', 'unknown-target', ' ']) {
      const cell = { role, escalateTabId }
      expect(migrated.fleetCellNavigateTab(cell)).toBe(fleetCellNavigateTab(cell))
      expect(compatibility.fleetCellNavigateTab(cell)).toBe(fleetCellNavigateTab(cell))
    }
    for (const env of ['dev', 'stg', 'prod', 'span'] as const) {
      expect(migrated.cellKey(role, env)).toBe(cellKey(role, env))
    }
  })
  it.each([undefined, null, '', 'dev', 'stg', 'prod', 'dev-local', ' DEV ', 'StG', 'PROD\n', ' Dev-Local ', 'production', 'unknown', '   '])('preserves environment normalization for %j', raw => {
    const normalized = normalizeViewerEnv(raw)
    expect(migrated.normalizeViewerEnv(raw)).toBe(normalized)
    expect(migrated.viewerEnvBadgeLabel(normalized)).toBe(viewerEnvBadgeLabel(normalized))
  })
  it('retains the original import path and function identities', () => {
    for (const key of Object.keys(migrated) as (keyof typeof migrated)[]) expect(compatibility[key]).toBe(migrated[key])
  })
})
