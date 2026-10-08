#!/usr/bin/env node
/**
 * Bifrost Ops Platform MCP server (P5).
 * Proxies platform-api — same routes, Bearer auth, audit on actuation side.
 */
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js'
import { z } from 'zod'
import { getRequest, listRequests, requestAction, waitForRequest } from './approvalTools.js'
import { jsonResult, platformDelete, platformGet, platformPost } from './platformClient.js'
import { registerPrometheusBridge } from './prometheusBridge.js'
import { registerApproveBridge } from './registerApprove.js'
import { registerLocalBridge } from './registerLocal.js'
import { focusAllowList } from './focusBridges.js'

const SERVER_NAME = 'mcp-server-platform'
const SERVER_VERSION = '0.1.0'
const bridgeFocus = process.env.MCP_BRIDGE_FOCUS?.trim().toLowerCase() ?? ''

const server = new McpServer({ name: SERVER_NAME, version: SERVER_VERSION })

if (bridgeFocus === 'prometheus') {
  registerPrometheusBridge(server)
} else if (bridgeFocus === 'approve') {
  // Chat approval only. Not part of the full server, and not a focus allow-list slice.
  registerApproveBridge(server)
} else if (bridgeFocus === 'local') {
  // Laptop bdev + git-bridge. PROD does not serve these.
  registerLocalBridge(server)
} else {
// focus 桥：按领域白名单过滤要注册的工具。focus 为空 → allow=null → 注册全量；未知 focus → 退出。
// 白名单定义与授权原则见 focusBridges.ts。
let allow: Set<string> | null
try {
  allow = focusAllowList(bridgeFocus)
} catch (err) {
  console.error(`${SERVER_NAME}: ${(err as Error).message}`)
  process.exit(1)
}
// 保留 server.tool 的重载签名，否则各 handler 的解构参数无法从 zod schema 推断类型。
const reg = ((...args: unknown[]) => {
  if (allow && !allow.has(String(args[0]))) return undefined
  return (server.tool as unknown as (...a: unknown[]) => unknown)(...args)
}) as unknown as typeof server.tool

reg('platform_mcp_health', 'MCP server health + version', {}, async () =>
  jsonResult({
    ok: true,
    server: SERVER_NAME,
    version: SERVER_VERSION,
    focus: bridgeFocus || 'full',
    tools_scoped: allow ? allow.size : null,
    platform_api_url: process.env.PLATFORM_API_URL ?? 'http://127.0.0.1:8780',
  }),
)

reg('get_connectivity_matrix', 'Environment connectivity matrix', {}, async () =>
  jsonResult(await platformGet('/api/v1/matrix')),
)

reg(
  'verify_payload',
  'Matrix vs cluster datastore classification (NOMINAL/PROBE_DRIFT/DATA_LAYER/HTTP_FAIL per env)',
  {},
  async () => jsonResult(await platformGet('/api/v1/mission/verify-payload')),
)

reg(
  'verify_mission_snapshot',
  'Fresh matrix reprobe + verify_payload + post_fix_verification (required before closing remediation jobs)',
  {},
  async () => jsonResult(await platformGet('/api/v1/mission/verify-snapshot')),
)

reg('list_environments', 'Registered environments', {}, async () =>
  jsonResult(await platformGet('/api/v1/environments')),
)

reg('get_ops_context', 'Spine context (milestones, tracks)', {}, async () =>
  jsonResult(await platformGet('/api/v1/context')),
)

reg('get_auth_capabilities', 'Bearer token role and capabilities', {}, async () =>
  jsonResult(await platformGet('/api/v1/auth/capabilities')),
)

reg('get_audit_log', 'Recent actuation audit records', {}, async () =>
  jsonResult(await platformGet('/api/v1/audit')),
)

reg('get_cluster_summary', 'Cluster summary probe', {}, async () =>
  jsonResult(await platformGet('/api/v1/cluster/')),
)

reg('get_cluster_nodes', 'Kubernetes node list', {}, async () =>
  jsonResult(await platformGet('/api/v1/cluster/nodes')),
)

reg(
  'get_data_freshness',
  'CNPG logical DB activity freshness (dev/stg vs prod)',
  {},
  async () => jsonResult(await platformGet('/api/v1/cluster/data-freshness')),
)

reg(
  'market_data_doctor',
  'Market Data Plugin doctor: what the last session should hold vs what it does (snapshot/OI/bars/ratios), staleness, failed jobs, workers, vendor — each finding carries a prescription',
  { probes: z.boolean().optional() },
  async ({ probes }) =>
    jsonResult(
      await platformGet(`/api/v1/plugins/market-data/api/market/doctor${probes === false ? '?probes=false' : ''}`),
    ),
)

reg(
  'market_data_heal',
  'Execute the Market Data doctor prescriptions (enqueue-slot / retry-jobs). dry_run=true previews; finding_ids restricts to chosen findings (operator)',
  { dry_run: z.boolean().optional(), finding_ids: z.array(z.string()).optional() },
  async ({ dry_run, finding_ids }) =>
    jsonResult(
      await platformPost('/api/v1/plugins/market-data/api/market/doctor/heal', {
        dry_run: dry_run ?? false,
        ...(finding_ids != null && finding_ids.length > 0 ? { finding_ids } : {}),
      }),
    ),
)

reg(
  'get_postgres_backup_status',
  'CNPG Backup CR freshness (completed < 48h)',
  {},
  async () => jsonResult(await platformGet('/api/v1/cluster/postgres/backup-status')),
)

reg(
  'trigger_cnpg_backup',
  'Create on-demand CNPG Backup CR (barmanObjectStore)',
  {},
  async () => jsonResult(await platformPost('/api/v1/cluster/postgres/backup', {})),
)

reg(
  'repair_cnpg_wal_store',
  'Repair MinIO WAL object store, delete stuck Backup CRs, trigger on-demand backup',
  {},
  async () => jsonResult(await platformPost('/api/v1/cluster/postgres/wal-store/repair', {})),
)

reg(
  'trigger_data_clone',
  'Clone bifrost_prod → non-prod (admin; confirmation_token + confirm:true required). Default targets=["bifrost_dev"]; pass stg explicitly if needed.',
  {
    source: z.string().optional(),
    targets: z.array(z.string()).optional(),
    mode: z.enum(['full', 'selective']).optional(),
    tables: z.array(z.string()).optional(),
    confirmation_token: z.string(),
    confirm: z.literal(true),
  },
  async ({ source, targets, mode, tables, confirmation_token, confirm }) =>
    jsonResult(
      await platformPost('/api/v1/cluster/data-clone', {
        source: source ?? 'bifrost_prod',
        targets: targets ?? ['bifrost_dev'],
        mode: mode ?? 'full',
        tables,
        confirmation_token,
        confirm,
      }),
    ),
)

reg(
  'get_data_clone_status',
  'Poll data-clone job progress',
  { id: z.string() },
  async ({ id }) =>
    jsonResult(await platformGet(`/api/v1/cluster/data-clone/${encodeURIComponent(id)}`)),
)

reg('get_gitops_apps', 'Argo CD applications', {}, async () =>
  jsonResult(await platformGet('/api/v1/gitops/apps')),
)

reg('get_delivery_pipelines', 'Tekton pipeline catalog', {}, async () =>
  jsonResult(await platformGet('/api/v1/delivery/pipelines')),
)

reg(
  'get_delivery_run_logs',
  'PipelineRun log tail',
  { run_id: z.string(), namespace: z.string().optional() },
  async ({ run_id, namespace }) => {
    const qs = namespace != null && namespace !== '' ? `?ns=${encodeURIComponent(namespace)}` : ''
    return jsonResult(await platformGet(`/api/v1/delivery/runs/${encodeURIComponent(run_id)}/logs${qs}`))
  },
)

reg(
  'gitops_sync_app',
  'Trigger Argo CD sync to HEAD (operator)',
  { name: z.string() },
  async ({ name }) =>
    jsonResult(await platformPost(`/api/v1/gitops/apps/${encodeURIComponent(name)}/sync`)),
)

reg(
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

reg(
  'start_pipeline_run',
  'Start Tekton PipelineRun (operator). Pass revision (Gitea tag) to pin deploy version. Pass who (same value as release.sh hold prints) when a release window is open — the server refuses without it. For bifrost-deliver-research, pass tag (semver image pin, e.g. 0.48.4) — default tag is the moving smoke tag `dev` and must not pin k8s. For bifrost-build-research-dagster, pass the same semver: the `-dagster` suffix is appended for you, because both research image lines share one repository and a bare tag would build the Dagster image over the one research-api runs.',
  { name: z.string(), revision: z.string().optional(), tag: z.string().optional(), who: z.string().optional() },
  async ({ name, revision, tag, who }) =>
    jsonResult(
      await platformPost(`/api/v1/delivery/pipelines/${encodeURIComponent(name)}/runs`, {
        revision: revision ?? '',
        ...(tag != null && tag.trim() !== '' ? { tag: tag.trim() } : {}),
        ...(who != null && who.trim() !== '' ? { who: who.trim() } : {}),
      }),
    ),
)

reg(
  'delete_pipeline_run',
  'Delete terminal Tekton PipelineRun CR + pods (operator)',
  { id: z.string(), namespace: z.string().optional() },
  async ({ id, namespace }) => {
    const qs = namespace != null && namespace !== '' ? `?ns=${encodeURIComponent(namespace)}` : ''
    return jsonResult(
      await platformDelete(`/api/v1/delivery/runs/${encodeURIComponent(id)}${qs}`),
    )
  },
)

reg(
  'cordon_node',
  'Cordon node (operator)',
  { name: z.string() },
  async ({ name }) =>
    jsonResult(await platformPost(`/api/v1/cluster/nodes/${encodeURIComponent(name)}/cordon`)),
)

reg(
  'uncordon_node',
  'Uncordon node (operator)',
  { name: z.string() },
  async ({ name }) =>
    jsonResult(await platformPost(`/api/v1/cluster/nodes/${encodeURIComponent(name)}/uncordon`)),
)

reg(
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

reg('ensure_bifrost_namespaces', 'Create Bifrost namespaces (operator)', {}, async () =>
  jsonResult(await platformPost('/api/v1/cluster/namespaces/ensure-bifrost')),
)

reg(
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

reg(
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

reg(
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

reg(
  'wake_compute_node',
  'Wake-on-LAN compute node (operator)',
  { name: z.string() },
  async ({ name }) =>
    jsonResult(await platformPost(`/api/v1/cluster/nodes/${encodeURIComponent(name)}/wake`)),
)

reg(
  'poweroff_compute_node',
  'Drain + power off compute node (admin)',
  { name: z.string() },
  async ({ name }) =>
    jsonResult(await platformPost(`/api/v1/cluster/nodes/${encodeURIComponent(name)}/poweroff`)),
)

reg('get_agent_bridge', 'Agent host + MCP bridge status', {}, async () =>
  jsonResult(await platformGet('/api/v1/agent/bridge')),
)

reg(
  'get_trust_matrix',
  'Flight Director — trust & autonomy matrix with earned autonomy hints',
  {},
  async () => jsonResult(await platformGet('/api/v1/agent/governance/trust-matrix')),
)

reg('get_remediation_health', 'Remediation runner health', {}, async () =>
  jsonResult(await platformGet('/api/v1/remediation/health')),
)

reg('list_remediation_jobs', 'List remediation / agent tasks (operator)', {}, async () =>
  jsonResult(await platformGet('/api/v1/remediation/')),
)

reg('get_stg_smoke', 'STG environment HTTP smoke probes', {}, async () =>
  jsonResult(await platformGet('/api/v1/delivery/stg/smoke')),
)

reg(
  'get_delivery_revisions',
  'Available Gitea tags for deploy revision selection',
  { repos: z.string().optional().describe('Comma-separated repo names') },
  async ({ repos }) => {
    const qs = repos != null && repos !== '' ? `?repos=${encodeURIComponent(repos)}` : ''
    return jsonResult(await platformGet(`/api/v1/delivery/revisions${qs}`))
  },
)

reg(
  'get_commit_lineage',
  'Agent threads and the commits they stamped across all repos (Claude-Session / Claude-Transcript / Change-Id trailers). ' +
    'Filter by session (local_…) to see what a thread landed, or by change_id to check whether a change reached main ' +
    '(it matches rebased, version-bumped and squashed copies).',
  {
    days: z.number().int().min(1).max(90).optional().describe('Window in days (default 14)'),
    session: z.string().optional().describe('Claude-Session value, e.g. local_…'),
    change_id: z.string().optional().describe('Change-Id value, e.g. I0123…'),
    refresh: z.boolean().optional().describe('Bypass the 5-minute cache'),
  },
  async ({ days, session, change_id, refresh }) => {
    const q = new URLSearchParams()
    if (days != null) q.set('days', String(days))
    if (session) q.set('session', session)
    if (change_id) q.set('change_id', change_id)
    if (refresh) q.set('refresh', 'true')
    const qs = q.toString()
    return jsonResult(await platformGet(`/api/v1/lineage${qs ? `?${qs}` : ''}`))
  },
)

reg(
  'get_checklist_signals',
  'Latest Daily Ops Checklist per-item signals + KPIs',
  {},
  async () => jsonResult(await platformGet('/api/v1/checklist/signals')),
)

reg(
  'get_code_health',
  'Code-health ratchet readings (duplication, oversized files, contract coverage, image spread). reported=false means NOT OBSERVED — never read it as healthy.',
  {},
  async () => jsonResult(await platformGet('/api/v1/code-health')),
)

reg(
  'get_telemetry_overview',
  'Prometheus telemetry overview snapshot (preset metrics)',
  { namespace: z.string().optional().describe('Optional K8s namespace filter') },
  async ({ namespace }) => {
    const qs = namespace != null && namespace !== '' ? `?ns=${encodeURIComponent(namespace)}` : ''
    return jsonResult(await platformGet(`/api/v1/telemetry/overview${qs}`))
  },
)

reg(
  'get_telemetry_alerts',
  'Prometheus firing and pending alerts',
  {},
  async () => jsonResult(await platformGet('/api/v1/telemetry/alerts')),
)

reg(
  'get_telemetry_targets',
  'Prometheus scrape target health',
  {
    state: z
      .enum(['any', 'active', 'dropped'])
      .optional()
      .describe('Target state filter (default: any)'),
  },
  async ({ state }) => {
    const qs = state != null && state !== 'any' ? `?state=${encodeURIComponent(state)}` : ''
    return jsonResult(await platformGet(`/api/v1/telemetry/targets${qs}`))
  },
)

reg(
  'request_action',
  'Create an approval request and return its id. Does not run the action. Requires MCP_WRITES=on.',
  {
    action: z.string().describe('Action id from the approvals catalog'),
    params: z.record(z.string(), z.unknown()).optional().describe('Parameters signed into the approval'),
    reason: z.string().describe('Why this action is requested'),
    rollback: z.string().describe('How to undo it if the approval is executed'),
  },
  async ({ action, params, reason, rollback }) =>
    jsonResult(await requestAction({ action, params, reason, rollback })),
)

reg(
  'get_request',
  'Read one approval request by id',
  { id: z.string() },
  async ({ id }) => jsonResult(await getRequest(id)),
)

reg(
  'list_requests',
  'List approval requests. status is pending (default) or all.',
  { status: z.enum(['pending', 'all']).optional() },
  async ({ status }) => jsonResult(await listRequests(status ?? 'pending')),
)

reg(
  'wait_for_request',
  'Poll an approval until executed, failed, rejected, expired, or the timeout',
  {
    id: z.string(),
    timeout_seconds: z.number().int().min(1).max(600).optional().describe('Default 120, max 600'),
  },
  async ({ id, timeout_seconds }) => jsonResult(await waitForRequest(id, timeout_seconds ?? 120)),
)

} // end platform tools (non-prometheus focus)

async function main() {
  const transport = new StdioServerTransport()
  await server.connect(transport)
}

main().catch(err => {
  console.error(err)
  process.exit(1)
})
