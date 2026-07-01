/** Mission Signal Program — Phase 4 (Hermes First Task) delivery checklist. */

export const MISSION_SIGNAL_PHASE4_VERSION = '2026-07-02'

export interface MissionSignalPhase4DeliveryItem {
  id: 'MSP4-1' | 'MSP4-2' | 'MSP4-3' | 'MSP4-4'
  title: string
  summary: string
  verifySteps: string[]
}

export const MISSION_SIGNAL_PHASE4_DELIVERY_ITEMS: MissionSignalPhase4DeliveryItem[] = [
  {
    id: 'MSP4-1',
    title: 'get_hermes_readiness API + MCP aggregates gateway, LLM key, blockers',
    summary:
      'GET /api/v1/agent/hermes/readiness returns ready, blockers[], llm_key, nous_hermes, platform MCP counts, and embedded first_task.',
    verifySteps: [
      'curl http://127.0.0.1:8780/api/v1/agent/hermes/readiness — JSON with ready, blockers, llm_key.configured, first_task.id.',
      'MCP catalog lists get_hermes_readiness (implemented=true).',
      'When NOUS_HERMES_URL unset, blockers explain not_configured; API still returns first_task prompt.',
    ],
  },
  {
    id: 'MSP4-2',
    title: 'Hermes First Task catalog — L0 read-only Mission health pass',
    summary:
      'GET /api/v1/agent/hermes/first-task + MCP get_hermes_first_task return hermes-mission-health-l0 prompt requiring verify_mission_snapshot (no actuation).',
    verifySteps: [
      'first_task.autonomy is L0; required_mcp_tools includes verify_mission_snapshot, verify_payload, get_connectivity_matrix.',
      'Control Room Phase 4 panel — Copy first-task prompt copies full prompt text.',
      'MCP get_hermes_first_task returns same prompt as readiness.first_task.',
    ],
  },
  {
    id: 'MSP4-3',
    title: 'Agent Protocol documents Hermes First Task playbook',
    summary:
      'Architecture → Agent Protocol includes Hermes First Task section (readiness gate + L0 steps). Agent task catalog lists Hermes · First task.',
    verifySteps: [
      'Agent Protocol page — “Hermes First Task (L0)” section with readiness MCP + task steps.',
      'Agent task catalog includes Hermes · First task (hermes-first-task scope).',
      'LLM pack mentions get_hermes_readiness and get_hermes_first_task.',
    ],
  },
  {
    id: 'MSP4-4',
    title: 'Control Room live Hermes readiness strip',
    summary:
      'Phase 4 sign-off panel polls readiness and shows gateway / LLM / ready status before Owner sign-off.',
    verifySteps: [
      'Control Room bottom — Mission Signal Phase 4 panel above Phase 3.',
      'Live strip shows Hermes status, LLM key configured, ready YES/NO, blockers count.',
      'Phase 4 gated on Phase 3 signed off.',
    ],
  },
]

export interface MissionSignalPhase4ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface MissionSignalPhase4SignoffState {
  version: string
  items: Record<string, MissionSignalPhase4ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_mission_signal_phase4_signoff'

function emptyItemState(): MissionSignalPhase4ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultMissionSignalPhase4SignoffState(): MissionSignalPhase4SignoffState {
  const items: Record<string, MissionSignalPhase4ItemVerification> = {}
  for (const item of MISSION_SIGNAL_PHASE4_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: MISSION_SIGNAL_PHASE4_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadMissionSignalPhase4SignoffState(): MissionSignalPhase4SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultMissionSignalPhase4SignoffState()
    const parsed = JSON.parse(raw) as MissionSignalPhase4SignoffState
    if (parsed.version !== MISSION_SIGNAL_PHASE4_VERSION) {
      return defaultMissionSignalPhase4SignoffState()
    }
    const merged = defaultMissionSignalPhase4SignoffState()
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
    return defaultMissionSignalPhase4SignoffState()
  }
}

export function saveMissionSignalPhase4SignoffState(state: MissionSignalPhase4SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
  } catch {
    // storage unavailable
  }
}

export function allMissionSignalPhase4ItemsVerified(state: MissionSignalPhase4SignoffState): boolean {
  return MISSION_SIGNAL_PHASE4_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function missionSignalPhase4VerificationCount(state: MissionSignalPhase4SignoffState): {
  verified: number
  total: number
} {
  const verified = MISSION_SIGNAL_PHASE4_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: MISSION_SIGNAL_PHASE4_DELIVERY_ITEMS.length }
}

export function isMissionSignalPhase4SignedOff(): boolean {
  return loadMissionSignalPhase4SignoffState().signedOffAt != null
}
