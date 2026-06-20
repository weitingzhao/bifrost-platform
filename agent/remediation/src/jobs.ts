import { randomUUID } from 'node:crypto'
import type { RemediationEvent, RemediationJob, RemediationPhase } from './types.js'

type Listener = (event: RemediationEvent) => void

interface JobRecord extends RemediationJob {
  listeners: Set<Listener>
  abort?: AbortController
}

const jobs = new Map<string, JobRecord>()

function nowIso(): string {
  return new Date().toISOString()
}

function makeEvent(type: RemediationEvent['type'], text: string, meta?: Record<string, unknown>): RemediationEvent {
  return { id: randomUUID(), at: nowIso(), type, text, meta }
}

export function listJobs(): RemediationJob[] {
  return [...jobs.values()]
    .map(({ listeners: _l, abort: _a, ...job }) => job)
    .sort((a, b) => b.created_at.localeCompare(a.created_at))
}

export function getJob(id: string): RemediationJob | undefined {
  const job = jobs.get(id)
  if (job == null) return undefined
  const { listeners: _l, abort: _a, ...rest } = job
  return rest
}

export function createJob(): JobRecord {
  const ts = nowIso()
  const job: JobRecord = {
    id: randomUUID(),
    phase: 'starting',
    status: 'running',
    created_at: ts,
    updated_at: ts,
    events: [],
    listeners: new Set(),
    abort: new AbortController(),
  }
  jobs.set(job.id, job)
  return job
}

export function subscribe(id: string, listener: Listener): () => void {
  const job = jobs.get(id)
  if (job == null) return () => undefined
  job.listeners.add(listener)
  for (const event of job.events) listener(event)
  return () => job.listeners.delete(listener)
}

export function appendEvent(id: string, event: RemediationEvent): void {
  const job = jobs.get(id)
  if (job == null) return
  job.events.push(event)
  job.updated_at = nowIso()
  for (const listener of job.listeners) listener(event)
}

export function setPhase(id: string, phase: RemediationPhase): void {
  const job = jobs.get(id)
  if (job == null) return
  job.phase = phase
  job.updated_at = nowIso()
  appendEvent(id, makeEvent('status', phase, { phase }))
}

export function finishJob(
  id: string,
  status: RemediationJob['status'],
  summary: string,
  error?: string,
): void {
  const job = jobs.get(id)
  if (job == null) return
  job.status = status
  job.phase = status === 'done' ? 'done' : status === 'cancelled' ? 'cancelled' : 'failed'
  job.summary = summary
  job.error = error
  job.updated_at = nowIso()
  const type = status === 'done' ? 'done' : 'error'
  appendEvent(id, makeEvent(type, summary, error != null ? { error } : undefined))
}

export function cancelJob(id: string): boolean {
  const job = jobs.get(id)
  if (job == null || job.status !== 'running') return false
  job.abort?.abort()
  finishJob(id, 'cancelled', 'Remediation cancelled by operator.')
  return true
}

export function jobAbortSignal(id: string): AbortSignal | undefined {
  return jobs.get(id)?.abort?.signal
}

export { makeEvent }
