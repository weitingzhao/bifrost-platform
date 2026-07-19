/**
 * Daily Ops Checklist Action-column live progress (MVP Wave 2 + Phase 4.2).
 *
 * Naming lock:
 * - AI Check → scope daily-ops-checklist-run (prober)
 * - Fleet cell Fix → per-cell remediation (unchanged)
 * - Operator Plane Fix → operator-plane-remediate (do NOT conflate with Checklist Check)
 */
import type { ChecklistDispatchActionDto } from '@/api/platform'
import type { RemediationJob, RemediationPhase } from '@/api/types'
import { DAILY_OPS_CHECKLIST_RUN_SCOPE } from '@/lib/agent/agentScopes'
import type { ChecklistDispatchGate } from '@/lib/control-room/checklistDispatch'

export type ChecklistProgressState =
  | 'idle'
  | 'checking'
  | 'reported'
  | 'queued'
  | 'auto_running'
  | 'notify'
  | 'skip'
  | 'done'
  | 'failed'

export type ChecklistSkipKind = 'd10' | 'dedup' | 'other'

export type ChecklistItemProgress = {
  state: ChecklistProgressState
  /** Short Action-column label (English UI). */
  label: string
  title: string
  jobId?: string
  queueId?: string
  /** Row affordance: open remediation job or Operate Queue. */
  openTarget?: 'job' | 'queue'
  phase?: RemediationPhase | string
  detail?: string
  skipKind?: ChecklistSkipKind
  skippedD10?: boolean
  /** Wave 4.3 — demoted from auto because concurrent auto slot busy. */
  busyDemote?: boolean
}

export type ChecklistHeaderProgress = {
  /** Table-level prober state when a checklist-run job is active. */
  checking: boolean
  proberLabel: string | null
  proberElapsedSec: number | null
  dispatchAuto: number
  dispatchQueued: number
  dispatchNotify: number
  dispatchSkip: number
  remediating: number
  done: number
  failed: number
}

const PHASE_SHORT: Partial<Record<RemediationPhase, string>> = {
  starting: 'starting',
  diagnosing: 'diagnosing',
  awaiting_approval: 'awaiting approval',
  remediating: 'remediating',
  verifying: 'verifying',
  done: 'done',
  failed: 'failed',
  cancelled: 'cancelled',
}

export function isChecklistRunJob(job: Pick<RemediationJob, 'scope'>): boolean {
  return job.scope === DAILY_OPS_CHECKLIST_RUN_SCOPE
}

export function findActiveChecklistRunJob(
  jobs: RemediationJob[],
): RemediationJob | undefined {
  return jobs.find(j => isChecklistRunJob(j) && j.status === 'running')
}

export function resolveSkipKind(dispatch: ChecklistDispatchActionDto): ChecklistSkipKind {
  if (dispatch.skipped_d10) return 'd10'
  const detail = (dispatch.detail ?? '').toLowerCase()
  if (detail.includes('dedup') || detail.includes('24h')) return 'dedup'
  if (detail.includes('d10') || detail.includes('ib feed') || detail.includes('observe')) {
    return 'd10'
  }
  return 'other'
}

/** Phase 4.2 — Skip never implies "in progress". */
export function skipProgressLabel(skipKind: ChecklistSkipKind): string {
  switch (skipKind) {
    case 'd10':
      return 'Skip · D10'
    case 'dedup':
      return 'Skip · dedup 24h'
    default:
      return 'Skip'
  }
}

/** Wave 4.3 — concurrent auto=1 demote to queue. */
export function isBusyQueueDemote(detail?: string): boolean {
  if (detail == null || detail === '') return false
  const d = detail.toLowerCase()
  return (
    d.includes('concurrent auto') ||
    d.includes('demoted') ||
    (d.includes('busy') && d.includes('queue'))
  )
}

/**
 * Wave 3.1 — Fleet lamps stay authoritative; hint when agent checklist signal
 * polarity disagrees (ok vs fail/degraded). Unknown/unavailable do not count.
 */
export function fleetAgentSignalDisagree(
  fleetSignal: string,
  agentSignal: string | undefined | null,
): boolean {
  if (agentSignal == null || agentSignal === '') return false
  const fleet = fleetSignal === 'unavailable' ? 'unknown' : fleetSignal
  const agent = agentSignal
  if (fleet === 'unknown' || agent === 'unknown') return false
  const fleetBad = fleet === 'fail' || fleet === 'degraded'
  const agentBad = agent === 'fail' || agent === 'degraded'
  const fleetOk = fleet === 'ok'
  const agentOk = agent === 'ok'
  return (fleetOk && agentBad) || (agentOk && fleetBad)
}

