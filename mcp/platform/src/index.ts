#!/usr/bin/env node
/**
 * Bifrost Ops Platform MCP server (P5).
 * Proxies platform-api — same routes, Bearer auth, audit on actuation side.
 */
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js'
import { z } from 'zod'
import { getRequest, listRequests, requestAction, waitForRequest } from './approvalTools.js'
import { registerQuestionTools } from './registerQuestions.js'
import { jsonResult, platformDelete, platformGet, platformPost } from './platformClient.js'
import { registerPrometheusBridge } from './prometheusBridge.js'
import { registerApproveBridge } from './registerApprove.js'
import { registerLocalBridge } from './registerLocal.js'
import { focusAllowList } from './focusBridges.js'
import {
  RUN_POLL_DEFAULT_SECONDS,
  RUN_POLL_MIN_SECONDS,
  RUN_WAIT_DEFAULT_SECONDS,
  RUN_WAIT_MAX_SECONDS,
  checkReleasePolicy,
  getDelivery,
  waitForPipelineRun,
} from './deliveryTools.js'
import { AUDIT_MAX_LIMIT, REQUESTS_DEFAULT_LIMIT, REQUESTS_MAX_LIMIT, filterAudit } from './readFilters.js'
import {
  PROPOSAL_KEY_RULE,
  PROPOSE_MISSION_INPUT,
  RUNTIME_DATA_NOTE,
  STOP_KEY_RULE,
  WITHDRAW_KEY_RULE,
  WITHDRAW_TASKS_INPUT,
  withdrawTasks,
  getArtifact,
  searchArtifacts,
  getMission,
  getPlan,
  getPlanView, registerExternalWork, recordExternalWorkEvent,
  COMMIT_SHA,
  isRFC3339,
  getTask,
  listTasks,
  proposeMission,
  proposePlanChange,
  proposeTasks,
  putArtifact,
  requestStop,
  whoami,
  WAVE_NOTE,
  openWave,
  endWave,
  listWaves,
} from './runtimeTools.js'
import { registerMessageTools } from './registerMessages.js'
import { registerPlanImportTools } from './registerPlanImport.js'
import { registerIssueTools } from './registerIssues.js'

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

reg(
  'get_audit_log',
  `Actuation audit records, newest first: {records: [{id, at, actor, role, action, target, status, detail}]}. The platform keeps the newest ${AUDIT_MAX_LIMIT}. With no argument every kept record is returned. The filters are applied by this tool, not by the server (records must pass all that are given), and the answer then also carries matched (records that passed) and returned.`,
  {
    action_prefix: z.string().optional().describe('Only actions starting with this, e.g. release_policy.'),
    since: z.string().refine(isRFC3339, 'an RFC 3339 date and time, as 2026-11-12T09:00:00-06:00').optional().describe('Only records at or after this time (RFC 3339)'),
    since_minutes: z.number().int().min(1).max(43200).optional().describe('Only records of the last n minutes. With since too, the later of the two bounds holds'),
    target_contains: z.string().optional().describe('Only records whose target contains this text, ignoring case'),
    limit: z.number().int().min(1).max(AUDIT_MAX_LIMIT).optional().describe(`At most this many, newest first (max ${AUDIT_MAX_LIMIT}). Omitted: all that matched`),
  },
  async (filter) => jsonResult(filterAudit(await platformGet('/api/v1/audit'), filter, Date.now())),
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
  'Start Tekton PipelineRun (operator) and return {ok, action, target, message, run}. The call goes to the run route itself and never creates an approval request. A tier C (PROD) run starts only when the signed release policy covers this exact request; otherwise nothing starts and the answer is {started: false, error: "approval required", action, release_policy: [the policy\'s reasons], note} (ask first with check_release_policy). Pass revision (Gitea tag) to pin deploy version. Pass who (same value as release.sh hold prints) when a release window is open — the server refuses without it. For bifrost-deliver-research, pass tag (semver image pin, e.g. 0.48.4) — default tag is the moving smoke tag `dev` and must not pin k8s. For bifrost-build-research-dagster, pass the same semver: the `-dagster` suffix is appended for you, because both research image lines share one repository and a bare tag would build the Dagster image over the one research-api runs.',
  {
    name: z.string(),
    revision: z.string().optional(),
    tag: z.string().optional(),
    who: z.string().optional(),
    params: z.record(z.string(), z.string()).optional().describe('Pinned git revisions only: revision or *Revision params the Pipeline declares, each a 40-character lowercase SHA. Source, registry, image and tag are not settable.'),
  },
  async ({ name, revision, tag, who, params }) =>
    jsonResult(
      await platformPost(`/api/v1/delivery/pipelines/${encodeURIComponent(name)}/runs`, {
        revision: revision ?? '',
        ...(tag != null && tag.trim() !== '' ? { tag: tag.trim() } : {}),
        ...(who != null && who.trim() !== '' ? { who: who.trim() } : {}),
        ...(params != null && Object.keys(params).length > 0 ? { params } : {}),
      }),
    ),
)

