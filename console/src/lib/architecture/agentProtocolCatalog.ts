/**
 * Agent Protocol catalog — Agent Modes, context packs, forbidden actions.
 *
 * Authoritative source for Ops Console → Governance → Agent Protocol.
 * Single source of truth — do not duplicate elsewhere.
 *
 * System Domain taxonomy (Apollo planes / Defects filters): systemDomainCatalog.ts
 * — AgentTaskDomain remains the task-subject vocabulary; map scopes via scopeToDomain().
 */

import { buildSystemDomainLlmPack } from '@/lib/architecture/systemDomainCatalog'
import { buildConsoleSeatLlmPack } from '@/lib/architecture/consoleSeatCatalog'

export const AGENT_PROTOCOL_VERSION = '2026-08-21'
export const AGENT_PROTOCOL_SOURCE = 'console/src/lib/architecture/agentProtocolCatalog.ts'

/**
 * Mission Signal P4–P7 are program delivery/history, not per-session protocol.
 * Detailed step tables (HERMES_FIRST_TASK_STEPS, FLIGHT_DIRECTOR_STEPS,
 * FLIGHT_DIRECTOR_OPS_STEPS, MISSION_SIGNAL_CLOSURE_STEPS) stay exported for
 * program surfaces; the default LLM pack carries only this reference line.
 */
export const MISSION_SIGNAL_PROGRAM_REFERENCE =
  'Mission Signal Phases 4–7 (Hermes First Task, Flight Director governance, Flight Director operations, ' +
  'program closure) are program delivery history — see Delivery Board (Engineer → Delivery) and Agent System ' +
  '(Governance) for status and step detail. Active per-session protocol remains: modes, domains, forbidden ' +
  'actions, D10 freeze, and the P2/P3 diagnostic playbooks above.'

export type AgentModeRow = {
  mode: string
  flywheel: string
  defaultUI: string
  agentMay: string
  agentMustNot: string
}

export const AGENT_MODES: AgentModeRow[] = [
  {
    mode: 'Product',
    flywheel: 'A — Trade FE',
    defaultUI: 'bifrost-trade-frontend :5173 → bifrost-dev :30882 (D-IL1; not Prod browser)',
    agentMay: 'Migrate pages, Dense UI, hooks, Legacy equivalence; follow trade-dev-inner-loop smoke pack',
    agentMustNot: 'Change compose, prod cutover, K3s, API contracts; treat Prod refresh as UI accept; enable live trading or scale daemon for auto-trade (D10 BLOCKED until Owner unlock)',
  },
  {
    mode: 'Ops',
    flywheel: 'B — Runtime',
    defaultUI: 'Bifrost Ops Console :5180 → Control Room',
    agentMay:
      'Read spine, matrix, topology; infra YAML; K3s planning; network L0 zone-matrix + firewall audit (scripts/unifi_firewall_setup.py); L1 idempotent firewall apply per D9 Session v2',
    agentMustNot: 'Change trade page UI, expand FE scope; toggle Default Security Posture or disable IDS/IPS; scale daemon for live trading or remove STG daemon-scale-zero (D10)',
  },
  {
    mode: 'Promote',
    flywheel: 'A + B coupling',
    defaultUI: 'Rocket → Launch Rocket · Mission Control → Audit',
    agentMay: 'Query release-state, deploy via start_pipeline_run, run gates, verify smoke; follow next_action guidance',
    agentMustNot: 'Skip blockers (D1, gate), deploy PROD with different revision than STG, bypass admin role for gates; Promote rollout that enables live trading (D10 BLOCKED)',
  },
  {
    mode: 'Research',
    flywheel: 'C — OLAP',
    defaultUI: 'bifrost-research (dbt + engines + Research API :8795) · Ops Console Research governance (Wave 5)',
    agentMay:
      'Edit bifrost-research; run dbt on bifrost_golden_source analytics/research/features schemas; add Python engines; Research API read paths; K8s manifests under research NS',
    agentMustNot:
      'Write Trade DB (bifrost_dev/stg/prod); mutate raw_market.* ingest tables (Plugin owns); change Ops spine/compose for Trade cutover; enable live trading or daemon scale (D10)',
  },
]

export type ContextPackButton = {
  button: string
  contents: string
}

export const CONTEXT_PACK_BUTTONS: ContextPackButton[] = [
  { button: 'Copy Product', contents: 'Phase 1 discipline + spine focus/deployment — no matrix' },
  { button: 'Copy Ops', contents: 'Full spine section + matrix summary per environment' },
  { button: 'Copy Promote', contents: 'Ops pack + flywheel A/B checklist + promote blockers + prod fail list' },
  { button: 'Copy Research', contents: 'D13 domains + bifrost-research CLAUDE + Golden Source schema boundaries — no Trade DB writes' },
  { button: 'Copy Scoped', contents: '(when a pipeline milestone is selected) Ops pack + scoped milestone/decision' },
]

export type ContextPackLayer = {
  order: number
  name: string
  description: string
}

export const CONTEXT_PACK_LAYERS: ContextPackLayer[] = [
  {
    order: 0,
    name: 'Agent Briefing',
    description:
      'Briefing → session pack; reconcile gate per briefingReconciliationCatalog.ts (BRIEFING_STALE when queue/appendix/headline diverge from spine); Since your last session shows matrix/cluster deltas + agent tasks',
  },
  { order: 1, name: 'Discipline', description: 'Workspace rules, migration-protocol, dense-ui-system' },
  { order: 2, name: 'Spine', description: 'GET /api/v1/context or Ops Console → Catalog → Copy for LLM' },
  { order: 3, name: 'Task scope', description: 'One milestone id, one env (dev/prod), one repo' },
  { order: 4, name: 'Live probe', description: 'Only if task touches connectivity: GET /api/v1/matrix?env=...' },
  { order: 5, name: 'Deep doc', description: 'MIGRATION_TRACKING.md, sign-off runbooks (on demand)' },
]

export type ForbiddenAction = {
  action: string
  scope: string
}

export const FORBIDDEN_ACTIONS: ForbiddenAction[] = [
  { action: 'Redis daemon control write via platform AI (POST /api/monitor/control/*)', scope: 'All modes' },
  { action: 'ib:operator:cmd RPC', scope: 'All modes' },
  {
    action:
      'Session SDK runtime (bridge.ts Start/Approve/Reject/Cancel/Launch) — removed; phase work for Briefing→In Flight→Delivery runs in Cursor IDE Agent',
    scope: 'Session construction (Briefing→In Flight→Delivery)',
  },
  {
    action:
      'Live trading enablement — scale daemon for auto-trade, remove STG daemon-scale-zero, enable live hedge/place_order, or Monitor /control/* that arms live trading (spine D10 BLOCKED until Owner explicit unlock)',
    scope: 'All modes',
  },
  { action: 'Editing bifrost-trader-engine/ (read-only reference)', scope: 'All modes' },
  {
    action: 'Default Security Posture toggle (Allow All ↔ Block All) or disable IDS/IPS on UCG',
    scope: 'All modes',
  },
  { action: 'Bulk delete all Bifrost firewall zones / policies', scope: 'All modes' },
  {
    action: 'Manual UniFi UI firewall / zone / SSID changes (use platform-api + scripts executors)',
    scope: 'All modes',
  },
  {
    action: 'UniFi Integration API Key write path on UCG 10.4.57 (site UUID blocked — use Session v2 per spine D9)',
    scope: 'Ops mode',
  },
  {
    action:
      'Forced Agent Desk tab switch on Agent Fix start/running — use shell Operator Dock Agent slot (ambientJob + Expand dock; Recent rail adopts jobs in-dock); Agent Desk is archive only (explicit Open in Agent Desk / Archive)',
    scope: 'Ops Console shell',
  },
  {
    action:
      'Operator Dock embedded Agent host Update / Confirm / deploy log / smoke — Dock is L-1 pulse + deep-link only; Update SSOT = Launch Desk → Agent (AgentHostDeployPanel); Operator Plane = heartbeats / MCP / AI Fix',
    scope: 'Ops Console shell · Operator Dock',
  },
  {
    action:
      'In-page Commit & push / Skip / Cancel on Launch Live — approvals are Dock SSOT; Launch Live is telemetry (Agent one-line + Pipeline + Post-deploy) with Expand dock',
    scope: 'Mission Launch · Launch Live',
  },
  {
    action:
      'kubectl set image bypass for ib-gateway publish — use Launch Plugin lane + make install-ib-gateway only; IB Gateway manage reconnect is observe/repair not publish',
    scope: 'Mission Launch · Launch Plugin',
  },
  {
    action:
      'Direct kubectl / pg_dump against CNPG from Agent — use Platform API get_data_freshness / trigger_data_clone / get_data_clone_status instead',
    scope: 'Ops mode',
  },
  {
    action: 'Clone or restore into bifrost_prod (source-only; targets limited to bifrost_dev / bifrost_stg)',
    scope: 'All modes',
  },
]

