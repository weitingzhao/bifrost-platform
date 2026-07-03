/**
 * Network Health — Control Room projection helpers.
 * Merges spine (GET /api/v1/context) with networkUpgradeCatalog.ts fallback.
 * Live UniFi probe remains Phase 7 (/api/v1/network/*).
 */

import type { OpsContextResponse } from '@/api/types'
import {
  DEPLOYMENT_PROGRESS,
  FIREWALL_APPLIED,
  NET_UPGRADE_STATUS,
  NET_UPGRADE_VERSION,
} from './networkUpgradeCatalog'

export type NetworkStreamProjection = {
  stream: string
  label: string
  done: number
  total: number
  note: string
  source: 'spine' | 'catalog'
}

export function resolveNetworkStreamProjections(
  context: OpsContextResponse | undefined,
): NetworkStreamProjection[] {
  return DEPLOYMENT_PROGRESS.map(catalogRow => {
    const spineStream = context?.tracks?.infra?.streams?.find(s => s.id === catalogRow.stream)
    if (spineStream != null) {
      return {
        stream: catalogRow.stream,
        label: catalogRow.label,
        done: spineStream.done,
        total: spineStream.total,
        note: spineStream.note?.trim() ? spineStream.note : catalogRow.note,
        source: 'spine' as const,
      }
    }
    return {
      stream: catalogRow.stream,
      label: catalogRow.label,
      done: catalogRow.done,
      total: catalogRow.total,
      note: catalogRow.note,
      source: 'catalog' as const,
    }
  })
}

export const NETWORK_HEALTH_PROJECTION = {
  catalogVersion: NET_UPGRADE_VERSION,
  status: NET_UPGRADE_STATUS,
  firewall: FIREWALL_APPLIED,
  futureProbe:
    'GET /api/v1/network/* — see Architecture → Network API (networkApiContractCatalog.ts; planned, no Go handlers)',
  agentPlaybooks: 'Agent Protocol → Network diagnostic playbooks (POLICY_NOMINAL / POLICY_DRIFT / SESSION_PATH / POSTURE_FORBIDDEN)',
} as const
