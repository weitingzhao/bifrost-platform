/**
 * Mission timeline — merge Agent jobs, audit actuation, and nightly drift into a
 * commander-facing accountability trail (Control Room Phase 4).
 */

import type { AgentNightlyReportResponse, AuditRecord, RemediationJob } from '@/api/types'
import { parseNightlyLayerResults } from '@/lib/briefing/briefingSyncLoop'
import {
  formatRemediationJobWhen,
  remediationScopeShortLabel,
} from '@/lib/remediation/remediationJobDisplay'
import type { MissionSnapshot } from '@/lib/control-room/missionSignals'

export type MissionTimelineEventKind =
  | 'probe_alert'
  | 'agent_started'
  | 'agent_approval'
  | 'agent_finished'
  | 'audit_actuation'
  | 'nightly_drift'

export type MissionTimelineTone = 'ok' | 'warning' | 'fail' | 'neutral'

export interface MissionTimelineEvent {
  id: string
  at: string
  kind: MissionTimelineEventKind
  title: string
  detail: string
  tone: MissionTimelineTone
  jobId?: string
  auditId?: string
  scope?: string
}

export interface MissionTimelineTrajectory {
  jobId: string
  scope: string
  label: string
  eventIds: string[]
  startedAt: string
  finishedAt?: string
  status: RemediationJob['status']
}

export interface MissionTimelineModel {
  events: MissionTimelineEvent[]
  nightlySummary: string | null
  trajectories: MissionTimelineTrajectory[]
  windowLabel: string
}

const DEFAULT_WITHIN_MS = 86_400_000

const AUDIT_ACTION_PREFIXES = [
  'remediation.',
  'drift.',
  'delivery.',
  'cluster.',
  'promote.',
  'briefing.',
  'gitops.',
  'stack.',
  'buildgate.',
  'migrate.',
]

function withinWindow(at: string, cutoffMs: number): boolean {
  const t = Date.parse(at)
  return Number.isFinite(t) && t >= cutoffMs
}

function auditActionLabel(action: string): string {
  if (action === 'remediation.start') return 'Agent task dispatched'
  if (action === 'remediation.done') return 'Agent task completed'
  if (action === 'remediation.failed') return 'Agent task failed'
  if (action === 'remediation.respond') return 'Operator approval'
  if (action === 'remediation.cancel') return 'Agent task cancelled'
  if (action.startsWith('drift.proposal.')) return 'Drift proposal actuation'
  return action.replace(/\./g, ' · ')
}

function auditTone(status: string, action: string): MissionTimelineTone {
  const s = status.toLowerCase()
  if (s === 'failed' || s === 'fail' || s === 'rejected') return 'fail'
  if (s === 'done' || s === 'ok' || s === 'approved' || s === 'started') {
    if (action.includes('failed')) return 'fail'
    return action.includes('start') || action.includes('respond') ? 'neutral' : 'ok'
  }
  if (s === 'pending_approval' || s === 'running') return 'warning'
  return 'neutral'
}

function jobFinishTone(job: RemediationJob): MissionTimelineTone {
  if (job.status === 'done') return 'ok'
  if (job.status === 'failed') return 'fail'
  if (job.status === 'cancelled') return 'warning'
  return 'neutral'
}

function eventsFromJob(job: RemediationJob): MissionTimelineEvent[] {
  const scopeLabel = remediationScopeShortLabel(job.scope)
  const out: MissionTimelineEvent[] = []

  out.push({
    id: `job-start-${job.id}`,
    at: job.created_at,
    kind: 'agent_started',
    title: `Agent started · ${scopeLabel}`,
    detail: job.actor != null && job.actor !== '' ? `Actor ${job.actor}` : 'Runner accepted scope',
    tone: 'neutral',
    jobId: job.id,
    scope: job.scope,
  })

  const approvalEvent = job.events?.find(e => e.type === 'approval_request')
  if (job.phase === 'awaiting_approval' || approvalEvent != null) {
    out.push({
      id: `job-approval-${job.id}`,
      at: approvalEvent?.at ?? job.updated_at,
      kind: 'agent_approval',
      title: `Awaiting approval · ${scopeLabel}`,
      detail: approvalEvent?.text ?? 'Operator decision required before remediation continues',
      tone: 'warning',
      jobId: job.id,
      scope: job.scope,
    })
  }

  if (job.status === 'done' || job.status === 'failed' || job.status === 'cancelled') {
    const summary =
      job.summary != null && job.summary !== ''
        ? job.summary
        : job.error != null && job.error !== ''
          ? job.error
          : job.status
    out.push({
      id: `job-finish-${job.id}`,
      at: job.updated_at,
      kind: 'agent_finished',
      title: `Agent ${job.status} · ${scopeLabel}`,
      detail: summary,
      tone: jobFinishTone(job),
      jobId: job.id,
      scope: job.scope,
    })
  }

  return out
}

function eventsFromAudit(record: AuditRecord, jobIds: Set<string>): MissionTimelineEvent | null {
  if (!AUDIT_ACTION_PREFIXES.some(p => record.action.startsWith(p))) return null
  if (record.action === 'remediation.start' && jobIds.has(record.target)) return null
  if (
    (record.action === 'remediation.done' || record.action === 'remediation.failed') &&
    jobIds.has(record.target)
  ) {
    return null
  }

  return {
    id: `audit-${record.id}`,
    at: record.at,
    kind: 'audit_actuation',
    title: auditActionLabel(record.action),
    detail: [record.target, record.detail].filter(Boolean).join(' · '),
    tone: auditTone(record.status, record.action),
    auditId: record.id,
    jobId: jobIds.has(record.target) ? record.target : undefined,
  }
}