/** Wave 3 P0 — Owner-locked before implementation (spine D11, D12). */
export type Wave3P0Decision = {
  id: 'D11' | 'D12'
  topic: string
  rule: string
  wave3Deliverables: string[]
}

export const WAVE3_P0_DECISIONS: Wave3P0Decision[] = [
  {
    id: 'D11',
    topic: 'Operate Queue API',
    rule:
      'Post-completion remains NOT ASSESSED until Briefing Owner decision. Only approved structured handoffs inject into GET/POST /api/v1/operate/queue (data/operate/queue.json); NO HANDOFF is explicit, and verified closure remains in recent_closed. Not spine tracks.operate.',
    wave3Deliverables: [
      'Operate queue store + GET list + POST enqueue on approve',
      'Control Room strip: open-count summary + deep-link to Agent Desk (full list in TCC / Desk)',
      'Briefing operate track reads queue API',
      'MCP get_operate_queue (read)',
      'Structured reason/task/criteria/verification/risk contract with legacy JSON compatibility',
      'Agent Desk Start/Prepare → execution_job_id → evidence-gated close',
    ],
  },
  {
    id: 'D12',
    topic: 'Sign-off single path api',
    rule:
      'Only POST /api/v1/programs/{id}/phases/{pid}/signoff writes phase_sign_offs. Session SDK runtime (bridge.ts /active/* /launch) is removed — phase work runs in Cursor IDE Agent. UI host = In Flight (Engineer → Delivery); Delivery is read-only catalog.',
    wave3Deliverables: [
      'Session SDK runtime removed; only programs signoff API writes phase_sign_offs',
      'Vision gate Owner sign → programs signoff API (gate JSON = run artifact only)',
      'YAML: all programs sign_off_mechanism: api',
      'Remove vision_gate branch from programs_delivery.go reads',
      'In Flight hosts phase Sign-off + post-completion Approve; Delivery is read-only catalog',
    ],
  },
]

/** Mission Signal Phase 2 — classify before remediating datastore / matrix failures. */
export type MissionDiagnosticPlaybook = {
  classification: 'NOMINAL' | 'PROBE_DRIFT' | 'DATA_LAYER' | 'HTTP_FAIL'
  trigger: string
  agentAction: string
  autonomy: 'L0' | 'L1' | 'L2'
  mustNot: string
}

export const MISSION_DIAGNOSTIC_PLAYBOOKS: MissionDiagnosticPlaybook[] = [
  {
    classification: 'NOMINAL',
    trigger: 'verify_payload: matrix and cluster agree for PG/Redis',
    agentAction: 'Skip datastore remediation; investigate other subsystems if Mission still degraded',
    autonomy: 'L0',
    mustNot: 'Restart CNPG/Redis or open platform defect PR without evidence',
  },
  {
    classification: 'PROBE_DRIFT',
    trigger: 'Matrix fail on postgres/redis but cluster/postgres + cluster/redis report ok (e.g. *.svc.cluster.local from Mac)',
    agentAction: 'Document probe defect; propose platform-api/environments.yaml fix; re-run verify_payload',
    autonomy: 'L2',
    mustNot: 'Treat as payload outage — do NOT restart PG/Redis pods or fail over CNPG',
  },
  {
    classification: 'DATA_LAYER',
    trigger: 'verify_payload: matrix and cluster both fail or cluster reports CNPG/Redis unhealthy',
    agentAction: 'Diagnose CNPG/Redis in data NS; L1 confirm before rollout restart or failover',
    autonomy: 'L1',
    mustNot: 'Prod cutover or spine writes without Owner approval',
  },
  {
    classification: 'HTTP_FAIL',
    trigger: 'Trade API or nginx-spa matrix targets fail; datastore classification NOMINAL',
    agentAction: 'Check bifrost-{env} deployments, ingress, and API pods — not datastore',
    autonomy: 'L1',
    mustNot: 'Conflate with PG/Redis probe drift',
  },
]

export const MISSION_DIAGNOSTIC_MCP = {
  verifyPayload: 'verify_payload — GET /api/v1/mission/verify-payload',
  verifyMissionSnapshot: 'verify_mission_snapshot — GET /api/v1/mission/verify-snapshot',
  matrix: 'get_connectivity_matrix',
  clusterPostgres: 'get_cluster_postgres (Console cluster API)',
  clusterRedis: 'get_cluster_redis (Console cluster API)',
  dataFreshness: 'get_data_freshness — CNPG logical DB lag vs bifrost_prod',
  dataClone: 'trigger_data_clone + get_data_clone_status — refresh bifrost_dev/stg from prod',
  deliveryRunLogs: 'get_delivery_run_logs — Tekton PipelineRun task logs',
  startPipelineRun: 'start_pipeline_run — re-run bifrost-deliver-stg after fix',
  stgSmoke: 'get_stg_smoke — STG runtime probes (stale-fail vs outage)',
  gitopsApps: 'get_gitops_apps — Argo CD Application sync/health',
  gitopsSync: 'gitops_sync_app — trigger Argo sync to HEAD',
} as const

/** Cluster / delivery remediation playbooks — L1 Ops scope (Mission Board + Cluster triage). */
export type ClusterRemediationPlaybook = {
  id: string
  title: string
  trigger: string
  agentAction: string
  autonomy: 'L0' | 'L1' | 'L2'
  mustNot: string
  mcpTools: string[]
}

