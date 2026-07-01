/** Mission Signal Program — Phase 5 (Flight Director) delivery checklist. */

export const MISSION_SIGNAL_PHASE5_VERSION = '2026-07-02'

export interface MissionSignalPhase5DeliveryItem {
  id: 'MSP5-1' | 'MSP5-2' | 'MSP5-3' | 'MSP5-4'
  title: string
  summary: string
  verifySteps: string[]
}

export const MISSION_SIGNAL_PHASE5_DELIVERY_ITEMS: MissionSignalPhase5DeliveryItem[] = [
  {
    id: 'MSP5-1',
    title: 'get_agent_performance API + MCP — 7d/30d KPIs from remediation JobStore',
    summary:
      'GET /api/v1/agent/governance/performance returns success rate, intervention rate, MTTR — sourced from remediation runner jobs (Hermes/GPU bypassed).',
    verifySteps: [
      'curl http://127.0.0.1:8780/api/v1/agent/governance/performance — windows 7d/30d, data_source remediation_jobs.',
      'MCP catalog lists get_agent_performance (implemented=true).',
      'Trust & Autonomy page Performance section loads without HTTP error.',
    ],
  },
  {
    id: 'MSP5-2',
    title: 'get_trust_matrix — earned autonomy per task scope',
    summary:
      'GET /api/v1/agent/governance/trust-matrix returns catalog tasks with L0/L1/L2, consecutive_successes, promotion_eligible, demotion_triggered.',
    verifySteps: [
      'curl trust-matrix — entries for Health · Check, Platform · Release, Health · Post-fix, etc.',
      'MCP get_trust_matrix implemented; runner tool available.',
      'Trust & Autonomy page Trust Matrix table renders entries (empty job history shows catalog defaults).',
    ],
  },
  {
    id: 'MSP5-3',
    title: 'Capability map + Agent Protocol Flight Director playbook',
    summary:
      'GET /api/v1/agent/governance/capability-map maps task scopes → MCP tools → mission signals; Agent Protocol documents Flight Director loop.',
    verifySteps: [
      'curl capability-map — gap_count and per-task mcp_tools; post-fix-verification has no MCP gap.',
      'Agent Protocol — “Flight Director governance” section with performance/trust/briefing steps.',
      'Trust & Autonomy page shows Capability map table.',
    ],
  },
  {
    id: 'MSP5-4',
    title: 'Control Room Flight Director live strip + program completion',
    summary:
      'get_flight_director_snapshot powers Phase 5 sign-off panel; Mission Signal Program complete after Owner sign-off.',
    verifySteps: [
      'Control Room bottom — Mission Signal Phase 5 panel above Phase 4.',
      'Live strip shows 7d success rate, trust entries, capability gaps, 24h briefing digest.',
      'Phase 5 gated on Phase 4 signed off; sign-off completes Mission Signal Program.',
    ],
  },
]

export interface MissionSignalPhase5ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface MissionSignalPhase5SignoffState {
  version: string
  items: Record<string, MissionSignalPhase5ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_mission_signal_phase5_signoff'

function emptyItemState(): MissionSignalPhase5ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultMissionSignalPhase5SignoffState(): MissionSignalPhase5SignoffState {
  const items: Record<string, MissionSignalPhase5ItemVerification> = {}
  for (const item of MISSION_SIGNAL_PHASE5_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: MISSION_SIGNAL_PHASE5_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadMissionSignalPhase5SignoffState(): MissionSignalPhase5SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultMissionSignalPhase5SignoffState()
    const parsed = JSON.parse(raw) as MissionSignalPhase5SignoffState
    if (parsed.version !== MISSION_SIGNAL_PHASE5_VERSION) {
      return defaultMissionSignalPhase5SignoffState()
    }
    const merged = defaultMissionSignalPhase5SignoffState()
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
    return defaultMissionSignalPhase5SignoffState()
  }
}

export function saveMissionSignalPhase5SignoffState(state: MissionSignalPhase5SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
  } catch {
    // storage unavailable
  }
}

export function allMissionSignalPhase5ItemsVerified(state: MissionSignalPhase5SignoffState): boolean {
  return MISSION_SIGNAL_PHASE5_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function missionSignalPhase5VerificationCount(state: MissionSignalPhase5SignoffState): {
  verified: number
  total: number
} {
  const verified = MISSION_SIGNAL_PHASE5_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: MISSION_SIGNAL_PHASE5_DELIVERY_ITEMS.length }
}

export function isMissionSignalPhase5SignedOff(): boolean {
  return loadMissionSignalPhase5SignoffState().signedOffAt != null
}
