import type { DeliveryPipelineRunView } from '@/api/types'

const LOG_TAIL_MAX = 12_000

const STG_V2_CHECKLIST = [
  'S10 config.stg.yaml + sync_stg_config',
  'S11 worker/socket manifests + Kaniko images',
  'S12 deliver-stg v2 (prepare → build → rollout → verify-stg → gitops)',
  'S13 bifrost-stg-secrets (MASSIVE_API_KEY) + IB/Massive live',
  'S14 verify-phase-b-stg-v2 + Stg smoke + release gate',
].join('\n- ')

/**
 * Context that customizes the Ask-AI pack for a given deliver pipeline.
 * Defaults target the Trade STG rollout; platform deliver supplies its own.
 */
export interface PipelineRunAskContext {
  /** One-line task framing in the "## Task" section. */
  task: string
  /** "## <title> milestones (reference)" — optional reference checklist. */
  milestonesTitle?: string
  milestones?: string
  /** Pipeline tasks in typical order (one bullet each). */
  pipelineTitle: string
  pipelineOrder: string[]
  /** Cluster signals to inspect (one bullet each). */
  clusterSignals: string[]
  /** Closing acceptance hint for "what I need from you" #4. */
  acceptanceHint: string
}

const TRADE_STG_ASK_CONTEXT: PipelineRunAskContext = {
  task:
    'Diagnose and fix this failed Tekton PipelineRun for Bifrost STG K3s rollout.',
  milestonesTitle: 'Phase B STG v2',
  milestones: STG_V2_CHECKLIST,
  pipelineTitle: 'bifrost-deliver-stg pipeline tasks (typical order)',
  pipelineOrder: [
    'clone / kaniko builds (api, frontend, worker, socket)',
    'rollout deployments in bifrost-stg',
    'gitops-sync (optional)',
  ],
  clusterSignals: [
    'verify: Operate → Cluster → bifrost-stg workloads (failing pods)',
    'scheduling: Observe → Scheduling → Placement (amd64_ci pool, policy violations)',
    'gateway: http://192.168.10.73:30880/',
  ],
  acceptanceHint: 'How to confirm S12/S14 acceptance after the fix.',
}

/** Ask context for an Ops Platform deliver pipeline (STG or PROD). */
export function platformDeliverAskContext(target: {
  shortLabel: string
  namespace: string
}): PipelineRunAskContext {
  const isProd = target.namespace.endsWith('-prod')
  return {
    task: `Diagnose and fix this failed Tekton PipelineRun for the Ops Platform ${target.shortLabel} (control-plane) deploy on K3s. The pipeline builds platform-api + platform-console images and rolls out the ${target.namespace} namespace.`,
    pipelineTitle: `${target.namespace === 'bifrost-platform-prod' ? 'bifrost-deliver-platform-prod' : 'bifrost-deliver-platform'} pipeline tasks (declared order)`,
    pipelineOrder: isProd
      ? [
          'preflight-stg (taskSpec): curl platform-stg API/Console /health, abort if not 200',
          'mirror-sync: bifrost-gitea-mirror-sync (bifrost-platform bifrost-ui)',
          'clone-platform + clone-ui: bifrost-git-clone-gitea → workspace build-context',
          'stage-api-dockerfile + stage-console-dockerfile (taskSpec): copy Dockerfile from CM',
          'build-platform-api + build-platform-console: Kaniko → registry :prod',
          'rollout: bifrost-rollout-platform-stg task, namespace=bifrost-platform-prod',
          'gitops-sync: bifrost-argocd-sync-platform-stg task, application=bifrost-platform-prod',
        ]
      : [
          'mirror-sync → clone → stage dockerfile → Kaniko build (:stg)',
          'rollout bifrost-platform-stg → Argo sync',
        ],
    clusterSignals: [
      `workloads: Operate → Cluster → ${target.namespace} pods (ImagePullBackOff = image not built yet)`,
      'pipeline workspace: the pipeline declares workspace "build-context"; InvalidWorkspaceBindings means the PipelineRun was started without a matching workspace binding (volumeClaimTemplate / PVC / emptyDir)',
      'registry: registry.cicd.svc.cluster.local:5000 (images pushed by Kaniko)',
      'ServiceAccount: tekton-deliver in cicd namespace (RBAC for cross-namespace rollout)',
    ],
    acceptanceHint: `How to confirm ${target.shortLabel} pods are Running and the release gate passes after the fix.`,
  }
}