reg(
  'check_release_policy',
  'Judge this exact run request against the signed release policy, once, without starting anything: no run is started and no approval request is created. Returns {action, pipeline, tier, needs_approval, covered, policy_id, reasons, clauses, shas, outcome}: covered says whether start_pipeline_run with the same arguments would start, and reasons lists every reason when it would not. Pass the same name, revision, tag, who and params you would pass to start_pipeline_run.',
  {
    name: z.string().describe('Pipeline name, e.g. bifrost-deliver-platform-prod'),
    revision: z.string().optional(),
    tag: z.string().optional(),
    who: z.string().optional().describe('The release window holder, as for start_pipeline_run'),
    params: z.record(z.string(), z.string()).optional().describe('Pipeline params, as for start_pipeline_run'),
  },
  async (input) => jsonResult(await checkReleasePolicy(input)),
)

reg(
  'wait_for_pipeline_run',
  `Wait for a Tekton PipelineRun to finish, polling the delivery read routes, and return {found, run, pipeline, revision, status, reason, finished, timed_out, started_at, completed_at, duration_seconds, tasks: [{name, status, reason, duration_seconds}], phases, waited_seconds}. status is Succeeded, Failed, Running, Pending or NotFound; reason is Tekton's (Cancelled, PipelineRunTimeout, …). Name the run, or pipeline and revision: then the newest run of that pipeline whose revision param equals the commit is the one waited for. A run that does not exist yet is waited for until the timeout (found: false). Read-only: it starts and cancels nothing. A task's duration_seconds is null while the steps route reports no task times.`,
  {
    run: z.string().optional().describe('PipelineRun name, e.g. bifrost-deliver-platform-1762950000'),
    pipeline: z.string().optional().describe('Pipeline name. Required with revision; with run it only saves a lookup'),
    revision: z.string().regex(/^[0-9a-f]{40}$/, 'a 40-character lowercase git SHA').optional().describe('With pipeline: the commit the run was started for'),
    timeout_seconds: z.number().int().min(1).max(RUN_WAIT_MAX_SECONDS).optional().describe(`Default ${RUN_WAIT_DEFAULT_SECONDS}, max ${RUN_WAIT_MAX_SECONDS}`),
    poll_seconds: z.number().int().min(RUN_POLL_MIN_SECONDS).max(60).optional().describe(`Default ${RUN_POLL_DEFAULT_SECONDS}, min ${RUN_POLL_MIN_SECONDS}`),
  },
  async (input) => jsonResult(await waitForPipelineRun(input)),
)

reg(
  'get_delivery',
  'Read the delivery progress strip of one change (handed in → CI → on main → STG → PROD). Name exactly one of commit or task. Returns the server body: {schema, commit, task_id, attempt_id, plan_node_id, repo, operation, stages: [{stage, state, entered_at, left_at, actor, actor_role, waits_on, reason, source}], warnings, as_of}; warnings say which stages could not be read. Read-only.',
  {
    commit: z.string().optional().describe('Commit SHA, or a prefix of 7 or more lowercase hex characters'),
    task: z.string().optional().describe('Agent runtime task id (t_…)'),
  },
  async (input) => jsonResult(await getDelivery(input)),
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
  'get_commit_evidence',
  'Is this commit green? Read from the evidence ledger (read-only; nothing is started). Returns {repo, commit, tree, unit, verdict, reason, tasks: [{task, verdict, input_key, inputs, beside: [{repo, commit, tree}], rows}], commit_rows}. verdict is green (every CI task has a full pass on these exact inputs: the commit\'s git tree and the tree of the main head of each repository used beside it), red (a task failed on them and none passed in full since) or none (nothing ran on these inputs, or only part was tested: start a CI run, do not read it as red). The key is content, not the commit, so another commit with the same tree has the same answer. commit_rows is what was recorded for this commit itself, whatever the inputs were then.',
  {
    repo: z.string().describe('Repository name, e.g. bifrost-platform'),
    commit: z.string().regex(/^[0-9a-f]{40}$/, 'a 40-character lowercase git SHA'),
  },
  async ({ repo, commit }) =>
    jsonResult(await platformGet(`/api/v1/evidence/commit?repo=${encodeURIComponent(repo)}&commit=${encodeURIComponent(commit)}`)),
)

