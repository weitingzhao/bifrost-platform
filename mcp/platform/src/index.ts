#!/usr/bin/env node
/**
 * Bifrost Ops Platform MCP server (P5).
 * Proxies platform-api — same routes, Bearer auth, audit on actuation side.
 */
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js'
import { z } from 'zod'
import { jsonResult, platformDelete, platformGet, platformPost } from './platformClient.js'

const SERVER_NAME = 'mcp-server-platform'
const SERVER_VERSION = '0.1.0'

const server = new McpServer({ name: SERVER_NAME, version: SERVER_VERSION })

server.tool('platform_mcp_health', 'MCP server health + version', {}, async () =>
  jsonResult({
    ok: true,
    server: SERVER_NAME,
    version: SERVER_VERSION,
    platform_api_url: process.env.PLATFORM_API_URL ?? 'http://127.0.0.1:8780',
  }),
)

server.tool('platform_mcp_capabilities', 'List MCP tools from platform-api catalog', {}, async () =>
  jsonResult(await platformGet('/api/v1/mcp/tools')),
)

server.tool('get_connectivity_matrix', 'Environment connectivity matrix', {}, async () =>
  jsonResult(await platformGet('/api/v1/matrix')),
)

server.tool(
  'verify_payload',
  'Matrix vs cluster datastore classification (NOMINAL/PROBE_DRIFT/DATA_LAYER/HTTP_FAIL per env)',
  {},
  async () => jsonResult(await platformGet('/api/v1/mission/verify-payload')),
)

server.tool(
  'verify_mission_snapshot',
  'Fresh matrix reprobe + verify_payload + post_fix_verification (required before closing remediation jobs)',
  {},
  async () => jsonResult(await platformGet('/api/v1/mission/verify-snapshot')),
)

server.tool('list_environments', 'Registered environments', {}, async () =>
  jsonResult(await platformGet('/api/v1/environments')),
)

server.tool('get_ops_context', 'Spine context (milestones, tracks)', {}, async () =>
  jsonResult(await platformGet('/api/v1/context')),
)

server.tool('get_auth_capabilities', 'Bearer token role and capabilities', {}, async () =>
  jsonResult(await platformGet('/api/v1/auth/capabilities')),
)

server.tool('get_audit_log', 'Recent actuation audit records', {}, async () =>
  jsonResult(await platformGet('/api/v1/audit')),
)

server.tool('get_cluster_summary', 'Cluster summary probe', {}, async () =>
  jsonResult(await platformGet('/api/v1/cluster/')),
)

server.tool('get_cluster_nodes', 'Kubernetes node list', {}, async () =>
  jsonResult(await platformGet('/api/v1/cluster/nodes')),
)

server.tool('get_gitops_apps', 'Argo CD applications', {}, async () =>
  jsonResult(await platformGet('/api/v1/gitops/apps')),
)

server.tool('get_stack_addons', 'CI/CD stack add-on status', {}, async () =>
  jsonResult(await platformGet('/api/v1/stack/addons')),
)

server.tool('get_delivery_pipelines', 'Tekton pipeline catalog', {}, async () =>
  jsonResult(await platformGet('/api/v1/delivery/pipelines')),
)

server.tool(
  'get_delivery_run_logs',
  'PipelineRun log tail',
  { run_id: z.string(), namespace: z.string().optional() },
  async ({ run_id, namespace }) => {
    const qs = namespace != null && namespace !== '' ? `?ns=${encodeURIComponent(namespace)}` : ''
    return jsonResult(await platformGet(`/api/v1/delivery/runs/${encodeURIComponent(run_id)}/logs${qs}`))
  },
)

server.tool(
  'gitops_sync_app',
  'Trigger Argo CD sync to HEAD (operator)',
  { name: z.string() },
  async ({ name }) =>
    jsonResult(await platformPost(`/api/v1/gitops/apps/${encodeURIComponent(name)}/sync`)),
)

server.tool(
  'gitops_rollback_app',
  'Rollback Argo CD app (admin)',
  { name: z.string(), revision: z.string().optional() },
  async ({ name, revision }) =>
    jsonResult(
      await platformPost(`/api/v1/gitops/apps/${encodeURIComponent(name)}/rollback`, {
        revision: revision ?? '',
      }),
    ),
)

