/**
 * MCP Contract catalog — standards for building and operating MCP servers.
 *
 * Authoritative source for Ops Console → Architecture → Standards → MCP Contract.
 * Defines the technical enforcement of Vision § Decoupling Principle:
 * what MCP servers must expose, how permissions work, and what is forbidden.
 */

export const MCP_CONTRACT_VERSION = '2026-06-19'
export const MCP_CONTRACT_SOURCE = 'console/src/lib/standards/mcpContractCatalog.ts'

// ---------------------------------------------------------------------------
// Core contract
// ---------------------------------------------------------------------------

export const MCP_CONTRACT_STATEMENT =
  'Every MCP server is the standard bridge between AI Agents and infrastructure. ' +
  'Platform MCP servers (K8s, Redis, PG, ArgoCD, Tekton, Prometheus) are generic — ' +
  'they know nothing about Trade business logic. Business MCP servers (mcp-trade-api) ' +
  'are domain-specific and pluggable. The contract below applies to all servers equally.'

// ---------------------------------------------------------------------------
// Server registry
// ---------------------------------------------------------------------------

export type McpServerDef = {
  name: string
  layer: 'platform' | 'business'
  namespace: string
  provides: string
  status: 'planned' | 'available'
}

export const MCP_SERVER_REGISTRY: McpServerDef[] = [
  { name: 'mcp-server-kubernetes', layer: 'platform', namespace: 'cicd or ai', provides: 'Pod/Node/Namespace CRUD, logs, events, rollout', status: 'planned' },
  { name: 'mcp-server-redis', layer: 'platform', namespace: 'ai', provides: 'Health keys read, Stream inspect, pub/sub status', status: 'planned' },
  { name: 'mcp-server-postgres', layer: 'platform', namespace: 'ai', provides: 'Schema browse, read queries (allowlist), connection status', status: 'planned' },
  { name: 'mcp-server-argocd', layer: 'platform', namespace: 'cicd', provides: 'App sync status, sync trigger, rollback', status: 'planned' },
  { name: 'mcp-server-tekton', layer: 'platform', namespace: 'cicd', provides: 'Pipeline list, run trigger, run logs', status: 'planned' },
  { name: 'mcp-server-prometheus', layer: 'platform', namespace: 'monitoring', provides: 'PromQL query, alert list, target status', status: 'planned' },
  { name: 'mcp-trade-api', layer: 'business', namespace: 'bifrost-prod (read)', provides: 'Trade domain APIs — quotes, portfolio, SEPA, strategy (read-only)', status: 'planned' },
]

// ---------------------------------------------------------------------------
// Required interface (every MCP server must implement)
// ---------------------------------------------------------------------------

export type McpRequiredTool = {
  tool: string
  description: string
  required: boolean
}

export const MCP_REQUIRED_INTERFACE: McpRequiredTool[] = [
  { tool: 'health', description: 'Return server status + version + uptime', required: true },
  { tool: 'capabilities', description: 'List available tools with permission level for each', required: true },
  { tool: 'list_resources', description: 'Expose readable resources (optional but recommended)', required: false },
]

// ---------------------------------------------------------------------------
// Permission model
// ---------------------------------------------------------------------------

export type McpPermissionLevel = {
  level: string
  label: string
  agentBehavior: string
  examples: string
}

export const MCP_PERMISSION_LEVELS: McpPermissionLevel[] = [
  {
    level: 'read',
    label: 'L0 — Read-only',
    agentBehavior: 'Agent calls freely; no confirmation needed',
    examples: 'get_pods, query_metrics, read_positions, list_apps',
  },
  {
    level: 'routine',
    label: 'L1 — Routine actuation',
    agentBehavior: 'Agent executes + writes audit log; no Owner confirmation',
    examples: 'rollout_restart, scale_deployment, delete_pod, argocd_sync',
  },
  {
    level: 'confirm',
    label: 'L2 — Owner confirm',
    agentBehavior: 'Agent proposes action; Owner approves via Console or chat',
    examples: 'argocd_rollback, node_drain, resource_limit_change',
  },
  {
    level: 'pr',
    label: 'L3 — PR-gated',
    agentBehavior: 'Agent opens PR in Gitea; Owner reviews + merges; ArgoCD applies',
    examples: 'PVC resize, RBAC change, Helm values, Gate parameter tuning',
  },
  {
    level: 'forbidden',
    label: 'DENY — Never exposed',
    agentBehavior: 'MCP server must not implement; Agent cannot invoke',
    examples: 'ib:operator:cmd write, daemon_control write, order placement',
  },
]

// ---------------------------------------------------------------------------
// Deny list (absolute — no MCP server may expose these)
// ---------------------------------------------------------------------------

