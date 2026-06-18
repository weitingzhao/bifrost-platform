import type { DeliveryPipelineRunView } from '@/api/types'

const LOG_TAIL_MAX = 12_000

const STG_V2_CHECKLIST = [
  'S10 config.stg.yaml + sync_stg_config',
  'S11 worker/socket manifests + Kaniko images',
  'S12 deliver-stg v2 pipeline (build + rollout worker/socket)',
  'S13 bifrost-stg-secrets (MASSIVE_API_KEY) + IB/Massive live',
  'S14 verify-phase-b-stg-v2 + release gate 9 APIs + frontend',
].join('\n- ')

function truncateLogs(logs: string): string {
  if (logs.length <= LOG_TAIL_MAX) return logs
  return `…(truncated ${logs.length - LOG_TAIL_MAX} chars)\n` + logs.slice(-LOG_TAIL_MAX)
}

export function isPipelineRunSucceeded(run: DeliveryPipelineRunView): boolean {
  const status = run.status.toLowerCase()
  const reason = (run.reason ?? '').toLowerCase()
  if (status === 'true' || status === 'succeeded' || status === 'success') return true
  if (reason === 'succeeded' || reason === 'completed') return true
  return false
}

export function formatPipelineRunStatus(run: DeliveryPipelineRunView): string {
  const status = run.status.toLowerCase()
  if (status === 'true') return run.reason != null && run.reason !== '' ? run.reason : 'Succeeded'
  if (status === 'false') return run.reason != null && run.reason !== '' ? run.reason : 'Failed'
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
}): string {
  const { pipeline, run, logs, stgSmokeDetail } = params
  const statusLabel = formatPipelineRunStatus(run)

  const lines = [
    'Mode: Ops',
    '',
    '## Task',
    `Diagnose and fix this failed Tekton PipelineRun for Bifrost STG K3s rollout.`,
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
    '',
    '## Phase B STG v2 milestones (reference)',
    `- ${STG_V2_CHECKLIST}`,
    '',
    '## bifrost-deliver-stg pipeline tasks (typical order)',
    '- clone / kaniko builds (api, frontend, worker, socket)',
    '- rollout deployments in bifrost-stg',
    '- gitops-sync (optional)',
    '',
    '## Cluster signals',
    stgSmokeDetail != null && stgSmokeDetail !== ''
      ? `- stg smoke: ${stgSmokeDetail}`
      : '- stg smoke: (not loaded — check Delivery → STG smoke panel)',
    '- verify: Runtime → Cluster → bifrost-stg workloads (failing pods)',
    '- gateway: http://192.168.10.73:30880/',
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
    '4. How to confirm S12/S14 acceptance after the fix.',
  ].filter((line): line is string => line != null)

  return lines.join('\n')
}