server.tool(
  'start_pipeline_run',
  'Start Tekton PipelineRun (operator). Pass revision (Gitea tag) to pin deploy version.',
  { name: z.string(), revision: z.string().optional() },
  async ({ name, revision }) =>
    jsonResult(
      await platformPost(`/api/v1/delivery/pipelines/${encodeURIComponent(name)}/runs`, {
        revision: revision ?? '',
      }),
    ),
)

server.tool(
  'stack_install_addon',
  'Install CI/CD stack add-on (admin)',
  { name: z.string() },
  async ({ name }) =>
    jsonResult(await platformPost(`/api/v1/stack/addons/${encodeURIComponent(name)}/install`)),
)

server.tool(
  'stack_upgrade_addon',
  'Upgrade stack add-on (admin)',
  { name: z.string() },
  async ({ name }) =>
    jsonResult(await platformPost(`/api/v1/stack/addons/${encodeURIComponent(name)}/upgrade`)),
)

server.tool(
  'cordon_node',
  'Cordon node (operator)',
  { name: z.string() },
  async ({ name }) =>
    jsonResult(await platformPost(`/api/v1/cluster/nodes/${encodeURIComponent(name)}/cordon`)),
)

server.tool(
  'uncordon_node',
  'Uncordon node (operator)',
  { name: z.string() },
  async ({ name }) =>
    jsonResult(await platformPost(`/api/v1/cluster/nodes/${encodeURIComponent(name)}/uncordon`)),
)

server.tool(
  'drain_node',
  'Drain node (admin)',
  { name: z.string(), force: z.boolean().optional(), grace_period_seconds: z.number().optional() },
  async ({ name, force, grace_period_seconds }) =>
    jsonResult(
      await platformPost(`/api/v1/cluster/nodes/${encodeURIComponent(name)}/drain`, {
        force: force ?? false,
        grace_period_seconds: grace_period_seconds ?? 300,
      }),
    ),
)

server.tool('ensure_bifrost_namespaces', 'Create Bifrost namespaces (operator)', {}, async () =>
  jsonResult(await platformPost('/api/v1/cluster/namespaces/ensure-bifrost')),
)

server.tool(
  'rollout_restart_deployment',
  'Rollout restart Deployment (operator)',
  { namespace: z.string(), name: z.string() },
  async ({ namespace, name }) =>
    jsonResult(
      await platformPost('/api/v1/cluster/workloads/rollout-restart', {
        namespace,
        kind: 'Deployment',
        name,
      }),
    ),
)

server.tool(
  'scale_deployment',
  'Scale Deployment (operator)',
  { namespace: z.string(), name: z.string(), replicas: z.number().int().min(0).max(20) },
  async ({ namespace, name, replicas }) =>
    jsonResult(
      await platformPost('/api/v1/cluster/workloads/scale', {
        namespace,
        kind: 'Deployment',
        name,
        replicas,
      }),
    ),
)