function phaseLabel(phase?: string): string {
  if (phase == null || phase === '') return 'remediating'
  return PHASE_SHORT[phase as RemediationPhase] ?? phase
}

function elapsedSeconds(iso: string | undefined, nowMs: number): number | null {
  if (iso == null || iso === '') return null
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return null
  return Math.max(0, Math.floor((nowMs - t) / 1000))
}

/**
 * Per-item Action progress. Does NOT invent row-level "checking" while the
 * prober runs — only table/header shows checking (unless the item already has
 * dispatch/job evidence).
 */
export function deriveChecklistItemProgress(opts: {
  dispatch?: ChecklistDispatchActionDto
  linkedJob?: RemediationJob
}): ChecklistItemProgress {
  const { dispatch, linkedJob } = opts

  if (linkedJob != null) {
    if (linkedJob.status === 'running') {
      const phase = linkedJob.phase
      const short = phaseLabel(phase)
      return {
        state: 'auto_running',
        label: `Auto · ${short}`,
        title: [
          `RemediationJob ${linkedJob.id}`,
          `status ${linkedJob.status}`,
          phase != null ? `phase ${phase}` : null,
          linkedJob.scope != null ? `scope ${linkedJob.scope}` : null,
          'Click to open job',
          linkedJob.summary ?? linkedJob.error,
        ]
          .filter(Boolean)
          .join(' · '),
        jobId: linkedJob.id,
        openTarget: 'job',
        phase,
        detail: linkedJob.summary ?? linkedJob.error,
      }
    }
    if (linkedJob.status === 'failed' || linkedJob.status === 'cancelled') {
      return {
        state: 'failed',
        label: 'Failed',
        title: [
          `RemediationJob ${linkedJob.id}`,
          linkedJob.status,
          'Click to open job',
          linkedJob.error ?? linkedJob.summary,
        ]
          .filter(Boolean)
          .join(' · '),
        jobId: linkedJob.id,
        openTarget: 'job',
        phase: linkedJob.phase,
        detail: linkedJob.error ?? linkedJob.summary,
      }
    }
    if (linkedJob.status === 'done') {
      return {
        state: 'done',
        label: 'Done',
        title: [
          `RemediationJob ${linkedJob.id}`,
          'done',
          'Click to open job',
          linkedJob.summary,
        ]
          .filter(Boolean)
          .join(' · '),
        jobId: linkedJob.id,
        openTarget: 'job',
        phase: linkedJob.phase,
        detail: linkedJob.summary,
      }
    }
  }

  if (dispatch == null) {
    return { state: 'idle', label: '—', title: 'No dispatch yet' }
  }

  const gate = dispatch.gate as ChecklistDispatchGate
  const detail = dispatch.detail

  if (gate === 'skip') {
    const skipKind = resolveSkipKind(dispatch)
    const label = skipProgressLabel(skipKind)
    return {
      state: 'skip',
      label,
      title: [label, detail, dispatch.job_id != null ? `job ${dispatch.job_id}` : null]
        .filter(Boolean)
        .join(' · '),
      jobId: dispatch.job_id,
      detail,
      skipKind,
      skippedD10: skipKind === 'd10' || dispatch.skipped_d10 === true,
    }
  }

  if (gate === 'queue') {
    const busy = isBusyQueueDemote(detail)
    const label = busy ? 'Queued (busy)' : 'Queued'
    return {
      state: 'queued',
      label,
      title: [
        busy
          ? 'Demoted from auto — concurrent slot busy; open Operate Queue'
          : 'Open Operate Queue (checklist_dispatch)',
        detail,
        dispatch.queue_id != null ? `queue ${dispatch.queue_id}` : null,
      ]
        .filter(Boolean)
        .join(' · '),
      queueId: dispatch.queue_id,
      openTarget: 'queue',
      detail,
      busyDemote: busy,
    }
  }

  if (gate === 'notify') {
    return {
      state: 'notify',
      label: 'Notify',
      title: ['Notify operator', detail].filter(Boolean).join(' · '),
      detail,
    }
  }

  if (gate === 'auto') {
    // Auto gate recorded but job not linked / not in list yet
    if (dispatch.job_id != null) {
      return {
        state: 'reported',
        label: 'Auto · dispatched',
        title: [`Auto dispatched`, `job ${dispatch.job_id}`, 'Click to open job', detail]
          .filter(Boolean)
          .join(' · '),
        jobId: dispatch.job_id,
        openTarget: 'job',
        detail,
      }
    }
    return {
      state: 'reported',
      label: 'Auto',
      title: ['Auto gate', detail].filter(Boolean).join(' · '),
      detail,
    }
  }

  return {
    state: 'reported',
    label: gate,
    title: detail ?? gate,
    detail,
  }
}

