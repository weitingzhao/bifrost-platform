/**
 * Vision V4 — Business Agent read-only contract.
 *
 * Authoritative for Ops Console → Architecture → Vision (V4 gate)
 * and Agent Briefing Business-layer advisory discipline.
 */

export const BUSINESS_AGENT_LOOP_VERSION = '2026-06-19'
export const BUSINESS_AGENT_LOOP_SOURCE = 'console/src/lib/architecture/businessAgentLoopCatalog.ts'

export const BUSINESS_AGENT_LOOP_STATEMENT =
  'Business Agent reads 9 Trade API domains via mcp-trade-api (read-only). ' +
  'Scheduled pre/post-market briefs via Cursor SDK; ad-hoc Q&A in chat. ' +
  'Never writes orders, daemon_control, or strategy config — advisory only.'

export type TradeAPIDomain = {
  id: string
  port: number
  probePath: string
  readExamples: string
}

/** Nine Trade FastAPI domains — read-only probe + example endpoints. */
export const TRADE_API_DOMAINS: TradeAPIDomain[] = [
  { id: 'monitor', port: 8765, probePath: '/status', readExamples: 'GET /status, GET /operations' },
  { id: 'massive', port: 8766, probePath: '/health', readExamples: 'Polygon data queries' },
  { id: 'docs', port: 8767, probePath: '/health', readExamples: 'OpenAPI schema' },
  { id: 'ops', port: 8768, probePath: '/health', readExamples: 'Celery queue status' },
  { id: 'trading', port: 8769, probePath: '/health', readExamples: 'Positions, orders, history' },
  { id: 'strategy', port: 8770, probePath: '/health', readExamples: 'Instances, gates, structures' },
  { id: 'portfolio', port: 8771, probePath: '/health', readExamples: 'Accounts, Greeks aggregation' },
  { id: 'market', port: 8772, probePath: '/health', readExamples: 'Quotes SSE, ingest status' },
  { id: 'research', port: 8773, probePath: '/health', readExamples: 'SEPA, screener, backtest' },
]

export type BusinessAgentLoopStep = {
  order: number
  phase: string
  actor: string
  action: string
  verify: string
}

export const BUSINESS_AGENT_LOOP_STEPS: BusinessAgentLoopStep[] = [
  {
    order: 1,
    phase: 'Connect',
    actor: 'Owner IDE',
    action: 'config/cursor-mcp-trade.json → mcp-server-trade (stdio)',
    verify: 'GET /api/v1/trade-agent/catalog lists read tools',
  },
  {
    order: 2,
    phase: 'Probe',
    actor: 'Business Agent',
    action: 'MCP read tools across 9 Trade API domains via gateway',
    verify: 'STG/dev smoke 9/9 HTTP 200 on probe paths',
  },
  {
    order: 3,
    phase: 'Daily brief',
    actor: 'Cursor SDK schedule',
    action: 'Pre-market + post-market brief per business-agent-brief-schedule.yaml',
    verify: 'Brief references live matrix + trade domain health',
  },
  {
    order: 4,
    phase: 'Ad-hoc Q&A',
    actor: 'Business Agent',
    action: 'Owner asks in Cursor chat — "current IV?", "PnL today?", "daemon state?"',
    verify: 'Agent cites Trade API responses; no write calls',
  },
  {
    order: 5,
    phase: 'Escalate',
    actor: 'Business Agent → Dev/Ops',
    action: 'Code change → Dev Agent PR; infra issue → Ops Agent L1',
    verify: 'Agent Protocol escalation rules',
  },
  {
    order: 6,
    phase: 'Boundary',
    actor: 'MCP deny-list',
    action: 'No POST /control/*, no ib:operator:cmd, no daemon_control write',
    verify: 'mcpContractCatalog + VISION_BOUNDARIES enforced',
  },
]

export const BUSINESS_AGENT_CONFIG = {
  domains: 'config/trade-api-domains.yaml',
  briefSchedule: 'config/business-agent-brief-schedule.yaml',
  cursorMcp: 'config/cursor-mcp-trade.json',
  mcpServer: 'mcp/trade/src/index.ts',
  catalogAPI: 'GET /api/v1/trade-agent/catalog',
} as const

export function buildBusinessAgentLoopLlmPack(): string {
  const lines = [
    '# Bifrost — Business Agent Read-Only Loop (Vision V4)',
    `# Source: ${BUSINESS_AGENT_LOOP_SOURCE} v${BUSINESS_AGENT_LOOP_VERSION}`,
    '',
    BUSINESS_AGENT_LOOP_STATEMENT,
    '',
    '## Trade API domains (read-only)',
    ...TRADE_API_DOMAINS.map(d => `- **${d.id}** :${d.port} ${d.probePath} — ${d.readExamples}`),
    '',
    '## Loop steps',
    ...BUSINESS_AGENT_LOOP_STEPS.map(s =>
      `${s.order}. **${s.phase}** (${s.actor}): ${s.action} → verify: ${s.verify}`),
    '',
    '## Config',
    ...Object.entries(BUSINESS_AGENT_CONFIG).map(([k, v]) => `- ${k}: \`${v}\``),
  ]
  return lines.join('\n')
}
