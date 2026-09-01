/**
 * Console seat map — one control plane, two seats (Engineer local vs Flight Director Prod).
 *
 * Local :5180 and in-cluster Prod Console observe the same k3s cluster.
 * The difference is seat: who sits, which posture lamps listen to Mac / git WIP.
 *
 * Cross-refs:
 * - DAILY_OPS_FLEET_DESK (agentProtocolCatalog.ts) — viewer_env + Mac informational
 * - fleetSnapshot.buildEngineerCell — dirty repos informational (not degradation)
 * - missionSignals.agentSignal — must match Fleet (dirty does not degrade ROOM POSTURE)
 */

import type { ConsoleNavPlane } from '@/lib/consoleNavConfig'

export const CONSOLE_SEAT_VERSION = '2026-08-31'
export const CONSOLE_SEAT_SOURCE = 'console/src/lib/architecture/consoleSeatCatalog.ts'

/** Where an operator should habitually sit for this page. Home ≠ hide — both seats keep all pages. */
export type ConsoleSeatHome = 'engineer' | 'fleet' | 'both'

export type ConsoleSeatId = 'engineer' | 'flight-director'

export type ConsoleSeatDef = {
  id: ConsoleSeatId
  label: string
  /** How the seat is selected at runtime. */
  how: string
  /** Question this seat answers. */
  answers: string
}

export type ConsoleSeatPageRow = {
  id: string
  label: string
  plane: ConsoleNavPlane
  home: ConsoleSeatHome
  /**
   * Whether signals from this surface feed Control Room ROOM POSTURE / Mission CAUTION.
   * Git dirty never feeds ROOM even when listed on Engineer pages.
   */
  roomPosture: boolean
  note?: string
}

export const CONSOLE_SEATS: ConsoleSeatDef[] = [
  {
    id: 'engineer',
    label: 'Engineer (local)',
    how: 'Local Console (:5180); no KUBERNETES_SERVICE_HOST → viewer DEV (OPS_VIEWER_ENV overrides)',
    answers: 'Can I act on the desk? Can I ship? What WIP is open?',
  },
  {
    id: 'flight-director',
    label: 'Flight Director (Prod)',
    how: 'In-cluster Console; clusters.yaml viewer_env=prod (OPS_VIEWER_ENV overrides)',
    answers: 'Is the fleet healthy? Can we Promote?',
  },
]

/**
 * Page home map — habitual seat, not nav gating.
 * roomPosture=true means fleet/cluster/release/operate signals from that surface
 * may contribute to ROOM; Engineer WIP (git dirty) never does.
 */
export const CONSOLE_SEAT_PAGES: ConsoleSeatPageRow[] = [
  // Seat — Mission Control
  {
    id: 'task-cc',
    label: 'Task Control Center',
    plane: 'Mission Control',
    home: 'both',
    roomPosture: true,
    note: 'Fleet GO/NO-GO is viewer-aware; Mac seat informational on Prod/STG',
  },
  {
    id: 'control-room',
    label: 'Control Room',
    plane: 'Mission Control',
    home: 'both',
    roomPosture: true,
    note: 'ROOM = cluster / payload / release / operate — not git dirty',
  },
  {
    id: 'observability',
    label: 'Observability',
    plane: 'Mission Control',
    home: 'fleet',
    roomPosture: false,
  },
  {
    id: 'defects',
    label: 'Defects',
    plane: 'Mission Control',
    home: 'both',
    roomPosture: false,
  },
  {
    id: 'audit',
    label: 'Audit',
    plane: 'Mission Control',
    home: 'both',
    roomPosture: false,
  },

  // Partner — Build / Launch / Ops / Analysis
  {
    id: 'briefing',
    label: 'Briefing',
    plane: 'Engineer',
    home: 'engineer',
    roomPosture: false,
  },
  {
    id: 'active-session',
    label: 'In Flight',
    plane: 'Engineer',
    home: 'engineer',
    roomPosture: false,
  },
  {
    id: 'delivery-board',
    label: 'Delivery',
    plane: 'Engineer',
    home: 'engineer',
    roomPosture: false,
  },
  {
    id: 'platform-release',
    label: 'Rocket (Launch)',
    plane: 'Rocket',
    home: 'engineer',
    roomPosture: true,
    note: 'Launch actuation lives on Engineer seat; release lamp feeds ROOM',
  },
  {
    id: 'trade-release',
    label: 'Satellite · Trade (Launch)',
    plane: 'Satellite',
    home: 'engineer',
    roomPosture: true,
  },
  {
    id: 'research-release',
    label: 'Satellite · Research (Launch)',
    plane: 'Research',
    home: 'engineer',
    roomPosture: true,
  },
  {
    id: 'plugin-release',
    label: 'Plugin (Launch)',
    plane: 'Plugin',
    home: 'engineer',
    roomPosture: true,
  },
  {
    id: 'agent-release',
    label: 'Agent (L-1 Launch)',
    plane: 'Engineer',
    home: 'engineer',
    roomPosture: false,
    note: 'Mac Mini publish — Engineer only',
  },
  {
    id: 'queue',
    label: 'Operate Queue',
    plane: 'Engineer',
    home: 'both',
    roomPosture: true,
    note: 'Open handoffs / pending briefs can CAUTION Operate bay',
  },
  {
    id: 'autonomous-skills',
    label: 'Patrol',
    plane: 'Engineer',
    home: 'both',
    roomPosture: false,
  },
  {
    id: 'execution-log',
    label: 'Patrol Log',
    plane: 'Engineer',
    home: 'both',
    roomPosture: false,
  },
  {
    id: 'operator-plane',
    label: 'Operator Plane',
    plane: 'Engineer',
    home: 'engineer',
    roomPosture: false,
  },
  {
    id: 'agent-governance',
    label: 'Trust & Autonomy',
    plane: 'Engineer',
    home: 'engineer',
    roomPosture: false,
  },
  {
    id: 'agent-capability',
    label: 'Agent Capability',
    plane: 'Engineer',
    home: 'engineer',
    roomPosture: false,
  },
  {
    id: 'analysis-workspace',
    label: 'Analysis Workspace',
    plane: 'Engineer',
    home: 'engineer',
    roomPosture: false,
  },
  {
    id: 'insight-log',
    label: 'Insight Log',
    plane: 'Engineer',
    home: 'engineer',
    roomPosture: false,
  },
  {
    id: 'hermes-status',
    label: 'Hermes Status',
    plane: 'Engineer',
    home: 'engineer',
    roomPosture: false,
  },

  // Mission groups
  {
    id: 'cluster',
    label: 'Cluster',
    plane: 'Rocket',
    home: 'fleet',
    roomPosture: true,
  },
  {
    id: 'rocket-health',
    label: 'Rocket Health',
    plane: 'Rocket',
    home: 'fleet',
    roomPosture: true,
  },
  {
    id: 'satellite-bus',
    label: 'Bus Status',
    plane: 'Satellite',
    home: 'fleet',
    roomPosture: true,
  },
  {
    id: 'satellite-health',
    label: 'Satellite Health',
    plane: 'Satellite',
    home: 'fleet',
    roomPosture: true,
  },
  {
    id: 'research-engine',
    label: 'Research Engine',
    plane: 'Research',
    home: 'both',
    roomPosture: false,
  },
  {
    id: 'plugin-gallery',
    label: 'Plugin Gallery',
    plane: 'Plugin',
    home: 'both',
    roomPosture: false,
  },
  {
    id: 'ib-gateway-manage',
    label: 'IB Client',
    plane: 'Plugin',
    home: 'both',
    roomPosture: false,
    note: 'Health observe → fleet; maintain / publish → engineer',
  },
  {
    id: 'market-data-manage',
    label: 'Massive',
    plane: 'Plugin',
    home: 'both',
    roomPosture: false,
  },
  {
    id: 'flex-query-manage',
    label: 'IB Flex',
    plane: 'Plugin',
    home: 'both',
    roomPosture: false,
  },
  {
    id: 'network',
    label: 'Network',
    plane: 'Plugin',
    home: 'fleet',
    roomPosture: false,
  },

  // Governance (Guides)
  {
    id: 'agent-protocol',
    label: 'Agent Protocol',
    plane: 'Governance',
    home: 'both',
    roomPosture: false,
  },
  {
    id: 'blueprint',
    label: 'Blueprint',
    plane: 'Governance',
    home: 'both',
    roomPosture: false,
  },
]

