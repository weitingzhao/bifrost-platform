/**
 * Agent Protocol catalog — Agent Modes, context packs, forbidden actions.
 *
 * Authoritative source for Ops Console → Architecture → Agent Protocol.
 * Single source of truth — do not duplicate elsewhere.
 */

export const AGENT_PROTOCOL_VERSION = '2026-06-19'
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
    defaultUI: 'Observe → Diagnosis · Observe → Scheduling (Placement) · Operate → Cluster ops · Observe → Audit',
    agentMay: 'Single-variable release checks, sign-off docs',
    agentMustNot: 'Skip blockers (D1, gate), mix API + FE in one change',
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
  { order: 0, name: 'Agent Briefing', description: 'Ops Console → Agent Briefing → pick intent → Generate → Copy all (new sessions)' },
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
    '## Example opening prompts',
    ...OPENING_PROMPTS.map(p => `- [${p.mode}] "${p.example}"`),
  ]
  return lines.join('\n')
}
