/**
 * Blueprint catalog — North Star, system architecture, design principles.
 *
 * Authoritative source for Ops Console → Governance → Blueprint.
 * Single source of truth — do not duplicate elsewhere.
 */

import type { OpsContextResponse } from '@/api/opsContextTypes'
import {
  SPINE_MILESTONE_STATUS_DEFINITIONS,
  SPINE_STATUS_SEMANTICS_NOTE,
} from '@/lib/architecture/spineSemantics'
import { buildSystemDomainLlmPack } from '@/lib/architecture/systemDomainCatalog'
import { buildOpsUiActuationSignoffMarkdown } from '@/lib/architecture/opsUiActuationSignoffChecklist'
import { buildPostQaOwnerGateMarkdown } from '@/lib/architecture/postQaOwnerGatePack'

export const BLUEPRINT_VERSION = '2026-08-09'
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
    content: 'North Star, Strategy C, design principles, L0/L1/L2/forbidden (incl. network), AI boundaries',
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
  {
    allowed: 'Physical hardware swap (UCG / Switch / AP — requires physical access)',
    forbidden: 'Manual UniFi UI firewall / zone / SSID changes (use platform-api + scripts)',
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
    layer: 'Research Engine',
    responsibility:
      'OLAP analysis domain (bifrost-research) — dbt SEPA analytics, Python engines (IV/GEX/Momentum/Forecast), Research API :8795; single Golden Source bifrost_golden_source; never writes Trade DB or trade-execution paths (D10/D13)',
  },
  {
    layer: 'Mature components',
    responsibility:
      'Argo CD, Tekton, Headlamp/Rancher, UniFi Controller — wrapped via API (Session v2 / future Integration), not replacing control plane',
  },
  {
    layer: 'Infra scripts',
    responsibility: 'install-server.sh, fetch-kubeconfig.sh, etc. — executor implementation only, operators do not run manually',
  },
  {
    layer: 'Out-of-band Operator Plane (L-1)',
    responsibility: 'AI Agent runners (dual Mac Mini, outside K8s) + mutual watchdog — automate the Owner out-of-band action: recover the platform/cluster when the single pane itself is down. Fate-isolated; see cicdBootstrapCatalog.ts L-1 / Operator Plane.',
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
  { id: 5, title: 'Forbidden unchanged', description: 'Monitor POST /control/*, Redis daemon control, ib:operator:cmd, R-DV3 auto-order bypass — never exposed to platform AI.' },
  { id: 6, title: 'Out-of-band recovery never shares fate', description: 'The Agent that recovers the platform/cluster (L-1 Operator Plane) runs OUTSIDE K8s on dual Mac Minis with a mutual watchdog; it must never be scheduled into the cluster it recovers. The engineer stands on the ground, not inside the rocket.' },
  { id: 7, title: 'Earned autonomy over granted trust', description: 'Agent Skills start at L1 (confirm); consecutive successes earn L0 (auto); failure spikes trigger demotion back to L1. Owner governs via policy, not per-action approval — Flight Director model.' },
  {
    id: 8,
    title: 'Network is the ground floor',
    description:
      'Network infrastructure (UCG / Switch / AP) is the physical substrate all layers depend on; probe and actuation must work independently of K8s (platform-api Session-connects UCG directly, bypassing cluster).',
  },
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
  { view: 'Queue', plane: 'Engineer', purpose: 'Ops Desk queue — operate, remediate, close sessions (legacy Agent Desk)' },
  { view: 'Analysis Workspace', plane: 'Engineer', purpose: 'Analysis Desk V1 — Hermes status, Chat UI, First Task (D10 read-only)' },
  { view: 'Insight Log', plane: 'Engineer', purpose: 'Hermes insight history' },
  { view: 'Hermes Status', plane: 'Engineer', purpose: 'Nous Hermes gateway lamp, model, version, MCP tools' },
  { view: 'Agent Briefing', plane: 'Engineer', purpose: 'New-session entry — work intent, progress, briefing pack' },
  { view: 'Agent Protocol', plane: 'Governance', purpose: 'Agent doctrine — modes, architecture, forbidden actions' },
  { view: 'Briefing Reconciliation', plane: 'Governance', purpose: 'Spine projection rules and drift reconciliation' },
  { view: 'MCP Contract', plane: 'Governance', purpose: 'Agent tool contract — read / routine / confirm / forbidden' },
  { view: 'Skills & Schedules', plane: 'Engineer', purpose: 'Autonomous skill registry and triggers' },
  { view: 'Patrol Log', plane: 'Engineer', purpose: 'Patrol execution history (cron/manual Skill runs)' },
  { view: 'Trust & Autonomy', plane: 'Engineer', purpose: 'Earned autonomy KPIs and trust matrix' },
  { view: 'Launch Agent (L-1)', plane: 'Engineer', purpose: 'Mac Mini Agent host publish (deploy_mac_mini; fate-isolated)' },
  { view: 'Operator Plane (L-1)', plane: 'Engineer', purpose: 'Out-of-band runner heartbeats / MCP / AI Fix (publish → Launch Agent)' },
  {
    view: 'Task Control Center',
    plane: 'Mission Control',
    purpose:
      'Sole Mission / Task Mode execution entry — phased playbook, Ops Desk Board (Fleet + Queue + Patrol), Three Desks switcher, Agent CTAs',
  },
  {
    view: 'Three Desks Strip',
    plane: 'Mission Control',
    purpose:
      'Three-up Build / Ops / Analysis switcher on TCC (System) + Control Room — no extra chrome row',
  },
  {
    view: 'Control Room',
    plane: 'Mission Control',
    purpose:
      'Situation / bay posture cockpit (not Mission launch home) — Bay Scan, topology sheet, Operate/Release context; hand off execution to TCC',
  },
  {
    view: 'Observability',
    plane: 'Mission Control',
    purpose:
      'Apollo-domain read-only system health hub — domain signals, Attention, Grafana deep evidence (not a control plane)',
  },
  {
    view: 'Defects',
    plane: 'Mission Control',
    purpose:
      'Cross-job Agent remediation pattern analysis — filter by System Domain (systemDomainCatalog.ts)',
  },
  {
    view: 'Network Health (Control Room)',
    plane: 'Mission Control',
    purpose:
      'Ground-floor LAN — spine + catalog projection, ZBF status, live UniFi probe; catalogs: networkUpgradeCatalog.ts + networkApiContractCatalog.ts',
  },
  { view: 'Delivery', plane: 'Rocket', purpose: 'CI/CD pipelines and release coupling' },
  { view: 'Runtime Map', plane: 'Mission Control', purpose: 'Topology drill-down sheet from Control Room (not a top-level daily page)' },
  { view: 'Placement', plane: 'Rocket', purpose: 'Fleet facility constraints — node-pool / scheduling policy vs live cluster (Rocket CI, Satellite, shared infra)' },
  { view: 'Cluster', plane: 'Rocket', purpose: 'Cluster operations' },
  { view: 'Audit', plane: 'Mission Control', purpose: 'Platform actuation audit history' },
  { view: 'Promote', plane: 'Rocket', purpose: 'Release readiness (flywheels A + B)' },
  { view: 'Flywheel Vision', plane: 'Governance', purpose: 'Three-layer Agent convergence vision' },
  { view: 'Governance catalogs', plane: 'Governance', purpose: 'Governance catalogs and Copy Prompt' },
  {
    view: 'Operator Dock · Console',
    plane: 'Shell',
    purpose: 'SSH to K3s / Mac Agent hosts — shell Operator Dock Console slot (not a Ground nav page)',
  },
  {
    view: 'Plugin Gallery',
    plane: 'Subcontractors',
    purpose:
      'Observe IB Gateway + Market Data pipeline health (workers / freshness); readiness_rollup from Plugin snapshot-coverage / vendor-gap; Coverage → Readiness / Financials producer DQ panels (not Trade runbook / SEPA criteria / stock_readiness_daily; Gallery ≠ Publish)',
  },
]