function truncateLogs(logs: string): string {
  if (logs.length <= LOG_TAIL_MAX) return logs
  return `…(truncated ${logs.length - LOG_TAIL_MAX} chars)\n` + logs.slice(-LOG_TAIL_MAX)
}

export function isPipelineRunSucceeded(run: DeliveryPipelineRunView): boolean {
  const status = run.status.toLowerCase()
  const reason = (run.reason ?? '').toLowerCase()
  if (reason === 'running' || reason === 'pending') return false
  if (status === 'true') return true
  if (reason === 'succeeded' || reason === 'completed') return true
  return false
}

export function isPipelineRunFailed(run: DeliveryPipelineRunView): boolean {
  const status = run.status.toLowerCase()
  const reason = (run.reason ?? '').toLowerCase()
  if (status === 'false') return true
  if (reason === 'failed' || reason === 'pipelinerunfailed') return true
  return false
}

export function isPipelineRunRunning(run: DeliveryPipelineRunView): boolean {
  if (isPipelineRunSucceeded(run) || isPipelineRunFailed(run)) return false
  const reason = (run.reason ?? '').toLowerCase()
  if (reason === 'running' || reason === 'pending') return true
  const status = run.status.toLowerCase()
  if (status === 'unknown') return true
  return run.completion_time == null || run.completion_time === ''
}

export function formatPipelineRunStatus(run: DeliveryPipelineRunView): string {
  if (isPipelineRunSucceeded(run)) return run.reason != null && run.reason !== '' ? run.reason : 'Succeeded'
  if (isPipelineRunFailed(run)) return run.reason != null && run.reason !== '' ? run.reason : 'Failed'
  const reason = (run.reason ?? '').trim()
  if (reason !== '') return reason
  const status = run.status.toLowerCase()
  if (status === 'unknown') return 'Running'
  if (status === 'true') return 'Succeeded'
  if (status === 'false') return 'Failed'
  return run.status
}

export type PipelineRunSortKey = 'status' | 'started'
export type PipelineRunSortDir = 'asc' | 'desc'

export function defaultPipelineRunSort(): { key: PipelineRunSortKey; dir: PipelineRunSortDir } {
  return { key: 'started', dir: 'desc' }
}

export function togglePipelineRunSort(
  current: { key: PipelineRunSortKey; dir: PipelineRunSortDir },
  nextKey: PipelineRunSortKey,
): { key: PipelineRunSortKey; dir: PipelineRunSortDir } {
  if (current.key !== nextKey) {
    return { key: nextKey, dir: nextKey === 'started' ? 'desc' : 'asc' }
  }
  return { key: nextKey, dir: current.dir === 'asc' ? 'desc' : 'asc' }
}

function statusSortRank(run: DeliveryPipelineRunView): number {
  if (isPipelineRunSucceeded(run)) return 0
  const status = run.status.toLowerCase()
  const reason = (run.reason ?? '').toLowerCase()
  if (status === 'unknown' || status === 'running' || reason === 'running') return 1
  if (status === 'false' || status === 'failed' || reason === 'failed') return 2
  return 3
}

function runStartedAt(run: DeliveryPipelineRunView): number | null {
  if (run.start_time != null && run.start_time !== '') {
    const ts = Date.parse(run.start_time)
    if (!Number.isNaN(ts)) return ts
  }
  const match = run.name.match(/-(\d{10,})$/)
  if (match != null) {
    const sec = Number(match[1])
    if (Number.isFinite(sec)) return sec * 1000
  }
  return null
}