reg(
  'compare_commit_evidence',
  'Put the evidence ledger beside the release policy\'s CI reading for every commit that reading has a verdict for (read-only; nothing is started or written). Returns {at, counts, commits: [{repo, commit, old, old_run, verdict, as_tested, class, note}], skipped}. old is the policy\'s reading (Succeeded / Failed / Unknown), verdict the ledger\'s answer now (green / red / none), as_tested whether the ledger has a full pass recorded for the commit itself. class: agree; beside_moved (old green, the full pass is in the ledger, a repository beside it moved since); not_in_ledger (old green, no full pass in the ledger: a missed input, must be zero); ledger_only (ledger green, old not).',
  {},
  async () => jsonResult(await platformGet('/api/v1/evidence/compare')),
)

reg(
  'get_unit_plan',
  'CI\'s plan of one commit, computed from its tree and its bifrost.units.yaml (read-only; nothing is started). Returns {repo, commit, tree, pins: [{repo, commit, tree}], units: [{unit, task, module, input_key, shards, verdict}], to_run, counts}. A unit is a Go package with tests, the console, or the repository; input_key is the hash of exactly what the task reads; verdict is what the evidence ledger says of that key (green, red, none); to_run counts the tasks that are not green. Pass inputs: true to get the document each key is the hash of. The first plan after a platform restart reads every Go file once and can take a minute.',
  {
    repo: z.string().describe('Repository name, e.g. bifrost-platform'),
    commit: z.string().regex(/^[0-9a-f]{40}$/, 'a 40-character lowercase git SHA'),
    inputs: z.boolean().optional().describe('Include each task\'s key document'),
  },
  async ({ repo, commit, inputs }) =>
    jsonResult(
      await platformGet(`/api/v1/units/plan?repo=${encodeURIComponent(repo)}&commit=${encodeURIComponent(commit)}${inputs ? '&inputs=1' : ''}`),
    ),
)

reg(
  'request_unit_evidence',
  'Have the platform run the unit tasks of one commit that the evidence ledger has no verdict on (operator, tier B). It computes the commit\'s plan and creates the TaskRuns that are not there yet; a run is named by its plan and number, so asking twice starts nothing twice. Returns {repo, commit, plan, mode, tasks, to_run, created: [run names], present, deferred, retried: [run names], refused: [why a task cannot be dispatched], lost: [runs the cluster lost twice]}. deferred counts runs put off because the platform\'s limit of unit runs at a time is reached: ask again later. Results arrive in the ledger as the runs finish; read them with get_unit_plan. Answers 409 while unit dispatch is off on that platform.',
  {
    repo: z.string().describe('Repository name, e.g. bifrost-platform'),
    commit: z.string().regex(/^[0-9a-f]{40}$/, 'a 40-character lowercase git SHA'),
  },
  async ({ repo, commit }) => jsonResult(await platformPost('/api/v1/units/evidence', { repo, commit })),
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
  'plan_manifest',
  'Render a git path at a full commit and return a plan id. Tier B.',
  {
    repo: z.string().describe('Repository name from the actuation policy'),
    path: z.string().describe('Path inside the repository'),
    commit: z.string().describe('Full 40-character git SHA'),
  },
  async ({ repo, path, commit }) =>
    jsonResult(await platformPost('/api/v1/actuation/manifests/plan', { repo, path, commit })),
)