/** Task mode lenses — focused Console navigation for ops vs build loops (Constitution). */
export const TASK_MODE_BLUEPRINT = {
  version: '2026-08-09',
  source: 'console/src/lib/task-mode/taskModeCatalog.ts',
  statement:
    'Task modes filter sidebar navigation and land on Task Control Center (or Analysis Workspace) for phased playbooks. ' +
    'Four views: System · Build · Ops · Analysis. Launch / Daily Ops / Patrol merge into Ops. ' +
    'Three Desks (Build / Ops / Analysis) is the System + Control Room switcher. ' +
    'Ops / Dev Mode: Launch and primary Mission actions live on TCC Ops Desk Board + release tabs — Control Room is posture deep-dive (ROOM POSTURE + bays), not a second Mission home. ' +
    'Ops uses Fleet + Queue + Patrol on one board (Discover → Remediate → Deploy → Patrol → Clear). ' +
    'unavailable cells are display-only (Excluded from GO); Prod pins clusters.yaml viewer_env=prod (OPS_VIEWER_ENV overrides). ' +
    'Build (unified) chains Briefing → Implement → Pre-push → Deliver STG → Sign-off; component line inherits from Active Session. ' +
    'Analysis Desk V1 is Hermes status + Chat UI + First Task (read-only, D10 blocked; no stock-analysis engine). ' +
    'Patrol skills stay on Ops Desk (Cursor SDK nightshift via GET /api/v1/patrol/*) — distinct from Hermes Analysis. ' +
    'Nav lens is focused-only (no More domains); phase-aware dimming highlights phase-relevant tabs. ' +
    'System Mode may land Control Room for panoramic posture; Observability remains read-only health.',
  modes: [
    'system',
    'build',
    'ops',
    'analysis',
  ] as const,
  escapeHatch: 'Switch to System view restores full CONSOLE_NAV_GROUPS.',
}