export const CLUSTER_REMEDIATION_PLAYBOOKS: ClusterRemediationPlaybook[] = [
  {
    id: 'trade-dev-inner-loop',
    title: 'Trade DEV inner loop (local FE + PG freshness + redis-ib Live)',
    trigger:
      'Product-mode Trade UI work: need accept path, bifrost_dev ledger freshness, or Live quotes on bifrost-dev without touching Prod FE',
    agentAction:
      'L0/L1: Follow Program trade-dev-inner-loop + TRADE_DEV_INNER_LOOP.md. UI accept = Vite :5173 → 30882 (D-IL1). Observe get_data_freshness (also last_clone_at ≤7d cadence). Owner-gated trigger_data_clone → bifrost_dev → poll → bounce DEV api-*. Live = assert redis-ib ExternalName + probe_dev_live_readiness (never dump redis-live-prod). Distinguish PG stale vs Gateway vs api-market failure UX.',
    autonomy: 'L1',
    mustNot:
      'Do not treat Prod browser as daily UI accept (D-IL4); do not dump redis-live-prod → redis-dev; do not auto-clone without Owner; do not unlock D10',
    mcpTools: [
      'get_data_freshness',
      'trigger_data_clone',
      'get_data_clone_status',
      'rollout_restart_deployment',
      'list_dev_sessions',
    ],
  },
  {
    id: 'data-freshness-clone',
    title: 'Refresh non-prod CNPG databases from prod',
    trigger:
      'get_data_freshness shows bifrost_dev or bifrost_stg aging (≥3d) or stale (≥7d) lag vs bifrost_prod; local Trade/STG needs current schema/data; or last_clone_at >7d before ledger-heavy Trade UI (Program trade-dev-inner-loop)',
    agentAction:
      'L1: Owner TCC ConfirmDialog (Refresh DEV ledger) starts Agent Task data-layer-clone. get_data_freshness → trigger_data_clone (confirm:true + confirmation_token=CLONE-FROM-PROD; Full targets=["bifrost_dev"] only) → poll get_data_clone_status → rollout_restart_deployment bifrost-dev api-*. If Cursor MCP lacks data tools, reload bifrost-platform MCP (Settings → MCP).',
    autonomy: 'L1',
    mustNot: 'Do not target bifrost_prod; do not kubectl exec; do not enable live trading (D10); do not enable weekly auto-clone unless Owner explicitly requests; do not dump redis-live-prod',
    mcpTools: ['get_data_freshness', 'trigger_data_clone', 'get_data_clone_status', 'rollout_restart_deployment'],
  },
  {
    id: 'deliver-stg-recover',
    title: 'Deliver STG pipeline recovery',
    trigger:
      'Last bifrost-deliver-stg PipelineRun failed (Mission Release fail/degraded) — especially when STG smoke is green (stale pipeline fail, not K8s nodes)',
    agentAction:
      'L1: get_delivery_run_logs → identify failing Tekton task/step (rollout first) → spawn_trade_release_fix if repo fix needed → start_pipeline_run bifrost-deliver-stg → get_stg_smoke + verify_mission_snapshot',
    autonomy: 'L1',
    mustNot: 'Do not cordon nodes; do not enable live trading (D10); do not bypass release gates',
    mcpTools: ['get_delivery_run_logs', 'start_pipeline_run', 'get_stg_smoke', 'verify_mission_snapshot'],
  },
  {
    id: 'gitops-config-repair',
    title: 'GitOps / ConfigMap repair',
    trigger: 'PipelineRun gitops-sync or rollout fails on missing ConfigMap/Secret; CreateContainerConfigError pods',
    agentAction: 'Restore manifest in Gitea programs/ path; ArgoCD sync; re-run deliver-stg',
    autonomy: 'L1',
    mustNot: 'Patch prod secrets in-cluster without Owner review',
    mcpTools: ['gitops_sync_app', 'get_delivery_run_logs', 'start_pipeline_run'],
  },
  {
    id: 'elastic-node-recover',
    title: 'Elastic node recover (WOL / k3s-agent)',
    trigger: 'Elastic node degraded — host online but K3s agent down or node cordoned',
    agentAction: 'POST /cluster/nodes/{name}/wake or restart k3s-agent; uncordon when Ready',
    autonomy: 'L1',
    mustNot: 'Replace hardware or drain core nodes without triage',
    mcpTools: ['get_cluster_nodes', 'cordon_node', 'uncordon_node', 'drain_node'],
  },
  {
    id: 'platform-self-health-recover',
    title: 'Platform self-health recovery',
    trigger: 'Control plane self-health probes failing (platform-api, console, nginx routes)',
    agentAction:
      'L1: verify_mission_snapshot → rollout_restart platform-api/console in bifrost-platform-prod → confirm NodePort reachability',
    autonomy: 'L1',
    mustNot: 'Do not patch prod secrets in-cluster without Owner review',
    mcpTools: ['verify_mission_snapshot', 'rollout_restart_deployment', 'get_cluster_summary'],
  },
  {
    id: 'registry-pull-recover',
    title: 'Registry image pull recovery',
    trigger: 'ImagePullBackOff / ErrImagePull from registry.cicd:30500 after deliver rollout',
    agentAction:
      'L1: describe failing pods → confirm Kaniko tag + registry reachability → fix image/build → rollout_restart',
    autonomy: 'L1',
    mustNot: 'Do not replace nodes for registry pull failures',
    mcpTools: ['get_cluster_summary', 'rollout_restart_deployment'],
  },
]

/** Network governance Phase 4 — classify before firewall/zone actuation (parallel to verify_payload). */
export type NetworkDiagnosticPlaybook = {
  classification: 'POLICY_NOMINAL' | 'POLICY_DRIFT' | 'SESSION_PATH' | 'POSTURE_FORBIDDEN'
  trigger: string
  agentAction: string
  autonomy: 'L0' | 'L1' | 'L2'
  mustNot: string
}

export const NETWORK_DIAGNOSTIC_PLAYBOOKS: NetworkDiagnosticPlaybook[] = [
  {
    classification: 'POLICY_NOMINAL',
    trigger: 'unifi_firewall_setup.py audit — all Bifrost zones + policies match networkUpgradeCatalog.ts FIREWALL_RULES',
    agentAction:
      'Document clean zone-matrix; if connectivity issue persists, classify as matrix/K8s/TWS — not firewall drift',
    autonomy: 'L0',
    mustNot: 'Run apply or change zones when audit reports no drift',
  },
  {
    classification: 'POLICY_DRIFT',
    trigger: 'Audit reports missing Bifrost policies or zone-matrix mismatch vs FIREWALL_RULES',
    agentAction:
      'Ops mode L1: run unifi_firewall_setup.py apply (idempotent Session v2 path per spine D9); re-audit before closing',
    autonomy: 'L1',
    mustNot: 'Use Integration API Key write on UCG 10.4.57; do not bulk-delete zones',
  },
  {
    classification: 'SESSION_PATH',
    trigger: 'Integration /sites missing site UUID or firewall write fails via Integration Key',
    agentAction:
      'Confirm spine decision D9; actuation via bifrost-agent Session v2 + CSRF only; document unlock condition (firmware/API fix)',
    autonomy: 'L0',
    mustNot: 'Retry Integration Key writes or store Super Admin credentials in git',
  },
  {
    classification: 'POSTURE_FORBIDDEN',
    trigger: 'Remediation requires Default Security Posture toggle, IDS/IPS off, or manual UniFi UI zone/SSID edit',
    agentAction: 'Stop — escalate to Owner; cite Blueprint forbidden + AI Platform Network Security Posture boundary',
    autonomy: 'L2',
    mustNot: 'Proceed with posture change or UniFi UI edits even with Owner chat approval without explicit L2 sign-off in Console',
  },
]

export const NETWORK_DIAGNOSTIC_MCP = {
  auditScript: 'scripts/unifi_firewall_setup.py audit',
  applyScript: 'scripts/unifi_firewall_setup.py apply (L1 — Owner confirm if prod-adjacent)',
  firewallCatalog: 'console/src/lib/architecture/networkUpgradeCatalog.ts — FIREWALL_RULES',
  spineDecision: 'GET /api/v1/context — decisions D9, coupling unifi_session_v2',
  futureApi: 'GET /api/v1/network/* (Projection — planned; Constitution North Star criterion)',
} as const

