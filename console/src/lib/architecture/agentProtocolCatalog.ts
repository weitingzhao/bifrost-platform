/**
 * Agent Protocol catalog — Agent Modes, context packs, forbidden actions.
 *
 * Authoritative source for Ops Console → Architecture → Agent Protocol.
 * Do not duplicate in docs/ — see docs/STAGING.md.
 */

export const AGENT_PROTOCOL_VERSION = '2026-06-15'
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
    defaultUI: 'Ops → Control Room / Promote',
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

/** Build LLM-optimized text for the Agent Protocol page. */
export function buildAgentProtocolLlmPack(): string {
  const lines: string[] = [
    '# Bifrost Ops — Agent Protocol (Modes & Context Packs)',
    `# Source: ${AGENT_PROTOCOL_SOURCE} v${AGENT_PROTOCOL_VERSION}`,
    '',
    '## Agent modes',
    ...AGENT_MODES.map(m =>
      `- **${m.mode}** [${m.flywheel}]: UI=${m.defaultUI} | May: ${m.agentMay} | Must-not: ${m.agentMustNot}`),
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