export type AuthorizationLevel = {
  level: string
  behavior: string
}

export const BLUEPRINT_AUTHORIZATION_LEVELS: AuthorizationLevel[] = [
  {
    level: 'L0',
    behavior:
      'Read-only probes (matrix, topology, cluster, logs) + network zone-matrix audit, AP status, VLAN binding check',
  },
  {
    level: 'L1',
    behavior:
      'Safe actuation via platform-api (rollout restart, scale, sync — audited) + firewall policy apply (idempotent Bifrost rules), AP restart',
  },
  {
    level: 'L2',
    behavior:
      'Owner-confirmed changes (node join, stack install, Argo rollback) + zone restructure, SSID CRUD, port profile change, Default Security Posture toggle',
  },
  {
    level: 'forbidden',
    behavior:
      'Monitor POST /control/* · Redis daemon control · ib:operator:cmd · R-DV3 auto-trade bypass · live trading enablement (D10 BLOCKED) · bulk delete all Bifrost firewall zones · disable IDS/IPS',
  },
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
      'Control Room → Runtime Map sheet + Operate views form a closed Observe→Act loop with deep-links; live readiness is Projection (matrix/gate)',
  },
  { area: 'Spine', criterion: 'GET /api/v1/context + Program page always shows north star' },
  { area: 'MCP', criterion: 'MCP Tools and UI — same permissions, same audit (AI Agent self-interaction loop)' },
  {
    area: 'Network',
    criterion:
      'Zone-policy audit clean, AP coverage baseline met, Default VLAN device count → 0 — all verifiable via /api/v1/network/*',
  },
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
  'Bifrost runs as three system domains: OLTP (Trade), OLAP (Research Engine), and Control Plane (Ops). ' +
  'Trade workloads (frontend, API, Worker, Socket) evolve safely, observably, and rollback-ready on this platform. ' +
  'Research (bifrost-research) owns analytics, forecast, and backtest on bifrost_golden_source — isolated from trade execution (D10/D13). ' +
  'Downstream product lines on this foundation: (1) page continuous refactoring (Dense UI / frontend migration); ' +
  '(2) Research / trade-review AI (read-only analysis, isolated from live trading). ' +
  'Ultimate convergence target: see Governance → Vision (dualFlywheelVisionCatalog.ts) — ' +
  'three-layer Agent (Dev / Ops / Business) unifying code, operations, and trade intelligence in one Cursor window.'

export const AI_MERGE_RATIONALE =
  'Splitting into two projects causes duplicate MCP, context, and gates. Merged: one platform, one Tool contract, one release mainline.'

/** Three system domains — OLTP + OLAP + Control Plane (D13). */
export type SystemDomainDef = {
  id: string
  name: string
  role: string
  primaryRepos: string
  database: string
  mustNot: string
}