/** Mission Signal Phase 3 — autonomous fix validation loop. */
export type MissionPostFixStep = {
  step: string
  tool: string
  required: boolean
  detail: string
}

export const MISSION_POST_FIX_LOOP: MissionPostFixStep[] = [
  {
    step: '1. Remediate',
    tool: 'platform-api / kubectl tools',
    required: true,
    detail: 'Apply fix per diagnostic playbook (L0–L2). Do not skip verify_payload classification.',
  },
  {
    step: '2. Re-probe',
    tool: 'verify_mission_snapshot',
    required: true,
    detail: 'Fresh matrix + verify_payload; read post_fix_verification.passed before closing job.',
  },
  {
    step: '3. Close or iterate',
    tool: 'finish_job (runner auto-runs step 2)',
    required: true,
    detail: 'If post_fix_verification.passed is false, continue diagnosis — do not declare success.',
  },
]

/** Mission Signal Phase 4 — Hermes First Task (L0 read-only onboarding). */
export type HermesFirstTaskStep = {
  step: string
  tool: string
  required: boolean
  detail: string
}

export const HERMES_FIRST_TASK_MCP = {
  readiness: 'get_hermes_readiness — GET /api/v1/agent/hermes/readiness',
  firstTask: 'get_hermes_first_task — GET /api/v1/agent/hermes/first-task',
  bridge: 'get_agent_bridge',
  verifySnapshot: 'verify_mission_snapshot',
} as const

export const HERMES_FIRST_TASK_STEPS: HermesFirstTaskStep[] = [
  {
    step: '0. Readiness gate',
    tool: 'get_hermes_readiness',
    required: true,
    detail: 'Confirm ready=true (Hermes gateway, LLM key, platform MCP agent tools). blockers[] + blocker_details[] cite codes (LLM_KEY_MISSING is Owner-only on Nous Hermes host). Agent must not configure LLM keys.',
  },
  {
    step: '1. Bridge check',
    tool: 'get_agent_bridge',
    required: true,
    detail: 'Confirm Nous Hermes + platform MCP stdio bridge on agent host.',
  },
  {
    step: '2. Mission snapshot',
    tool: 'verify_mission_snapshot',
    required: true,
    detail: 'Fresh reprobe + post_fix_verification; cite passed/false in report.',
  },
  {
    step: '3. Matrix context',
    tool: 'get_connectivity_matrix',
    required: true,
    detail: 'List failing trade/datastore targets; classify PROBE_DRIFT vs DATA_LAYER via verify_payload guidance.',
  },
  {
    step: '4. Report only',
    tool: 'L0 — no actuation',
    required: true,
    detail: 'Structured English summary for Owner. Do not call rollout_restart, deploy, or L1+ tools on first task.',
  },
]

/** Mission Signal Phase 5 — Flight Director governance (remediation JobStore; Hermes optional). */
export type FlightDirectorStep = {
  step: string
  tool: string
  required: boolean
  detail: string
}

export const FLIGHT_DIRECTOR_MCP = {
  performance: 'get_agent_performance — GET /api/v1/agent/governance/performance',
  trustMatrix: 'get_trust_matrix — GET /api/v1/agent/governance/trust-matrix',
  snapshot: 'get_flight_director_snapshot — GET /api/v1/agent/governance/snapshot',
  capabilityMap: 'GET /api/v1/agent/governance/capability-map',
} as const

export const FLIGHT_DIRECTOR_STEPS: FlightDirectorStep[] = [
  {
    step: '1. Performance KPIs',
    tool: 'get_agent_performance',
    required: true,
    detail: '7d/30d success rate, intervention rate, MTTR from remediation JobStore.',
  },
  {
    step: '2. Trust matrix',
    tool: 'get_trust_matrix',
    required: true,
    detail: 'Per-task L0/L1/L2, consecutive successes, promotion_eligible, demotion_triggered.',
  },
  {
    step: '3. Capability gaps',
    tool: 'get_flight_director_snapshot',
    required: true,
    detail: 'Task scope × MCP tools × mission signals; highlight gaps before expanding autonomy.',
  },
  {
    step: '4. Owner briefing',
    tool: 'Control Room / Trust & Autonomy',
    required: true,
    detail: '24h digest: jobs completed/failed, escalations, promotion/demotion flags — replaces manual Audit scanning.',
  },
]

/**
 * Daily Ops Fleet Desk — per-cell Agent Fix (do not silently pickFixScope across roles).
 * Engineer CRITICAL → inline Operator Plan + AI Fix on TCC; full Operator Plane is escape hatch.
 */
