/** Governance boundary program — Phase 4 Reconciliation extension delivery checklist. */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'

export const GOVERNANCE_PHASE4_VERSION = '2026-07-01'

export interface GovernancePhase4DeliveryItem {
  id: 'GP4-1' | 'GP4-2' | 'GP4-3' | 'GP4-4' | 'GP4-5'
  title: string
  summary: string
  verifySteps: string[]
}

export const GOVERNANCE_PHASE4_DELIVERY_ITEMS: GovernancePhase4DeliveryItem[] = [
  {
    id: 'GP4-1',
    title: 'Reconcile gate catalog-spine-parity',
    summary:
      'reconcileBriefing detects deployMainlineCatalog hardcoded status vs spine milestone (warning, CATALOG_DRIFT).',
    verifySteps: [
      'gate-catalog-spine-parity listed in Briefing Reconciliation → Reconcile gate table.',
      'Live panel shows CATALOG_DRIFT when seq 5 claims IN_PROGRESS but spine 2c-b-prod-cutover is SIGNED.',
    ],
  },
  {
    id: 'GP4-2',
    title: 'Catalog milestone ref validation',
    summary:
      'Delivery / Vision / Deploy Mainline milestone ids must exist on spine — gate-catalog-milestone-refs.',
    verifySteps: [
      'architectureCatalogMilestoneRefs includes k3s-stg-v2-deliver, 2c-b-prod-cutover, vision-v* ids.',
      'Missing spine id emits CATALOG_DRIFT warning (not silent).',
    ],
  },
  {
    id: 'GP4-3',
    title: 'Briefing Reconciliation UI CATALOG_DRIFT panel',
    summary:
      'Agent → Briefing Reconciliation shows live catalog-spine drift findings with CATALOG_DRIFT tag.',
    verifySteps: [
      'Open Agent → Briefing Reconciliation — Catalog ↔ spine drift section at top.',
      'When drift exists: CATALOG_DRIFT tag + finding list; when clean: “No catalog-spine drift”.',
    ],
  },
  {
    id: 'GP4-4',
    title: 'Nightly scan_layer3 catalog_spine_parity',
    summary:
      'Layer 3 semantic scan includes deployMainlineCatalog ↔ spine parity (offline mirror of Console gate).',
    verifySteps: [
      'agent/drift/scan_layer3.py reports catalog_spine_parity findings for MAINLINE seq 5 vs 2c-b-prod-cutover.',
      'DRIFT_LAYER_MAP L3 extension documents catalog-spine coverage.',
    ],
  },
  {
    id: 'GP4-5',
    title: 'CI check_spine_catalog milestone refs',
    summary:
      'scripts/ci/check_spine_catalog.sh validates delivery/spine milestone ids exist in ops-context.yaml.',
    verifySteps: [
      'Run scripts/ci/check_spine_catalog.sh — exits 0 when refs resolve.',
      'Script prints checked milestone id list on success.',
    ],
  },
]

export interface GovernancePhase4ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface GovernancePhase4SignoffState {
  version: string
  items: Record<string, GovernancePhase4ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_governance_phase4_signoff'

function emptyItemState(): GovernancePhase4ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultPhase4SignoffState(): GovernancePhase4SignoffState {
  const items: Record<string, GovernancePhase4ItemVerification> = {}
  for (const item of GOVERNANCE_PHASE4_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: GOVERNANCE_PHASE4_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadGovernancePhase4SignoffState(): GovernancePhase4SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultPhase4SignoffState()
    const parsed = JSON.parse(raw) as GovernancePhase4SignoffState
    if (parsed.version !== GOVERNANCE_PHASE4_VERSION) {
      return defaultPhase4SignoffState()
    }
    const merged = defaultPhase4SignoffState()
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
    return defaultPhase4SignoffState()
  }
}

export function saveGovernancePhase4SignoffState(state: GovernancePhase4SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allGovernancePhase4ItemsVerified(state: GovernancePhase4SignoffState): boolean {
  return GOVERNANCE_PHASE4_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function governancePhase4VerificationCount(state: GovernancePhase4SignoffState): {
  verified: number
  total: number
} {
  const verified = GOVERNANCE_PHASE4_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: GOVERNANCE_PHASE4_DELIVERY_ITEMS.length }
}

export function isGovernancePhase4SignedOff(): boolean {
  return loadGovernancePhase4SignoffState().signedOffAt != null
}