export const SYSTEM_DOMAINS: SystemDomainDef[] = [
  {
    id: 'trade',
    name: 'Trade (OLTP)',
    role: 'Execution, positions, real-time monitoring, env-isolated operational data',
    primaryRepos: 'bifrost-trade-* (frontend :5173, APIs :8765–8773)',
    database: 'bifrost_dev / bifrost_stg / bifrost_prod (environment-isolated)',
    mustNot: 'Own Golden Source analytics/research schemas; bypass D10 freeze for live trading',
  },
  {
    id: 'research',
    name: 'Research (OLAP)',
    role: 'Analysis, screening, forecast, backtest, AI intelligence on shared market facts',
    primaryRepos: 'bifrost-research (dbt + engines + Research API :8795)',
    database: 'bifrost_golden_source single instance (raw_market.* / dw_stock.* / features_* / ops_jobs.*)',
    mustNot: 'Write Trade DB; write raw_market.* (Plugin owns ingest); trigger trade execution (D10)',
  },
  {
    id: 'ops',
    name: 'Ops (Control Plane)',
    role: 'Environment governance, health probes, deploy orchestration, Agent protocol',
    primaryRepos: 'bifrost-platform (Console :5180, platform-api :8780)',
    database: 'Control-plane state (spine, programs, operate queue) — not Trade/Research business DBs',
    mustNot: 'Implement OLAP engines inside Plugins; expose Monitor POST /control/* or ib:operator:cmd to platform AI',
  },
]

export type AiCapability = { name: string; description: string; examples: string[] }