export const CONSOLE_SEAT_RULES = [
  'One Console, two seats — not two products. Changing URL does not change the cluster.',
  'Viewer seat: OPS_VIEWER_ENV > (in-cluster only) clusters.yaml viewer_env > dev.',
  'Mac seat is Engineer — not a fourth env column. Prod/STG viewer: Mac seat informational only.',
  'ROOM POSTURE / Mission CAUTION roll up: cluster degraded, payload unreachable, release blocked, Operate pending (queue / brief / live job), Bridge or Runner down.',
  'Git dirty (any seat) does NOT degrade ROOM POSTURE or Mission agentSignal — Owner WIP; keep as Engineer annotation + Propose commit (git-dirty-remediate).',
  'Control Room Mission path (missionSignals.agentSignal) must match Fleet buildEngineerCell: Bridge ok + dirty → ok signal with dirty in detail.',
  'CI/CD Launch Desk actuation prefers Engineer (local) seat — needs Mac git / Agent. Fleet health observe prefers Flight Director seat.',
  'Do not hide pages by seat in this contract — home is habitual guidance only.',
] as const

export const CONSOLE_SEAT_ACCEPTANCE = [
  'Q1: Local Console default viewer DEV; dirty repos alone → ROOM / Mission NOMINAL (detail may still say Bridge N dirty).',
  'Q2: Prod in-cluster viewer from yaml; Mac seat does not score Fleet NO-GO.',
  'Q3: Bridge unreachable still fails agentSignal / Engineer cell.',
  'Q4: Pending Operate brief or open queue can still CAUTION Operate bay without dirty.',
  'Q5: Agent Protocol → Seats section lists CONSOLE_SEAT_PAGES home + roomPosture.',
] as const

export function buildConsoleSeatLlmPack(): string {
  const lines: string[] = [
    '# Bifrost Ops — Console seat map (Engineer vs Flight Director)',
    `# Source: ${CONSOLE_SEAT_SOURCE} v${CONSOLE_SEAT_VERSION}`,
    '',
    '## Seats',
    ...CONSOLE_SEATS.map(s => `- **${s.label}** (${s.id}): ${s.how} — ${s.answers}`),
    '',
    '## Rules',
    ...CONSOLE_SEAT_RULES.map(r => `- ${r}`),
    '',
    '## Page home (habitual seat; pages remain available on both)',
    ...CONSOLE_SEAT_PAGES.map(
      p =>
        `- \`${p.id}\` ${p.label} [${p.plane}] home=${p.home} roomPosture=${p.roomPosture}${
          p.note != null ? ` — ${p.note}` : ''
        }`,
    ),
    '',
    '## Acceptance',
    ...CONSOLE_SEAT_ACCEPTANCE.map(a => `- ${a}`),
  ]
  return lines.join('\n')
}
