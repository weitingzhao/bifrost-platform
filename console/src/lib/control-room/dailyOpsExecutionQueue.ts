/**
 * Daily Ops Execution → Queue & History helpers.
 * Human vs Agent lanes + noise filter for open-count trust.
 */
import type { OperateQueueItem } from '@/api/operateQueueTypes'
import type { RemediationJob } from '@/api/types'
import { remediationJobStatusLabel } from '@/lib/remediation/remediationJobDisplay'

export type QueueOriginKind = 'agent' | 'human' | 'skipped' | 'ask-ai' | 'handoff' | 'queue'

export type QueueLane = 'human' | 'agent'

/** Human = manual / notify / observe / D10; Agent = semi/auto / queued remediation. */
export function queueLaneForOrigin(origin: QueueOriginKind): QueueLane {
  if (origin === 'human' || origin === 'skipped') return 'human'
  return 'agent'
}

export function originFromOperateItem(item: OperateQueueItem): QueueOriginKind {
  const blob = [item.source, item.reason, item.title, item.description, item.owner]
    .filter(Boolean)
    .join(' ')
    .toLowerCase()
  if (blob.includes('ask-ai') || blob.includes('ask for ai') || blob.includes('cursor failover')) {
    return 'ask-ai'
  }
  if (item.source === 'checklist_dispatch' || item.reason === 'checklist_dispatch') {
    return 'queue'
  }
  if (item.source === 'post_completion') return 'handoff'
  if (item.source === 'manual') return 'human'
  if (
    blob.includes('d10') ||
    blob.includes('observe') ||
    blob.includes('skipped') ||
    blob.includes('never auto-dispatch')
  ) {
    return 'skipped'
  }
  if (blob.includes('physical') || blob.includes('human') || /\bmanual\b/.test(blob)) {
    return 'human'
  }
  return 'handoff'
}

export function originFromRemediationJob(job: RemediationJob): QueueOriginKind {
  const blob = [job.scope, job.summary, job.error, job.actor, job.init_brief]
    .filter(Boolean)
    .join(' ')
    .toLowerCase()
  if (blob.includes('ask-ai') || blob.includes('ask for ai') || blob.includes('cursor failover')) {
    return 'ask-ai'
  }
  if (
    blob.includes('d10') ||
    blob.includes('observe-only') ||
    (job.status === 'cancelled' && (blob.includes('skip') || blob.includes('observe')))
  ) {
    return 'skipped'
  }
  if (blob.includes('manual') || blob.includes('human') || job.actor === 'operator') {
    return 'human'
  }
  return 'agent'
}

/**
 * Pure skip/dedup noise — still listable under Human, but excluded from
 * "truly pending" open badge (does not need operator action right now).
 */
export function isOperateQueueNoise(item: OperateQueueItem): boolean {
  const blob = [item.reason, item.title, item.description]
    .filter(Boolean)
    .join(' ')
    .toLowerCase()
  if (blob.includes('dedup') || blob.includes('24h')) return true
  if (/\bskip\b/.test(blob) && (blob.includes('d10') || blob.includes('observe'))) {
    // D10 / observe skip is informational — not an actionable queue debt
    return true
  }
  if (blob.includes('never auto-dispatch') && !blob.includes('notify')) return true
  return false
}

export function actionableOpenCount(open: OperateQueueItem[]): number {
  return open.filter(i => !isOperateQueueNoise(i)).length
}

export function partitionOpenQueue(
  open: OperateQueueItem[],
  opts?: { drainItemIds?: ReadonlySet<string> },
): {
  human: OperateQueueItem[]
  agent: OperateQueueItem[]
  drain: OperateQueueItem[]
  noise: OperateQueueItem[]
  actionable: number
  humanActionable: number
  agentActionable: number
  drainCount: number
} {
  const drainIds = opts?.drainItemIds
  const human: OperateQueueItem[] = []
  const agent: OperateQueueItem[] = []
  const drain: OperateQueueItem[] = []
  const noise: OperateQueueItem[] = []
  for (const item of open) {
    if (isOperateQueueNoise(item)) {
      noise.push(item)
      continue
    }
    if (drainIds != null && drainIds.has(item.id)) {
      drain.push(item)
      continue
    }
    const lane = queueLaneForOrigin(originFromOperateItem(item))
    if (lane === 'human') human.push(item)
    else agent.push(item)
  }
  return {
    human,
    agent,
    drain,
    noise,
    actionable: human.length + agent.length + drain.length,
    humanActionable: human.length,
    agentActionable: agent.length,
    drainCount: drain.length,
  }
}

/** Closed items older than this are dropped from History (keep recent terminal). */
export const HISTORY_CLOSED_MAX_AGE_MS = 7 * 24 * 60 * 60 * 1000

export function isRecentClosedItem(item: OperateQueueItem, nowMs = Date.now()): boolean {
  const at = item.closed_at || item.updated_at || item.created_at
  const t = Date.parse(at)
  if (!Number.isFinite(t)) return true
  return nowMs - t <= HISTORY_CLOSED_MAX_AGE_MS
}

/**
 * Link operate-queue item → remediation job when execution_job_id is present.
 * Returns null when unlinked (caller must keep honest Review deep-link).
 */
export function linkedRemediationJob(
  item: OperateQueueItem,
  jobs: RemediationJob[],
): RemediationJob | null {
  const id = item.execution_job_id
  if (id == null || id === '') return null
  return jobs.find(j => j.id === id) ?? null
}

/** Compact in-flight status for queue rows; terminal jobs are History, not Queue. */
export function queueLinkedJobStatusLabel(job: RemediationJob): string | null {
  if (job.status === 'done' || job.status === 'failed' || job.status === 'cancelled') {
    return null
  }
  if (job.phase === 'awaiting_approval') return 'awaiting_approval'
  if (job.status === 'running') return 'running'
  return 'queued'
}

export function queueLinkedJobChip(job: RemediationJob): {
  label: string
  variant: 'warning' | 'category' | 'neutral'
} | null {
  const short = queueLinkedJobStatusLabel(job)
  if (short == null) return null
  if (short === 'awaiting_approval') {
    return { label: 'awaiting_approval', variant: 'category' }
  }
  if (short === 'running') return { label: 'running', variant: 'warning' }
  return { label: 'queued', variant: 'neutral' }
}

export { remediationJobStatusLabel }
