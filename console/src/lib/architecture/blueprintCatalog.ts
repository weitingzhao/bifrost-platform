/**
 * Blueprint catalog — North Star, system architecture, design principles.
 *
 * Authoritative source for Ops Console → Architecture → Blueprint.
 * Single source of truth — do not duplicate elsewhere.
 */

import type { OpsContextResponse } from '@/api/types'
import {
  SPINE_MILESTONE_STATUS_DEFINITIONS,
  SPINE_STATUS_SEMANTICS_NOTE,
} from '@/lib/architecture/spineSemantics'

export const BLUEPRINT_VERSION = '2026-07-01'
export const BLUEPRINT_SOURCE = 'console/src/lib/architecture/blueprintCatalog.ts'

/** Slow-changing principles — North Star, design rules, forbidden actions. */
export const GOVERNANCE_LAYER_CONSTITUTION = 'Constitution' as const
/** Owner sign-off milestones — ops-context.yaml via GET /api/v1/context. */
export const GOVERNANCE_LAYER_SPINE = 'Spine' as const
/** Live capability — platform-api routes, MCP tools, matrix/gate verdicts. */
export const GOVERNANCE_LAYER_PROJECTION = 'Projection' as const

export type GovernanceLayerRow = {
  layer: typeof GOVERNANCE_LAYER_CONSTITUTION | typeof GOVERNANCE_LAYER_SPINE | typeof GOVERNANCE_LAYER_PROJECTION
  changeRate: string
  authority: string
  content: string
}

export const GOVERNANCE_LAYERS: GovernanceLayerRow[] = [
  {
    layer: GOVERNANCE_LAYER_CONSTITUTION,
    changeRate: 'Slow (months / Owner principle changes)',
    authority: 'Blueprint + Agent Protocol catalogs',
    content: 'North Star, Strategy C, design principles, L0/L1/L2/forbidden, AI boundaries',
  },
  {
    layer: GOVERNANCE_LAYER_SPINE,
    changeRate: 'Medium (milestone sign-off)',
    authority: 'config/ops-context.yaml → GET /api/v1/context',
    content: 'Milestones, decisions, focus, streams — SIGNED = historical Owner approval, not live gate ready',
  },
  {
    layer: GOVERNANCE_LAYER_PROJECTION,
    changeRate: 'Fast (PR / deploy)',
    authority: 'platform-api + GET /api/v1/mcp/tools',
    content: 'Implemented routes, MCP tools, matrix/gate verdicts, UI delivery sign-offs',
  },
]

export type BoundaryRuleRow = {
  question: string
  answerLayer: typeof GOVERNANCE_LAYER_CONSTITUTION | typeof GOVERNANCE_LAYER_SPINE | typeof GOVERNANCE_LAYER_PROJECTION
}

export const BOUNDARY_RULES: BoundaryRuleRow[] = [
  { question: 'Can we do X / is X implemented?', answerLayer: GOVERNANCE_LAYER_PROJECTION },
  { question: 'Should we do X / what is forbidden?', answerLayer: GOVERNANCE_LAYER_CONSTITUTION },
  { question: 'Was milestone M historically signed off?', answerLayer: GOVERNANCE_LAYER_SPINE },
  { question: 'Is Promote / cutover ready right now?', answerLayer: GOVERNANCE_LAYER_PROJECTION },
]

export const NORTH_STAR_STATEMENT =
  'All routine environment, cluster, release, and ops actions go through Bifrost Ops Console and platform-api; infra scripts run only as API executors. The Owner\'s only out-of-band action is restarting the Ops Platform itself.'

export const NORTH_STAR_STRATEGY = 'C — Hybrid control plane'
export const NORTH_STAR_DECISION = 'D6'

export type OwnerException = {
  allowed: string
  forbidden: string
}

