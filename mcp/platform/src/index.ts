#!/usr/bin/env node
import { actionTraceFields } from './actionTraceFields.js'
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
  RUNTIME_DATA_NOTE,
  STOP_KEY_RULE,
  getArtifact,
  searchArtifacts,
  getPlan,
  getPlanView, registerExternalWork, recordExternalWorkEvent,
  COMMIT_SHA,
  defaultRuntimeDeps,
  isRFC3339,
  proposePlanChange,
  putArtifact,
  requestStop,
  whoami,
} from './runtimeTools.js'
import { registerMessageTools } from './registerMessages.js'
import { getReleaseWindow, holdReleaseWindow, releaseReleaseWindow } from './windowTools.js'
import { presenceSkip, registerPresence } from './presence.js'
import { registerPlanImportTools } from './registerPlanImport.js'
import { CATALOG_INDEX_PATH, GET_CATALOG_INDEX_DESCRIPTION, GET_OWNER_DESCRIPTION, GET_UNPERCEIVED_DESCRIPTION, ownerPath, unperceivedPath } from './catalogTools.js'
import { FULL_CI_PATH, fullCICheckPath, unitConfigPath, unitRunsPath, unitScopePath } from './unitsTools.js'
import { BUS_WAIT_MAX_SECONDS, GET_BUS_STATES_DESCRIPTION, LIST_REGRESSIONS_DESCRIPTION, MARK_REGRESSION_FIXED_DESCRIPTION, REPORT_REGRESSION_DESCRIPTION, WAIT_FOR_EVENT_DESCRIPTION, busEventsPath, busStatesPath, regressionsPath } from './busTools.js'

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
    ...actionTraceFields,
    name: z.string(),
    revision: z.string().optional(),
    tag: z.string().optional(),
    who: z.string().optional(),
    params: z.record(z.string(), z.string()).optional().describe('Pinned git revisions only: revision or *Revision params the Pipeline declares, each a 40-character lowercase SHA. Source, registry, image and tag are not settable.'),
  },
  async ({ name, revision, tag, who, params, reason, unchecked, rollback }) =>
    jsonResult(
      await platformPost(`/api/v1/delivery/pipelines/${encodeURIComponent(name)}/runs`, {
        reason, unchecked, rollback,
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
  `Wait for a Tekton PipelineRun to finish, polling the delivery read routes, and return {found, run, pipeline, revision, status, reason, finished, timed_out, started_at, completed_at, duration_seconds, tasks: [{name, status, reason, duration_seconds}], phases, results, waited_seconds}. results is a name-to-value object from the steps response (for example ui-commit and infra-commit), or {} when absent on an older server. status is Succeeded, Failed, Running, Pending or NotFound; reason is Tekton's (Cancelled, PipelineRunTimeout, …). Name the run, or pipeline and revision: then the newest run of that pipeline whose revision param equals the commit is the one waited for. A run that does not exist yet is waited for until the timeout (found: false). Read-only: it starts and cancels nothing. A task's duration_seconds is null while the steps route reports no task times. If the caller's own tool timeout is shorter than timeout_seconds, the caller will cut the call off; set a smaller timeout_seconds and call again in a loop.`,
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

reg(
  'ensure_bifrost_namespaces',
  'Create the named Bifrost namespaces that do not exist yet (tier C). Each name must be under bifrost_namespaces in the platform config.',
  { names: z.array(z.string()).min(1) },
  async ({ names }) => jsonResult(await platformPost('/api/v1/cluster/namespaces/ensure-bifrost', { names })),
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
  'get_release_context',
  'One read of the live release situation of one repository (read-only; nothing is started or written): ' +
    'what STG and PROD run (release records), main\'s head on the Gitea mirror, whether main is ahead of STG, ' +
    'PipelineRuns of the repo not finished (CI, landing, delivery), approvals still pending that concern it, ' +
    'the release window as stored (holder and queue), the latest time the Owner decided an approval or answered a question ' +
    '(only that time; it says nothing about whether the Owner is around), and the known regressions on main with no fix recorded (information, no gate). ' +
    'Returns {schema, repo, commit?, as_of, facts: {stg, prod, main, main_ahead_of_stg, runs_in_flight, pending_approvals, window, owner_last_action, open_regressions}, verdicts?}. ' +
    'Every fact is {value, read_at, source, error?}: each is read on its own with a timeout, and one that cannot be read now is value null with error set ' +
    '(an unsettled STG included) instead of an older value. With commit, verdicts {commit_is_stg, main_ahead_of_commit, others_in_flight} are each {value: true|false|null, why}. ' +
    'Unknown repo answers 400.',
  {
    repo: z.string().describe('Repository name, e.g. bifrost-platform'),
    commit: z
      .string()
      .regex(/^[0-9a-f]{40}$/, 'a 40-character lowercase git SHA')
      .optional()
      .describe('Commit to judge (adds verdicts)'),
  },
  async ({ repo, commit }) => {
    const q = new URLSearchParams({ repo })
    if (commit) q.set('commit', commit)
    return jsonResult(await platformGet(`/api/v1/delivery/context?${q.toString()}`))
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

// The CI views' data, for the lines and agents that use CI (the Owner,
// 2026-11-14: CI's users are the lines up and down the ring and CI itself; they
// use it through these answers, the page is one more reader). Each body is
// the contract in api/internal/server/testdata/contract/units, compared with
// what the route computes on every test run.
const SHA = z.string().regex(/^[0-9a-f]{40}$/, 'a 40-character lowercase git SHA')
const REPO = z.string().regex(/^[A-Za-z0-9][A-Za-z0-9._-]*$/, 'a repository name').describe('Repository name as Gitea has it')

reg(
  'get_unit_scope',
  'Which modules of one commit CI has to verify again, against a base commit of the same repository (read-only; nothing is started). The answer is computed from the two commits\' own declarations (bifrost.units.yaml): a module whose tasks all keep the keys they had at the base does not run, and the base\'s passing CI run stands for it. Returns {repo, commit, base, declared, modules: [{module, tasks, changed, run, why, run_units, same_units}], note?, base_run?}. modules[].run is whether the module runs; run_units and same_units name the module\'s units (a Go package with tests is one) that have a task whose key the base does not have, and those whose tasks all keep the base\'s keys: CI acts on run alone, the units say what a run by unit would be; changed of tasks is how many of its tasks have a key the base does not have; why names what differs, most tasks first: "needs <input>" is a declared input by its reference ("needs other-repo:path" is a path of a pinned repository, "needs ." the whole repository), any other word is a part of the key ("tree" the module\'s own directory, "libs" Go library code it imports, "tests"/"test_files" its tests, "script" the entry script, "image", "new: the base has no such task"). base_run is the CI run whose pass stands for the modules that do not run. EVERYTHING RUNS, and note says why, when: declared is false (the commit has no bifrost.units.yaml: the project is one block); the declaration does not read (note carries the parser\'s reason); no base was given, or the base is the commit itself; the base has no declaration that reads; or the evidence ledger has no passing full CI of the base ("it says none" or "it says Failed"): a base that was red or never verified vouches for nothing. In those cases every listed module has run=true and why repeats the note. A commit or base Gitea does not have is an error, not "everything runs". The first answer after a platform restart can take about 30 seconds.',
  { repo: REPO, commit: SHA, base: SHA.optional().describe('The commit to compare with, usually the merge base with main; left out, everything runs') },
  async ({ repo, commit, base }) =>
    jsonResult(await platformGet(unitScopePath(repo, commit, base))),
)

reg(
  'get_unit_config',
  'How CI is set up for one project, read from the declaration CI itself reads (read-only). Without commit it is main\'s head. Returns {repo, commit, declared, note?, pins: [{repo, commit, tree}], modules: [{name, kind?, root, image?, template?, needs, tasks: [{task, units, shards?}], units, declared_inputs, whole_repository}]}. declared false: the project has no bifrost.units.yaml, it is one block and all of it runs on every change (note says so; modules is empty). declared true with a note and no modules: the declaration does not read, so everything runs; note carries the parser\'s reason. pins are the other repositories this one is tested with, each at a commit. For a module: root is its directory; needs are the inputs of every one of its tasks beside its own directory ("other-repo:path" is a path of a pinned repository); tasks lists each task with how many units it has (a unit is a Go package with tests, or the module itself) and into how many shards the largest is cut; units is the module\'s distinct units; declared_inputs is how many units declare inputs of their own; whole_repository is how many units have the whole repository as their input: those run on every change, whatever it touches, which is why a declared project can still run whole. Not in the answer yet: contracts between modules, which module ships alone, and a module\'s duration. The first answer for a large project after a platform restart can take about 30 seconds.',
  { repo: REPO, commit: SHA.optional().describe('Left out: the head of main') },
  async ({ repo, commit }) =>
    jsonResult(await platformGet(unitConfigPath(repo, commit))),
)

reg(
  'get_unit_runs',
  'The newest CI runs of one project, newest first, read from the runs the cluster still retains (read-only). A project is the value of the bifrost.io/repo label its runs carry; a run that merges a branch is not listed. Returns {repo, as_of, runs: [{name, pipeline, revision, status, trigger?, full, created_at, seconds?, tested_by?, asked, run_modules?, scope_why?, tasks: [{task, status, seconds?, waited?}]}]}. as_of is when the cluster was read; one answer a project is kept for 30 seconds. status is Succeeded, Failed, Running or Unknown; seconds (start to completion) is absent while a run is going. full true: a daily full run, everything runs whatever changed. tested_by set: this run ran nothing itself and stands on that run\'s verdict (the run of main\'s push after a landing); ignore its run_modules, which is what its own check answered about a commit already on main. asked false: the run\'s pipeline does not report which modules ran (never read that as "all" or "none"). asked true: run_modules is "*" when every module ran, otherwise the names of the modules that ran, each between spaces (" api repo " means api and repo ran; the modules not named were left out and the base\'s passing run stands for them); scope_why is the reason as the run wrote it. EVERYTHING RAN ("*") with these reasons: "the platform says every module runs" (every module\'s keys changed, or the platform answered everything for its own reason: see get_unit_scope); "the commit has no declaration: the project is one block"; "the platform gave no scope" (it did not answer in time, e.g. while restarting); "the answer is not one this task reads"; "not asked: the changes against main were not read" (a commit already on main, a full run). tasks: seconds is creation to completion, waited is creation to its first step starting. An empty runs list means no CI run of that project is retained, not that none ever ran.',
  { repo: REPO, limit: z.number().int().min(1).max(100).optional().describe('Default 30') },
  async ({ repo, limit }) =>
    jsonResult(await platformGet(unitRunsPath(repo, limit))),
)

reg(
  'get_unperceived',
  GET_UNPERCEIVED_DESCRIPTION,
  { repo: REPO },
  async ({ repo }) =>
    jsonResult(await platformGet(unperceivedPath(repo))),
)

reg(
  'get_owner',
  GET_OWNER_DESCRIPTION,
  { repo: REPO, path: z.string().min(1).max(1024).describe('A path from the repository root, e.g. api/internal/catalog/scan.go') },
  async ({ repo, path }) =>
    jsonResult(await platformGet(ownerPath(repo, path))),
)

reg(
  'get_catalog_index',
  GET_CATALOG_INDEX_DESCRIPTION,
  {},
  async () =>
    jsonResult(await platformGet(CATALOG_INDEX_PATH)),
)

reg(
  'list_records',
  'The action record, newest first (read-only): every direct call of a catalogued action is recorded when it ends, for the Owner\'s review by seat, law and day. Returns {records: [{id, at, seat, action, target, reason, regret, regret_state, law, outcome, check_result, not_verified, params_hash}], returned, more}. seat is the calling session or principal; law names what the action rested on ("action catalog: tier B", "signed release policy"); outcome is done, refused (403) or failed. regret_state: ready (machine-verified), declared (the caller wrote what undoes it, unverified), undeclared (the caller wrote nothing), not_ready. reason "(the caller gave no reason)" means the call carried no reason. more true: the filter matched more than limit (default 100, max 1000), so the page is not the whole. since and until are RFC 3339 times. An answer of 503 means the record is not available on this platform; an empty list means no matching call was recorded since the record began.',
  {
    seat: z.string().max(128).optional(),
    law: z.string().max(200).optional(),
    action: z.string().max(100).optional(),
    since: z.string().max(40).optional().describe('RFC 3339'),
    until: z.string().max(40).optional().describe('RFC 3339'),
    limit: z.number().int().min(1).max(1000).optional().describe('Default 100'),
  },
  async ({ seat, law, action, since, until, limit }) => {
    const q = new URLSearchParams()
    for (const [k, v] of Object.entries({ seat, law, action, since, until, limit })) {
      if (v !== undefined && v !== '') q.set(k, String(v))
    }
    const qs = q.toString()
    return jsonResult(await platformGet('/api/v1/records' + (qs ? '?' + qs : '')))
  },
)

reg(
  'get_evaluation_readings',
  'The evaluation readout (law 47, read-only): the numbers an evaluation uses, computed by the system from kept records over a closed window (the last `days` whole days in America/Chicago that have ended; default 7, max 31), the same value whoever reads it and whenever. Returns {window, definition_version, readings: [{name, labels, value, unit, status, samples, min_samples, window, definition: {version, source, rule, code}, provenance, note}]}. status ok has a value; too_few means fewer samples than min_samples (no percentile is given) or a record that covers too little of the window; unmeasured means no records (value null; say "unmeasured", never compute it yourself). Names include ci_run_p50_seconds, ci_queue_p50_seconds, ci_success_ratio (per pipeline), delivery_success_ratio, owner_actions, owner_wait_p50_seconds, prod_deploys, main_to_prod_p50_seconds, metric_coverage_ratio (per scope), bus_events (per env and event type), bus_outage_minutes, alert_activations (per alertname and severity), agent_actions (per law and outcome), action_rollback_declared_ratio (per law), post_release_fixes, plan_nodes_done. name keeps only that reading.',
  {
    days: z.number().int().min(1).max(31).optional().describe('Default 7'),
    name: z.string().regex(/^[a-z0-9_]{1,64}$/).optional(),
  },
  async ({ days, name }) => {
    const q = new URLSearchParams()
    if (days !== undefined) q.set('days', String(days))
    if (name) q.set('name', name)
    const qs = q.toString()
    return jsonResult(await platformGet('/api/v1/readings/evaluation' + (qs ? '?' + qs : '')))
  },
)

reg(
  'wait_for_event',
  WAIT_FOR_EVENT_DESCRIPTION,
  {
    types: z.string().min(1).max(400).describe('Comma list of registered event types, e.g. git.main.advanced'),
    match: z.string().max(200).optional().describe("A substring of the event's subject, e.g. bifrost-platform@"),
    after: z.string().regex(/^[0-9]+\.[0-9]+$/).optional().describe('The next cursor of the previous call; left out: from now'),
    since: z.string().refine(isRFC3339, 'an RFC 3339 date and time, as 2026-11-20T00:00:00Z').optional().describe('Read back from this RFC 3339 time (at most 24 hours); not with after'),
    limit: z.number().int().min(1).max(100).optional().describe('At most this many events per call (1 to 100)'),
    wait_seconds: z.number().int().min(0).max(BUS_WAIT_MAX_SECONDS).optional().describe('Default 25'),
    client: z.string().regex(/^[\x21-\x7e]{1,40}$/).optional().describe('Who waits, for the receipts; default mcp'),
  },
  async (args) => jsonResult(await platformGet(busEventsPath(args))),
)

reg(
  'get_bus_states',
  GET_BUS_STATES_DESCRIPTION,
  {
    type: z.string().regex(/^[a-z]+\.[a-z]+\.[a-z]+$/).describe('A registered state type, e.g. cluster.workload.changed'),
    tenant: z.string().regex(/^[a-z][a-z0-9-]{0,39}$/).optional().describe('Default rocket'),
  },
  async (args) => jsonResult(await platformGet(busStatesPath(args))),
)

const sha40 = z.string().regex(/^[0-9a-f]{40}$/)
const repoName = z.string().regex(/^[a-z0-9][a-z0-9._-]{0,63}$/)
reg(
  'report_regression',
  REPORT_REGRESSION_DESCRIPTION,
  {
    repo: repoName.describe('The repository whose main is broken, e.g. bifrost-platform'),
    introduced: sha40.describe('The commit that broke it (40 characters)'),
    impact: z.string().min(1).max(200).describe('In a line, what is broken'),
  },
  async ({ repo, introduced, impact }) => jsonResult(await platformPost('/api/v1/bus/regressions', { repo, introduced, impact })),
)
reg(
  'mark_regression_fixed',
  MARK_REGRESSION_FIXED_DESCRIPTION,
  {
    repo: repoName.describe('The repository'),
    introduced: sha40.describe('The commit the regression was recorded with'),
    fixed: sha40.describe('The commit that fixed it'),
  },
  async ({ repo, introduced, fixed }) => jsonResult(await platformPost('/api/v1/bus/regressions/fixed', { repo, introduced, fixed })),
)
reg(
  'list_regressions',
  LIST_REGRESSIONS_DESCRIPTION,
  {
    repo: repoName.optional().describe('One repository; left out: all'),
    open: z.boolean().optional().describe('Default true: only those with no fix'),
  },
  async (args) => jsonResult(await platformGet(regressionsPath(args))),
)

reg(
  'get_full_ci',
  'What the daily full CI recorded: its last 20 notes, newest first (read-only). The daily full run of main is the run that catches a module wrongly left out of a landing\'s own CI. Returns {runs: [{at, text, full_ci}]}. text is the note as written. full_ci.started: the runs a day started (a note that says only that). full_ci.run: a RED full run, of full_ci.commit, with failed_tasks (the pipeline tasks that failed; absent when none could be read), failed_key (the failed Go tests, space separated, when known), suspects: [{commit, subject, run, modules}] (the commits landed since the last green full run whose own CI run left a module out: run is that run, modules its run-modules as written, see get_unit_runs), unread (how many landed commits\' own run could not be read: these are not cleared, they are unknown), unreconciled (why the landings could not be looked at, or not all of them; when it is set an empty suspects list does not mean "none"). A red run with no suspects, no unread and no unreconciled: no landed commit left a module out. Notes written before 2026-11-14 may carry other keys (mission, open_mission, deviation); ignore them. An empty runs list means nothing has been recorded on this platform (the loop runs on PROD only).',
  {},
  async () => jsonResult(await platformGet(FULL_CI_PATH)),
)

reg(
  'check_full_ci_landings',
  'What the reconciliation of a red daily full run WOULD name, for two commits you give; it writes nothing and starts nothing (read-only). green is the commit of the last green full run, red the commit of the red one; both on the repository\'s main. Returns {repo, green, red, landings: [{commit, subject, run?, modules?, read, suspect}], suspects, unread, unreconciled?, run?, failed_tasks?}. landings are main\'s first-parent commits after green up to red, newest first, at most 30. For each: read is "ok" (run is the commit\'s own CI run, not one that reused another\'s verdict, and modules its run-modules as written), "no_run" (no run of its own that says which modules ran: unknown, not cleared) or "error" (the run could not be read); suspect is true when its own run left a module out (modules is neither "*" nor empty). suspects and unread count them. unreconciled is set when more than 30 commits landed and only the newest 30 were looked at. With run (a CI run\'s name), failed_tasks names that run\'s pipeline tasks that failed (an empty list: none failed). A green that is not behind red on main\'s first-parent line is an error. Use it to see which landings reached main with a module left out of their own CI, without waiting for a red full run.',
  { repo: REPO, green: SHA, red: SHA, run: z.string().regex(/^[A-Za-z0-9][A-Za-z0-9._-]*$/).optional().describe('A CI run whose failed tasks to name') },
  async ({ repo, green, red, run }) =>
    jsonResult(await platformGet(fullCICheckPath(repo, green, red, run))),
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
  'run_agentrt_migration',
  'Have the platform run, on its own environment, the agentrt migrations of one commit that its database has not applied (operator, tier C). The platform creates the environment\'s migration Job (bifrost-trade-infra k8s/agentrt-migrate/<env>/job.yaml at main) with the api image the source environment\'s successful delivery of that commit built, waits for it (up to five minutes) and returns {env, commit, job, namespace, image, pending, applied: [migration numbers], already_current}. already_current: true means the database had every migration of the commit and nothing was created. The signed release policy covers the call when the commit was delivered to the source environment (STG before PROD) and every pending migration file is new and cannot destroy rows; otherwise this returns an approval request for the Owner, and the platform runs the migration once he approves it. Run it before the release that needs the migration: the PROD pipeline refuses a build whose migrations are not applied.',
  {
    commit: z.string().regex(/^[0-9a-f]{40}$/, 'a 40-character lowercase git SHA'),
  },
  async ({ commit }) => jsonResult(await platformPost('/api/v1/agentrt/migrations/run', { commit })),
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
  'apply_manifest',
  'Apply the objects of one plan (the plan_id plan_manifest returned) and return {apply_run, tier}. The call goes to the apply route itself and never creates an approval request. A tier C or D plan is applied only when the signed release policy covers it (the namespace, kinds and paths the policy lists, every object last applied by the platform\'s applier); otherwise nothing is applied and the answer is {started: false, error: "approval required", action, release_policy: [the policy\'s reasons], note}: create the request yourself with request_action (action apply_manifest, params {plan_id}). Read the plan\'s diff first (get_delivery_run_logs on the plan id).',
  {
    ...actionTraceFields,
    plan_id: z.string().describe('The plan_id plan_manifest returned'),
  },
  async ({ plan_id, reason, unchecked, rollback }) =>
    jsonResult(await platformPost('/api/v1/actuation/manifests/apply', { plan_id, reason, unchecked, rollback })),
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
  'withdraw_request',
  'Withdraw an approval card THIS thread built, before an executor has claimed it (a card built wrong or made moot; the Owner should not be asked to reject it). reason is required. The card ends rejected with a reason that starts "withdrawn by requester". It only narrows: it cannot approve or run anything, another thread\'s card is refused, and a card already running or run is refused. Returns {id, status, withdrawn}.',
  {
    id: z.string().describe('Approval id (appr_…) or number (57 or #57)'),
    reason: z.string().min(1).max(500).describe('Why the card is taken back, one line'),
  },
  async ({ id, reason }) => jsonResult(await platformPost(`/api/v1/approvals/${encodeURIComponent(id)}/withdraw`, { reason })),
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

registerMessageTools(reg)

reg(
  'get_release_window',
  'Release window: who holds it, until when, and who is queued behind (position, waited, until).',
  {},
  async () => jsonResult(await getReleaseWindow()),
)

reg(
  'hold_release_window',
  'Take the release window as this session, or queue for it (queue defaults to true): a live holder answers 202 with your position, and thread:<this session> gets window.your_turn when it is reserved for you (about 2 minutes: call this again to take it). what: comma-separated repo names. resume: one line you will act on then. Release with release_release_window when done.',
  {
    what: z.string().min(1),
    reason: z.string().max(256).optional(),
    ttl_minutes: z.number().int().min(1).max(60).optional(),
    queue: z.boolean().optional(),
    queue_minutes: z.number().int().min(1).max(120).optional(),
    resume: z.string().max(256).optional(),
  },
  async (input) => jsonResult(await holdReleaseWindow(input)),
)

reg(
  'release_release_window',
  'Give back the release window this session holds (or a reservation made for it); the next in queue is reserved and notified.',
  {},
  async () => jsonResult(await releaseReleaseWindow()),
)
registerPlanImportTools(reg)

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

} // end platform tools (non-prometheus focus)

async function main() {
  const transport = new StdioServerTransport()
  await server.connect(transport)
  // Register this thread so others can message it (presence.ts); never blocks or stops the server.
  const skip = presenceSkip(process.env, bridgeFocus)
  if (skip === undefined) {
    void registerPresence(defaultRuntimeDeps()).then((r) => {
      if (r !== 'registered') console.error(`${SERVER_NAME}: presence ${r}`)
    })
  }
}

main().catch(err => {
  console.error(err)
  process.exit(1)
})
