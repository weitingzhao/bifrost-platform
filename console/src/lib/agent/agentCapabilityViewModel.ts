/**
 * Live Agent Capability map — pure view-model over catalog + remediation jobs + bridge.
 * Governance → Agent System stays design-only; Engineer → Agent Capability uses this overlay.
 */

import type { AgentBridgeResponse } from '@/api/agentTypes'
import type { RemediationHealthResponse, RemediationJob } from '@/api/remediationTypes'
import type { AgentTaskEntry } from '@/lib/agent/agentTaskCatalog'
import { AGENT_TASK_RELATIONS } from '@/lib/agent/agentTaskCatalog'

export type AgentCapabilityLiveStatus =
  | 'ready'
  | 'running'
  | 'awaiting'
  | 'failed'
  | 'degraded'
  | 'idle'

export type AgentCapabilityFilter = 'all' | 'attention' | 'ready'

export type AgentCapabilityNode = {
  task: AgentTaskEntry
  status: AgentCapabilityLiveStatus
  /** Active or most recent job for this scope (if any). */
  jobId: string | null
  jobSummary: string | null
}

export type AgentCapabilityStrip = {
  runtimeReachable: boolean
  runtimeLabel: string
  running: number
  awaiting: number
  failed: number
  ready: number
  idle: number
  degraded: number
  total: number
}

export type AgentCapabilityViewModel = {
  strip: AgentCapabilityStrip
  nodes: AgentCapabilityNode[]
  statusByTaskId: Record<string, AgentCapabilityLiveStatus>
  /** Relation keys `fromId→toId` soft-highlighted when a recent escalation job matches. */
  highlightedEdgeKeys: string[]
  summaryLine: string
}

export type AgentCapabilityViewModelInput = {
  tasks: AgentTaskEntry[]
  jobs: RemediationJob[]
  bridge?: AgentBridgeResponse | null
  health?: RemediationHealthResponse | null
}

function runnerOk(
  bridge: AgentBridgeResponse | null | undefined,
  health: RemediationHealthResponse | null | undefined,
): { ok: boolean; label: string } {
  const runner = bridge?.remediation_runner
  // Bridge runner is authoritative when present — do not let health=ok mask unavailable.
  if (runner != null) {
    if (runner.status === 'ok') return { ok: true, label: 'Runtime ready' }
    if (runner.status === 'unavailable') return { ok: false, label: 'Runtime unreachable' }
    if (runner.status === 'not_configured') return { ok: false, label: 'Runtime not configured' }
    return { ok: false, label: `Runtime ${runner.status}` }
  }
  if (health?.status === 'ok') return { ok: true, label: 'Runtime ready' }
  if (health?.status === 'unavailable') return { ok: false, label: 'Runtime unreachable' }
  if (health?.status === 'not_configured') return { ok: false, label: 'Runtime not configured' }
  if (health != null) return { ok: false, label: `Runtime ${health.status}` }
  return { ok: false, label: 'Runtime unknown' }
}

function scopeKeysForTask(task: AgentTaskEntry): Set<string> {
  const keys = new Set<string>([task.scope, task.id])
  for (const a of task.aliases ?? []) keys.add(a)
  return keys
}

function jobsForTask(task: AgentTaskEntry, jobs: RemediationJob[]): RemediationJob[] {
  const keys = scopeKeysForTask(task)
  return jobs
    .filter(j => j.scope != null && keys.has(j.scope))
    .sort((a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at))
}

function isOrphaned(job: RemediationJob): boolean {
  return (
    job.error === 'orphaned' ||
    (job.status === 'running' && (job.summary?.includes('Orphaned') ?? false))
  )
}

function statusFromJobs(
  taskJobs: RemediationJob[],
  runtimeOk: boolean,
): { status: AgentCapabilityLiveStatus; job: RemediationJob | null } {
  if (!runtimeOk) {
    const latest = taskJobs[0] ?? null
    return { status: 'degraded', job: latest }
  }

  const active = taskJobs.find(
    j =>
      (j.status === 'running' || j.phase === 'awaiting_approval') && !isOrphaned(j),
  )
  if (active != null) {
    if (active.phase === 'awaiting_approval') {
      return { status: 'awaiting', job: active }
    }
    return { status: 'running', job: active }
  }

  const latest = taskJobs[0] ?? null
  if (latest == null) return { status: 'idle', job: null }
  if (isOrphaned(latest)) return { status: 'degraded', job: latest }
  if (latest.status === 'failed') return { status: 'failed', job: latest }
  if (latest.status === 'done') return { status: 'ready', job: latest }
  if (latest.status === 'cancelled') return { status: 'idle', job: latest }
  return { status: 'idle', job: latest }
}