export const DAILY_OPS_FLEET_DESK = {
  version: '2026-07-19-row-fix-ask-ai',
  source:
    'console/src/lib/control-room/fleetSnapshot.ts · fleetCellFix.ts · dailyOpsWorkflow.ts · dailyOpsPrimaryBlocker.ts · DailyOpsProcessStrip.tsx · DailyOpsOperatorPlanPanel.tsx · checklistDispatch.ts · checklistProgress.ts · checklistCursorFailoverPrompt.ts · dailyOpsChecklistCatalog.ts',
  roles: ['rocket', 'satellite', 'engineer', 'ground', 'vendor'] as const,
  envColumns: ['dev', 'stg', 'prod'] as const,
  verdict: 'GO | NO-GO',
  workflow: ['discover', 'remediate', 'verify', 'clear'] as const,
  rules: [
    'Ops loop → Execution Agent panel → Fleet board: viewer env + GO/NO-GO + circle Discover→Remediate→Verify→Clear + one primary CTA; fix progress sits directly under Ops loop.',
    'Fleet board is health ground truth; Help · reference is a muted collapsed entry inside Ops loop (deep links only — not a footer row or phase strip).',
    'At most one primary CTA — no dual VerdictBar + WorkflowBar buttons.',
    'Stage-driven single primary CTA: Discover → AI Check (daily-ops-checklist-run); Remediate → blocker-typed CTA (see below); Verify → Re-probe; Clear → Clear queue / Run daily check.',
    'Remediate primary CTA follows highest-priority Checklist×Fleet blocker (fail>degraded; manual/observe before AI-fixable at same severity): git-bridge dirty → Propose commit (git-dirty-remediate, approval required); other full_auto/semi_auto+scope → Agent Fix / AI Fix · Operator Plan; manual/observe/null-scope → Manual next step (no sparkles AI Fix); mixed → primary manual + outline Also: Propose commit (git dirty) / Also: AI Fix (sibling).',
    'Engineer CRITICAL: fleet cell Agent Fix stays disabled; git dirty uses Propose commit only (stash removed — causes code loss); other AI-fixable → Operator Plan; else Manual next (e.g. Mac seat). Full Operator Plane page is escape hatch only.',
    'Full Operator Plane page is secondary escape (MCP / host deploy / self-smoke) — not the default primary CTA.',
    'Operator Dock × L-1 Host: head shows dual heartbeat (Host · P✓ S✓) + optional Deploy · role… read-only; host/deploy CTA deep-links to Launch Desk → Agent. Operator Plane button keeps heartbeats / smoke / MCP / AI Fix. Console Mac chips tag Primary/Standby from bridge runners only. Publish SSOT = Launch Agent AI Launch Agent (manual Update = escape).',
    'Agent Fix running: CTA becomes Expand dock; shell Operator Dock (Agent slot) is SSOT for live feed/approvals; in-page panel is one-line summary only; Agent Desk is archive (explicit Open in Agent Desk).',
    'Mission Launch · Launch Live: telemetry-only for Agent (one-line + Expand dock); Pipeline + Post-deploy stay in-page; Commit & push / approvals only in Operator Dock Agent slot (same Daily Ops protocol).',
    'Mission Launch · Launch Plugin (third lane): Detect → Approve → Install → Verify → Live check; executor make install-ib-gateway (not Tekton); Gallery ≠ Publish; AI Launch Plugin ambient agent; checklist NO-GO → Agent Fix (scope plugin-runtime-remediate, repair ≠ publish); approvals in Operator Dock; D10 quotes-only.',
    'Launch Desk · Launch Agent (L-1): Detect → Approve → Deploy → Verify → Live check; executor POST /api/v1/agent/deploy → deploy_mac_mini.sh (not Tekton; never in-cluster); AI Launch Agent ambient scope agent-launch; approvals in Operator Dock; manual Update on AgentHostDeployPanel = escape only.',
    'Plugin repo changes: prefer Launch Plugin lane as primary publish path — do not treat Phase G as rocket-only side quest.',
    'Verify = re-probe fleet after Agent Fix; Clear = fleetClear + operate queue open===0.',
    'W3 Auto-remediate default OFF — Assisted “Ready to Agent Fix” hint only; never auto-trigger.',
    'Checklist AI Check (scope daily-ops-checklist-run): Ops loop owns the green primary; Checklist header keeps muted Re-check + Ask for AI secondary — never two magic-wand primaries.',
    'Naming lock: AI Check ≠ Fleet cell Fix ≠ Operator Plane Fix (operator-plane-remediate) ≠ Git Dirty Remediate (git-dirty-remediate).',
    'Operate Queue: Close (verified) requires evidence + job/post-fix gates; Dismiss allows stale/resolved close with evidence without those gates.',
    'Action column live progress from last_dispatch + jobs; Skip · dedup 24h / Skip · D10 never imply in-progress.',
    'Notes fleet≠agent when agent signals disagree with fleet polarity; lamps remain fleet-sourced (no full merge).',
    'Action: auto job click → Agent Desk; queue → Agent Desk Operate handoffs; Queued (busy) when concurrent auto demote.',
    'Row Fix: per non-ok full_auto/semi_auto item with fixScope → ambient startRemediation(scope); observe/manual null-scope → Ask for AI only.',
    'Ask for AI: copy Cursor IDE failover pack (header all non-ok or per-row) — paste into Cursor Agent when Ops Agent path fails or is blocked.',
    'Trust boundary: checklist-run is L0 probe; actuation only via existing remediation scopes + Operate Queue. Concurrent auto limit 1; 24h dedup per item.',
    'D10: IB feed fixCapability=observe — never auto-dispatch (skip).',
    'Standards taxonomy: Control/GitOps/Release · Edge/APIs/Data · Automation/Mac seat · Cluster · Feeds/Tooling.',
    'Board shows group rollups (ok/total); leaf standards only when failing; full grouped list in Detail.',
    'Any non-green required standard ⇒ cell NO-GO; fleet GO only when every scored cell is GO.',
    'Mac seat is Engineer — not a fourth env column. Prod/STG viewer: Mac seat informational only.',
    'Viewer seat: OPS_VIEWER_ENV > (in-cluster only) clusters.yaml viewer_env > dev.',
    'Git dirty is Owner WIP — Fleet Engineer cell and Control Room missionSignals.agentSignal stay ok when Bridge is reachable; dirty does NOT enter ROOM POSTURE / Mission CAUTION. Cross-ref: consoleSeatCatalog.ts.',
    'D10 live trading remains BLOCKED.',
  ],
  /** Acceptance checkpoints (Fleet Desk QA + Ops loop). */
  acceptance: [
    'Q1: Structural unavailable (Rocket DEV pull) does not NO-GO — GO when scored cells are ok.',
    'Q2: Local (no KUBERNETES_SERVICE_HOST) → DEV; Prod in-cluster → yaml viewer_env=prod; OPS_VIEWER_ENV always overrides.',
    'Q3: Operate Clear ≠ fleet clear when fleetClear=false; fleetClear follows scored verdict.',
    'Q4: Daily Ops Agent Fix error surfaces without Launch Pad; Ops loop Remediate CTA aligns with pickFleetFixCell.',
    'Q5: Satellite scopes do not cross — stg=deliver-stg-recover, prod/dev=cluster_issues_full_auto.',
    'Q6: Compact group rollups on board; Detail below board lists standards by group; no GET API strings.',
    'Q7: Single Ops loop strip (no dual Verdict+Workflow cards); circle stepper shows done/active/blocked; at most one primary CTA.',
    'Q8: Engineer CRITICAL → blocker-typed CTA (manual-next for seat/manual/observe; Propose commit for git dirty; AI Fix · Operator Plan when other AI-fixable); Full page → secondary; canOperate gates; D10 copy on remediate.',
    'Q9: Discover strip primary is AI Check; Clear idle offers Run daily check (same Checklist probe); Agent Fix in flight expands Operator Dock (not forced Agent Desk tab); in-page summary + Expand dock.',
    'Q10: Strip AI Check / Checklist Re-check start daily-ops-checklist-run; git dirty handoff uses git-dirty-remediate; Operator Plane Fix stays separate; no dual green AI Check+AI Fix.',
    'Q11: Notes show fleet≠agent on polarity mismatch; Action opens job/queue; Queued (busy) when auto demoted by concurrency; Queue Dismiss available with evidence for stale/resolved.',
    'Q12: Non-ok row shows Fix (Ops Agent when fixScope) and/or Ask for AI (Cursor failover copy); header Ask for AI packs all non-ok items.',
    'Q13: Dirty details panel lists repo/files/+N/−M from git-bridge via agent/bridge; Propose commit never auto-commits without approval card.',
    'Q14: Bridge ok + dirty_repos>0 → Fleet + Mission agentSignal ok (detail may note dirty); ROOM POSTURE not CAUTION from dirty alone — see consoleSeatCatalog.ts.',
  ],
} as const

/** Mission Signal Phase 6 — Flight Director daily ops (briefing digest + trust overrides). */
export const FLIGHT_DIRECTOR_OPS_STEPS: FlightDirectorStep[] = [
  {
    step: '1. Daily digest',
    tool: 'get_flight_director_snapshot',
    required: true,
    detail: 'Agent Briefing → Flight Director 24h panel; review completed/failed/escalations before opening Cursor.',
  },
  {
    step: '2. Trust override',
    tool: 'PUT /api/v1/agent/governance/trust-overrides/{skill_id}',
    required: true,
    detail: 'Owner sets L0/L1/L2 per skill; accept_promotion / apply_demotion actions apply earned autonomy suggestions.',
  },
  {
    step: '3. Verify matrix',
    tool: 'get_trust_matrix',
    required: true,
    detail: 'data_source includes owner_overrides after Owner actuation.',
  },
]

/** Mission Signal Phase 7 — Program closure (maintenance mode). */
export const MISSION_SIGNAL_CLOSURE_STEPS: FlightDirectorStep[] = [
  {
    step: '1. Program status',
    tool: 'Control Room → Mission Signal strip',
    required: true,
    detail: 'P1–P6 show ✓ when Owner signed each phase via Briefing Session · mission-signal (visible on Delivery Board catalog); all six unlock Phase 7 closure.',
  },
  {
    step: '2. Agent Protocol reference',
    tool: 'Agent Protocol',
    required: true,
    detail: 'Phases 1–6 playbooks + this closure section — single Mission Signal arc for Agent modes.',
  },
  {
    step: '3. Maintenance mode',
    tool: 'Owner sign-off Phase 7',
    required: true,
    detail: 'After MISSION SIGNAL PROGRAM COMPLETE — signal fixes are event-driven patches, not new program phases.',
  },
]

