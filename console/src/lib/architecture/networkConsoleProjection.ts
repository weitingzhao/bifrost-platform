/**
 * Network Health — Control Room projection helpers.
 * Merges spine (GET /api/v1/context) with networkUpgradeCatalog.ts fallback.
 * Live UniFi probe: GET /api/v1/network/status + audit (UMS3).
 */

import type { OpsContextResponse } from '@/api/opsContextTypes'
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
  liveProbeNote:
    'Live probe polls platform-api every 30s — requires UNIFI_HOST/USER/PASS on platform-api host. L1 apply via POST /api/v1/network/firewall/apply (operator) or MCP apply_network_firewall.',
  agentPlaybooks: 'Agent Protocol → Network diagnostic playbooks (POLICY_NOMINAL / POLICY_DRIFT / SESSION_PATH / POSTURE_FORBIDDEN)',
} as const