export function deriveChecklistHeaderProgress(opts: {
  jobs: RemediationJob[]
  lastDispatch?: ChecklistDispatchActionDto[]
  nowMs?: number
}): ChecklistHeaderProgress {
  const nowMs = opts.nowMs ?? Date.now()
  const activeRun = findActiveChecklistRunJob(opts.jobs)
  const elapsed = activeRun != null ? elapsedSeconds(activeRun.created_at, nowMs) : null

  let dispatchAuto = 0
  let dispatchQueued = 0
  let dispatchNotify = 0
  let dispatchSkip = 0
  let remediating = 0
  let done = 0
  let failed = 0

  for (const a of opts.lastDispatch ?? []) {
    const linked =
      a.job_id != null ? opts.jobs.find(j => j.id === a.job_id) : undefined
    const progress = deriveChecklistItemProgress({ dispatch: a, linkedJob: linked })
    switch (progress.state) {
      case 'auto_running':
        remediating += 1
        dispatchAuto += 1
        break
      case 'queued':
        dispatchQueued += 1
        break
      case 'notify':
        dispatchNotify += 1
        break
      case 'skip':
        dispatchSkip += 1
        break
      case 'done':
        done += 1
        dispatchAuto += 1
        break
      case 'failed':
        failed += 1
        break
      case 'reported':
        if (a.gate === 'auto') dispatchAuto += 1
        break
      default:
        break
    }
  }

  const checking = activeRun != null
  let proberLabel: string | null = null
  if (activeRun != null) {
    const short = phaseLabel(activeRun.phase)
    proberLabel =
      elapsed != null ? `Prober: ${short} · ${elapsed}s` : `Prober: ${short}`
  }

  return {
    checking,
    proberLabel,
    proberElapsedSec: elapsed,
    dispatchAuto,
    dispatchQueued,
    dispatchNotify,
    dispatchSkip,
    remediating,
    done,
    failed,
  }
}

export function formatDispatchHeaderStrip(header: ChecklistHeaderProgress): string | null {
  const parts: string[] = []
  if (header.remediating > 0) {
    parts.push(`${header.remediating} remediating`)
  }
  if (header.dispatchAuto > 0 && header.remediating === 0) {
    parts.push(`${header.dispatchAuto} auto`)
  } else if (header.dispatchAuto > header.remediating && header.remediating > 0) {
    parts.push(`${header.dispatchAuto} auto`)
  }
  if (header.dispatchQueued > 0) parts.push(`${header.dispatchQueued} queued`)
  if (header.dispatchNotify > 0) parts.push(`${header.dispatchNotify} notify`)
  if (header.dispatchSkip > 0) parts.push(`${header.dispatchSkip} skip`)
  if (header.done > 0) parts.push(`${header.done} done`)
  if (header.failed > 0) parts.push(`${header.failed} failed`)
  if (parts.length === 0) return null
  return `Last AI Check dispatch: ${parts.join(' · ')}`
}

/** Prompt aligned with scripts/agent/daily_ops_checklist.sh */
export const DAILY_OPS_CHECKLIST_RUN_PROMPT = [
  'Scheduled Daily Ops Checklist probe (scope daily-ops-checklist-run).',
  '1. Call verify_mission_snapshot, get_cluster_summary, get_agent_bridge, get_gitops_apps, get_stg_smoke, get_delivery_pipelines.',
  '2. Map evidence to all 18 checklist item_ids (ok/degraded/fail/unknown).',
  '3. Call report_checklist_signals with auto_dispatch=true and the full signals array.',
  '4. Do not actuate directly in this job — platform gates dispatch (D10: never auto IB).',
].join('\n')
