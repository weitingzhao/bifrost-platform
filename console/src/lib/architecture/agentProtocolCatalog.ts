/**
 * Agent Protocol catalog — Agent Modes, context packs, forbidden actions.
 *
 * Authoritative source for Ops Console → Architecture → Agent Protocol.
 * Single source of truth — do not duplicate elsewhere.
 */

export const AGENT_PROTOCOL_VERSION = '2026-07-02'
export const AGENT_PROTOCOL_SOURCE = 'console/src/lib/architecture/agentProtocolCatalog.ts'

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
    defaultUI: 'bifrost-trade-frontend :5173',
    agentMay: 'Migrate pages, Dense UI, hooks, Legacy equivalence',
    agentMustNot: 'Change compose, prod cutover, K3s, API contracts',
  },
  {
    mode: 'Ops',
    flywheel: 'B — Runtime',
    defaultUI: 'Bifrost Ops Console :5180 → Control Room',
    agentMay: 'Read spine, matrix, topology; infra YAML; K3s planning',
    agentMustNot: 'Change trade page UI, expand FE scope',
  },
  {
    mode: 'Promote',
    flywheel: 'A + B coupling',
    defaultUI: 'Promote → Platform Release · Observe → Audit',
    agentMay: 'Query release-state, deploy via start_pipeline_run, run gates, verify smoke; follow next_action guidance',
    agentMustNot: 'Skip blockers (D1, gate), deploy PROD with different revision than STG, bypass admin role for gates',
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
  { action: 'daemon_control write via platform AI', scope: 'All modes' },
  { action: 'ib:operator:cmd RPC', scope: 'All modes' },
  { action: 'Editing bifrost-trader-engine/ (read-only reference)', scope: 'All modes' },
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
    detail: 'Confirm ready=true (Hermes gateway, LLM key, platform MCP agent tools). If blockers present, fix config before running task.',
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
    detail: 'P1–P6 tags show ✓ when Owner signed each phase panel; all six unlock Phase 7 closure.',
  },
  {
    step: '2. Doctrine reference',
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
  { mode: 'Ops', example: 'Mode: Ops. Task: verify prod matrix blockers; read spine D1. No frontend edits.' },
  { mode: 'Promote', example: 'Mode: Promote. Task: assess if prod cutover is allowed; list blockers from spine + matrix.' },
]

export const MODE_SELECTION_HINTS = [
  'focus.blocker or flywheel_primary === B → Ops',
  'Promote bay or cutover milestone → Promote',
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
  },
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
    '## Mission diagnostic playbooks (verify_payload)',
    `- MCP: \`${MISSION_DIAGNOSTIC_MCP.verifyPayload}\``,
    ...MISSION_DIAGNOSTIC_PLAYBOOKS.map(
      p =>
        `- **${p.classification}** [${p.autonomy}]: ${p.trigger} → ${p.agentAction} | Must-not: ${p.mustNot}`,
    ),
    '',
    '## Mission post-fix validation loop (Autonomous Loop)',
    `- MCP: \`${MISSION_DIAGNOSTIC_MCP.verifyMissionSnapshot}\` — required before closing remediation`,
    ...MISSION_POST_FIX_LOOP.map(s => `- ${s.step}: \`${s.tool}\` — ${s.detail}`),
    '- Runner emits event kind=post_fix_verification on job complete; Control Room banner shows reprobe result.',
    '',
    '## Hermes First Task (L0 — Mission Signal Phase 4)',
    `- Readiness: \`${HERMES_FIRST_TASK_MCP.readiness}\``,
    `- Prompt: \`${HERMES_FIRST_TASK_MCP.firstTask}\` — task id hermes-mission-health-l0`,
    ...HERMES_FIRST_TASK_STEPS.map(s => `- ${s.step}: \`${s.tool}\` — ${s.detail}`),
    '',
    '## Flight Director governance (Mission Signal Phase 5)',
    `- Performance: \`${FLIGHT_DIRECTOR_MCP.performance}\``,
    `- Trust: \`${FLIGHT_DIRECTOR_MCP.trustMatrix}\``,
    `- Snapshot: \`${FLIGHT_DIRECTOR_MCP.snapshot}\` (includes capability map + 24h briefing)`,
    ...FLIGHT_DIRECTOR_STEPS.map(s => `- ${s.step}: \`${s.tool}\` — ${s.detail}`),
    '- Data source: remediation runner JobStore (Hermes/GPU LLM path optional — bypass OK for governance KPIs).',
    '',
    '## Flight Director operations (Mission Signal Phase 6)',
    '- Trust override: PUT /api/v1/agent/governance/trust-overrides/{skill_id} — level or action accept_promotion / apply_demotion.',
    ...FLIGHT_DIRECTOR_OPS_STEPS.map(s => `- ${s.step}: \`${s.tool}\` — ${s.detail}`),
    '',
    '## Mission Signal Program closure (Phase 7)',
    '- Control Room status strip: P1 Signal Truth → P6 Flight Director Ops — all signed before program closure.',
    ...MISSION_SIGNAL_CLOSURE_STEPS.map(s => `- ${s.step}: \`${s.tool}\` — ${s.detail}`),
    '- After Owner sign-off: Mission Signal enters maintenance mode; new work is scoped patches, not program phases.',
    '',
    '## Example opening prompts',
    ...OPENING_PROMPTS.map(p => `- [${p.mode}] "${p.example}"`),
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
