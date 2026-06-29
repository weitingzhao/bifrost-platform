/**
 * Blueprint catalog — North Star, system architecture, design principles.
 *
 * Authoritative source for Ops Console → Architecture → Blueprint.
 * Single source of truth — do not duplicate elsewhere.
 */

import type { OpsContextResponse } from '@/api/types'

export const BLUEPRINT_VERSION = '2026-06-28'
export const BLUEPRINT_SOURCE = 'console/src/lib/architecture/blueprintCatalog.ts'

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
  { view: 'Agent Desk', plane: 'Agent', purpose: 'Engineer workspace — composer + run history; the actor that services rocket (Ops) + payload (Trade)' },
  { view: 'Agent Briefing', plane: 'Agent', purpose: 'New-session entry — work-intent picker, UI progress, live snapshot, full LLM briefing pack' },
  { view: 'Agent Protocol', plane: 'Agent', purpose: 'Doctrine — interaction modes, three-layer architecture, context pack layers, forbidden actions' },
  { view: 'MCP Contract', plane: 'Agent', purpose: 'Doctrine — Agent tool contract (read / routine / confirm / forbidden) mirrored UI + MCP' },
  { view: 'Skills & Schedules', plane: 'Agent', purpose: 'Autonomous — Hermes Gateway registered skills, cron/webhook/manual triggers, per-skill actuation level (L0/L1/L2)' },
  { view: 'Execution Log', plane: 'Agent', purpose: 'Autonomous — Hermes execution history; trigger, result, duration, summary for all autonomous and dispatched runs' },
  { view: 'Trust & Autonomy', plane: 'Agent', purpose: 'Governance — Flight Director performance KPIs (success rate, MTTR, intervention rate), earned autonomy trust matrix' },
  { view: 'Operator Plane (L-1)', plane: 'Agent', purpose: 'Infrastructure — Runner + Hermes Gateway heartbeats, dual Mac Mini deploy, watchdog; fate-isolated (D7)' },
  { view: 'Control Room', plane: 'Observe', purpose: 'Diagnosis step 1 — live KPI + matrix summary; deep-link to Runtime Map; dual flywheel + Agent focus dock' },
  { view: 'Delivery', plane: 'Operate', purpose: 'CI/CD actuation — Operate / Observe / Blueprint tabs; coupling gate summary' },
  { view: 'Runtime Map', plane: 'Observe', purpose: 'Diagnosis step 2 — topology-first hardware + SCOPE stack, per-target drawer, gap analysis, runtime-scoped Agent pack' },
  { view: 'Placement', plane: 'Observe', purpose: 'Scheduling — node pools, workload placement policy matrix, violations; CI preflight for Delivery; workloadPlacementCatalog live evaluation' },
  { view: 'Cluster', plane: 'Operate', purpose: 'Cluster ops — K3s L0 probe + L1 namespace ensure; nodes, namespaces, workloads via platform-api' },
  { view: 'Audit', plane: 'Observe', purpose: 'Session & audit — platform-api actuation history (Delivery, Cluster, GitOps, namespace ensure)' },
  { view: 'Milestones', plane: 'Architecture', purpose: 'Milestones, decisions D1–Dn, north star, roadmap (ops-context spine)' },
  { view: 'Promote', plane: 'Operate', purpose: 'Read-only release readiness (flywheel A + B)' },
  { view: 'Deploy Mainline', plane: 'Operate', purpose: 'Local Prod Final → K3s → Compose → Legacy retirement — deployment decision chain' },
  { view: 'Flywheel Vision', plane: 'Architecture', purpose: 'Ultimate North Star — three-layer Agent convergence (Dev / Ops / Business), Redis topology, Dev thin + K3s thick, MCP bridges' },
  { view: 'Architecture catalogs', plane: 'Architecture', purpose: 'Blueprint, Environments, Platform Roadmap, K3s Architecture, K3s Bootstrap, Standards — Copy Prompt for LLM' },
  { view: 'Server Console', plane: 'Operate', purpose: 'SSH/WebSocket server console (Tools)' },
]

export type AuthorizationLevel = {
  level: string
  behavior: string
}

export const BLUEPRINT_AUTHORIZATION_LEVELS: AuthorizationLevel[] = [
  { level: 'L0', behavior: 'Read-only probes (matrix, topology, cluster, logs)' },
  { level: 'L1', behavior: 'Safe actuation via platform-api (rollout restart, scale, sync — north star P1)' },
  { level: 'L2', behavior: 'Owner-confirmed changes (node join, stack install, Argo rollback — north star P2+)' },
  { level: 'forbidden', behavior: 'daemon_control write · ib:operator:cmd · R-DV3 auto-trade bypass' },
]

export type ApiEndpointRow = {
  method: string
  path: string
  description: string
}