reg(
  'create_job_from_cronjob',
  'Create one Job from a CronJob. Tier follows the actuation policy; denied namespaces are refused.',
  {
    namespace: z.string(),
    cronjob: z.string(),
  },
  async ({ namespace, cronjob }) =>
    jsonResult(await platformPost('/api/v1/actuation/jobs/from-cronjob', { namespace, cronjob })),
)

reg(
  'delete_finished_jobs',
  'Delete Jobs that are Complete or Failed, and finished probe Pods. Provide names or label_selector.',
  {
    namespace: z.string(),
    names: z.array(z.string()).optional(),
    label_selector: z.string().optional(),
  },
  async ({ namespace, names, label_selector }) =>
    jsonResult(
      await platformPost('/api/v1/actuation/jobs/delete-finished', {
        namespace,
        ...(names != null ? { names } : {}),
        ...(label_selector != null && label_selector !== '' ? { label_selector } : {}),
      }),
    ),
)

reg(
  'run_probe_pod',
  'Run a short-lived probe Job from an allow-listed image. env_from is a Deployment name in the same namespace.',
  {
    namespace: z.string(),
    image: z.string(),
    command: z.array(z.string()),
    args: z.array(z.string()).optional(),
    env_from: z.string().optional(),
    timeout_seconds: z.number().int().min(1).max(900).optional(),
  },
  async ({ namespace, image, command, args, env_from, timeout_seconds }) =>
    jsonResult(
      await platformPost('/api/v1/actuation/probes', {
        namespace,
        image,
        command,
        ...(args != null ? { args } : {}),
        ...(env_from != null && env_from !== '' ? { env_from } : {}),
        ...(timeout_seconds != null ? { timeout_seconds } : {}),
      }),
    ),
)

reg(
  'request_action',
  'Create an approval request and return its id and number (#n). Does not run the action.',
  {
    action: z.string().describe('Action id from the approvals catalog'),
    params: z.record(z.string(), z.unknown()).optional().describe('Parameters signed into the approval'),
    reason: z.string().describe('Why this action is requested'),
    rollback: z.string().describe('How to undo it if the approval is executed'),
    work_id: z.string().optional().describe('Work item this request belongs to: W-n, TD-n or LANE-name'),
    requester_thread: z.string().optional().describe('One line naming the requesting thread'),
    drill: z.boolean().optional().describe('A drill: shown on the pages and in the notice as one, never approved, nothing runs'),
  },
  async ({ action, params, reason, rollback, work_id, requester_thread, drill }) =>
    jsonResult(await requestAction({ action, params, reason, rollback, work_id, requester_thread, drill })),
)

reg(
  'get_request',
  'Read one approval request by id (appr_…) or number (57 or #57)',
  { id: z.string() },
  async ({ id }) => jsonResult(await getRequest(id)),
)

reg(
  'list_requests',
  `List approval requests: {approvals: [...]}, newest created first. status is pending (default), open (pending, approved, running, unknown) or all, and is applied by the server. action and limit are filtered by this tool: with either, only requests of that action are kept, at most limit of them (default ${REQUESTS_DEFAULT_LIMIT}, max ${REQUESTS_MAX_LIMIT}), and the answer also carries matched (requests that passed) and returned. With neither, the whole list is returned.`,
  {
    status: z.enum(['pending', 'open', 'all']).optional(),
    action: z.string().optional().describe('Exact action id, e.g. start_pipeline_run'),
    limit: z.number().int().min(1).max(REQUESTS_MAX_LIMIT).optional().describe(`Newest first; default ${REQUESTS_DEFAULT_LIMIT} when action is given, max ${REQUESTS_MAX_LIMIT}`),
  },
  async ({ status, action, limit }) => jsonResult(await listRequests(status ?? 'pending', { action, limit })),
)

reg(
  'wait_for_request',
  'Poll an approval until executed, failed, rejected, expired, unknown, or the timeout. approved and running keep waiting.',
  {
    id: z.string().describe('Approval id (appr_…) or number (57 or #57)'),
    timeout_seconds: z.number().int().min(1).max(600).optional().describe('Default 120, max 600'),
  },
  async ({ id, timeout_seconds }) => jsonResult(await waitForRequest(id, timeout_seconds ?? 120)),
)

registerQuestionTools(reg)