export const OWNER_EXCEPTIONS: OwnerException[] = [
  {
    allowed: 'Start / restart bifrost-platform (make start, upgrade control plane)',
    forbidden: 'kubectl, ssh, make k3s-*, manual cluster changes, manual release_gate.sh',
  },
  {
    allowed: 'First-time install Go/Node, clone repo (cold start)',
    forbidden: 'Daily probes, releases, node join, Pod restart, Argo sync',
  },
  {
    allowed: 'Edit ops-context.yaml / Goal (Owner strategic changes)',
    forbidden: 'Bypass platform-api to invoke Trade write paths',
  },
]

export type StrategyCLayer = {
  layer: string
  responsibility: string
}

export const STRATEGY_C_LAYERS: StrategyCLayer[] = [
  {
    layer: 'Ops Console',
    responsibility: 'Unified navigation, confirmation flows, audit display, Copy-for-LLM / Agent packs',
  },
  {
    layer: 'platform-api',
    responsibility: 'Auth L0/L1/L2, job queue, audit log, no arbitrary shell',
  },
  {
    layer: 'Mature components',
    responsibility: 'Argo CD, Tekton, Headlamp/Rancher — wrapped via API or deep-linked, not replacing control plane',
  },
  {
    layer: 'Infra scripts',
    responsibility: 'install-server.sh, fetch-kubeconfig.sh, etc. — executor implementation only, operators do not run manually',
  },
  {
    layer: 'Out-of-band Operator Plane (L-1)',
    responsibility: 'AI Agent runners (dual Mac Mini, outside K8s) + mutual watchdog — automate the Owner out-of-band action: recover the platform/cluster when the single pane itself is down. Fate-isolated; see K3s Bootstrap L-1.',
  },
]

export type DesignPrinciple = {
  id: number
  title: string
  description: string
}

export const DESIGN_PRINCIPLES: DesignPrinciple[] = [
  { id: 1, title: 'Single pane', description: 'Interaction entry is only Ops Console + GET/POST /api/v1/* (future MCP same contract).' },
  { id: 2, title: 'Scripts are implementation', description: 'Shell/Makefile in repo are API call implementation details, not operations manual.' },
  { id: 3, title: 'Graduated actuation', description: 'L0 diagnose → L1 safe retry → L2 Owner confirm; all leave audit trail.' },
  { id: 4, title: 'LLM-ready context', description: 'Every operation produces structured action/target/status/detail, feedable into spine and Agent packs.' },
  { id: 5, title: 'Forbidden unchanged', description: 'daemon_control write, ib:operator:cmd, R-DV3 auto-order bypass — never exposed to platform AI.' },
  { id: 6, title: 'Out-of-band recovery never shares fate', description: 'The Agent that recovers the platform/cluster (L-1 Operator Plane) runs OUTSIDE K8s on dual Mac Minis with a mutual watchdog; it must never be scheduled into the cluster it recovers. The engineer stands on the ground, not inside the rocket.' },
  { id: 7, title: 'Earned autonomy over granted trust', description: 'Agent Skills start at L1 (confirm); consecutive successes earn L0 (auto); failure spikes trigger demotion back to L1. Owner governs via policy, not per-action approval — Flight Director model.' },
]

export type AgentLayeringRecord = {
  layer: string
  substrate: string
  lifecycle: string
  extractionTriggers?: string[]
}

export const AGENT_LAYERING: AgentLayeringRecord[] = [
  {
    layer: 'L-1 Out-of-Band (Operator Plane)',
    substrate: 'Dual Mac Minis (.50 primary / .52 standby) · launchd · mutual watchdog',
    lifecycle: 'Monorepo-first (bifrost-platform/agent/). Versioned independently via package.json; deployed per-Mini with standby-first canary + post-deploy smoke. Stays on bare Mac — never scheduled into K8s.',
  },
  {
    layer: 'L0–L2 In-Band (future rich capability)',
    substrate: 'May run inside K3s (sidecar, CronJob) for deeper cluster integration',
    lifecycle: 'TBD — will share fate with cluster; limited to non-recovery tasks (observability, routine maintenance)',
    extractionTriggers: [
      'Drift scanner reads platform catalogs via API (not filesystem)',
      'tools↔platform-api contract is stable (≥2 months without breaking change)',
      'Agent has independent release cadence / Owner from platform',
      'Agent serves Trade payload (not just Ops Platform)',
      'Supply-chain isolation required (separate CI, audit, compliance)',
    ],
  },
]

