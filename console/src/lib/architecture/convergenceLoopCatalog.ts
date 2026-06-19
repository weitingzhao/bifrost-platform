/**
 * Vision V5 — Full convergence contract (Dev + Ops + Business merged).
 *
 * Authoritative for Ops Console → Architecture → Vision (V5 gate).
 */

export const CONVERGENCE_LOOP_VERSION = '2026-06-19'
export const CONVERGENCE_LOOP_SOURCE = 'console/src/lib/architecture/convergenceLoopCatalog.ts'

export const CONVERGENCE_LOOP_STATEMENT =
  'Single Cursor window: Dev Agent (code → Tekton → STG) + Ops Agent (L1/L2 actuation) + ' +
  'Business Agent (read-only trade intelligence). Ollama for sensitive strategy analysis on LAN. ' +
  'Trade insights feed platform improvements; Gate param changes → L3 PR → Owner approve.'

export type ConvergenceLoopStep = {
  order: number
  phase: string
  agents: string
  action: string
  verify: string
}

export const CONVERGENCE_LOOP_STEPS: ConvergenceLoopStep[] = [
  {
    order: 1,
    phase: 'Unified IDE',
    agents: 'Dev + Ops + Business',
    action: 'config/cursor-unified-mcp.json — platform + trade MCP servers in one Cursor window',
    verify: 'All three agent catalogs reachable from single session',
  },
  {
    order: 2,
    phase: 'Dev flywheel',
    agents: 'Dev Agent',
    action: 'Pre-push → Tekton deliver-stg → smoke → report (V2 loop)',
    verify: 'vision-v2-dev-agent SIGNED',
  },
  {
    order: 3,
    phase: 'Ops flywheel',
    agents: 'Ops Agent',
    action: 'Alert → MCP L1 fix → audit; L2 Owner confirm (V3 loop)',
    verify: 'vision-v3-ops-agent SIGNED',
  },
  {
    order: 4,
    phase: 'Business flywheel',
    agents: 'Business Agent',
    action: 'Daily brief + ad-hoc Q&A via mcp-trade-api (V4 loop)',
    verify: 'vision-v4-business-agent SIGNED',
  },
  {
    order: 5,
    phase: 'Sensitive analysis',
    agents: 'Business Agent',
    action: 'Ollama local inference per ollama-agent.yaml — strategy/Gate analysis stays on LAN',
    verify: 'No egress to cloud for strategy-sensitive prompts',
  },
  {
    order: 6,
    phase: 'Feedback loop',
    agents: 'Any Agent → Platform',
    action: 'Trade insights → platform improvements per convergence-feedback-loop.yaml',
    verify: 'Agent Briefing + spine updated from live matrix/trade state',
  },
  {
    order: 7,
    phase: 'L3 governance',
    agents: 'Dev Agent',
    action: 'Gate parameter / structural changes → Gitea PR → Owner merge → ArgoCD',
    verify: 'VISION_BOUNDARIES L3 requires Owner PR approval',
  },
]

export const CONVERGENCE_CONFIG = {
  unifiedMcp: 'config/cursor-unified-mcp.json',
  ollama: 'config/ollama-agent.yaml',
  feedbackLoop: 'config/convergence-feedback-loop.yaml',
  devCatalog: 'console/src/lib/architecture/devAgentLoopCatalog.ts',
  opsCatalog: 'console/src/lib/architecture/opsAgentLoopCatalog.ts',
  businessCatalog: 'console/src/lib/architecture/businessAgentLoopCatalog.ts',
} as const

export const CONVERGENCE_PREREQUISITE_MILESTONES = [
  'vision-v1-dev-topology',
  'vision-s3-briefing-alignment',
  'vision-v2-dev-agent',
  'vision-v3-ops-agent',
  'vision-v4-business-agent',
] as const

export function buildConvergenceLoopLlmPack(): string {
  const lines = [
    '# Bifrost — Full Convergence (Vision V5)',
    `# Source: ${CONVERGENCE_LOOP_SOURCE} v${CONVERGENCE_LOOP_VERSION}`,
    '',
    CONVERGENCE_LOOP_STATEMENT,
    '',
    '## Convergence steps',
    ...CONVERGENCE_LOOP_STEPS.map(s =>
      `${s.order}. **${s.phase}** (${s.agents}): ${s.action} → verify: ${s.verify}`),
    '',
    '## Prerequisites (all SIGNED)',
    ...CONVERGENCE_PREREQUISITE_MILESTONES.map(id => `- \`${id}\``),
    '',
    '## Config',
    ...Object.entries(CONVERGENCE_CONFIG).map(([k, v]) => `- ${k}: \`${v}\``),
  ]
  return lines.join('\n')
}
