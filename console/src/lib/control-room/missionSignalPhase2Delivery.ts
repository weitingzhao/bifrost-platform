/** Mission Signal Program — Phase 2 (Agent Diagnostic Playbook) delivery checklist. */

export const MISSION_SIGNAL_PHASE2_VERSION = '2026-07-02'

export interface MissionSignalPhase2DeliveryItem {
  id: 'MSP2-1' | 'MSP2-2' | 'MSP2-3' | 'MSP2-4'
  title: string
  summary: string
  verifySteps: string[]
}

export const MISSION_SIGNAL_PHASE2_DELIVERY_ITEMS: MissionSignalPhase2DeliveryItem[] = [
  {
    id: 'MSP2-1',
    title: 'verify_payload MCP tool + API returns structured classification',
    summary:
      'GET /api/v1/mission/verify-payload compares matrix vs cluster per env; MCP verify_payload proxies the same route.',
    verifySteps: [
      'curl http://127.0.0.1:8780/api/v1/mission/verify-payload — JSON with environments[], summary.overall, per-env postgres/redis classification.',
      'MCP catalog lists verify_payload (GET /api/v1/mission/verify-payload).',
      'dev/prod show NOMINAL when matrix and cluster agree (post Phase 1).',
    ],
  },
  {
    id: 'MSP2-2',
    title: 'Dispatch pack includes classification guidance',
    summary:
      'Diagnose & Fix and Verify payload prefill include verify_payload table and per-env PROBE_DRIFT / DATA_LAYER guidance.',
    verifySteps: [
      'Control Room → Diagnose & Fix — prefill contains “Payload verification (verify_payload)” section.',
      'Prefill lists NOMINAL / PROBE_DRIFT / DATA_LAYER / HTTP_FAIL actions for Agent.',
      'Command intent “Verify payload” chip uses the same enriched dispatch pack.',
    ],
  },
  {
    id: 'MSP2-3',
    title: 'Agent Protocol documents PROBE_DRIFT and DATA_LAYER playbooks',
    summary:
      'Architecture → Agent Protocol includes mission diagnostic playbooks with autonomy levels (L0 diagnose, L1 data layer fix, L2 probe drift).',
    verifySteps: [
      'Open Architecture → Agent Protocol — “Mission diagnostic playbooks” section visible.',
      'PROBE_DRIFT: do not restart PG/Redis; escalate platform probe fix.',
      'DATA_LAYER: L1 confirm before CNPG/Redis remediation.',
    ],
  },
  {
    id: 'MSP2-4',
    title: 'Retrospective recognizes probe_drift root cause',
    summary:
      'Retrospective classifier emits probe_drift for svc.cluster.local / matrix-vs-cluster contradiction signals.',
    verifySteps: [
      'GET /api/v1/agent/retrospective/report — root_cause_distribution may include probe_drift when applicable.',
      'Defects page labels probe_drift in root cause legend.',
      'Classifier signals include error_probe_drift_dns for in-cluster DNS from Mac host.',
    ],
  },
]

export interface MissionSignalPhase2ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface MissionSignalPhase2SignoffState {
  version: string
  items: Record<string, MissionSignalPhase2ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_mission_signal_phase2_signoff'

function emptyItemState(): MissionSignalPhase2ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultMissionSignalPhase2SignoffState(): MissionSignalPhase2SignoffState {
  const items: Record<string, MissionSignalPhase2ItemVerification> = {}
  for (const item of MISSION_SIGNAL_PHASE2_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: MISSION_SIGNAL_PHASE2_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadMissionSignalPhase2SignoffState(): MissionSignalPhase2SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultMissionSignalPhase2SignoffState()
    const parsed = JSON.parse(raw) as MissionSignalPhase2SignoffState
    if (parsed.version !== MISSION_SIGNAL_PHASE2_VERSION) {
      return defaultMissionSignalPhase2SignoffState()
    }
    const merged = defaultMissionSignalPhase2SignoffState()
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
    return defaultMissionSignalPhase2SignoffState()
  }
}

export function saveMissionSignalPhase2SignoffState(state: MissionSignalPhase2SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
  } catch {
    // storage unavailable
  }
}

export function allMissionSignalPhase2ItemsVerified(state: MissionSignalPhase2SignoffState): boolean {
  return MISSION_SIGNAL_PHASE2_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function missionSignalPhase2VerificationCount(state: MissionSignalPhase2SignoffState): {
  verified: number
  total: number
} {
  const verified = MISSION_SIGNAL_PHASE2_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: MISSION_SIGNAL_PHASE2_DELIVERY_ITEMS.length }
}

export function isMissionSignalPhase2SignedOff(): boolean {
  return loadMissionSignalPhase2SignoffState().signedOffAt != null
}