/** Soft-highlight escalation edges when a recent failed/running job sits on the `to` or `from` scope. */
function computeHighlightedEdges(
  nodes: AgentCapabilityNode[],
): string[] {
  const byId = new Map(nodes.map(n => [n.task.id, n]))
  const keys: string[] = []
  for (const rel of AGENT_TASK_RELATIONS) {
    if (rel.kind !== 'escalation' && rel.kind !== 'on-failure') continue
    const from = byId.get(rel.fromId)
    const to = byId.get(rel.toId)
    if (from == null || to == null) continue
    const hot =
      from.status === 'failed' ||
      from.status === 'running' ||
      from.status === 'awaiting' ||
      to.status === 'failed' ||
      to.status === 'running' ||
      to.status === 'awaiting' ||
      to.status === 'degraded'
    if (hot) keys.push(`${rel.fromId}→${rel.toId}`)
  }
  return keys
}

export function liveStatusStroke(status: AgentCapabilityLiveStatus): string {
  switch (status) {
    case 'ready':
      return 'var(--color-lamp-green)'
    case 'running':
      return 'var(--color-lamp-blue, var(--primary))'
    case 'awaiting':
      return 'var(--color-lamp-yellow)'
    case 'failed':
      return 'var(--color-lamp-red)'
    case 'degraded':
      return 'var(--color-lamp-yellow)'
    case 'idle':
    default:
      return 'var(--muted-foreground)'
  }
}

export function liveStatusLabel(status: AgentCapabilityLiveStatus): string {
  switch (status) {
    case 'ready':
      return 'Ready'
    case 'running':
      return 'Running'
    case 'awaiting':
      return 'Awaiting'
    case 'failed':
      return 'Failed'
    case 'degraded':
      return 'Degraded'
    case 'idle':
      return 'Idle'
  }
}

export function nodeMatchesFilter(
  status: AgentCapabilityLiveStatus,
  filter: AgentCapabilityFilter,
): boolean {
  if (filter === 'all') return true
  if (filter === 'attention') {
    return (
      status === 'failed' ||
      status === 'degraded' ||
      status === 'awaiting' ||
      status === 'running'
    )
  }
  return status === 'ready' || status === 'idle'
}

export function buildAgentCapabilityViewModel(
  input: AgentCapabilityViewModelInput,
): AgentCapabilityViewModel {
  const { ok: runtimeReachable, label: runtimeLabel } = runnerOk(input.bridge, input.health)
  const jobs = input.jobs ?? []

  const nodes: AgentCapabilityNode[] = input.tasks.map(task => {
    const taskJobs = jobsForTask(task, jobs)
    const { status, job } = statusFromJobs(taskJobs, runtimeReachable)
    return {
      task,
      status,
      jobId: job?.id ?? null,
      jobSummary: job?.summary ?? job?.error ?? null,
    }
  })

  const statusByTaskId: Record<string, AgentCapabilityLiveStatus> = {}
  const counts: Record<AgentCapabilityLiveStatus, number> = {
    ready: 0,
    running: 0,
    awaiting: 0,
    failed: 0,
    degraded: 0,
    idle: 0,
  }
  for (const n of nodes) {
    statusByTaskId[n.task.id] = n.status
    counts[n.status] += 1
  }

  const strip: AgentCapabilityStrip = {
    runtimeReachable,
    runtimeLabel,
    running: counts.running,
    awaiting: counts.awaiting,
    failed: counts.failed,
    ready: counts.ready,
    idle: counts.idle,
    degraded: counts.degraded,
    total: nodes.length,
  }

  const highlightedEdgeKeys = computeHighlightedEdges(nodes)

  const attention = counts.failed + counts.degraded + counts.awaiting + counts.running
  const summaryLine = runtimeReachable
    ? attention > 0
      ? `${runtimeLabel} · ${counts.running} running · ${counts.awaiting} awaiting · ${counts.failed} failed · ${counts.degraded} degraded`
      : `${runtimeLabel} · ${counts.ready + counts.idle}/${nodes.length} scopes idle or ready`
    : `${runtimeLabel} · ${nodes.length} catalog scopes (live status degraded)`

  return {
    strip,
    nodes,
    statusByTaskId,
    highlightedEdgeKeys,
    summaryLine,
  }
}

/** Active job id for Desk deep-link, else null (open Desk without job). */
export function activeJobIdForTask(node: AgentCapabilityNode): string | null {
  if (node.status === 'running' || node.status === 'awaiting') return node.jobId
  if (node.status === 'failed' && node.jobId != null) return node.jobId
  return null
}
