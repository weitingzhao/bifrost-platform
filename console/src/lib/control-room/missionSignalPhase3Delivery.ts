/** Mission Signal Program — Phase 3 (Autonomous Loop) delivery checklist. */

export const MISSION_SIGNAL_PHASE3_VERSION = '2026-07-02'

export interface MissionSignalPhase3DeliveryItem {
  id: 'MSP3-1' | 'MSP3-2' | 'MSP3-3' | 'MSP3-4'
  title: string
  summary: string
  verifySteps: string[]
}

export const MISSION_SIGNAL_PHASE3_DELIVERY_ITEMS: MissionSignalPhase3DeliveryItem[] = [
  {
    id: 'MSP3-1',
    title: 'verify_mission_snapshot API + MCP returns post_fix_verification',
    summary:
      'GET /api/v1/mission/verify-snapshot fresh-probes matrix + verify_payload and returns post_fix_verification.passed verdict.',
    verifySteps: [
      'curl http://127.0.0.1:8780/api/v1/mission/verify-snapshot — JSON with payload_verification, post_fix_verification.passed, trade_dev/prod signals.',
      'MCP catalog lists verify_mission_snapshot (implemented=true).',
      'post_fix_verification includes mission_matrix_nominal, datastore_verification_nominal, agent_guidance.',
    ],
  },
  {
    id: 'MSP3-2',
    title: 'Runner post_fix_verification after agent run',
    summary:
      'Remediation runner verifying phase calls verify-snapshot; job summary and events include post_fix_verification result.',
    verifySteps: [
      'Complete any Agent Desk remediation job — job log shows status event post_fix_verification: PASSED or NOT PASSED.',
      'Job summary appends Post-fix verification line.',
      'Runner custom tools verify_mission_snapshot and verify_payload available to agent mid-run.',
    ],
  },
  {
    id: 'MSP3-3',
    title: 'Control Room banner re-probes on job complete',
    summary:
      'useMissionVerification invalidates cockpit + fetches verify-snapshot; banner reflects post_fix pass/fail.',
    verifySteps: [
      'After Agent job finishes, Control Room shows “Refreshing mission probes…” then verify banner.',
      'Banner headline distinguishes Verified NOMINAL vs post-fix NOT passed.',
      'Detail includes verify_payload classification when verify-snapshot succeeds.',
    ],
  },
  {
    id: 'MSP3-4',
    title: 'Agent Protocol documents post-fix validation loop',
    summary:
      'Architecture → Agent Protocol includes Mission post-fix validation loop (verify_mission_snapshot required before close).',
    verifySteps: [
      'Agent Protocol page — “Mission post-fix validation loop” section with 3 steps.',
      'Agent task catalog includes Health · Post-fix (post-fix-verification scope).',
      'LLM pack mentions verify_mission_snapshot and runner post_fix_verification event.',
    ],
  },
]

export interface MissionSignalPhase3ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface MissionSignalPhase3SignoffState {
  version: string
  items: Record<string, MissionSignalPhase3ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_mission_signal_phase3_signoff'

function emptyItemState(): MissionSignalPhase3ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultMissionSignalPhase3SignoffState(): MissionSignalPhase3SignoffState {
  const items: Record<string, MissionSignalPhase3ItemVerification> = {}
  for (const item of MISSION_SIGNAL_PHASE3_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: MISSION_SIGNAL_PHASE3_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadMissionSignalPhase3SignoffState(): MissionSignalPhase3SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultMissionSignalPhase3SignoffState()
    const parsed = JSON.parse(raw) as MissionSignalPhase3SignoffState
    if (parsed.version !== MISSION_SIGNAL_PHASE3_VERSION) {
      return defaultMissionSignalPhase3SignoffState()
    }
    const merged = defaultMissionSignalPhase3SignoffState()
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
    return defaultMissionSignalPhase3SignoffState()
  }
}

export function saveMissionSignalPhase3SignoffState(state: MissionSignalPhase3SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
  } catch {
    // storage unavailable
  }
}

export function allMissionSignalPhase3ItemsVerified(state: MissionSignalPhase3SignoffState): boolean {
  return MISSION_SIGNAL_PHASE3_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function missionSignalPhase3VerificationCount(state: MissionSignalPhase3SignoffState): {
  verified: number
  total: number
} {
  const verified = MISSION_SIGNAL_PHASE3_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: MISSION_SIGNAL_PHASE3_DELIVERY_ITEMS.length }
}

export function isMissionSignalPhase3SignedOff(): boolean {
  return loadMissionSignalPhase3SignoffState().signedOffAt != null
}