export type OpeningPrompt = {
  mode: string
  example: string
}

export const OPENING_PROMPTS: OpeningPrompt[] = [
  { mode: 'Product', example: 'Mode: Product. Task: migrate LivePage SSE hook only. No API or infra changes.' },
  {
    mode: 'Ops',
    example:
      'Mode: Ops. Task: audit Bifrost firewall vs FIREWALL_RULES; L1 apply only if drift. Spine D9 Session v2. No FE edits.',
  },
  { mode: 'Promote', example: 'Mode: Promote. Task: assess if prod cutover is allowed; list blockers from spine + matrix.' },
  {
    mode: 'Research',
    example:
      'Mode: Research. Task: extend bifrost-research dbt/engines on bifrost_golden_source only. No Trade DB writes. D10 blocked.',
  },
]

export const MODE_SELECTION_HINTS = [
  'focus.blocker or flywheel_primary === B → Ops',
  'tracks.infra / network-upgrade-* stream / VLAN-firewall task → Ops (network playbooks + D9 Session v2)',
  'Promote bay or cutover milestone → Promote',
  'dbt / dw_stock.* / features.* / Golden Source OLAP / bifrost-research → Research (D13)',
  'Otherwise → Product',
]

// ---------------------------------------------------------------------------
// Three-layer Agent architecture (from Vision)
// ---------------------------------------------------------------------------

export type AgentLayerDef = {
  layer: string
  persona: string
  scope: string
  cursorRole: string
  k8sRole: string
  forbiddenActions: string
}

export const AGENT_LAYERS: AgentLayerDef[] = [
  {
    layer: 'Dev Agent',
    persona: 'Senior engineer assisting Owner in coding, testing, and release',
    scope: 'Source code, tests, CI/CD pipelines, config YAML, documentation',
    cursorRole: 'Agent mode — full repo read/write + terminal + MCP tools',
    k8sRole: 'Trigger Tekton pipelines (build/test/deliver), read Pod logs, read ArgoCD status',
    forbiddenActions: 'No production cluster mutations; no trade commands; no direct DB DDL in prod',
  },
  {
    layer: 'Ops Agent',
    persona: 'SRE/DevOps engineer assisting Owner in runtime monitoring and remediation',
    scope: 'K3s cluster state, Pod health, metrics, alerts, deployment rollouts, scaling',
    cursorRole: 'Agent mode — read cluster state via MCP + limited L1/L2 actuation',
    k8sRole: 'rollout restart, scale, drain, ArgoCD sync/rollback, Prometheus query, log tail',
    forbiddenActions: 'No Trade business decisions; no order placement; no strategy config direct-write',
  },
  {
    layer: 'Business Agent',
    persona: 'Market analyst providing strategy insights and risk monitoring (read-only)',
    scope: 'Trade API read endpoints — positions, Greeks, SEPA, market data, strategy status',
    cursorRole: 'Ask mode (read-only) — fetches via mcp-trade-api, generates analysis',
    k8sRole: 'None — accesses Trade API HTTP endpoints only, never touches cluster',
    forbiddenActions: 'No write operations of any kind; no order placement; no config changes; advisory only',
  },
]

export type AgentEscalationRule = {
  from: string
  to: string
  trigger: string
  example: string
}

export const AGENT_ESCALATION: AgentEscalationRule[] = [
  { from: 'Dev Agent', to: 'Ops Agent', trigger: 'Deployment failure needs runtime diagnosis', example: 'Build passed but Pod CrashLoopBackOff → Ops Agent inspects logs + events' },
  { from: 'Ops Agent', to: 'Dev Agent', trigger: 'Root cause is a code bug, not infra', example: 'OOM caused by new feature memory leak → Dev Agent opens fix PR' },
  { from: 'Release Agent', to: 'Release-Fix Agent', trigger: 'Gate/build/deploy failure caused by code/config bug', example: 'STG gate returns 502 due to read-only FS write → Release-Fix Agent patches store.go + manifest, commits, pushes → Release Agent retries gate' },
  { from: 'Release-Fix Agent', to: 'IDE Agent (Owner)', trigger: 'Auto-fix too complex or too risky', example: 'Fix requires multi-repo architectural change → Release-Fix reports analysis, Owner uses IDE Agent' },
  { from: 'Business Agent', to: 'Dev Agent', trigger: 'Strategy suggestion requires code change', example: 'Analysis suggests new Gate parameter → Dev Agent prepares PR (Owner approves)' },
  { from: 'Any Agent', to: 'Owner', trigger: 'L2+ action or ambiguous situation', example: 'Ops Agent wants to rollback prod → confirms with Owner before executing' },
]

export type AgentModelGuidance = {
  task: string
  recommendedModel: string
  reason: string
}

export const AGENT_MODEL_GUIDANCE: AgentModelGuidance[] = [
  { task: 'Complex refactoring / architecture', recommendedModel: 'claude-opus-4 (xhigh thinking)', reason: 'Deep reasoning for multi-file changes' },
  { task: 'Standard feature development', recommendedModel: 'claude-sonnet-4 (medium thinking)', reason: 'Good balance of speed and quality' },
  { task: 'Quick fixes / formatting', recommendedModel: 'composer-2.5-fast', reason: 'Low-latency for simple edits' },
  { task: 'Ops diagnosis / metrics analysis', recommendedModel: 'claude-opus-4 (high thinking)', reason: 'Complex reasoning over live system state' },
  { task: 'Business analysis / market research', recommendedModel: 'claude-sonnet-4 or gpt-5.5', reason: 'Broad knowledge for financial analysis' },
]

/** Vision V2 — Dev Agent closed-loop (push → Tekton → STG verify). */
export const DEV_AGENT_CLOSED_LOOP = {
  prePushScript: 'bifrost-trade-frontend/scripts/agent-pre-push.sh',
  stgPipeline: 'bifrost-deliver-stg',
  prodPipeline: 'bifrost-deliver-prod',
  stgSmoke: 'GET /api/v1/delivery/stg/smoke',
  releaseGate: 'GET /api/v1/promote/release-gate',
  releaseState: 'GET /api/v1/promote/release-state',
  catalog: 'console/src/lib/architecture/devAgentLoopCatalog.ts',
  mcpTools: {
    deploy: 'start_pipeline_run (name, revision?)',
    queryState: 'get_release_state (tier?)',
    queryGate: 'get_release_gate (tier?)',
    runGate: 'run_release_gate (tier?) — admin',
    smoke: 'get_stg_smoke',
    revisions: 'get_delivery_revisions (repos?)',
    /** HTTP only today — constellation path impact (Wave 4). */
    compare: 'GET /api/v1/delivery/compare?repo&from&to (read-only)',
    runLogs: 'get_delivery_run_logs (run_id)',
    gitopsApps: 'get_gitops_apps',
    gitopsSync: 'gitops_sync_app (name)',
  },
} as const