export type ConsoleViewRow = {
  view: string
  plane: string
  purpose: string
}

export const CONSOLE_VIEWS: ConsoleViewRow[] = [
  { view: 'Agent Desk', plane: 'Agent', purpose: 'Engineer workspace — Ops + Trade payload actor' },
  { view: 'Agent Briefing', plane: 'Agent', purpose: 'New-session entry — work intent, progress, briefing pack' },
  { view: 'Agent Protocol', plane: 'Agent', purpose: 'Agent doctrine — modes, architecture, forbidden actions' },
  { view: 'Briefing Reconciliation', plane: 'Agent', purpose: 'Spine projection rules and drift reconciliation' },
  { view: 'MCP Contract', plane: 'Agent', purpose: 'Agent tool contract — read / routine / confirm / forbidden' },
  { view: 'Skills & Schedules', plane: 'Agent', purpose: 'Autonomous skill registry and triggers' },
  { view: 'Execution Log', plane: 'Agent', purpose: 'Autonomous execution history' },
  { view: 'Trust & Autonomy', plane: 'Agent', purpose: 'Earned autonomy KPIs and trust matrix' },
  { view: 'Operator Plane (L-1)', plane: 'Agent', purpose: 'Out-of-band runner infrastructure (fate-isolated)' },
  { view: 'Control Room', plane: 'Observe', purpose: 'Mission diagnosis — KPIs, matrix, flywheels, commander cockpit' },
  { view: 'Delivery', plane: 'Operate', purpose: 'CI/CD pipelines and release coupling' },
  { view: 'Runtime Map', plane: 'Observe', purpose: 'Topology-first runtime diagnosis' },
  { view: 'Placement', plane: 'Observe', purpose: 'Workload placement policy and violations' },
  { view: 'Cluster', plane: 'Operate', purpose: 'Cluster operations' },
  { view: 'Audit', plane: 'Observe', purpose: 'Platform actuation audit history' },
  { view: 'Milestones', plane: 'Architecture', purpose: 'Spine milestones, decisions, focus' },
  { view: 'Promote', plane: 'Operate', purpose: 'Release readiness (flywheels A + B)' },
  { view: 'Deploy Mainline', plane: 'Operate', purpose: 'Deployment decision chain' },
  { view: 'Flywheel Vision', plane: 'Architecture', purpose: 'Three-layer Agent convergence vision' },
  { view: 'Architecture catalogs', plane: 'Architecture', purpose: 'Governance catalogs and Copy Prompt' },
  { view: 'Server Console', plane: 'Operate', purpose: 'Remote server console (Tools)' },
]

export type AuthorizationLevel = {
  level: string
  behavior: string
}

export const BLUEPRINT_AUTHORIZATION_LEVELS: AuthorizationLevel[] = [
  { level: 'L0', behavior: 'Read-only probes (matrix, topology, cluster, logs)' },
  { level: 'L1', behavior: 'Safe actuation via platform-api (rollout restart, scale, sync — audited)' },
  { level: 'L2', behavior: 'Owner-confirmed changes (node join, stack install, Argo rollback)' },
  { level: 'forbidden', behavior: 'daemon_control write · ib:operator:cmd · R-DV3 auto-trade bypass' },
]

export type SuccessCriterion = {
  area: string
  criterion: string
}