// The agent runtime (design v5 §5.2 「MCP 工具」). Writes are recorded as
// this thread; see runtimeTools.ts.
reg('whoami', 'Agent runtime: who this caller is, who its writes are recorded as and what it may do', {}, async () =>
  jsonResult(await whoami()),
)

reg(
  'get_plan',
  `Agent runtime plan: the roots; with root, that subtree (each step's state and the event it came from); with node, one node in full. ${RUNTIME_DATA_NOTE}`,
  { root: z.string().optional(), node: z.string().optional() },
  async (input) => jsonResult(await getPlan(input)),
)


reg('get_plan_view', `One read-only plan snapshot, including external work freshness. ${RUNTIME_DATA_NOTE}`,
  { root: z.string(), recent_cursor: z.string().optional() }, async (input) => jsonResult(await getPlanView(input)))
// registerTool accepts an intact Zod object; legacy tool(rawShape) would
// recreate a stripping object and silently discard misspelled bindings.
if (!allow || allow.has('register_external_work')) {
  server.registerTool('register_external_work', {
    description: 'Register external work before starting it. Registration grants no execution or completion authority. artifact_id names the work\'s description, uploaded first with put_artifact (kind "other", media_type "application/json"). The description is one JSON object with exactly these nine fields, all required, no others: {"v": 1, "kind": "agentrt.external_work/v1", "task": "Add release-policy tools to the MCP server", "executor": "claude-code", "provider": "anthropic", "repo": "bifrost-platform", "branch": "w2b/mcp", "acceptance_refs": ["mcp/platform type-check and tests pass"], "estimate_minutes": [30, 90]}. Every text is non-empty and at most 2000 bytes; acceptance_refs has at least one entry; estimate_minutes is two integers [low, high] with 0 <= low <= high. Returns {external_work_id, event_id, seq, state, plan_url, registered_at}.',
    inputSchema: z.object({
      idempotency_key: z.string().min(1), root: z.string(), plan_node_id: z.string(), source_run_key: z.string(), artifact_id: z.string(),
      mission_id: z.string().optional(), task_id: z.string().optional(), observed_at: z.string().optional(),
    }).strict(),
  }, async (input) => jsonResult(await registerExternalWork(input)))
}
if (!allow || allow.has('record_external_work_event')) {
  server.registerTool('record_external_work_event', {
    description: 'Append an external work observation and return {external_work_id, event_id, seq, state, plan_url}. Submitted is not verified or done. seq is optional: leave it out and the platform assigns the next one; given, it must be the work\'s last seq + 1. idempotency_key is optional: leave it out and one is generated and returned as idempotency_key; to retry an event, send it again with the same key. commit_shas (full lowercase SHAs) may be named on heartbeat and submitted events; the report digests and exit_code on submitted only.',
    inputSchema: z.object({
      external_work_id: z.string(), idempotency_key: z.string().min(1).optional(), seq: z.number().int().min(1).optional(),
      type: z.enum(['started', 'heartbeat', 'submitted', 'cancelled', 'linked']), observed_at: z.string().optional(),
      root: z.string().optional(), plan_node_id: z.string().optional(), mission_id: z.string().optional(), task_id: z.string().optional(),
      report_path_digest: z.string().optional(), report_digest: z.string().optional(), commit_shas: z.array(z.string().regex(COMMIT_SHA, 'a full lowercase git SHA')).max(100).optional(), exit_code: z.number().int().optional(),
    }).strict(),
  }, async (input) => jsonResult(await recordExternalWorkEvent(input)))
}

reg(
  'get_mission',
  `Agent runtime mission (M-n) with its tasks counted by state. ${RUNTIME_DATA_NOTE}`,
  { id: z.string() },
  async ({ id }) => jsonResult(await getMission(id)),
)

reg(
  'list_tasks',
  `Agent runtime tasks, oldest first, at most 500; filter by mission (M-n) and state. ${RUNTIME_DATA_NOTE}`,
  { mission: z.string().optional(), state: z.string().optional() },
  async (input) => jsonResult(await listTasks(input)),
)

reg(
  'get_task',
  `Agent runtime task (t_…). ${RUNTIME_DATA_NOTE}`,
  { id: z.string() },
  async ({ id }) => jsonResult(await getTask(id)),
)

