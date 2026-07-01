/** Mission Signal Program — Phase 1 (Signal Truth) delivery checklist. */

export const MISSION_SIGNAL_PHASE1_VERSION = '2026-07-01'

export interface MissionSignalPhase1DeliveryItem {
  id: 'MSP1-1' | 'MSP1-2' | 'MSP1-3' | 'MSP1-4' | 'MSP1-5'
  title: string
  summary: string
  verifySteps: string[]
}

export const MISSION_SIGNAL_PHASE1_DELIVERY_ITEMS: MissionSignalPhase1DeliveryItem[] = [
  {
    id: 'MSP1-1',
    title: 'Matrix datastore probes use cluster API (not in-cluster DNS)',
    summary:
      'platform-api on Mac resolves PG/Redis via cluster/postgres + cluster/redis signals; matrix target detail shows cluster_api (not TCP dial failed on *.svc.cluster.local).',
    verifySteps: [
      'Open Observe → Matrix — dev/stg/prod postgres and redis rows show reachability ok.',
      'Target detail includes cluster_api (e.g. database bifrost_dev or live=ok queue=ok).',
      'No TCP dial failed: lookup *.svc.cluster.local on postgres/redis targets.',
    ],
  },
  {
    id: 'MSP1-2',
    title: 'Mission Status no longer CRITICAL from false datastore fail',
    summary:
      'Control Room Mission Board aggregate reflects real payload health; datastore false negatives removed.',
    verifySteps: [
      'Open Control Room — Mission Status is NOMINAL or CAUTION (not CRITICAL solely from PG/Redis matrix fail).',
      'Mission header lamp matches worst real subsystem signal.',
    ],
  },
  {
    id: 'MSP1-3',
    title: 'Payload readiness PG / Redis green for dev + prod',
    summary: 'Dual Flywheel payload rows for datastore show ok when cluster APIs report healthy CNPG + Bitnami Redis.',
    verifySteps: [
      'Control Room → Payload (Trade) — dev PG/Redis cells green.',
      'Prod PG/Redis cells green when cluster targets ready.',
    ],
  },
  {
    id: 'MSP1-4',
    title: 'Coupling gate not blocked by postgres matrix false fail',
    summary:
      'Promote / cutover strip and coupling gate no longer list postgres matrix fail when cluster datastore is healthy.',
    verifySteps: [
      'Control Room → Promote / cutover — primary blocker is not “Prod matrix has failing targets” for postgres alone.',
      'Operate → Promote coupling gate matches Control Room when matrix datastore is ok.',
    ],
  },
  {
    id: 'MSP1-5',
    title: 'Diagnostic prompt quiet at NOMINAL',
    summary:
      'When Mission is NOMINAL, buildDiagnosticPrompt returns null — Agent is not paged for probe drift.',
    verifySteps: [
      'Mission NOMINAL — Diagnose & Fix / Agent dispatch does not prefill a CRITICAL datastore prompt.',
      'Focus strip shows nominal mission without false PG/Redis CRITICAL narrative.',
    ],
  },
]

export interface MissionSignalPhase1ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface MissionSignalPhase1SignoffState {
  version: string
  items: Record<string, MissionSignalPhase1ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_mission_signal_phase1_signoff'

function emptyItemState(): MissionSignalPhase1ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultMissionSignalPhase1SignoffState(): MissionSignalPhase1SignoffState {
  const items: Record<string, MissionSignalPhase1ItemVerification> = {}
  for (const item of MISSION_SIGNAL_PHASE1_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: MISSION_SIGNAL_PHASE1_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadMissionSignalPhase1SignoffState(): MissionSignalPhase1SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultMissionSignalPhase1SignoffState()
    const parsed = JSON.parse(raw) as MissionSignalPhase1SignoffState
    if (parsed.version !== MISSION_SIGNAL_PHASE1_VERSION) {
      return defaultMissionSignalPhase1SignoffState()
    }
    const merged = defaultMissionSignalPhase1SignoffState()
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
    return defaultMissionSignalPhase1SignoffState()
  }
}

export function saveMissionSignalPhase1SignoffState(state: MissionSignalPhase1SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
  } catch {
    // storage unavailable
  }
}

export function allMissionSignalPhase1ItemsVerified(state: MissionSignalPhase1SignoffState): boolean {
  return MISSION_SIGNAL_PHASE1_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function missionSignalPhase1VerificationCount(state: MissionSignalPhase1SignoffState): {
  verified: number
  total: number
} {
  const verified = MISSION_SIGNAL_PHASE1_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: MISSION_SIGNAL_PHASE1_DELIVERY_ITEMS.length }
}

export function isMissionSignalPhase1SignedOff(): boolean {
  return loadMissionSignalPhase1SignoffState().signedOffAt != null
}