export const AI_PLATFORM_CAPABILITIES: AiCapability[] = [
  {
    name: 'Discovery',
    description: 'System auto-exposes topology and state understandable by Agent and humans — no manual port tables or SSH log checking.',
    examples: [
      'Service inventory from K8s API / Compose labels → unified list',
      'Health & dependencies from Monitor + Ops + Socket health Redis',
      'Config & versions from Git tag, image digest, ArgoCD sync status',
      'Network topology from UniFi API — UCG / Switch / AP inventory, zone-matrix, client count per VLAN',
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
      'Firewall policy drift — audit Bifrost zones/policies against networkUpgradeCatalog.ts FIREWALL_RULES',
      'Three Desks landing — Build / Ops (queue + Patrol Cursor SDK) / Analysis (Hermes premium, D10 blocked)',
    ],
  },
  {
    name: 'Repair',
    description: 'AI and rule engine attempt recovery within permission boundaries, not just alerting.',
    examples: [
      'L0 read-only: diagnose, root cause summary, Runbook link',
      'L1 safe retry: retry-failed, restart Celery worker instance via Ops API',
      'L2 controlled change: ArgoCD rollback, scaling — requires Owner confirmation',
      'L1 network: re-sync missing Bifrost firewall policies via unifi_firewall_setup.py apply (idempotent)',
      'L2 network: zone restructure or SSID CRUD — requires Owner confirmation',
      'Forbidden: LLM direct to trade — Monitor POST /control/*, Redis daemon control, ib:operator:cmd, R-DV3 violation',
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
  { area: 'Isolation', criterion: 'Trade review AI and ops Agent cannot trigger Monitor POST /control/* or IB Operator RPC' },
  { area: 'Page refactoring', criterion: 'Each migrated page reaches Staging after CI gate; Owner sign-off chain complete' },
  { area: 'Trade review AI', criterion: 'At least one daily review report (positions + trades + PnL) generated locally; data source read-only and auditable' },
  {
    area: 'Network',
    criterion:
      'One platform-api call returns zone-matrix + firewall policy list + AP health; firewall drift auto-detected',
  },
  {
    area: 'Three Desks',
    criterion:
      'Build / Ops / Analysis share Console Task Mode; Patrol = Cursor SDK nightshift on Ops Desk; Hermes = Analysis Desk premium analysis (Trade later); D10 blocked',
  },
]

export type AiBoundary = { rule: string; detail: string }

export const AI_PLATFORM_BOUNDARIES: AiBoundary[] = [
  { rule: 'R-DV3', detail: 'One auto-trade Engine per IB account; Dev/Prod separate client_id' },
  { rule: 'Trade write path', detail: 'Only daemon → ib:operator:cmd; AI read-only or via verified Ops API' },
  { rule: 'TWS', detail: 'Win11 dedicated machine, never scheduled into K3s' },
  { rule: 'Phase 1 constraint', detail: 'While frontend points at Legacy API, platform must not mix "API migration" and "release" into one change (single-variable principle)' },
  {
    rule: 'Network Security Posture',
    detail:
      'Agent must not toggle Default Security Posture (Allow All ↔ Block All) or disable IDS/IPS; physical UCG access is Owner-only',
  },
]

/** Actuation phase definitions (Constitution) — which phase means what; live progress is Projection. */
export const ACTUATION_PHASES: ActuationPhaseRow[] = [
  {
    phase: 'P0',
    deliverables: 'Cluster L0 probes, Delivery dual track display + UCG reachability probe + zone-matrix read',
    eliminates: 'Observation only',
  },
  {
    phase: 'P1',
    deliverables: 'Auth + audit + workload L1 + logs + firewall audit automation + AP status probe',
    eliminates: 'Daily kubectl',
  },
  {
    phase: 'P2',
    deliverables:
      'Node lifecycle job + Cluster UI wizard + AP lifecycle (adopt / restart / firmware) — AP slice Owner-deferred (D6 appendix)',
    eliminates: 'install-server.sh, join, drain',
  },
  { phase: 'P3', deliverables: 'GitOps + CI execution (Argo/Tekton API)', eliminates: 'Argo UI, tkn CLI' },
  { phase: 'P4', deliverables: 'Platform stack install wizard', eliminates: 'Manual Helm install' },
  {
    phase: 'P5',
    deliverables: 'MCP actuation Tools + UniFi MCP tools (network-read / network-write)',
    eliminates: 'Agent direct shell',
  },
]

/** D6 appendix — Owner-deferred actuation (Wave A Phase A3). Do not implement without Owner program unlock. */
export type D6DeferredActuation = {
  id: string
  scope: string
  deferred: string
  ownerReason: string
  nextUnlock: string
}

export const D6_APPENDIX_OWNER_DEFERRED: D6DeferredActuation[] = [
  {
    id: 'network-l2-zones-wlan',
    scope: 'Network L2 (Constitution authorization L2)',
    deferred: 'POST /api/v1/network/zones/restructure · POST /api/v1/network/wlan — zone restructure + SSID CRUD',
    ownerReason:
      'Physical UniFi substrate; bulk zone/SSID changes require Owner confirmation. L1 firewall apply (D9 Session v2) is the only routine network write today.',
    nextUnlock: 'Owner program after Default VLAN baseline + sustained POLICY_NOMINAL audit',
  },
  {
    id: 'network-p2-ap-lifecycle',
    scope: 'Actuation P2 / Network AP lifecycle',
    deferred: 'AP adopt · restart · firmware — ACTUATION_PHASES P2 deliverable (partial)',
    ownerReason: 'Owner-deferred in Wave A Phase A3; AP operations remain UniFi UI or future scoped L1 restart API.',
    nextUnlock: 'Owner sign-off checklist item network-ap-p2-deferred + dedicated network program',
  },
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
    '### System domains (OLTP + OLAP + Ops)',
    ...SYSTEM_DOMAINS.map(
      d =>
        `- **${d.name}** [${d.id}]: ${d.role} | Repos: ${d.primaryRepos} | DB: ${d.database} | Must-not: ${d.mustNot}`,
    ),
    '',
    '## Design principles',
    ...DESIGN_PRINCIPLES.map(p => `${p.id}. **${p.title}** — ${p.description}`),
    '',
    buildSystemDomainLlmPack(),
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
    '## Wave A Phase A3 — ops-ui-actuation progress (Projection snapshot)',
    '- Cluster / Launch Rocket / Deploy Satellite / MCP Contract UI: agent-completable slices marked done in uiProgressOverrides.ts',
    '- Audit: GET /api/v1/audit + Console Download JSON export (no P4 retention/replay yet)',
    '- Owner-deferred: Network L2 zone/SSID + P2 AP lifecycle — see D6 appendix below',
    '- Owner CLOSED gate: opsUiActuationSignoffChecklist.ts — CLOSED 2026-07-22 (Owner waived runtime-observe-act-loop residual)',
    '',
    '## D6 appendix — Owner-deferred actuation',
    ...D6_APPENDIX_OWNER_DEFERRED.map(
      d =>
        `- **${d.id}** (${d.scope}): ${d.deferred} — ${d.ownerReason} · Unlock: ${d.nextUnlock}`,
    ),
    '',
    buildOpsUiActuationSignoffMarkdown(),
    '',
    buildPostQaOwnerGateMarkdown(),
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