export function buildNightlyDriftSummary(
  nightlyReport?: AgentNightlyReportResponse,
  jobs: RemediationJob[] = [],
): string | null {
  if (nightlyReport == null) return null

  if (!nightlyReport.available) {
    return nightlyReport.hint ?? 'Nightly drift scan not available yet — awaiting Mac Mini run.'
  }

  const layers = parseNightlyLayerResults(nightlyReport.content)
  const fails = [layers.l1, layers.l2, layers.l3].filter(s => s === 'fail').length
  const briefJob = jobs.find(j => j.scope === 'nightly-drift-briefing')
  const when =
    nightlyReport.generated_at != null
      ? formatRemediationJobWhen(nightlyReport.generated_at)
      : 'recent'

  if (fails > 0) {
    return `Nightly drift ${when}: L1=${layers.l1} · L2=${layers.l2} · L3=${layers.l3} — review Briefing sync loop${briefJob != null ? ` · brief job ${briefJob.status}` : ''}.`
  }

  return `Nightly drift ${when}: Layer 1–3 passed — engineer scan clean${briefJob != null ? ` · brief job ${briefJob.status}` : ''}.`
}

function buildTrajectories(
  jobs: RemediationJob[],
  events: MissionTimelineEvent[],
): MissionTimelineTrajectory[] {
  const byJob = new Map<string, MissionTimelineEvent[]>()
  for (const ev of events) {
    if (ev.jobId == null) continue
    const list = byJob.get(ev.jobId)
    if (list == null) byJob.set(ev.jobId, [ev])
    else list.push(ev)
  }

  const trajectories: MissionTimelineTrajectory[] = []
  for (const job of jobs) {
    const jobEvents = byJob.get(job.id)
    if (jobEvents == null || jobEvents.length < 2) continue
    const sorted = [...jobEvents].sort((a, b) => Date.parse(a.at) - Date.parse(b.at))
    trajectories.push({
      jobId: job.id,
      scope: job.scope ?? 'agent-desk',
      label: remediationScopeShortLabel(job.scope),
      eventIds: sorted.map(e => e.id),
      startedAt: sorted[0].at,
      finishedAt:
        job.status === 'done' || job.status === 'failed' || job.status === 'cancelled'
          ? job.updated_at
          : undefined,
      status: job.status,
    })
  }

  return trajectories.sort((a, b) => Date.parse(b.startedAt) - Date.parse(a.startedAt))
}

export function buildMissionTimelineModel(input: {
  jobs: RemediationJob[]
  auditRecords: AuditRecord[]
  nightlyReport?: AgentNightlyReportResponse
  snapshot?: MissionSnapshot
  probeObservedAt?: number
  withinMs?: number
}): MissionTimelineModel {
  const withinMs = input.withinMs ?? DEFAULT_WITHIN_MS
  const cutoffMs = Date.now() - withinMs

  const recentJobs = input.jobs.filter(
    j => withinWindow(j.created_at, cutoffMs) || withinWindow(j.updated_at, cutoffMs),
  )
  const jobIds = new Set(recentJobs.map(j => j.id))

  const events: MissionTimelineEvent[] = []

  if (input.snapshot != null) {
    const degraded: string[] = []
    if (input.snapshot.payloadOverall !== 'ok') {
      degraded.push(`Payload ${input.snapshot.payloadOverall}`)
    }
    if (input.snapshot.release.signal !== 'ok') {
      degraded.push(`Release ${input.snapshot.release.signal}`)
    }
    if (input.snapshot.missionOverall !== 'ok') {
      degraded.push(`Mission ${input.snapshot.missionOverall}`)
    }
    if (degraded.length > 0) {
      const at =
        input.probeObservedAt != null && Number.isFinite(input.probeObservedAt)
          ? new Date(input.probeObservedAt).toISOString()
          : new Date().toISOString()
      events.push({
        id: 'probe-alert-current',
        at,
        kind: 'probe_alert',
        title: 'Mission probes degraded',
        detail: degraded.join(' · '),
        tone: degraded.some(d => d.includes('fail') || d.includes('critical')) ? 'fail' : 'warning',
      })
    }
  }

  for (const job of recentJobs) {
    events.push(...eventsFromJob(job))
  }

  for (const record of input.auditRecords) {
    if (!withinWindow(record.at, cutoffMs)) continue
    const ev = eventsFromAudit(record, jobIds)
    if (ev != null) events.push(ev)
  }

  const nightlySummary = buildNightlyDriftSummary(input.nightlyReport, input.jobs)
  if (nightlySummary != null && input.nightlyReport?.generated_at != null) {
    events.push({
      id: 'nightly-drift-summary',
      at: input.nightlyReport.generated_at,
      kind: 'nightly_drift',
      title: 'Nightly drift scan',
      detail: nightlySummary,
      tone: nightlySummary.includes('passed') ? 'ok' : nightlySummary.includes('L1=') ? 'warning' : 'neutral',
    })
  }

  events.sort((a, b) => Date.parse(b.at) - Date.parse(a.at))

  const trajectories = buildTrajectories(recentJobs, events)

  return {
    events: events.slice(0, 48),
    nightlySummary,
    trajectories,
    windowLabel: 'Last 24h',
  }
}

export function missionTimelineToneToLamp(
  tone: MissionTimelineTone,
): 'ok' | 'degraded' | 'fail' | 'unknown' {
  switch (tone) {
    case 'ok':
      return 'ok'
    case 'warning':
      return 'degraded'
    case 'fail':
      return 'fail'
    default:
      return 'unknown'
  }
}
