/**
 * Vision V3 — Ops Agent L1/L2 closed-loop contract.
 *
 * Authoritative for Ops Console → Architecture → Vision (V3 gate)
 * and Agent Briefing Ops-layer runtime discipline.
 */

export const OPS_AGENT_LOOP_VERSION = '2026-06-19'
export const OPS_AGENT_LOOP_SOURCE = 'console/src/lib/architecture/opsAgentLoopCatalog.ts'

export const OPS_AGENT_LOOP_STATEMENT =
  'Alert fires → Alertmanager webhook → platform-api Ops Agent handler → Cursor SDK Agent diagnoses → ' +
  'L1 routine actuation (rollout restart, delete pod, scale) via mcp-server-platform + audit log. ' +
  'L2 confirm actions (drain, rollback) require Owner approval in Console or chat.'

export type OpsAgentLoopStep = {
  order: number
  phase: string
  actor: string
  action: string
  verify: string
  level: 'L0' | 'L1' | 'L2'
}

export const OPS_AGENT_LOOP_STEPS: OpsAgentLoopStep[] = [
  {
    order: 1,
    phase: 'Alert',
    actor: 'Prometheus / Alertmanager',
    action: 'POST /api/v1/ops-agent/alertmanager with firing alert payload',
    verify: 'HTTP 200 + diagnosis JSON',
    level: 'L0',
  },
  {
    order: 2,
    phase: 'Diagnose',
    actor: 'Ops Agent',
    action: 'MCP: get_cluster_workloads, get_pod_logs, get_audit_log, get_ops_context',
    verify: 'Root cause hypothesis in Agent report',
    level: 'L0',
  },
  {
    order: 3,
    phase: 'L1 fix',
    actor: 'Ops Agent',
    action: 'MCP routine tools: rollout_restart_deployment, delete_pod, scale_deployment, gitops_sync_app',
    verify: 'Audit record action=cluster.* status=ok',
    level: 'L1',
  },
  {
    order: 4,
    phase: 'L2 confirm',
    actor: 'Ops Agent → Owner',
    action: 'Propose drain_node, gitops_rollback_app, poweroff_compute_node',
    verify: 'Owner confirms in Console before MCP invoke',
    level: 'L2',
  },
  {
    order: 5,
    phase: 'Report',
    actor: 'Ops Agent',
    action: 'Summarize alert, actions taken, matrix/cluster state to Owner',
    verify: 'Audit log + Observe → Audit page',
    level: 'L0',
  },
  {
    order: 6,
    phase: 'Cursor bridges',
    actor: 'Owner IDE',
    action: 'config/cursor-mcp-bridges.json — platform + K8s/Redis/PG bridge hints',
    verify: 'GET /api/v1/mcp/status script_path resolves',
    level: 'L0',
  },
]

export const OPS_AGENT_MCP_SERVERS = {
  platform: 'mcp-server-platform',
  kubernetes: 'mcp-server-kubernetes',
  redis: 'mcp-server-redis',
  postgres: 'mcp-server-postgres',
} as const

export const OPS_AGENT_WEBHOOK_PATH = '/api/v1/ops-agent/alertmanager'
export const OPS_AGENT_ALERTMANAGER_CONFIG = 'config/ops-agent-alertmanager.yaml'
export const OPS_AGENT_CURSOR_BRIDGES = 'config/cursor-mcp-bridges.json'

export function buildOpsAgentLoopLlmPack(): string {
  const lines = [
    '# Bifrost — Ops Agent L1/L2 Loop (Vision V3)',
    `# Source: ${OPS_AGENT_LOOP_SOURCE} v${OPS_AGENT_LOOP_VERSION}`,
    '',
    OPS_AGENT_LOOP_STATEMENT,
    '',
    '## Loop steps',
    ...OPS_AGENT_LOOP_STEPS.map(s =>
      `${s.order}. **${s.phase}** [${s.level}] (${s.actor}): ${s.action} → verify: ${s.verify}`),
    '',
    '## MCP servers',
    ...Object.entries(OPS_AGENT_MCP_SERVERS).map(([k, v]) => `- ${k}: \`${v}\``),
    '',
    '## Config',
    `- Alertmanager receiver: \`${OPS_AGENT_ALERTMANAGER_CONFIG}\``,
    `- Cursor MCP bridges: \`${OPS_AGENT_CURSOR_BRIDGES}\``,
    `- Webhook: \`POST ${OPS_AGENT_WEBHOOK_PATH}\``,
  ]
  return lines.join('\n')
}
