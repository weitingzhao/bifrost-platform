/** unifi-mcp-server stream — Phase 3 Control Room live probe delivery checklist. */

import { isUnifiMcpServerPhase2SignedOff } from './unifiMcpServerPhase2Delivery'
import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'

export const UNIFI_MCP_SERVER_PHASE3_VERSION = '2026-07-03'

export interface UnifiMcpServerPhase3DeliveryItem {
  id: 'UMS3-1' | 'UMS3-2' | 'UMS3-3' | 'UMS3-4' | 'UMS3-5'
  title: string
  summary: string
  verifySteps: string[]
}

export const UNIFI_MCP_SERVER_PHASE3_DELIVERY_ITEMS: UnifiMcpServerPhase3DeliveryItem[] = [
  {
    id: 'UMS3-1',
    title: 'Console API client — network live probe',
    summary:
      'fetchNetworkStatus + fetchNetworkAudit in api/platform.ts — graceful 503 when UNIFI_* unset.',
    verifySteps: [
      'GET /api/v1/network/status and /audit via console proxy (same as matrix probes).',
      '503 responses include hint for UNIFI_HOST/USER/PASS — no unhandled throw in panel.',
      'Types NetworkStatusResponse / NetworkAuditResponse in api/types.ts.',
    ],
  },
  {
    id: 'UMS3-2',
    title: 'Control Room NetworkHealthPanel live probe',
    summary:
      'useNetworkLiveProbe hook — TanStack Query polls status + audit every 30s; StatusLamp + POLICY_* classification.',
    verifySteps: [
      'Mission Control → Control Room — Network Health summary; Ground Systems → Network — full devices/clients tables.',
      'With UNIFI_* configured: controller version + POLICY_NOMINAL or POLICY_DRIFT.',
      'Without credentials: unknown lamp + platform-api hint (not blank catalog-only).',
    ],
  },
  {
    id: 'UMS3-3',
    title: 'Projection catalog update',
    summary:
      'networkConsoleProjection.ts — liveProbeNote replaces stale “planned, no Go handlers” futureProbe text.',
    verifySteps: [
      'Network Health footer references live probe polling note.',
      'Panel description mentions GET /api/v1/network/status + audit.',
      'Catalog firewall block (FIREWALL_APPLIED) still shown alongside live probe.',
    ],
  },
  {
    id: 'UMS3-4',
    title: 'Implementation catalog — stream 3/4',
    summary: 'unifiMcpServerCatalog.ts UMS3 marked done; progress 3/4.',
    verifySteps: [
      'Governance → Standards — UniFi MCP Server stream section shows 3/4 progress.',
      'Phase ③ Live probe row status done; Phase ④ MCP write pending.',
      'Network API contract status still L0 live; POST routes planned until UMS4.',
    ],
  },
  {
    id: 'UMS3-5',
    title: 'Phase 3 sign-off panel',
    summary:
      'Network API page mounts UniFi MCP Server Phase 3 sign-off; requires Phase 2 signed off.',
    verifySteps: [
      'Governance → Standards — UniFi MCP Server Phase 3 · Live probe sign-off panel.',
      'Mark all UMS3 items verified → Sign off Phase 3 delivery (Admin token).',
      'MCP write tools + POST /api/v1/network/firewall/apply follow in Phase 4.',
    ],
  },
]

export interface UnifiMcpServerPhase3ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface UnifiMcpServerPhase3SignoffState {
  version: string
  items: Record<string, UnifiMcpServerPhase3ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_unifi_mcp_server_phase3_signoff'

function emptyItemState(): UnifiMcpServerPhase3ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultUnifiMcpServerPhase3SignoffState(): UnifiMcpServerPhase3SignoffState {
  const items: Record<string, UnifiMcpServerPhase3ItemVerification> = {}
  for (const item of UNIFI_MCP_SERVER_PHASE3_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: UNIFI_MCP_SERVER_PHASE3_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadUnifiMcpServerPhase3SignoffState(): UnifiMcpServerPhase3SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultUnifiMcpServerPhase3SignoffState()
    const parsed = JSON.parse(raw) as UnifiMcpServerPhase3SignoffState
    if (parsed.version !== UNIFI_MCP_SERVER_PHASE3_VERSION) {
      return defaultUnifiMcpServerPhase3SignoffState()
    }
    const merged = defaultUnifiMcpServerPhase3SignoffState()
    for (const id of Object.keys(merged.items)) {
      if (parsed.items[id] != null) {
        merged.items[id] = parsed.items[id]
      }
    }
    merged.signedOffAt = parsed.signedOffAt
    merged.signedOffBy = parsed.signedOffBy
    merged.note = parsed.note
    return merged
  } catch {
    return defaultUnifiMcpServerPhase3SignoffState()
  }
}

export function saveUnifiMcpServerPhase3SignoffState(state: UnifiMcpServerPhase3SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allUnifiMcpServerPhase3ItemsVerified(state: UnifiMcpServerPhase3SignoffState): boolean {
  return UNIFI_MCP_SERVER_PHASE3_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function unifiMcpServerPhase3VerificationCount(state: UnifiMcpServerPhase3SignoffState): {
  verified: number
  total: number
} {
  const verified = UNIFI_MCP_SERVER_PHASE3_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: UNIFI_MCP_SERVER_PHASE3_DELIVERY_ITEMS.length }
}

export function isUnifiMcpServerPhase3SignedOff(): boolean {
  return loadUnifiMcpServerPhase3SignoffState().signedOffAt != null
}

export function priorUnifiMcpServerPhase3Prerequisites(): { ok: boolean; missing: string[] } {
  const missing: string[] = []
  if (!isUnifiMcpServerPhase2SignedOff()) {
    missing.push('UniFi MCP Server Phase 2 (L0 routes + MCP read)')
  }
  return { ok: missing.length === 0, missing }
}
