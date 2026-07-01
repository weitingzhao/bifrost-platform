/** Governance boundary program — Phase 1 Constitution purification delivery checklist. */

export const GOVERNANCE_PHASE1_VERSION = '2026-07-01'

export interface GovernancePhase1DeliveryItem {
  id: 'GP1-1' | 'GP1-2' | 'GP1-3' | 'GP1-4' | 'GP1-5'
  title: string
  summary: string
  verifySteps: string[]
}

export const GOVERNANCE_PHASE1_DELIVERY_ITEMS: GovernancePhase1DeliveryItem[] = [
  {
    id: 'GP1-1',
    title: 'Governance boundary + Constitution section',
    summary:
      'Blueprint page shows Governance layers (Constitution / Spine / Projection) and BOUNDARY_RULES; North Star through AI boundaries sit under Constitution.',
    verifySteps: [
      'Open Architecture → Blueprint — “Governance boundary” section lists three layers and decision rules.',
      'Constitution block tag appears above North Star; Projection block appears after Success criteria.',
      'Copy Prompt for LLM output has “## Constitution” and “## Projection” sections.',
    ],
  },
  {
    id: 'GP1-2',
    title: 'Actuation phases are pure definitions (no current marker)',
    summary:
      'ACTUATION_PHASES rows are P0–P5 definitions only; live progress comes from MCP tools phase labels in Projection.',
    verifySteps: [
      'Actuation phases table shows P0, P1, … P5 without “(current)”.',
      'Projection → Actuation progress table shows implemented/total per phase from GET /api/v1/mcp/tools.',
      'Constitution actuation rows may show optional MCP progress hint when tools load.',
    ],
  },
  {
    id: 'GP1-3',
    title: 'API inventory moved to Projection authority',
    summary:
      'Static PLATFORM_API_ENDPOINTS table removed; Projection points to GET /api/v1/mcp/tools + catalog.go with live tool list.',
    verifySteps: [
      'No “Platform API endpoints” static table on Blueprint page.',
      'Projection section shows authority pointers and live MCP tool count.',
      'Architecture → MCP Contract still mirrors the same catalog.',
    ],
  },
  {
    id: 'GP1-4',
    title: 'Console views + success criteria purified',
    summary:
      'Console view purposes are one-line roles (no L0/L1 implementation detail); success criteria labeled as North Star completion (Constitution).',
    verifySteps: [
      'Cluster view purpose does not mention “L0 probe” or “L1 namespace”.',
      'Success criteria section title includes “Constitution — North Star completion”.',
      'Runtime criterion describes Observe→Act loop goal; live readiness deferred to Projection.',
    ],
  },
  {
    id: 'GP1-5',
    title: 'AI Platform phases without absolute time boxes',
    summary: 'AI Platform phases use sequence order (First / Second / Third) instead of “now ~3mo” calendar ranges.',
    verifySteps: [
      'AI Platform phases table column is “Sequence” not “Time”.',
      'Rows show First / Second / Third — no calendar month ranges.',
      'Copy Prompt AI Platform phases section matches UI.',
    ],
  },
]

export interface GovernancePhase1ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface GovernancePhase1SignoffState {
  version: string
  items: Record<string, GovernancePhase1ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_governance_phase1_signoff'

function emptyItemState(): GovernancePhase1ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultPhase1SignoffState(): GovernancePhase1SignoffState {
  const items: Record<string, GovernancePhase1ItemVerification> = {}
  for (const item of GOVERNANCE_PHASE1_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: GOVERNANCE_PHASE1_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadGovernancePhase1SignoffState(): GovernancePhase1SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultPhase1SignoffState()
    const parsed = JSON.parse(raw) as GovernancePhase1SignoffState
    if (parsed.version !== GOVERNANCE_PHASE1_VERSION) {
      return defaultPhase1SignoffState()
    }
    const merged = defaultPhase1SignoffState()
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
    return defaultPhase1SignoffState()
  }
}

export function saveGovernancePhase1SignoffState(state: GovernancePhase1SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
  } catch {
    // storage unavailable
  }
}

export function allGovernancePhase1ItemsVerified(state: GovernancePhase1SignoffState): boolean {
  return GOVERNANCE_PHASE1_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function governancePhase1VerificationCount(state: GovernancePhase1SignoffState): {
  verified: number
  total: number
} {
  const verified = GOVERNANCE_PHASE1_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: GOVERNANCE_PHASE1_DELIVERY_ITEMS.length }
}

export function isGovernancePhase1SignedOff(): boolean {
  return loadGovernancePhase1SignoffState().signedOffAt != null
}