reg(
  'propose_mission',
  `Propose a mission; the platform numbers it (M-n). It names a title, a goal and its success criteria; tenant dev (the default) or business; the repos it works in; max_action none or branch (the default); a budget, or none to take the tenant's budget rule's; no_merge, for a drill, a benchmark replay or a gate walk, says why its accepted results stay off main (the coordinator's merge passes it over); needs_owner, for a mission whose cards must wait for the Owner himself, says why (a card-auto-answer rule, STG only, never answers them; a walk does not set it: the paths behind his buttons are covered by the Console's tests, TD-393). Inside the standing envelope in force for its tenant it starts active; otherwise it waits as awaiting_owner on the Owner's decision card (the answer names question_id) until the Owner starts or stops it, or stays proposed where the platform has no question store; the answer says why. Words a card cannot hold are refused: a title of up to 200 bytes, a goal of up to 1200, at most 10 success criteria of up to 200 bytes each, at most 12 repos; put the details in a document the goal names. Then propose its tasks (propose_tasks). ${PROPOSAL_KEY_RULE}`,
  PROPOSE_MISSION_INPUT,
  async (input) => jsonResult(await proposeMission(input)),
)

reg(
  'propose_tasks',
  `Propose tasks to a mission, all or nothing. Upload each spec first (put_artifact, kind task_spec). A task names its plan_node_id or, off the plan, its off_plan_reason. Roles: plan, implement, brief, other (verification and review are the runtime's). An implement task on a branch has as its spec the JSON task spec store.ImplementSpec builds ({"text", "verify": [[argv...]], "setup": [[argv...]], ...}), with its repository's verify commands and setup; a markdown spec, or one without verify or setup, is refused. depends_on: refs of this proposal or tasks of the mission. Tasks start proposed; the runtime readies them. ${PROPOSAL_KEY_RULE}`,
  {
    mission_id: z.string(),
    tasks: z.array(
      z.object({
        ref: z.string(),
        plan_node_id: z.string().optional(),
        off_plan_reason: z.string().optional(),
        subject_task_id: z.string().optional(),
        role: z.enum(['plan', 'implement', 'brief', 'other']),
        task_class: z.string(),
        title: z.string(),
        spec_artifact_id: z.string(),
        effect_class: z.enum(['none', 'branch']),
        repo: z.string().optional(),
        base_commit: z.string().optional(),
        core: z.boolean().optional(),
        needs: z.array(z.string()).optional(),
        depends_on: z.array(z.string()).optional(),
        hard_limit_s: z.number().int().min(0).max(86400).optional(),
        wave: z.string().optional().describe('The open wave (open_wave) the task belongs to: held when accepted, landed with its wave'),
      }),
    ),
    idempotency_key: z.string().min(1).describe('One key per proposal; see the description'),
  },
  async (input) => jsonResult(await proposeTasks(input)),
)

registerMessageTools(reg)
registerPlanImportTools(reg)
registerIssueTools(reg)

reg(
  'propose_plan_change',
  `Propose changes to a plan tree (root R2, …). Ops: adopt_plan_view (artifact digest, expected_version; dev only, no completion bindings, existing decision path), add_node (parent, kind item or step; a step needs step_kind and may name mission_id), retitle (node, title), set_done_rule (node, rule, bind: approvals and questions the rule names), rebind_step (node, mission_id: the step and its tasks move), move_task (task, node), drop_step (node: the step leaves the count and keeps its number; refused while it has unfinished tasks), set_marks (node, owner_step, effect_step: either or both). Within the standing authorization (a step added to an item with an active step, the title of a step not started, and while the signed plan-standing rule grants it, the first done rule of a step not done that names no manual atom) the change lands at once; otherwise it is checked, rolled back, and the Owner decides on a card; a change the plan's rules refuse is refused with the reason. ${PROPOSAL_KEY_RULE}`,
  {
    root: z.string(),
    reason: z.string(),
    changes: z.array(
      z.object({
        op: z.enum(['add_node', 'retitle', 'set_done_rule', 'rebind_step', 'move_task', 'drop_step', 'set_marks', 'adopt_plan_view']),
        node: z.string().optional(),
        parent: z.string().optional(),
        kind: z.enum(['item', 'step']).optional(),
        step_kind: z.string().optional(),
        title: z.string().optional(),
        mission_id: z.string().optional(),
        rule: z.unknown().optional(),
        bind: z.array(z.object({ kind: z.enum(['approval', 'question']), ref: z.string() })).optional(),
        task: z.string().optional(),
        owner_step: z.boolean().optional(),
        effect_step: z.boolean().optional(),
        artifact: z.string().optional(),
        expected_version: z.number().int().min(0).optional(),
      }),
    ),
    idempotency_key: z.string().min(1).describe('One key per proposal; see the description'),
  },
  async (input) => jsonResult(await proposePlanChange(input)),
)

