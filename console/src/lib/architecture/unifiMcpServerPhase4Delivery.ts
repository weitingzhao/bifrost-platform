/** unifi-mcp-server stream — Phase 4 L1 apply + MCP write delivery checklist. */

import { isUnifiMcpServerPhase3SignedOff } from './unifiMcpServerPhase3Delivery'
import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'

export const UNIFI_MCP_SERVER_PHASE4_VERSION = '2026-07-03'

export interface UnifiMcpServerPhase4DeliveryItem {
  id: 'UMS4-1' | 'UMS4-2' | 'UMS4-3' | 'UMS4-4' | 'UMS4-5'
  title: string
  summary: string
  verifySteps: string[]
}

export const UNIFI_MCP_SERVER_PHASE4_DELIVERY_ITEMS: UnifiMcpServerPhase4DeliveryItem[] = [
  {
    id: 'UMS4-1',
    title: 'POST /api/v1/network/firewall/apply (L1 operator)',
    summary:
      'platform-api operator route runs idempotent firewall re-sync; audit trail via actuation.AuditLog.',
    verifySteps: [
      'POST /api/v1/network/firewall/apply with operator Bearer token returns 200 + post_apply_audit.',
      'Without UNIFI_* → 503 with hint (same as L0 routes).',
      'GET /api/v1/audit lists network.firewall.apply after successful apply.',
    ],
  },
  {
    id: 'UMS4-2',
    title: 'Apply executor — scripts/unifi_firewall_setup.py apply',
    summary:
      'network.Service.ApplyFirewall wraps python3 scripts/unifi_firewall_setup.py apply (Session v2 per D9).',
    verifySteps: [
      'Handler passes include_default_deny from JSON body to script (--include-default-deny flag).',
      'Script stdout captured in response result.stdout.',
      'go test ./internal/network -count=1 — HandleFirewallApply mock passes.',
    ],
  },
  {
    id: 'UMS4-3',
    title: 'MCP write tool — apply_network_firewall',
    summary:
      'mcp/unifi apply_network_firewall → POST /api/v1/network/firewall/apply (operator token via PLATFORM_OPERATOR_TOKEN).',
    verifySteps: [
      'Tool schema: optional include_default_deny boolean.',
      'platformPost in mcp/unifi/src/platformClient.ts mirrors mcp/platform pattern.',
      'Requires operator/admin token — same auth as other L1 MCP actuation.',
    ],
  },
  {
    id: 'UMS4-4',
    title: 'Contract catalog — stream 4/4 complete',
    summary:
      'networkApiContractCatalog + unifiMcpServerCatalog mark firewall/apply + apply_network_firewall implemented; progress 4/4.',
    verifySteps: [
      'Network API page — POST firewall/apply row shows implemented.',
      'MCP tools table shows apply_network_firewall implemented (routine/L1).',
      'UniFi MCP Server stream section shows 4/4; UMS4 status done.',
    ],
  },
  {
    id: 'UMS4-5',
    title: 'Phase 4 sign-off panel — stream complete',
    summary:
      'Network API page mounts UniFi MCP Server Phase 4 sign-off; requires Phase 3 signed off. Completes unifi-mcp-server spine ①–④.',
    verifySteps: [
      'Governance → Standards — UniFi MCP Server Phase 4 · MCP write sign-off panel.',
      'Mark all UMS4 items verified → Sign off Phase 4 delivery (Admin token).',
      'zones/restructure + wlan POST routes remain planned (out of UMS4 scope).',
    ],
  },
]

export interface UnifiMcpServerPhase4ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface UnifiMcpServerPhase4SignoffState {
  version: string
  items: Record<string, UnifiMcpServerPhase4ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_unifi_mcp_server_phase4_signoff'

function emptyItemState(): UnifiMcpServerPhase4ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultUnifiMcpServerPhase4SignoffState(): UnifiMcpServerPhase4SignoffState {
  const items: Record<string, UnifiMcpServerPhase4ItemVerification> = {}
  for (const item of UNIFI_MCP_SERVER_PHASE4_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: UNIFI_MCP_SERVER_PHASE4_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadUnifiMcpServerPhase4SignoffState(): UnifiMcpServerPhase4SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultUnifiMcpServerPhase4SignoffState()
    const parsed = JSON.parse(raw) as UnifiMcpServerPhase4SignoffState
    if (parsed.version !== UNIFI_MCP_SERVER_PHASE4_VERSION) {
      return defaultUnifiMcpServerPhase4SignoffState()
    }
    const merged = defaultUnifiMcpServerPhase4SignoffState()
    for (const item of UNIFI_MCP_SERVER_PHASE4_DELIVERY_ITEMS) {
      merged.items[item.id] = parsed.items[item.id] ?? emptyItemState()
    }
    merged.signedOffAt = parsed.signedOffAt
    merged.signedOffBy = parsed.signedOffBy
    merged.note = parsed.note
    return merged
  } catch {
    return defaultUnifiMcpServerPhase4SignoffState()
  }
}

export function saveUnifiMcpServerPhase4SignoffState(state: UnifiMcpServerPhase4SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allUnifiMcpServerPhase4ItemsVerified(state: UnifiMcpServerPhase4SignoffState): boolean {
  return UNIFI_MCP_SERVER_PHASE4_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function unifiMcpServerPhase4VerificationCount(state: UnifiMcpServerPhase4SignoffState): {
  verified: number
  total: number
} {
  const verified = UNIFI_MCP_SERVER_PHASE4_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: UNIFI_MCP_SERVER_PHASE4_DELIVERY_ITEMS.length }
}

export function isUnifiMcpServerPhase4SignedOff(): boolean {
  return loadUnifiMcpServerPhase4SignoffState().signedOffAt != null
}

export function priorUnifiMcpServerPhase4Prerequisites(): { ok: boolean; missing: string[] } {
  const missing: string[] = []
  if (!isUnifiMcpServerPhase3SignedOff()) {
    missing.push('UniFi MCP Server Phase 3 (Control Room live probe)')
  }
  return { ok: missing.length === 0, missing }
}
