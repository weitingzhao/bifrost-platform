#!/usr/bin/env node
/**
 * Bifrost Ops Platform MCP server (P5).
 * Proxies platform-api — same routes, Bearer auth, audit on actuation side.
 */
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js'
import { z } from 'zod'
import { jsonResult, platformDelete, platformGet, platformPatch, platformPost } from './platformClient.js'
import { registerPrometheusBridge } from './prometheusBridge.js'

const SERVER_NAME = 'mcp-server-platform'
const SERVER_VERSION = '0.1.0'
const bridgeFocus = process.env.MCP_BRIDGE_FOCUS?.trim().toLowerCase() ?? ''

const server = new McpServer({ name: SERVER_NAME, version: SERVER_VERSION })

if (bridgeFocus === 'prometheus') {
  registerPrometheusBridge(server)
} else {
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

server.tool(
  'get_data_freshness',
  'CNPG logical DB activity freshness (dev/stg vs prod)',
  {},
  async () => jsonResult(await platformGet('/api/v1/cluster/data-freshness')),
)

server.tool(
  'trigger_data_clone',
  'Clone bifrost_prod → bifrost_dev/stg (admin; confirmation_token + confirm:true required)',
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
        targets: targets ?? ['bifrost_dev', 'bifrost_stg'],
        mode: mode ?? 'full',
        tables,
        confirmation_token,
        confirm,
      }),
    ),
)

server.tool(
  'get_data_clone_status',
  'Poll data-clone job progress',
  { id: z.string() },
  async ({ id }) =>
    jsonResult(await platformGet(`/api/v1/cluster/data-clone/${encodeURIComponent(id)}`)),
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
  'wake_compute_node',
  'Wake-on-LAN compute node (operator)',
  { name: z.string() },
  async ({ name }) =>
    jsonResult(await platformPost(`/api/v1/cluster/nodes/${encodeURIComponent(name)}/wake`)),
)

server.tool(
  'join_cluster_node',
  'K3s agent join job (admin)',
  { profile: z.string() },
  async ({ profile }) =>
    jsonResult(await platformPost('/api/v1/cluster/nodes/join', { profile })),
)

server.tool(
  'poweroff_compute_node',
  'Drain + power off compute node (admin)',
  { name: z.string() },
  async ({ name }) =>
    jsonResult(await platformPost(`/api/v1/cluster/nodes/${encodeURIComponent(name)}/poweroff`)),
)

server.tool('ensure_metrics_server', 'Install metrics-server add-on (admin)', {}, async () =>
  jsonResult(await platformPost('/api/v1/cluster/addons/metrics-server/ensure')),
)