export function comparePipelineRuns(
  a: DeliveryPipelineRunView,
  b: DeliveryPipelineRunView,
  key: PipelineRunSortKey,
  dir: PipelineRunSortDir,
): number {
  let cmp = 0
  if (key === 'status') {
    cmp = statusSortRank(a) - statusSortRank(b)
    if (cmp === 0) {
      cmp = formatPipelineRunStatus(a).localeCompare(formatPipelineRunStatus(b))
    }
    if (cmp === 0) {
      const ta = runStartedAt(a)
      const tb = runStartedAt(b)
      if (ta != null && tb != null) cmp = ta - tb
      else cmp = a.name.localeCompare(b.name)
    }
  } else {
    const ta = runStartedAt(a)
    const tb = runStartedAt(b)
    if (ta == null && tb == null) cmp = a.name.localeCompare(b.name)
    else if (ta == null) cmp = 1
    else if (tb == null) cmp = -1
    else cmp = ta - tb
    if (cmp === 0) cmp = a.name.localeCompare(b.name)
  }
  return dir === 'asc' ? cmp : -cmp
}

export function sortPipelineRuns(
  runs: DeliveryPipelineRunView[],
  key: PipelineRunSortKey,
  dir: PipelineRunSortDir,
): DeliveryPipelineRunView[] {
  return [...runs].sort((a, b) => comparePipelineRuns(a, b, key, dir))
}

export function buildPipelineRunAskPack(params: {
  pipeline: string
  run: DeliveryPipelineRunView
  logs: string
  stgSmokeDetail?: string
  context?: PipelineRunAskContext
}): string {
  const { pipeline, run, logs, stgSmokeDetail, context = TRADE_STG_ASK_CONTEXT } = params
  const statusLabel = formatPipelineRunStatus(run)

  const milestonesBlock =
    context.milestones != null && context.milestones !== ''
      ? ['', `## ${context.milestonesTitle ?? 'Milestones'} (reference)`, `- ${context.milestones}`]
      : []

  const clusterSignalLines = context.clusterSignals.map(s => `- ${s}`)
  if (stgSmokeDetail != null && stgSmokeDetail !== '') {
    clusterSignalLines.unshift(`- stg smoke: ${stgSmokeDetail}`)
  }

  const lines = [
    'Mode: Ops',
    '',
    '## Task',
    context.task,
    'Respond in Chinese. Propose the smallest single-variable fix; prefer Ops Console / platform-api over manual ssh when possible.',
    '',
    '## Pipeline run',
    `- pipeline: ${pipeline}`,
    `- run: ${run.name}`,
    `- namespace: ${run.namespace}`,
    `- status: ${statusLabel} (raw: ${run.status})`,
    run.reason != null && run.reason !== '' ? `- reason: ${run.reason}` : null,
    run.start_time != null && run.start_time !== '' ? `- started: ${run.start_time}` : null,
    run.completion_time != null && run.completion_time !== ''
      ? `- completed: ${run.completion_time}`
      : null,
    ...milestonesBlock,
    '',
    `## ${context.pipelineTitle}`,
    ...context.pipelineOrder.map(t => `- ${t}`),
    '',
    '## Cluster signals',
    ...clusterSignalLines,
    '',
    '## Log tail (PipelineRun task pods)',
    '```',
    truncateLogs(logs.trim() !== '' ? logs : '(no logs returned yet)'),
    '```',
    '',
    '## What I need from you',
    '1. Root cause of this PipelineRun failure (name the failing Tekton task/step).',
    '2. Whether infra code, cluster state, or secrets/config is wrong.',
    '3. Exact next commands or Console actions (Deliver rerun, Cluster restart, secret apply, etc.).',
    `4. ${context.acceptanceHint}`,
  ].filter((line): line is string => line != null)

  return lines.join('\n')
}
