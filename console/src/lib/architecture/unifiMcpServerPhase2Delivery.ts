/** unifi-mcp-server stream — Phase 2 L0 routes + MCP read tools delivery checklist. */

import { isUnifiMcpServerPhase1SignedOff } from './unifiMcpServerPhase1Delivery'
import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'

export const UNIFI_MCP_SERVER_PHASE2_VERSION = '2026-07-03'

export interface UnifiMcpServerPhase2DeliveryItem {
  id: 'UMS2-1' | 'UMS2-2' | 'UMS2-3' | 'UMS2-4' | 'UMS2-5'
  title: string
  summary: string
  verifySteps: string[]
}

export const UNIFI_MCP_SERVER_PHASE2_DELIVERY_ITEMS: UnifiMcpServerPhase2DeliveryItem[] = [
  {
    id: 'UMS2-1',
    title: 'platform-api L0 GET /api/v1/network/*',
    summary:
      'Six read-only routes registered in server.go — status, zones, policies, audit, devices, clients.',
    verifySteps: [
      'GET /api/v1/network/status returns controller_version + session_path when UNIFI_* set.',
      'Routes mounted without operator auth (L0 viewer probe).',
      'POST /api/v1/network/firewall/apply not registered yet (UMS4).',
    ],
  },
  {
    id: 'UMS2-2',
    title: 'network.Handler + Service',
    summary:
      'api/internal/network — Service wraps unifi client; audit returns POLICY_NOMINAL | POLICY_DRIFT classification.',
    verifySteps: [
      'Handler uses api/internal/network/unifi from UMS1.',
      'GET /api/v1/network/audit mirrors unifi_firewall_setup.py zone/policy gap checks.',
      'go test ./internal/network -count=1 passes (httptest mock).',
    ],
  },
  {
    id: 'UMS2-3',
    title: 'mcp/unifi read tools',
    summary:
      'mcp/unifi/src/index.ts — 7 stdio tools proxy platform-api GET routes (decoupling principle).',
    verifySteps: [
      'Tools: unifi_mcp_health, get_network_status, get_network_zones, get_network_policies, audit_network_firewall, get_network_devices, get_network_clients.',
      'Each tool calls platformGet(/api/v1/network/…) — same auth as mcp/platform.',
      'No write tools yet — apply_network_firewall follows UMS4.',
    ],
  },
  {
    id: 'UMS2-4',
    title: 'Contract catalog — L0 implemented flags',
    summary:
      'networkApiContractCatalog.ts marks 6 GET routes + read MCP tools implemented; stream progress 2/4.',
    verifySteps: [
      'Network API page — planned routes table shows implemented for L0 GET rows.',
      'MCP tools table shows read tools implemented; apply_network_firewall still planned.',
      'UniFi MCP Server stream section shows 2/4 progress; UMS2 status done.',
    ],
  },
  {
    id: 'UMS2-5',
    title: 'Phase 2 sign-off panel',
    summary:
      'Network API page mounts UniFi MCP Server Phase 2 sign-off; requires Phase 1 signed off.',
    verifySteps: [
      'Governance → Standards — UniFi MCP Server Phase 2 · L0 routes + MCP read sign-off panel.',
      'Mark all UMS2 items verified → Sign off Phase 2 delivery (Admin token).',
      'Control Room live probe still catalog-only until UMS3.',
    ],
  },
]

export interface UnifiMcpServerPhase2ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface UnifiMcpServerPhase2SignoffState {
  version: string
  items: Record<string, UnifiMcpServerPhase2ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_unifi_mcp_server_phase2_signoff'

function emptyItemState(): UnifiMcpServerPhase2ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultUnifiMcpServerPhase2SignoffState(): UnifiMcpServerPhase2SignoffState {
  const items: Record<string, UnifiMcpServerPhase2ItemVerification> = {}
  for (const item of UNIFI_MCP_SERVER_PHASE2_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: UNIFI_MCP_SERVER_PHASE2_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadUnifiMcpServerPhase2SignoffState(): UnifiMcpServerPhase2SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultUnifiMcpServerPhase2SignoffState()
    const parsed = JSON.parse(raw) as UnifiMcpServerPhase2SignoffState
    if (parsed.version !== UNIFI_MCP_SERVER_PHASE2_VERSION) {
      return defaultUnifiMcpServerPhase2SignoffState()
    }
    const merged = defaultUnifiMcpServerPhase2SignoffState()
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
    return defaultUnifiMcpServerPhase2SignoffState()
  }
}

export function saveUnifiMcpServerPhase2SignoffState(state: UnifiMcpServerPhase2SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allUnifiMcpServerPhase2ItemsVerified(state: UnifiMcpServerPhase2SignoffState): boolean {
  return UNIFI_MCP_SERVER_PHASE2_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function unifiMcpServerPhase2VerificationCount(state: UnifiMcpServerPhase2SignoffState): {
  verified: number
  total: number
} {
  const verified = UNIFI_MCP_SERVER_PHASE2_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: UNIFI_MCP_SERVER_PHASE2_DELIVERY_ITEMS.length }
}

export function isUnifiMcpServerPhase2SignedOff(): boolean {
  return loadUnifiMcpServerPhase2SignoffState().signedOffAt != null
}

export function priorUnifiMcpServerPhase2Prerequisites(): { ok: boolean; missing: string[] } {
  const missing: string[] = []
  if (!isUnifiMcpServerPhase1SignedOff()) {
    missing.push('UniFi MCP Server Phase 1 (REST client library)')
  }
  return { ok: missing.length === 0, missing }
}
