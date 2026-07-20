/**
 * Mission Signal program — Delivery Board phase catalog (Wave 4b).
 * Authoritative playbooks: agentProtocolCatalog.ts (P2–P7 sections).
 */

export type MissionSignalPhaseId = 'P1' | 'P2' | 'P3' | 'P4' | 'P5' | 'P6' | 'P7'

export type MissionSignalPhaseDef = {
  id: MissionSignalPhaseId
  title: string
  summary: string
  verifyApi?: string
  mcpTools?: string[]
  acceptance: string[]
  dependsOn?: MissionSignalPhaseId[]
}

export const MISSION_SIGNAL_PROGRAM_ID = 'mission-signal'

export const MISSION_SIGNAL_PHASES: MissionSignalPhaseDef[] = [
  {
    id: 'P1',
    title: 'Signal Truth',
    summary: 'Control Room cockpit — live mission / rocket / payload signals from matrix + cluster probes.',
    verifyApi: 'GET /api/v1/matrix + cockpit probes (useMissionSnapshot)',
    acceptance: [
      'Mission Control header shows Mission / Rocket / Payload status',
      'Rocket subsystems grid (infra, release, control, agent) reachable',
      'Trade dev + prod matrix targets probed',
    ],
  },
  {
    id: 'P2',
    title: 'Diagnostic playbooks',
    summary: 'Classify failures with verify_payload before remediating datastore or matrix targets.',
    verifyApi: 'GET /api/v1/mission/verify-payload',
    mcpTools: ['verify_payload', 'get_connectivity_matrix'],
    dependsOn: ['P1'],
    acceptance: [
      'NOMINAL / PROBE_DRIFT / DATA_LAYER / HTTP_FAIL playbooks visible in Agent Protocol',
      'verify_payload returns per-env postgres/redis classification',
      'Command intent strip cites verify_payload before Diagnose & Fix',
    ],
  },
  {
    id: 'P3',
    title: 'Post-fix validation',
    summary: 'Autonomous remediation must re-probe verify_mission_snapshot before closing jobs.',
    verifyApi: 'GET /api/v1/mission/verify-snapshot',
    mcpTools: ['verify_mission_snapshot'],
    dependsOn: ['P2'],
    acceptance: [
      'Mission verify banner after Agent job completion',
      'post_fix_verification.passed surfaced before job close',
      'MISSION_POST_FIX_LOOP documented in Agent Protocol',
    ],
  },
  {
    id: 'P4',
    title: 'Hermes first task',
    summary: 'L0 read-only onboarding — Hermes readiness + first mission-health task.',
    verifyApi: 'GET /api/v1/agent/hermes/readiness',
    mcpTools: ['get_hermes_readiness', 'get_hermes_first_task', 'verify_mission_snapshot'],
    dependsOn: ['P3'],
    acceptance: [
      'Hermes readiness gate (ready=true, blockers empty)',
      'First task prompt references verify_mission_snapshot + matrix',
      'L0 report only — no actuation on first task',
    ],
  },
  {
    id: 'P5',
    title: 'Flight Director governance',
    summary: 'Owner manages Agent team — performance KPIs, trust matrix, capability map.',
    verifyApi: 'GET /api/v1/agent/governance/snapshot',
    mcpTools: ['get_agent_performance', 'get_trust_matrix', 'get_flight_director_snapshot'],
    dependsOn: ['P4'],
    acceptance: [
      'Trust & Autonomy page shows per-skill L0/L1/L2',
      'Flight Director snapshot includes performance + capability map',
      '24h briefing digest replaces manual Audit scanning',
    ],
  },
  {
    id: 'P6',
    title: 'Flight Director ops',
    summary: 'Daily ops — digest review + trust overrides actuation.',
    verifyApi: 'GET /api/v1/agent/governance/trust-matrix',
    mcpTools: ['get_flight_director_snapshot', 'get_trust_matrix'],
    dependsOn: ['P5'],
    acceptance: [
      'Agent Briefing Flight Director 24h digest panel',
      'PUT trust-overrides/{skill_id} documented and reachable (admin)',
      'Trust matrix reflects owner_overrides after actuation',
    ],
  },
  {
    id: 'P7',
    title: 'Program closure',
    summary: 'Maintenance mode — P1–P6 signed via Briefing Session (Board catalog); event-driven patches only.',
    dependsOn: ['P1', 'P2', 'P3', 'P4', 'P5', 'P6'],
    acceptance: [
      'All phases P1–P6 signed via programs API',
      'Mission Signal strip shows program complete',
      'New signal work scoped as patches, not new program phases',
    ],
  },
]

export function missionSignalPhase(id: MissionSignalPhaseId): MissionSignalPhaseDef | undefined {
  return MISSION_SIGNAL_PHASES.find(p => p.id === id)
}