export type McpDenyRule = {
  action: string
  reason: string
  enforcement: string
}

export const MCP_DENY_LIST: McpDenyRule[] = [
  { action: 'Write to ib:operator:cmd Redis Stream', reason: 'R-DV3: only Daemon may send trade commands', enforcement: 'MCP redis server: XADD deny-list on key pattern' },
  { action: 'Write daemon_control table', reason: 'Platform L0 never invokes Engine control', enforcement: 'MCP PG server: write deny; read-only connection' },
  { action: 'Place / modify / cancel orders via IB API', reason: 'Trade write path belongs to Daemon + Operator only', enforcement: 'No IB MCP server exists; TWS physically isolated' },
  { action: 'Modify strategy_* / gate_safety_* tables directly', reason: 'L3 only via PR → ArgoCD config apply', enforcement: 'MCP PG server: DDL/DML deny; suggest PR instead' },
  { action: 'Read TWS credentials or IB API keys', reason: 'Secrets isolation — never in MCP context', enforcement: 'K8s Secret not mounted in MCP server Pods' },
  { action: 'Egress to external networks with trade data', reason: 'LAN-only policy for sensitive data', enforcement: 'NetworkPolicy egress deny on ai/cicd namespaces' },
]

// ---------------------------------------------------------------------------
// Authentication & transport
// ---------------------------------------------------------------------------

export type McpAuthRule = {
  aspect: string
  standard: string
}

export const MCP_AUTH_STANDARDS: McpAuthRule[] = [
  { aspect: 'Transport', standard: 'stdio (local Cursor ↔ MCP) or HTTP+SSE (remote, within cluster)' },
  { aspect: 'Authentication', standard: 'Bearer token from K8s Secret (platform-api issues tokens for Agents)' },
  { aspect: 'Authorization', standard: 'Each tool declares its permission level; MCP server enforces before executing' },
  { aspect: 'Audit', standard: 'Every L1+ invocation logged to platform-api audit endpoint with tool/args/result/timestamp' },
  { aspect: 'Rate limiting', standard: 'Optional per-tool rate limit (prevent runaway Agent loops); default: 60 calls/min per L1 tool' },
  { aspect: 'Timeout', standard: 'Tools must respond within 30s or return timeout error; Agent retries once then reports' },
]

// ---------------------------------------------------------------------------
// Decoupling enforcement
// ---------------------------------------------------------------------------

export type McpDecouplingRule = {
  rule: string
  detail: string
}

export const MCP_DECOUPLING: McpDecouplingRule[] = [
  { rule: 'No bifrost_core import', detail: 'Platform MCP servers never import Trade Python packages (bifrost_core, bifrost_api, etc.)' },
  { rule: 'No Trade schema knowledge', detail: 'mcp-server-postgres uses allowlisted queries or dynamic schema browse — not hardcoded table names' },
  { rule: 'Business MCP is pluggable', detail: 'mcp-trade-api is a separate deployment; removing it does not break platform MCP servers' },
  { rule: 'Same contract, different app', detail: 'Replace mcp-trade-api with mcp-{other-app}-api; Dev + Ops Agents still function unchanged' },
]

// ---------------------------------------------------------------------------
// LLM pack
// ---------------------------------------------------------------------------

export function buildMcpContractLlmPack(): string {
  const lines: string[] = [
    '# Bifrost Ops — MCP Contract (Standards)',
    `# Source: ${MCP_CONTRACT_SOURCE} v${MCP_CONTRACT_VERSION}`,
    '',
    '## Core contract',
    MCP_CONTRACT_STATEMENT,
    '',
    '## Server registry',
    ...MCP_SERVER_REGISTRY.map(s => `- **${s.name}** [${s.layer}] @ ${s.namespace}: ${s.provides} (${s.status})`),
    '',
    '## Required interface (all servers)',
    ...MCP_REQUIRED_INTERFACE.map(t => `- ${t.required ? '✓' : '○'} **${t.tool}**: ${t.description}`),
    '',
    '## Permission levels',
    ...MCP_PERMISSION_LEVELS.map(p => `- **${p.level}** (${p.label}): ${p.agentBehavior} — e.g. ${p.examples}`),
    '',
    '## Deny list (absolute)',
    ...MCP_DENY_LIST.map(d => `- ✗ ${d.action}: ${d.reason} [enforcement: ${d.enforcement}]`),
    '',
    '## Authentication & transport',
    ...MCP_AUTH_STANDARDS.map(a => `- **${a.aspect}**: ${a.standard}`),
    '',
    '## Decoupling enforcement',
    ...MCP_DECOUPLING.map(r => `- **${r.rule}**: ${r.detail}`),
  ]
  return lines.join('\n')
}