/** North Star completion conditions (Constitution) — not current implementation progress. */
export const SUCCESS_CRITERIA: SuccessCriterion[] = [
  { area: 'Cluster', criterion: 'Node join/drain, namespace, workload restart/scale/logs — UI/API only' },
  { area: 'Delivery', criterion: 'Tekton run, Argo sync/rollback — UI/API only' },
  { area: 'Promote', criterion: 'release_gate trigger and results — UI/API only' },
  {
    area: 'Runtime',
    criterion:
      'Runtime Map, Control Room, and Operate views form a closed Observe→Act loop with deep-links; live readiness is Projection (matrix/gate)',
  },
  { area: 'Spine', criterion: 'GET /api/v1/context + Program page always shows north star' },
  { area: 'MCP', criterion: 'MCP Tools and UI — same permissions, same audit (AI Agent self-interaction loop)' },
]

export type ActuationPhaseRow = {
  phase: string
  deliverables: string
  eliminates: string
}

// ---------------------------------------------------------------------------
// AI Native Ops Platform — integrated from Goal/AI_NATIVE_OPS_PLATFORM.md
// ---------------------------------------------------------------------------

export const AI_PLATFORM_MISSION =
  'Build an AI-native, self-discovering, self-maintaining, self-healing release and operations environment. ' +
  'Bifrost Trade workloads (frontend, API, Worker, Socket) evolve safely, observably, and rollback-ready on this platform. ' +
  'Two downstream product lines share this unified foundation: (1) page continuous refactoring (Dense UI / frontend migration); ' +
  '(2) trade review AI (read-only analysis, isolated from trade execution path). ' +
  'Ultimate convergence target: see Architecture → Flywheel Vision (dualFlywheelVisionCatalog.ts) — ' +
  'three-layer Agent (Dev / Ops / Business) unifying code, operations, and trade intelligence in one Cursor window.'

export const AI_MERGE_RATIONALE =
  'Splitting into two projects causes duplicate MCP, context, and gates. Merged: one platform, one Tool contract, one release mainline.'

export type AiCapability = { name: string; description: string; examples: string[] }

export const AI_PLATFORM_CAPABILITIES: AiCapability[] = [
  {
    name: 'Discovery',
    description: 'System auto-exposes topology and state understandable by Agent and humans — no manual port tables or SSH log checking.',
    examples: [
      'Service inventory from K8s API / Compose labels → unified list',
      'Health & dependencies from Monitor + Ops + Socket health Redis',
      'Config & versions from Git tag, image digest, ArgoCD sync status',
    ],
  },
  {
    name: 'Maintenance',
    description: 'Daily changes default to automation; humans handle policy and exceptions only.',
    examples: [
      'Build & test via Tekton Pipeline (lint / pytest / npm build)',
      'Release via ArgoCD GitOps; release_gate.sh aggregates prod-health',
      'Config drift detection via ArgoCD diff + periodic make prod-health',
      'Agent Briefing reconcile gate — briefingReconciliationCatalog.ts (queue ≟ spine ≟ appendix)',
    ],
  },
  {
    name: 'Repair',
    description: 'AI and rule engine attempt recovery within permission boundaries, not just alerting.',
    examples: [
      'L0 read-only: diagnose, root cause summary, Runbook link',
      'L1 safe retry: retry-failed, restart Celery worker instance via Ops API',
      'L2 controlled change: ArgoCD rollback, scaling — requires Owner confirmation',
      'Forbidden: LLM direct to trade — daemon_control write, ib:operator:cmd, R-DV3 violation',
    ],
  },
]

export type AiPlatformPhase = { id: string; sequence: string; deliverables: string; businessUnlock: string }

export const AI_PLATFORM_PHASES: AiPlatformPhase[] = [
  {
    id: 'A — Gates',
    sequence: 'First',
    deliverables: 'release_gate.sh, Mac Mini CI, MkDocs+Goal, 2C-B Prod',
    businessUnlock: 'Page refactoring continues; trade review AI offline trial (4090 Ollama)',
  },
  {
    id: 'B — GitOps',
    sequence: 'Second',
    deliverables: 'K3s + Gitea + Tekton + ArgoCD + k8s/base/',
    businessUnlock: 'Frontend Staging on K8s; review index CronJob',
  },
  {
    id: 'C — Closed loop',
    sequence: 'Third',
    deliverables: 'Prometheus/Loki/Grafana + bifrost-ops-mcp + AlertManager',
    businessUnlock: 'Ops Copilot production-ready; trade review RAG via Open-WebUI',
  },
]

