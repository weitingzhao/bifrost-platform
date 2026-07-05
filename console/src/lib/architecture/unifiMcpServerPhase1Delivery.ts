/** unifi-mcp-server stream — Phase 1 REST client library delivery checklist. */

import { isNetworkGovernancePhase8SignedOff } from './networkGovernancePhase8Delivery'
import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'

export const UNIFI_MCP_SERVER_PHASE1_VERSION = '2026-07-03'

export interface UnifiMcpServerPhase1DeliveryItem {
  id: 'UMS1-1' | 'UMS1-2' | 'UMS1-3' | 'UMS1-4' | 'UMS1-5'
  title: string
  summary: string
  verifySteps: string[]
}

export const UNIFI_MCP_SERVER_PHASE1_DELIVERY_ITEMS: UnifiMcpServerPhase1DeliveryItem[] = [
  {
    id: 'UMS1-1',
    title: 'Go UniFi client package',
    summary:
      'api/internal/network/unifi — ConfigFromEnv (UNIFI_HOST/USER/PASS/API_KEY), Client with Session v2 login (cookie + CSRF).',
    verifySteps: [
      'Repo path api/internal/network/unifi/client.go + config.go exists.',
      'Login uses POST /api/auth/login; subsequent requests send Cookie + X-CSRF-Token.',
      'Matches scripts/unifi_firewall_setup.py UniFiSession semantics (spine D9 Session v2).',
    ],
  },
  {
    id: 'UMS1-2',
    title: 'Read methods — legacy v1 + v2',
    summary:
      'Client exposes LegacyGet, V2Get, ListDevices, ListClients, Health, ListZones, ListPolicies, IntegrationSitesHaveID.',
    verifySteps: [
      'Legacy paths use /proxy/network/api/s/{site}/… (stat/device, stat/sta, stat/health).',
      'V2 paths use /proxy/network/v2/api/site/{site}/firewall/zone and firewall-policies.',
      'IntegrationGet uses X-API-KEY for optional audit-only /integration/v1/sites probe.',
    ],
  },
  {
    id: 'UMS1-3',
    title: 'Unit tests (httptest, no live UCG)',
    summary: 'go test ./internal/network/unifi — mocks login + device/zone/integration flows.',
    verifySteps: [
      'From api/: go test ./internal/network/unifi -count=1 passes.',
      'Tests cover login cookie/CSRF, ListDevices, ListZones, IntegrationSitesHaveID.',
      'ConfigFromEnv fails when UNIFI_USER/UNIFI_PASS unset.',
    ],
  },
  {
    id: 'UMS1-4',
    title: 'Implementation catalog + stream progress',
    summary:
      'unifiMcpServerCatalog.ts documents 4 stream phases; UMS1 marked done; progress 1/4.',
    verifySteps: [
      'Governance → Standards — “UniFi MCP Server stream” section shows 1/4 progress.',
      'Phase ① REST client row status done; phases ②–④ pending.',
      'Copy for LLM includes client path api/internal/network/unifi.',
    ],
  },
  {
    id: 'UMS1-5',
    title: 'Phase 1 sign-off panel',
    summary:
      'Network API page mounts UniFi MCP Server Phase 1 sign-off; requires Network Governance program complete (NG8).',
    verifySteps: [
      'Governance → Standards — UniFi MCP Server Phase 1 · REST client sign-off panel.',
      'Mark all UMS1 items verified → Sign off Phase 1 delivery (Admin token).',
      'Sign-off does not register /api/v1/network/* routes yet — Phase 2 adds handlers + MCP read.',
    ],
  },
]

export interface UnifiMcpServerPhase1ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface UnifiMcpServerPhase1SignoffState {
  version: string
  items: Record<string, UnifiMcpServerPhase1ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_unifi_mcp_server_phase1_signoff'

function emptyItemState(): UnifiMcpServerPhase1ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultUnifiMcpServerPhase1SignoffState(): UnifiMcpServerPhase1SignoffState {
  const items: Record<string, UnifiMcpServerPhase1ItemVerification> = {}
  for (const item of UNIFI_MCP_SERVER_PHASE1_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: UNIFI_MCP_SERVER_PHASE1_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadUnifiMcpServerPhase1SignoffState(): UnifiMcpServerPhase1SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultUnifiMcpServerPhase1SignoffState()
    const parsed = JSON.parse(raw) as UnifiMcpServerPhase1SignoffState
    if (parsed.version !== UNIFI_MCP_SERVER_PHASE1_VERSION) {
      return defaultUnifiMcpServerPhase1SignoffState()
    }
    const merged = defaultUnifiMcpServerPhase1SignoffState()
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
    return defaultUnifiMcpServerPhase1SignoffState()
  }
}

export function saveUnifiMcpServerPhase1SignoffState(state: UnifiMcpServerPhase1SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allUnifiMcpServerPhase1ItemsVerified(state: UnifiMcpServerPhase1SignoffState): boolean {
  return UNIFI_MCP_SERVER_PHASE1_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function unifiMcpServerPhase1VerificationCount(state: UnifiMcpServerPhase1SignoffState): {
  verified: number
  total: number
} {
  const verified = UNIFI_MCP_SERVER_PHASE1_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: UNIFI_MCP_SERVER_PHASE1_DELIVERY_ITEMS.length }
}

export function isUnifiMcpServerPhase1SignedOff(): boolean {
  return loadUnifiMcpServerPhase1SignoffState().signedOffAt != null
}

export function priorUnifiMcpServerPrerequisites(): { ok: boolean; missing: string[] } {
  const missing: string[] = []
  if (!isNetworkGovernancePhase8SignedOff()) {
    missing.push('Network Governance Phase 8 (program complete)')
  }
  return { ok: missing.length === 0, missing }
}