/** Ops Desk — Patrol (scheduled health skills via Cursor SDK nightshift). Distinct from Hermes Analysis. */
export const PATROL_AGENT = {
  surface: 'Ops Desk · Patrol skills (Console Task Mode ops)',
  runtime:
    'L0: platform-api local probe (same GET routes as Platform MCP, live evidence). L1+: Cursor SDK HTTP POST /v1/agents when PATROL_DISPATCH=live|cursor. Force all-cloud with PATROL_DISPATCH=cursor (LAN MCP usually unreachable).',
  trigger: 'cron goroutine in platform-api + manual POST /api/v1/patrol/trigger/{id}',
  trust: {
    L0: 'read',
    L1: 'manual write & cron escalate',
    L2: 'reserved escalate',
  },
  cost: 'Cursor subscription monthly quota (PATROL_DISPATCH=stub|live)',
  skillsAPI: 'GET /api/v1/patrol/skills',
  runsAPI: 'GET /api/v1/patrol/runs',
  dispatchEnv: 'PATROL_DISPATCH=stub|live',
  distinctFrom:
    'Hermes / Analysis Desk is premium analysis (Chat UI + First Task; Trade later). Patrol is not Hermes metered API. D10 blocked — no trading actuation.',
} as const

/** Analysis Desk V1 — Nous Hermes premium analysis (read-only). */
export const HERMES_ANALYSIS_DESK = {
  surface: 'Analysis Desk · Analysis Workspace / Insight Log / Hermes Status',
  chatUI: 'http://192.168.10.50:9119/chat',
  readinessAPI: 'GET /api/v1/agent/hermes/readiness',
  healthAPI: 'GET /api/v1/agent/hermes/health',
  insightsAPI: 'GET /api/v1/hermes/insights?limit=50',
  firstTaskAPI: 'POST /api/v1/hermes/run-first-task',
  d10: 'Analysis is read-only. No trading actuation.',
} as const

/** Vision V3 — Ops Agent L1/L2 (Alertmanager → MCP actuation + audit). */
export const OPS_AGENT_CLOSED_LOOP = {
  webhook: 'POST /api/v1/ops-agent/alertmanager',
  alertmanagerConfig: 'config/ops-agent-alertmanager.yaml',
  cursorBridges: 'config/cursor-mcp-bridges.json',
  mcpStatus: 'GET /api/v1/mcp/status',
  auditLog: 'GET /api/v1/audit',
  catalog: 'console/src/lib/architecture/opsAgentLoopCatalog.ts',
} as const

/** Vision V4 — Business Agent read-only (9 Trade API domains + daily brief). */
export const BUSINESS_AGENT_CLOSED_LOOP = {
  catalogAPI: 'GET /api/v1/trade-agent/catalog',
  domainsAPI: 'GET /api/v1/trade-agent/domains',
  domainsConfig: 'config/trade-api-domains.yaml',
  briefSchedule: 'config/business-agent-brief-schedule.yaml',
  cursorMcp: 'config/cursor-mcp-trade.json',
  mcpServer: 'mcp/trade/src/index.ts',
  catalog: 'console/src/lib/architecture/businessAgentLoopCatalog.ts',
} as const

/** Vision V5 — Full convergence (Dev + Ops + Business unified). */
export const CONVERGENCE_CLOSED_LOOP = {
  unifiedMcp: 'config/cursor-unified-mcp.json',
  ollama: 'config/ollama-agent.yaml',
  feedbackLoop: 'config/convergence-feedback-loop.yaml',
  catalog: 'console/src/lib/architecture/convergenceLoopCatalog.ts',
  prerequisites: [
    'vision-v1-dev-topology',
    'vision-s3-briefing-alignment',
    'vision-v2-dev-agent',
    'vision-v3-ops-agent',
    'vision-v4-business-agent',
  ],
} as const