reg(
  'put_artifact',
  'Upload text to the agent runtime as an artifact (addressed by its sha256; uploading it again is harmless). A local path in it is refused for most kinds.',
  {
    kind: z.string().describe('task_spec, mission_spec, brief, report, message, …'),
    content: z.string(),
    media_type: z.string().optional().describe('Default text/markdown'),
  },
  async (input) => jsonResult(await putArtifact(input)),
)

reg(
  'search_artifacts',
  `Agent runtime artifacts' metadata, newest first, at most limit (1–100, default 50): narrowed by kind (any of them), task (t_…) or mission (M-n) — the artifacts its records name —, author (the actor that stored it), since (RFC 3339); before continues from a previous answer's next. No bodies: read one with get_artifact. ${RUNTIME_DATA_NOTE}`,
  {
    kind: z.array(z.enum(['task_spec', 'mission_spec', 'report', 'verify', 'review', 'log_tail', 'checkpoint_patch', 'handover', 'brief', 'message', 'exam', 'other'])).optional(),
    task: z.string().optional(),
    mission: z.string().optional(),
    author: z.string().optional().describe('owner, controller, thread:<session>, seat:<name>, host:<id>, exec:<tool>'),
    since: z.string().optional().describe('RFC 3339'),
    limit: z.number().int().min(1).max(100).optional(),
    before: z.string().optional().describe("A previous answer's next"),
  },
  async (input) => jsonResult(await searchArtifacts(input)),
)

reg(
  'get_artifact',
  `Read an agent runtime artifact (sha256:…). ${RUNTIME_DATA_NOTE}`,
  { id: z.string() },
  async ({ id }) => jsonResult(await getArtifact(id)),
)

reg(
  'agentrt_withdraw_tasks',
  `Cancel tasks this session (or its mission's proposer) proposed, provided no attempt has started. This is not a stop: stop work that has started with request_stop; only the Owner lifts a stop. ${WITHDRAW_KEY_RULE}`,
  WITHDRAW_TASKS_INPUT,
  async (input) => jsonResult(await withdrawTasks(input)),
)

reg(
  'request_stop',
  `Stop or pause agent runtime work: global, a mission (M-n), a task (t_…) or a host. Anyone may set one; only the Owner lifts it. ${STOP_KEY_RULE}`,
  {
    scope: z.enum(['global', 'mission', 'task', 'host']),
    scope_ref: z.string().optional().describe('The mission, task or host; none for global'),
    kind: z.enum(['stop', 'pause']),
    reason: z.string(),
    idempotency_key: z.string().min(1).describe('One key per stop; see the description'),
  },
  async (input) => jsonResult(await requestStop(input)),
)

reg(
  'open_wave',
  `Open a wave of a repository on its main head; it answers the wave (id, n, base). Propose its tasks with propose_tasks and wave set to the id. ${WAVE_NOTE}`,
  {
    repo: z.string().describe('bifrost-platform'),
    plan_root: z.string().describe('The plan root the wave serves, e.g. R6'),
  },
  async (input) => jsonResult(await openWave(input)),
)

reg(
  'close_wave',
  `Close a wave so its members land together once (action close), or abandon it so nothing of it lands (action abandon). Close is refused while a member is not yet accepted. ${WAVE_NOTE}`,
  {
    id: z.string(),
    action: z.enum(['close', 'abandon']),
  },
  async (input) => jsonResult(await endWave(input)),
)

reg(
  'list_waves',
  `The waves (id, repo, plan_root, n, base, state), of one repository or all, optionally filtered by state (open, closed, abandoned). ${WAVE_NOTE}`,
  {
    repo: z.string().optional(),
    state: z.enum(['open', 'closed', 'abandoned']).optional(),
  },
  async (input) => jsonResult(await listWaves(input)),
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