server.tool(
  'delete_pod',
  'Delete Pod (operator)',
  { namespace: z.string(), name: z.string() },
  async ({ namespace, name }) =>
    jsonResult(
      await platformDelete(
        `/api/v1/cluster/workloads/pods/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
      ),
    ),
)

server.tool(
  'get_session_briefing',
  'Session briefing pack for Agent self-service (compact default). Params mirror Briefing URL state.',
  {
    track: z.string().optional(),
    lane: z.string().optional(),
    intent: z.string().optional(),
    pack: z.enum(['compact', 'full']).optional(),
  },
  async ({ track, lane, intent, pack }) => {
    const params = new URLSearchParams()
    if (track != null && track !== '') params.set('track', track)
    if (lane != null && lane !== '') params.set('lane', lane)
    if (intent != null && intent !== '') params.set('intent', intent)
    if (pack != null) params.set('pack', pack)
    const qs = params.toString()
    return jsonResult(await platformGet(`/api/v1/briefing/session-pack${qs !== '' ? `?${qs}` : ''}`))
  },
)

server.tool(
  'list_briefing_session_results',
  'Recent Agent Desk session close records',
  {},
  async () => jsonResult(await platformGet('/api/v1/briefing/session-results')),
)

server.tool(
  'close_briefing_session',
  'Record Agent Desk session close to audit (operator)',
  {
    job_id: z.string().optional(),
    outcome: z.enum(['done', 'failed', 'cancelled']),
    summary: z.string(),
    track: z.string().optional(),
    lane: z.string().optional(),
    intent: z.string().optional(),
    spine_note: z.string().optional(),
    request_spine_update: z.boolean().optional(),
  },
  async body => jsonResult(await platformPost('/api/v1/briefing/session-results', body)),
)

server.tool('get_agent_bridge', 'Agent host + MCP bridge status', {}, async () =>
  jsonResult(await platformGet('/api/v1/agent/bridge')),
)

server.tool(
  'get_hermes_readiness',
  'Hermes gateway + LLM key + platform MCP readiness for first L0 task',
  {},
  async () => jsonResult(await platformGet('/api/v1/agent/hermes/readiness')),
)

server.tool(
  'get_hermes_first_task',
  'Canonical Hermes First Task prompt (L0 read-only Mission health pass)',
  {},
  async () => jsonResult(await platformGet('/api/v1/agent/hermes/first-task')),
)

server.tool('get_agent_nightly_report', 'Nightly drift report from agent host', {}, async () =>
  jsonResult(await platformGet('/api/v1/agent/nightly-report')),
)

server.tool('get_remediation_health', 'Remediation runner health', {}, async () =>
  jsonResult(await platformGet('/api/v1/remediation/health')),
)

server.tool('list_remediation_jobs', 'List remediation / agent tasks (operator)', {}, async () =>
  jsonResult(await platformGet('/api/v1/remediation/')),
)

// --- Promote / Release tools (P4) ---

server.tool(
  'get_release_state',
  'Aggregated release state across STG/PROD with next-action guidance for agent-driven releases',
  { tier: z.string().optional().describe('platform (default) or trade') },
  async ({ tier }) => {
    const t = tier ?? 'platform'
    return jsonResult(await platformGet(`/api/v1/promote/release-state?tier=${encodeURIComponent(t)}`))
  },
)

server.tool(
  'get_release_gate',
  'Current release gate result, checks, blockers, and linked revision',
  { tier: z.string().optional().describe('stg | prod | platform-stg | platform-prod') },
  async ({ tier }) => {
    const qs = tier != null && tier !== '' ? `?tier=${encodeURIComponent(tier)}` : ''
    return jsonResult(await platformGet(`/api/v1/promote/release-gate${qs}`))
  },
)

server.tool(
  'get_gate_history',
  'Chronological gate run history for a tier',
  { tier: z.string().optional().describe('stg | prod | platform-stg | platform-prod') },
  async ({ tier }) => {
    const qs = tier != null && tier !== '' ? `?tier=${encodeURIComponent(tier)}` : ''
    return jsonResult(await platformGet(`/api/v1/promote/gate-history${qs}`))
  },
)

server.tool('get_stg_smoke', 'STG environment HTTP smoke probes', {}, async () =>
  jsonResult(await platformGet('/api/v1/delivery/stg/smoke')),
)

server.tool(
  'get_delivery_revisions',
  'Available Gitea tags for deploy revision selection',
  { repos: z.string().optional().describe('Comma-separated repo names') },
  async ({ repos }) => {
    const qs = repos != null && repos !== '' ? `?repos=${encodeURIComponent(repos)}` : ''
    return jsonResult(await platformGet(`/api/v1/delivery/revisions${qs}`))
  },
)

server.tool(
  'run_release_gate',
  'Run STG or PROD release gate (admin). Validates deploy health, captures revision, persists result.',
  { tier: z.string().optional().describe('stg | prod | platform-stg | platform-prod') },
  async ({ tier }) => {
    const qs = tier != null && tier !== '' ? `?tier=${encodeURIComponent(tier)}` : ''
    return jsonResult(await platformPost(`/api/v1/promote/release-gate${qs}`))
  },
)

server.tool(
  'ensure_kubeconfig_secret',
  'Sync kubeconfig and ensure bifrost-platform-kubeconfig Secret in platform STG/PROD namespaces (admin). ' +
    'Use when cluster reachability is "fail" due to missing kubeconfig secret.',
  {
    namespaces: z
      .array(z.string())
      .optional()
      .describe('Target namespaces (default: bifrost-platform-stg, bifrost-platform-prod)'),
    sync_first: z
      .boolean()
      .optional()
      .describe('Fetch kubeconfig from K3s server before creating secret'),
  },
  async ({ namespaces, sync_first }) => {
    const body: Record<string, unknown> = {}
    if (namespaces != null) body.namespaces = namespaces
    if (sync_first != null) body.sync_first = sync_first
    return jsonResult(
      await platformPost('/api/v1/cluster/kubeconfig-secret/ensure', body),
    )
  },
)

async function main() {
  const transport = new StdioServerTransport()
  await server.connect(transport)
}

main().catch(err => {
  console.error(err)
  process.exit(1)
})
