/**
 * Vision V4 — Business Agent read-only contract.
 *
 * Authoritative for Ops Console → Governance → Vision (V4 gate)
 * and Agent Business-layer advisory discipline.
 */

export const BUSINESS_AGENT_LOOP_VERSION = '2026-06-19'
export const BUSINESS_AGENT_LOOP_SOURCE = 'console/src/lib/architecture/businessAgentLoopCatalog.ts'

export const BUSINESS_AGENT_LOOP_STATEMENT =
  'Business Agent reads 6 Trade API domains on 4 processes via mcp-trade-api (read-only). ' +
  'Scheduled pre/post-market briefs via Cursor SDK; ad-hoc Q&A in chat. ' +
  'Never writes orders, daemon Redis control, or strategy config — advisory only.'

export type TradeAPIDomain = {
  id: string
  /** The process's own gateway prefix (TD-55: one per process); the probe is /api/{prefix}{probePath}. */
  prefix: string
  /** The Deployment that answers. */
  process: string
  port: number
  probePath: string
  readExamples: string
}

/**
 * Trade API domains over four processes, one gateway prefix each (TD-55, Owner option B) —
 * mirrors config/trade-api-domains.yaml and api/internal/probe/trade_routes.go. docs and ops are
 * routers on api-monitor (:8765, /api/monitor); account is api-account (:8769, /api/account).
 */
export const TRADE_API_DOMAINS: TradeAPIDomain[] = [
  { id: 'monitor', prefix: 'monitor', process: 'api-monitor', port: 8765, probePath: '/status', readExamples: 'GET /status, GET /operations' },
  { id: 'docs', prefix: 'monitor', process: 'api-monitor', port: 8765, probePath: '/research/docs/health', readExamples: 'OpenAPI aggregate' },
  { id: 'ops', prefix: 'monitor', process: 'api-monitor', port: 8765, probePath: '/ops/health', readExamples: 'Executor mode, market-ingest services' },
  { id: 'account', prefix: 'account', process: 'api-account', port: 8769, probePath: '/health', readExamples: 'Executions, performance, trades, gate sets, model analysis' },
  { id: 'market', prefix: 'market', process: 'api-market', port: 8772, probePath: '/health', readExamples: 'Quotes SSE, bars, watchlist' },
  { id: 'research', prefix: 'research', process: 'api-research', port: 8773, probePath: '/health', readExamples: 'Screener, Greeks, data readiness, feedback' },
]

/**
 * The alias prefixes TD-55 B2 removed from the gateway (Owner 2026-10-04): /api/<prefix>/… now
 * falls through to the SPA. docs and ops stay domain ids under /api/monitor.
 */
export const TRADE_API_RETIRED_PREFIXES: string[] = ['docs', 'ops', 'trading', 'strategy', 'portfolio']

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
    action: 'MCP read tools across 8 Trade API domains via gateway',
    verify: 'STG/dev smoke 8/8 HTTP 200 on probe paths',
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
    action: 'No POST /control/*, no ib:operator:cmd, no Redis daemon control write',
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
    ...TRADE_API_DOMAINS.map(d => `- **${d.id}** → ${d.process} :${d.port} /api/${d.id}${d.probePath} — ${d.readExamples}`),
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