server.tool(
  'ensure_kube_prometheus_stack',
  'Install kube-prometheus-stack add-on (admin)',
  {},
  async () =>
    jsonResult(await platformPost('/api/v1/cluster/addons/kube-prometheus-stack/ensure')),
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

server.tool(
  'prepare_briefing',
  'Write briefing pack to data/briefing/active-pack.md for Cursor IDE /briefing (operator)',
  {
    session_pack: z.string(),
    session_id: z.string().optional(),
    program_id: z.string().optional(),
    phase_id: z.string().optional(),
    lane: z.string().optional(),
    intent: z.string().optional(),
  },
  async body => jsonResult(await platformPost('/api/v1/briefing/prepare', body)),
)

server.tool(
  'update_lane',
  'Reclassify a Briefing lane (component_line / track_type / track / description). ID and label are immutable.',
  {
    id: z.string(),
    component_line: z.string().optional(),
    track_type: z.string().optional(),
    track: z.string().optional(),
    short_label: z.string().optional(),
    description: z.string().optional(),
    agent_mode: z.string().optional(),
    work_intent: z.string().optional(),
  },
  async ({ id, ...patch }) => {
    const body: Record<string, string> = {}
    for (const [k, v] of Object.entries(patch)) {
      if (typeof v === 'string' && v.trim() !== '') body[k] = v
    }
    return jsonResult(
      await platformPatch(`/api/v1/lanes/${encodeURIComponent(id)}`, body),
    )
  },
)

server.tool(
  'delete_lane',
  'Delete a Briefing work lane from lanes.yaml (operator).',
  { id: z.string() },
  async ({ id }) =>
    jsonResult(await platformDelete(`/api/v1/lanes/${encodeURIComponent(id)}`)),
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

server.tool(
  'get_agent_performance',
  'Flight Director — agent performance KPIs (7d/30d windows)',
  {},
  async () => jsonResult(await platformGet('/api/v1/agent/governance/performance')),
)

server.tool(
  'get_trust_matrix',
  'Flight Director — trust & autonomy matrix with earned autonomy hints',
  {},
  async () => jsonResult(await platformGet('/api/v1/agent/governance/trust-matrix')),
)

server.tool(
  'get_flight_director_snapshot',
  'Flight Director snapshot — performance + trust + capability + briefing digest',
  {},
  async () => jsonResult(await platformGet('/api/v1/agent/governance/snapshot')),
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

server.tool(
  'get_program_context',
  'Program blueprint + phase sign-off state for Delivery Board program',
  { program_id: z.string().describe('Program id e.g. trade-ib-migration') },
  async ({ program_id }) =>
    jsonResult(await platformGet(`/api/v1/programs/${encodeURIComponent(program_id)}`)),
)

server.tool(
  'create_session',
  'Create a Session Job archive (operator). Required before report_phase_progress for a phase. Use a new session when advancing to a different phase_id.',
  {
    program_id: z.string(),
    phase_id: z.string(),
    lane_id: z.string().optional(),
    session_id: z.string().optional(),
  },
  async ({ program_id, phase_id, lane_id, session_id }) =>
    jsonResult(
      await platformPost('/api/v1/sessions', {
        program_id,
        phase_id,
        lane_id: lane_id ?? '',
        ...(session_id != null && session_id !== '' ? { session_id } : {}),
      }),
    ),
)

server.tool(
  'report_phase_progress',
  'Report agent phase progress (operator). session_id required — create_session (or Console Copy pack) first; must match program_id+phase_id. When phase has verify_cmd, status=done requires verify_passed=true.',
  {
    program_id: z.string(),
    phase_id: z.string(),
    session_id: z.string().describe('Session job id from create_session or pack header'),
    status: z.string(),
    summary: z.string().optional(),
    verify_passed: z.boolean().optional(),
  },
  async ({ program_id, phase_id, session_id, status, summary, verify_passed }) =>
    jsonResult(
      await platformPost(
        `/api/v1/programs/${encodeURIComponent(program_id)}/phases/${encodeURIComponent(phase_id)}/progress`,
        {
          status,
          summary: summary ?? '',
          verify_passed: verify_passed ?? false,
          session_id,
        },
      ),
    ),
)

server.tool(
  'submit_post_completion',
  'Submit program completion; operate items enter pending_review (operator)',
  {
    program_id: z.string(),
    new_capabilities: z.array(z.string()).optional(),
    new_risks: z.array(z.string()).optional(),
    operate_queue_items: z
      .array(z.object({
        id: z.string().optional(),
        source_lane_id: z.string().optional(),
        operate_lane: z.enum(['governance', 'troubleshoot', 'release', 'business-advisory']),
        title: z.string(),
        description: z.string().optional(),
        handoff_kind: z.enum(['one_off', 'recurring_setup']),
        reason: z.string(),
        agent_task_id: z.string().optional().describe('Validated config/agent-tasks.yaml task id'),
        acceptance_criteria: z.array(z.string()).min(1),
        verification_steps: z.array(z.string()).min(1),
        risk_level: z.enum(['low', 'medium', 'high']),
        owner: z.string().optional(),
        due_at: z.string().datetime().optional(),
      }))
      .optional(),
  },
  async ({ program_id, new_capabilities, new_risks, operate_queue_items }) =>
    jsonResult(
      await platformPost(`/api/v1/programs/${encodeURIComponent(program_id)}/complete`, {
        new_capabilities: new_capabilities ?? [],
        new_risks: new_risks ?? [],
        operate_queue_items: operate_queue_items ?? [],
      }),
    ),
)

server.tool(
  'approve_post_completion_item',
  'Owner approve pending_review operate queue item (admin)',
  { item_id: z.string(), approved_by: z.string().optional() },
  async ({ item_id, approved_by }) =>
    jsonResult(
      await platformPost(
        `/api/v1/programs/post-completion/${encodeURIComponent(item_id)}/approve`,
        { approved_by: approved_by ?? '' },
      ),
    ),
)

server.tool(
  'reject_post_completion_item',
  'Owner reject a pending operational handoff; does not inject Operate Queue (admin)',
  { item_id: z.string(), reason: z.string().min(1), decision_by: z.string().optional() },
  async ({ item_id, reason, decision_by }) =>
    jsonResult(
      await platformPost(`/api/v1/programs/post-completion/${encodeURIComponent(item_id)}/reject`, {
        reason,
        decision_by: decision_by ?? '',
      }),
    ),
)

server.tool(
  'record_no_post_completion_handoff',
  'Record explicit Owner NO HANDOFF assessment for a Program (admin)',
  { program_id: z.string(), reason: z.string().min(1), decision_by: z.string().optional() },
  async ({ program_id, reason, decision_by }) =>
    jsonResult(
      await platformPost(`/api/v1/programs/${encodeURIComponent(program_id)}/post-completion/no-handoff`, {
        reason,
        decision_by: decision_by ?? '',
      }),
    ),
)

server.tool(
  'get_operate_queue',
  'Open operate queue items (Projection layer · D11)',
  {},
  async () => jsonResult(await platformGet('/api/v1/operate/queue')),
)

server.tool(
  'record_operate_queue_execution',
  'Attach a real remediation job to an open Operate Queue handoff (operator)',
  { item_id: z.string(), execution_job_id: z.string() },
  async ({ item_id, execution_job_id }) =>
    jsonResult(
      await platformPost(`/api/v1/operate/queue/${encodeURIComponent(item_id)}/execution`, {
        execution_job_id,
      }),
    ),
)

server.tool(
  'close_operate_queue_item',
  'Close only with persisted completion evidence; linked jobs must be done and post-fix verification passed (operator)',
  {
    item_id: z.string(),
    completion_evidence: z.array(z.string()).min(1),
    post_fix_verification_passed: z.boolean().optional(),
  },
  async ({ item_id, completion_evidence, post_fix_verification_passed }) =>
    jsonResult(
      await platformPost(`/api/v1/operate/queue/${encodeURIComponent(item_id)}/close`, {
        completion_evidence,
        post_fix_verification_passed: post_fix_verification_passed ?? false,
      }),
    ),
)

server.tool(
  'dismiss_operate_queue_item',
  'Dismiss stale/resolved Operate Queue handoff with evidence (skips job/post-fix gates)',
  {
    item_id: z.string(),
    completion_evidence: z.array(z.string()).min(1),
    reason: z.enum(['stale', 'resolved', 'other']).optional(),
  },
  async ({ item_id, completion_evidence, reason }) =>
    jsonResult(
      await platformPost(`/api/v1/operate/queue/${encodeURIComponent(item_id)}/dismiss`, {
        completion_evidence,
        reason: reason ?? 'stale',
      }),
    ),
)

server.tool(
  'get_checklist_signals',
  'Latest Daily Ops Checklist per-item signals + KPIs',
  {},
  async () => jsonResult(await platformGet('/api/v1/checklist/signals')),
)

server.tool(
  'get_checklist_kpis',
  'Checklist quiet-success streak + last-run summary',
  {},
  async () => jsonResult(await platformGet('/api/v1/checklist/kpis')),
)

server.tool(
  'get_telemetry_overview',
  'Prometheus telemetry overview snapshot (preset metrics)',
  { namespace: z.string().optional().describe('Optional K8s namespace filter') },
  async ({ namespace }) => {
    const qs = namespace != null && namespace !== '' ? `?ns=${encodeURIComponent(namespace)}` : ''
    return jsonResult(await platformGet(`/api/v1/telemetry/overview${qs}`))
  },
)

server.tool(
  'get_telemetry_alerts',
  'Prometheus firing and pending alerts',
  {},
  async () => jsonResult(await platformGet('/api/v1/telemetry/alerts')),
)

server.tool(
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

server.tool(
  'report_checklist_signals',
  'Merge Daily Ops Checklist probe signals (runner daily-ops-checklist-run)',
  {
    run_id: z.string().optional(),
    source: z.string().optional(),
    signals: z
      .array(
        z.object({
          item_id: z.string(),
          signal: z.enum(['ok', 'degraded', 'fail', 'unknown']),
          detail: z.string().optional(),
          env: z.string().optional(),
        }),
      )
      .min(1),
    auto_dispatch: z.boolean().optional(),
  },
  async ({ run_id, source, signals, auto_dispatch }) =>
    jsonResult(
      await platformPost('/api/v1/checklist/signals', {
        run_id: run_id ?? '',
        source: source ?? 'mcp',
        signals,
        auto_dispatch: auto_dispatch ?? false,
      }),
    ),
)

server.tool(
  'sign_tier_b',
  'Record Tier B Owner sign-off (admin)',
  { notes: z.string().optional() },
  async ({ notes }) =>
    jsonResult(await platformPost('/api/v1/promote/tier-b/signoff', { notes: notes ?? '' })),
)

// --- Dev Sessions tools ---

server.tool('list_dev_sessions', 'List sessions for the viewer seat (local bdev in DEV; catalog Deployments in STG/PROD)', {}, async () =>
  jsonResult(await platformGet('/api/v1/dev-sessions/')),
)

server.tool(
  'restart_dev_session',
  'Restart a session by name (bdev locally; K8s rollout restart in STG/PROD)',
  { name: z.string().describe('Session name from list_dev_sessions (e.g. platform-api, platform-console, git-bridge; compat: platform → api+console)') },
  async ({ name }) =>
    jsonResult(
      await platformPost(`/api/v1/dev-sessions/${encodeURIComponent(name)}/control`, {
        action: 'restart',
      }),
    ),
)

server.tool(
  'get_dev_session_logs',
  'Get recent log lines from a session (bdev log file or K8s pod logs)',
  {
    name: z.string().describe('Session name from list_dev_sessions'),
    lines: z.number().optional().describe('Number of log lines to return (default 100)'),
  },
  async ({ name, lines }) => {
    const n = lines ?? 100
    return jsonResult(
      await platformGet(`/api/v1/dev-sessions/${encodeURIComponent(name)}/logs?lines=${n}`),
    )
  },
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