export type AiSuccessCriterion = { area: string; criterion: string }

export const AI_PLATFORM_SUCCESS: AiSuccessCriterion[] = [
  { area: 'Discovery', criterion: 'One command or MCP call returns current Prod service list + health + version' },
  { area: 'Release', criterion: 'tag → Pipeline → image → ArgoCD sync → prod-health all-green (no manual SSH compose up)' },
  { area: 'Maintenance', criterion: 'Config drift detectable; docs (Goal + Migration + Sign-off) trackable against runtime' },
  { area: 'Repair', criterion: 'L0/L1 scenarios (Celery pending, Socket yellow) have Runbook + optional AI summary; L2 needs confirmation' },
  { area: 'Isolation', criterion: 'Trade review AI and ops Agent cannot trigger daemon_control or IB Operator RPC' },
  { area: 'Page refactoring', criterion: 'Each migrated page reaches Staging after CI gate; Owner sign-off chain complete' },
  { area: 'Trade review AI', criterion: 'At least one daily review report (positions + trades + PnL) generated locally; data source read-only and auditable' },
]

export type AiBoundary = { rule: string; detail: string }

export const AI_PLATFORM_BOUNDARIES: AiBoundary[] = [
  { rule: 'R-DV3', detail: 'One auto-trade Engine per IB account; Dev/Prod separate client_id' },
  { rule: 'Trade write path', detail: 'Only daemon → ib:operator:cmd; AI read-only or via verified Ops API' },
  { rule: 'TWS', detail: 'Win11 dedicated machine, never scheduled into K3s' },
  { rule: 'Phase 1 constraint', detail: 'While frontend points at Legacy API, platform must not mix "API migration" and "release" into one change (single-variable principle)' },
]

/** Actuation phase definitions (Constitution) — which phase means what; live progress is Projection. */
export const ACTUATION_PHASES: ActuationPhaseRow[] = [
  { phase: 'P0', deliverables: 'Cluster L0 probes, Delivery dual track display', eliminates: 'Observation only' },
  { phase: 'P1', deliverables: 'Auth + audit + workload L1 + logs', eliminates: 'Daily kubectl' },
  { phase: 'P2', deliverables: 'Node lifecycle job + Cluster UI wizard', eliminates: 'install-server.sh, join, drain' },
  { phase: 'P3', deliverables: 'GitOps + CI execution (Argo/Tekton API)', eliminates: 'Argo UI, tkn CLI' },
  { phase: 'P4', deliverables: 'Platform stack install wizard', eliminates: 'Manual Helm install' },
  { phase: 'P5', deliverables: 'MCP actuation Tools', eliminates: 'Agent direct shell' },
]

export type BlueprintLlmPackOptions = {
  spine?: OpsContextResponse
  projectionPack?: string
}