export const PLATFORM_API_ENDPOINTS: ApiEndpointRow[] = [
  { method: 'GET', path: '/health', description: 'Platform API health' },
  { method: 'GET', path: '/api/v1/context', description: 'Ops spine (milestones, decisions, focus)' },
  { method: 'GET', path: '/api/v1/matrix', description: 'Connectivity matrix' },
  { method: 'GET', path: '/api/v1/topology', description: 'Network topology + live status' },
  { method: 'GET', path: '/api/v1/cluster', description: 'K3s cluster summary' },
  { method: 'GET', path: '/api/v1/cluster/nodes', description: 'Node list' },
  { method: 'GET', path: '/api/v1/cluster/namespaces', description: 'Namespaces (?watch=bifrost)' },
  { method: 'GET', path: '/api/v1/cluster/workloads?ns=', description: 'Pods in namespace' },
  { method: 'GET', path: '/api/v1/cluster/metrics', description: 'Cluster CPU/Mem, top pods' },
  { method: 'GET', path: '/api/v1/cluster/observability', description: 'Layer B probe status' },
  { method: 'GET', path: '/api/v1/gitops/apps', description: 'Argo CD health and Application sync status (P3 L0)' },
  { method: 'POST', path: '/api/v1/gitops/apps/{name}/sync', description: 'Trigger Argo CD Application sync (operator)' },
  { method: 'GET', path: '/api/v1/stack/addons', description: 'CI/CD stack add-on probe — Gitea, Tekton, Registry (P2 L0)' },
  { method: 'GET', path: '/api/v1/delivery/pipelines', description: 'Tekton Pipeline list (P3 L0)' },
  { method: 'GET', path: '/api/v1/delivery/pipelines/{name}/runs', description: 'PipelineRun history for a pipeline' },
  { method: 'POST', path: '/api/v1/delivery/pipelines/{name}/runs', description: 'Start Tekton PipelineRun (operator)' },
  { method: 'GET', path: '/api/v1/delivery/runs/{id}/logs', description: 'PipelineRun pod log tail' },
  { method: 'POST', path: '/api/v1/cluster/sync-kubeconfig', description: 'Run fetch-kubeconfig.sh' },
]

export type ConfigFileRow = {
  file: string
  role: string
}

export const CONFIG_FILES: ConfigFileRow[] = [
  { file: 'config/environments.yaml', role: 'Dev/prod probe targets' },
  { file: 'config/ops-context.yaml', role: 'Spine — milestones, decisions, focus' },
  { file: 'config/topology.yaml', role: 'Hardware graph' },
  { file: 'config/clusters.yaml', role: 'K3s cluster registry + Bifrost namespaces' },
]

export type SuccessCriterion = {
  area: string
  criterion: string
}

export const SUCCESS_CRITERIA: SuccessCriterion[] = [
  { area: 'Cluster', criterion: 'Node join/drain, namespace, workload restart/scale/logs — UI/API only' },
  { area: 'Delivery', criterion: 'Tekton run, Argo sync/rollback — UI/API only' },
  { area: 'Promote', criterion: 'release_gate trigger and results — UI/API only' },
  { area: 'Runtime', criterion: 'Runtime Map / Control Room live strip / FocusStrip — clickable to executable actions, not read-only' },
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

export type AiPlatformPhase = { id: string; timeBox: string; deliverables: string; businessUnlock: string }

export const AI_PLATFORM_PHASES: AiPlatformPhase[] = [
  {
    id: 'A — Gates',
    timeBox: 'now ~3mo',
    deliverables: 'release_gate.sh, Mac Mini CI, MkDocs+Goal, 2C-B Prod',
    businessUnlock: 'Page refactoring continues; trade review AI offline trial (4090 Ollama)',
  },
  {
    id: 'B — GitOps',
    timeBox: '3~9mo',
    deliverables: 'K3s + Gitea + Tekton + ArgoCD + k8s/base/',
    businessUnlock: 'Frontend Staging on K8s; review index CronJob',
  },
  {
    id: 'C — Closed loop',
    timeBox: '9~18mo',
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

export const ACTUATION_PHASES: ActuationPhaseRow[] = [
  { phase: 'P0 (current)', deliverables: 'Cluster L0 probes, Delivery dual track display', eliminates: 'Observation only' },
  { phase: 'P1', deliverables: 'Auth + audit + workload L1 + logs', eliminates: 'Daily kubectl' },
  { phase: 'P2', deliverables: 'Node lifecycle job + Cluster UI wizard', eliminates: 'install-server.sh, join, drain' },
  { phase: 'P3', deliverables: 'GitOps + CI execution (Argo/Tekton API)', eliminates: 'Argo UI, tkn CLI' },
  { phase: 'P4', deliverables: 'Platform stack install wizard', eliminates: 'Manual Helm install' },
  { phase: 'P5', deliverables: 'MCP actuation Tools', eliminates: 'Agent direct shell' },
]

/** Build LLM-optimized text for the Blueprint page. */
export function buildBlueprintLlmPack(spine?: OpsContextResponse): string {
  const lines: string[] = [
    '# Bifrost Ops — Blueprint (Architecture & North Star)',
    `# Source: ${BLUEPRINT_SOURCE} v${BLUEPRINT_VERSION}`,
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
    '## Platform API endpoints',
    ...PLATFORM_API_ENDPOINTS.map(e => `- ${e.method} ${e.path} — ${e.description}`),
    '',
    '## Configuration files',
    ...CONFIG_FILES.map(c => `- **${c.file}** — ${c.role}`),
    '',
    '## Success criteria (north star completion)',
    ...SUCCESS_CRITERIA.map(s => `- [${s.area}] ${s.criterion}`),
    '',
    '## Actuation phases (P0–P5)',
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
    '## AI Platform phases',
    ...AI_PLATFORM_PHASES.map(p => `- **${p.id}** (${p.timeBox}): ${p.deliverables} → unlocks: ${p.businessUnlock}`),
    '',
    '## AI Platform success criteria',
    ...AI_PLATFORM_SUCCESS.map(s => `- [${s.area}] ${s.criterion}`),
    '',
    '## AI Platform boundaries',
    ...AI_PLATFORM_BOUNDARIES.map(b => `- **${b.rule}**: ${b.detail}`),
  ]

  if (spine != null) {
    lines.push(
      '',
      '## Live spine snapshot',
      `- phase: ${spine.deployment.phase}`,
      `- active_track: ${spine.deployment.active_track}`,
      `- focus: ${spine.focus.headline}`,
    )
  }

  return lines.join('\n')
}
