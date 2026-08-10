import type { DevAgentJob, DevAgentJobTrace } from '@/api/devAgentTypes'

export type AgentExecutionKpis = {
  totalRuns: number
  firstPassRate: number | null
  firstPassHeuristic: 'per-phase-first' | 'done-over-terminal'
  avgDurationMs: number | null
}

const IN_FLIGHT: ReadonlySet<DevAgentJob['status']> = new Set([
  'running',
  'awaiting_review',
  'idle',
])

/** Active job first, then history (oldest → newest), deduped by id. */
export function collectTraceJobs(trace: DevAgentJobTrace): DevAgentJob[] {
  const out: DevAgentJob[] = []
  const seen = new Set<string>()
  const push = (job: DevAgentJob | null | undefined) => {
    if (job == null || job.id === '' || seen.has(job.id)) return
    seen.add(job.id)
    out.push(job)
  }
  push(trace.active_job)
  for (const job of trace.history) push(job)
  return out
}

export function groupJobsByPhase(jobs: DevAgentJob[]): Array<{ phaseId: string; jobs: DevAgentJob[] }> {
  const order: string[] = []
  const byPhase = new Map<string, DevAgentJob[]>()
  for (const job of jobs) {
    const key = job.phase_id.trim() || '(unknown)'
    const list = byPhase.get(key)
    if (list == null) {
      byPhase.set(key, [job])
      order.push(key)
    } else {
      list.push(job)
    }
  }
  return order.map(phaseId => ({ phaseId, jobs: byPhase.get(phaseId) ?? [] }))
}

export function jobDurationMs(job: Pick<DevAgentJob, 'started_at' | 'completed_at'>): number | null {
  const started = job.started_at?.trim()
  const completed = job.completed_at?.trim()
  if (started == null || started === '' || completed == null || completed === '') return null
  const startMs = Date.parse(started)
  const endMs = Date.parse(completed)
  if (Number.isNaN(startMs) || Number.isNaN(endMs) || endMs < startMs) return null
  return endMs - startMs
}

export function computeAgentExecutionKpis(jobs: DevAgentJob[]): AgentExecutionKpis {
  const totalRuns = jobs.length

  let firstPassOk = 0
  let firstPassN = 0
  for (const { jobs: phaseJobs } of groupJobsByPhase(jobs)) {
    const first = phaseJobs[0]
    if (first == null || IN_FLIGHT.has(first.status) || first.status === 'cancelled') continue
    firstPassN += 1
    if (first.status === 'done') firstPassOk += 1
  }

  const done = jobs.filter(j => j.status === 'done').length
  const failed = jobs.filter(j => j.status === 'failed').length
  const terminal = done + failed
  const simpleRate = terminal > 0 ? done / terminal : null

  const firstPassRate = firstPassN > 0 ? firstPassOk / firstPassN : simpleRate
  const firstPassHeuristic = firstPassN > 0 ? 'per-phase-first' : 'done-over-terminal'

  let durSum = 0
  let durN = 0
  for (const job of jobs) {
    const ms = jobDurationMs(job)
    if (ms == null) continue
    durSum += ms
    durN += 1
  }

  return {
    totalRuns,
    firstPassRate,
    firstPassHeuristic,
    avgDurationMs: durN > 0 ? durSum / durN : null,
  }
}

export function formatDurationMs(ms: number): string {
  const sec = Math.round(ms / 1000)
  if (sec < 60) return `${sec}s`
  const min = Math.floor(sec / 60)
  const rem = sec % 60
  return rem === 0 ? `${min}m` : `${min}m ${rem}s`
}

export function formatRate(rate: number): string {
  return `${Math.round(rate * 100)}%`
}