/** Constitution-only LLM pack section. */
export function buildBlueprintConstitutionPack(): string {
  const lines: string[] = [
    '## Constitution (slow-changing principles)',
    '',
    '### Governance layers',
    ...GOVERNANCE_LAYERS.map(
      l => `- **${l.layer}** (${l.changeRate}): ${l.content} — authority: ${l.authority}`,
    ),
    '',
    '### Boundary rules',
    ...BOUNDARY_RULES.map(r => `- ${r.question} → **${r.answerLayer}**`),
    '',
    '## North Star',
    `Strategy: ${NORTH_STAR_STRATEGY} (decision ${NORTH_STAR_DECISION})`,
    NORTH_STAR_STATEMENT,
    '',
    '### Owner exceptions',
    ...OWNER_EXCEPTIONS.map(e => `- Allowed: ${e.allowed}\n  Forbidden: ${e.forbidden}`),
    '',
    '### Strategy C layers',
    ...STRATEGY_C_LAYERS.map(l => `- **${l.layer}**: ${l.responsibility}`),
    '',
    '## Design principles',
    ...DESIGN_PRINCIPLES.map(p => `${p.id}. **${p.title}** — ${p.description}`),
    '',
    '## Console views',
    ...CONSOLE_VIEWS.map(v => `- **${v.view}** [${v.plane}]: ${v.purpose}`),
    '',
    '## Authorization levels',
    ...BLUEPRINT_AUTHORIZATION_LEVELS.map(a => `- **${a.level}**: ${a.behavior}`),
    '',
    '## Success criteria (Constitution — North Star completion)',
    ...SUCCESS_CRITERIA.map(s => `- [${s.area}] ${s.criterion}`),
    '',
    '## Actuation phases (Constitution definitions P0–P5)',
    ...ACTUATION_PHASES.map(p => `- **${p.phase}**: ${p.deliverables} → eliminates: ${p.eliminates}`),
    '',
    '## AI Native Ops Platform — Mission',
    AI_PLATFORM_MISSION,
    AI_MERGE_RATIONALE,
    '',
    '## AI Platform capabilities',
    ...AI_PLATFORM_CAPABILITIES.flatMap(c => [
      `### ${c.name}`,
      c.description,
      ...c.examples.map(e => `- ${e}`),
    ]),
    '',
    '## AI Platform phases (sequence — no calendar time boxes)',
    ...AI_PLATFORM_PHASES.map(
      p => `- **${p.id}** (${p.sequence}): ${p.deliverables} → unlocks: ${p.businessUnlock}`,
    ),
    '',
    '## AI Platform success criteria',
    ...AI_PLATFORM_SUCCESS.map(s => `- [${s.area}] ${s.criterion}`),
    '',
    '## AI Platform boundaries',
    ...AI_PLATFORM_BOUNDARIES.map(b => `- **${b.rule}**: ${b.detail}`),
  ]
  return lines.join('\n')
}

/** Spine-only LLM pack section (live sign-off state + status definitions). */
export function buildBlueprintSpinePack(spine: OpsContextResponse): string {
  const lines: string[] = [
    '## Spine (live sign-off state — medium-changing)',
    '',
    SPINE_STATUS_SEMANTICS_NOTE,
    '',
    '### Milestone status definitions',
    ...SPINE_MILESTONE_STATUS_DEFINITIONS.map(
      row => `- **${row.status}**: ${row.meaning}`,
    ),
    '',
    '### Live snapshot (GET /api/v1/context)',
    `- phase: ${spine.deployment.phase}`,
    `- active_track: ${spine.deployment.active_track}`,
    `- focus: ${spine.focus.headline}`,
    '- Note: milestone SIGNED = Owner historical sign-off; live gate readiness from Projection (matrix/promote).',
  ]
  return lines.join('\n')
}

/** Build LLM-optimized text for the Blueprint page (Constitution → Spine → Projection). */
export function buildBlueprintLlmPack(options?: OpsContextResponse | BlueprintLlmPackOptions): string {
  const opts: BlueprintLlmPackOptions =
    options != null && 'deployment' in options
      ? { spine: options }
      : (options ?? {})
  const { spine, projectionPack } = opts

  const lines: string[] = [
    '# Bifrost Ops — Blueprint (Architecture & North Star)',
    `# Source: ${BLUEPRINT_SOURCE} v${BLUEPRINT_VERSION}`,
    '',
    buildBlueprintConstitutionPack(),
  ]

  if (spine != null) {
    lines.push('', buildBlueprintSpinePack(spine))
  }

  if (projectionPack != null && projectionPack.trim() !== '') {
    lines.push('', projectionPack)
  }

  return lines.join('\n')
}