/** Build LLM-optimized text for the Agent Protocol page. */
export function buildAgentProtocolLlmPack(): string {
  const lines: string[] = [
    '# Bifrost Ops — Agent Protocol (Modes, Three-Layer Agents & Context Packs)',
    `# Source: ${AGENT_PROTOCOL_SOURCE} v${AGENT_PROTOCOL_VERSION}`,
    '',
    '## Agent modes (per-session intent)',
    ...AGENT_MODES.map(m =>
      `- **${m.mode}** [${m.flywheel}]: UI=${m.defaultUI} | May: ${m.agentMay} | Must-not: ${m.agentMustNot}`),
    '',
    buildSystemDomainLlmPack(),
    '',
    buildConsoleSeatLlmPack(),
    '',
    '## Three-layer Agent architecture',
    ...AGENT_LAYERS.map(a =>
      `- **${a.layer}** (${a.persona}): scope=${a.scope} | cursor=${a.cursorRole} | k8s=${a.k8sRole} | DENY: ${a.forbiddenActions}`),
    '',
    '## Agent escalation',
    ...AGENT_ESCALATION.map(e => `- ${e.from} → ${e.to}: ${e.trigger} (e.g. ${e.example})`),
    '',
    '## Model guidance',
    ...AGENT_MODEL_GUIDANCE.map(m => `- ${m.task}: ${m.recommendedModel} — ${m.reason}`),
    '',
    '## Mode selection hints',
    ...MODE_SELECTION_HINTS.map(h => `- ${h}`),
    '',
    '## Control Room context pack buttons',
    ...CONTEXT_PACK_BUTTONS.map(b => `- **${b.button}**: ${b.contents}`),
    '',
    '## Context pack layers (session startup order)',
    ...CONTEXT_PACK_LAYERS.map(l => `${l.order}. **${l.name}** — ${l.description}`),
    '',
    '## Forbidden actions (all modes)',
    ...FORBIDDEN_ACTIONS.map(f => `- ${f.action} [${f.scope}]`),
    '',
    '## Wave 3 P0 decisions (locked — spine D11/D12)',
    ...WAVE3_P0_DECISIONS.flatMap(d => [
      `- **${d.id}** ${d.topic}: ${d.rule}`,
      ...d.wave3Deliverables.map(x => `  - ${x}`),
    ]),
    '',
    '## Network diagnostic playbooks (firewall / zone — Network Governance Phase 4)',
    `- Audit: \`${NETWORK_DIAGNOSTIC_MCP.auditScript}\``,
    `- Catalog: \`${NETWORK_DIAGNOSTIC_MCP.firewallCatalog}\``,
    `- Spine: \`${NETWORK_DIAGNOSTIC_MCP.spineDecision}\``,
    `- Future probe: \`${NETWORK_DIAGNOSTIC_MCP.futureApi}\``,
    ...NETWORK_DIAGNOSTIC_PLAYBOOKS.map(
      p =>
        `- **${p.classification}** [${p.autonomy}]: ${p.trigger} → ${p.agentAction} | Must-not: ${p.mustNot}`,
    ),
    '',
    '## Mission diagnostic playbooks (verify_payload)',
    `- MCP: \`${MISSION_DIAGNOSTIC_MCP.verifyPayload}\``,
    ...MISSION_DIAGNOSTIC_PLAYBOOKS.map(
      p =>
        `- **${p.classification}** [${p.autonomy}]: ${p.trigger} → ${p.agentAction} | Must-not: ${p.mustNot}`,
    ),
    '',
    '## Cluster remediation playbooks (delivery / infra L1)',
    ...CLUSTER_REMEDIATION_PLAYBOOKS.map(
      p =>
        `- **${p.id}** — ${p.title} [${p.autonomy}]: ${p.trigger} → ${p.agentAction} | Must-not: ${p.mustNot} | MCP: ${p.mcpTools.join(', ')}`,
    ),
    '',
    '## Mission post-fix validation loop (Autonomous Loop)',
    `- MCP: \`${MISSION_DIAGNOSTIC_MCP.verifyMissionSnapshot}\` — required before closing remediation`,
    ...MISSION_POST_FIX_LOOP.map(s => `- ${s.step}: \`${s.tool}\` — ${s.detail}`),
    '- Runner emits event kind=post_fix_verification on job complete; Control Room banner shows reprobe result.',
    '',
    '## Mission Signal program references (P4–P7 — program history, not per-session protocol)',
    MISSION_SIGNAL_PROGRAM_REFERENCE,
    '',
    '## Example opening prompts',
    ...OPENING_PROMPTS.map(p => `- [${p.mode}] "${p.example}"`),
    '',
    '## Trade DEV Inner Loop (Program trade-dev-inner-loop)',
    '- Contract: `bifrost-trade-infra/docs/TRADE_DEV_INNER_LOOP.md` · catalog `tradeDevInnerLoopCatalog.ts`',
    '- D-IL1: UI accept = Vite :5173 → 192.168.10.73:30882; Prod refresh ≠ daily QA (D-IL4 L2 only)',
    '- D-IL2: MCP get_data_freshness (+ last_clone_at ≤7d) → Owner trigger_data_clone → bifrost_dev → bounce',
    '- D-IL3: Live = redis-ib ExternalName + probe_dev_live_readiness; never dump redis-live-prod → redis-dev',
    '- Playbook id: `trade-dev-inner-loop` in CLUSTER_REMEDIATION_PLAYBOOKS',
    '',
    '## Dev Agent closed loop (Vision V2)',
    `- Pre-push: \`${DEV_AGENT_CLOSED_LOOP.prePushScript}\``,
    `- STG pipeline: \`${DEV_AGENT_CLOSED_LOOP.stgPipeline}\` via Console Delivery or MCP \`start_pipeline_run\``,
    `- Verify: \`${DEV_AGENT_CLOSED_LOOP.stgSmoke}\` or MCP \`get_stg_smoke\``,
    `- Release state: \`${DEV_AGENT_CLOSED_LOOP.releaseState}\` or MCP \`get_release_state\` → next_action guidance`,
    `- Promote: \`${DEV_AGENT_CLOSED_LOOP.releaseGate}\` or MCP \`run_release_gate\` before deliver-prod`,
    `- Available tags: MCP \`get_delivery_revisions\` — select revision for deploys`,
    `- Catalog: \`${DEV_AGENT_CLOSED_LOOP.catalog}\``,
    '',
    '## Three Desks (Build / Ops / Analysis)',
    '- Rail: System + Build + Ops + Analysis. Legacy daily-ops / mission-launch / patrol → ops.',
    '- Engineer Partner: Build Desk / Launch Desk / Ops Desk / Analysis Desk. Launch Desk: Rocket → Satellite(Trade, Research instruments) → Plugin → Agent (L-1 Mac Mini host publish). Ops Desk subgroups: Operate (Queue) · Patrol (Patrol + Patrol Log) · trail (Operator Plane / Trust / Capability). Queue tab id `queue` (`#agent-desk` alias).',
    '',
    '## Data husbandry (batch Golden Source)',
    '- Same problem class → Dagster Data Assets for **all** Golden Source husbandry periodic ignition (Massive full slots / Flex / Research day+short+agents+maintenance) via multi-schedule.',
    '- Plugin workers + ops_jobs.* remain executors; Dagster schedules/gates via HTTP enqueue only.',
    '- Outside Dagster: IB Gateway / IB Client / realtime WS Deployments only. Catalog: `dataHusbandryCatalog.ts`.',
    '- Ground truth = freshness/coverage/signal-health asof; K8s Job Complete ≠ success; void ≠ fail; Flex enqueue fail-closed when source=none.',
    '- Migrate one Cron → suspend before enabling Dagster schedule (no dual-write). Verify: `make verify-husbandry-schedulers`.',
    '- Research Engine observe = feedstock / batch (orchestration/status) / product_asof; sidebar Research icon follows research_olap only.',
    '',
    '## Patrol (Ops Desk — Cursor SDK nightshift)',
    `- Surface: ${PATROL_AGENT.surface}`,
    `- Runtime: ${PATROL_AGENT.runtime}`,
    `- Trigger: ${PATROL_AGENT.trigger}`,
    `- Trust: L0 ${PATROL_AGENT.trust.L0} / L1 ${PATROL_AGENT.trust.L1} / L2 ${PATROL_AGENT.trust.L2}`,
    `- Cost: ${PATROL_AGENT.cost}`,
    `- Skills: \`${PATROL_AGENT.skillsAPI}\` · Runs: \`${PATROL_AGENT.runsAPI}\``,
    `- Distinct from Hermes: ${PATROL_AGENT.distinctFrom}`,
    '',
    '## Hermes (Analysis Desk — premium analysis)',
    `- Surface: ${HERMES_ANALYSIS_DESK.surface}`,
    `- Chat UI: ${HERMES_ANALYSIS_DESK.chatUI}`,
    `- Readiness: \`${HERMES_ANALYSIS_DESK.readinessAPI}\` · Health: \`${HERMES_ANALYSIS_DESK.healthAPI}\``,
    `- Insights: \`${HERMES_ANALYSIS_DESK.insightsAPI}\` · First Task: \`${HERMES_ANALYSIS_DESK.firstTaskAPI}\``,
    `- D10: ${HERMES_ANALYSIS_DESK.d10}`,
    '',
    '## Ops Agent closed loop (Vision V3)',
    `- Webhook: \`${OPS_AGENT_CLOSED_LOOP.webhook}\``,
    `- Alertmanager: \`${OPS_AGENT_CLOSED_LOOP.alertmanagerConfig}\``,
    `- Cursor MCP bridges: \`${OPS_AGENT_CLOSED_LOOP.cursorBridges}\``,
    `- MCP status: \`${OPS_AGENT_CLOSED_LOOP.mcpStatus}\``,
    `- Audit: \`${OPS_AGENT_CLOSED_LOOP.auditLog}\``,
    `- Catalog: \`${OPS_AGENT_CLOSED_LOOP.catalog}\``,
    '',
    '## Business Agent closed loop (Vision V4)',
    `- Catalog API: \`${BUSINESS_AGENT_CLOSED_LOOP.catalogAPI}\``,
    `- Domains: \`${BUSINESS_AGENT_CLOSED_LOOP.domainsConfig}\` (9 read-only domains)`,
    `- Daily brief: \`${BUSINESS_AGENT_CLOSED_LOOP.briefSchedule}\``,
    `- Cursor MCP: \`${BUSINESS_AGENT_CLOSED_LOOP.cursorMcp}\``,
    `- MCP server: \`${BUSINESS_AGENT_CLOSED_LOOP.mcpServer}\``,
    `- Catalog: \`${BUSINESS_AGENT_CLOSED_LOOP.catalog}\``,
    '',
    '## Full convergence (Vision V5)',
    `- Unified MCP: \`${CONVERGENCE_CLOSED_LOOP.unifiedMcp}\``,
    `- Ollama (LAN): \`${CONVERGENCE_CLOSED_LOOP.ollama}\``,
    `- Feedback loop: \`${CONVERGENCE_CLOSED_LOOP.feedbackLoop}\``,
    `- Catalog: \`${CONVERGENCE_CLOSED_LOOP.catalog}\``,
    `- Prerequisites: ${CONVERGENCE_CLOSED_LOOP.prerequisites.map(p => `\`${p}\``).join(', ')}`,
  ]
  return lines.join('\n')
}
