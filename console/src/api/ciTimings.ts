import { authedFetch } from './client'

export type TimingResult = { name: string; value: string }
export type TimingStep = { name: string; status: string; started_at?: string; finished_at?: string; exit_code?: number }
export type TimingTask = {
  name: string; task: string; status: string; created_at?: string; finished_at?: string
  results: TimingResult[]; steps: TimingStep[]
}
export type TimingRun = {
  name: string; pipeline: string; revision?: string; status: string; reason?: string
  created_at: string; start_time?: string; completion_time?: string; trigger?: string
  params: Record<string, string>; results: TimingResult[]; tasks: TimingTask[]; skipped: string[]
}
export type PipelineTimings = { as_of: string; retained: number; limit: number; runs: TimingRun[] }

export async function fetchCITimings(pipeline: string, run?: string, signal?: AbortSignal): Promise<PipelineTimings> {
  const query = new URLSearchParams({ limit: '60' })
  if (pipeline === 'bifrost-land') query.set('repo', 'bifrost-platform')
  if (run) query.set('run', run)
  const response = await authedFetch('CI timings', `/api/v1/delivery/pipelines/${encodeURIComponent(pipeline)}/timings?${query}`, { signal })
  const body = await response.json() as PipelineTimings
  if (!Array.isArray(body.runs) || !body.runs.every(r => Array.isArray(r.tasks) && Array.isArray(r.results))) throw new Error('CI timings: incomplete answer')
  return body
}
