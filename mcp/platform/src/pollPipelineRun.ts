/**
 * Wait for one Tekton PipelineRun to finish, reading only the delivery GET
 * routes:
 *   GET /api/v1/delivery/pipelines/{name}/runs   the runs of one pipeline
 *   GET /api/v1/delivery/runs/{id}/steps         a run's pipeline, phases and tasks
 * There is no route that reads one run by name, so a run named without its
 * pipeline is found through the steps route (it answers the pipeline of a run
 * that exists) and then in that pipeline's list.
 */

export const FULL_SHA = /^[0-9a-f]{40}$/

export const RUN_NOT_SEEN_NOTE =
  'No such run was seen before the timeout. It may not have been created yet.'

export const TASK_DURATION_NOTE =
  'The steps route reports no task times, so each task duration_seconds is null.'

export interface PipelineRunWaitOptions {
  /** PipelineRun name. Given, it is the run waited for. */
  run?: string
  /** Pipeline name. Required with revision; optional with run. */
  pipeline?: string
  /** 40-character commit: the newest run of pipeline whose revision equals it. */
  revision?: string
  timeoutMs: number
  intervalMs: number
  now: () => number
  sleep: (ms: number) => Promise<void>
  /** GET a platform API path and return its JSON body; throws on a non-2xx answer. */
  get: (path: string) => Promise<unknown>
}

export interface PipelineTaskResult {
  name: string
  status: string
  reason?: string
  duration_seconds: number | null
}

export interface PipelineRunWaitResult {
  found: boolean
  run: string | null
  pipeline: string | null
  revision: string | null
  /** Succeeded, Failed, Running, Pending, or NotFound. */
  status: string
  /** The Tekton condition reason as the list reports it (Succeeded, Failed, Cancelled, Running, …). */
  reason: string
  finished: boolean
  timed_out: boolean
  started_at: string | null
  completed_at: string | null
  duration_seconds: number | null
  tasks: PipelineTaskResult[]
  phases: unknown[]
  waited_seconds: number
  note?: string
  last_error?: string
}

interface RunView {
  name: string
  pipeline: string
  revision: string
  status: string
  reason: string
  start_time: string
  completion_time: string
}

function asObject(value: unknown): Record<string, unknown> {
  if (value != null && typeof value === 'object' && !Array.isArray(value)) return value as Record<string, unknown>
  return {}
}

function text(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function runView(value: unknown): RunView {
  const o = asObject(value)
  return {
    name: text(o.name),
    pipeline: text(o.pipeline),
    revision: text(o.revision),
    status: text(o.status),
    reason: text(o.reason),
    start_time: text(o.start_time),
    completion_time: text(o.completion_time),
  }
}

export function runsPath(pipeline: string): string {
  return `/api/v1/delivery/pipelines/${encodeURIComponent(pipeline)}/runs`
}

export function stepsPath(run: string): string {
  return `/api/v1/delivery/runs/${encodeURIComponent(run)}/steps`
}

/** When the run started, in ms: its start time, else the unix seconds its name ends with, else 0. */
function startedAtMs(run: RunView): number {
  const parsed = Date.parse(run.start_time)
  if (!Number.isNaN(parsed)) return parsed
  const suffix = run.name.match(/-(\d{9,11})$/)
  return suffix ? Number(suffix[1]) * 1000 : 0
}

/** The newest run whose revision equals the commit; the list order breaks a tie. */
export function newestRunOfRevision(runs: unknown[], revision: string): RunView | undefined {
  let best: RunView | undefined
  let bestAt = -1
  for (const item of runs) {
    const run = runView(item)
    if (run.name === '' || run.revision !== revision) continue
    const at = startedAtMs(run)
    if (best === undefined || at > bestAt) {
      best = run
      bestAt = at
    }
  }
  return best
}

/**
 * The list's status is the Tekton Succeeded condition: "True", "False" or
 * "Unknown". A run without a condition yet has not started.
 */
export function runStatus(run: { status: string; reason: string; start_time: string }): { status: string; finished: boolean } {
  if (run.status === 'True') return { status: 'Succeeded', finished: true }
  if (run.status === 'False') return { status: 'Failed', finished: true }
  if (run.reason === '' && run.start_time === '') return { status: 'Pending', finished: false }
  return { status: 'Running', finished: false }
}

function secondsBetween(from: string, to: string): number | null {
  const a = Date.parse(from)
  const b = Date.parse(to)
  if (Number.isNaN(a) || Number.isNaN(b) || b < a) return null
  return Math.round((b - a) / 1000)
}

/** The steps route's tasks. Task times are read when the route reports them. */
export function taskResults(steps: unknown): PipelineTaskResult[] {
  const tasks = asObject(steps).tasks
  if (!Array.isArray(tasks)) return []
  return tasks.map((item) => {
    const o = asObject(item)
    const reason = text(o.reason)
    const reported = typeof o.duration_seconds === 'number' ? o.duration_seconds : null
    return {
      name: text(o.pipeline_task) || text(o.name),
      status: text(o.status),
      ...(reason !== '' ? { reason } : {}),
      duration_seconds: reported ?? secondsBetween(text(o.start_time), text(o.completion_time)),
    }
  })
}

/** Whether any task of the steps answer carries a time: today none does. */
export function reportsTaskTimes(steps: unknown): boolean {
  const tasks = asObject(steps).tasks
  if (!Array.isArray(tasks)) return false
  return tasks.some((item) => {
    const o = asObject(item)
    return 'start_time' in o || 'completion_time' in o || 'duration_seconds' in o
  })
}

/**
 * A 4xx answer will not change by asking again (a wrong token, a route this
 * platform does not serve; a run that does not exist is a 200, not a 404),
 * so it ends the wait. Anything else — the API restarting while it is the
 * thing being delivered, a gateway error, a dropped connection — is waited out.
 */
export function isPermanentReadError(err: unknown): boolean {
  const found = String((err as Error)?.message ?? err).match(/: HTTP (\d{3})\b/)
  if (!found) return false
  const status = Number(found[1])
  return status >= 400 && status < 500 && status !== 408 && status !== 429
}

class UnreadableList extends Error {}

async function listRuns(opts: PipelineRunWaitOptions, pipeline: string): Promise<unknown[]> {
  const body = asObject(await opts.get(runsPath(pipeline)))
  const reach = text(body.reachability)
  // An unreachable cluster answers 200 with no runs: that is not "no such run".
  if (reach !== '' && reach !== 'ok') {
    throw new UnreadableList(`runs of ${pipeline}: reachability ${reach}${text(body.detail) ? `: ${text(body.detail)}` : ''}`)
  }
  return Array.isArray(body.runs) ? body.runs : []
}

/** One look: the run waited for, or undefined while it is not in any list. */
async function findRun(opts: PipelineRunWaitOptions, locked: { run?: string; pipeline?: string }): Promise<RunView | undefined> {
  const name = locked.run ?? opts.run
  if (name === undefined) {
    return newestRunOfRevision(await listRuns(opts, opts.pipeline as string), opts.revision as string)
  }
  let pipeline = locked.pipeline ?? opts.pipeline
  if (pipeline === undefined || pipeline === '') {
    pipeline = text(asObject(await opts.get(stepsPath(name))).pipeline)
    if (pipeline === '') return undefined
  }
  const runs = await listRuns(opts, pipeline)
  for (const item of runs) {
    const run = runView(item)
    if (run.name === name) return { ...run, pipeline: run.pipeline || pipeline }
  }
  return undefined
}

function inputError(opts: PipelineRunWaitOptions): string | undefined {
  const run = opts.run ?? ''
  const pipeline = opts.pipeline ?? ''
  const revision = opts.revision ?? ''
  if (run !== '' && revision !== '') return 'name either run, or pipeline and revision, not both'
  if (run === '' && (pipeline === '' || revision === '')) return 'name either run, or pipeline and revision'
  if (run === '' && !FULL_SHA.test(revision)) return 'revision must be a 40-character lowercase git SHA'
  return undefined
}

/**
 * Poll until the run finishes or the deadline. A run that is not there yet is
 * waited for: it may not have been created. Once a run is found the wait
 * stays on that run.
 */
export async function pollPipelineRun(opts: PipelineRunWaitOptions): Promise<PipelineRunWaitResult | { error: string }> {
  const bad = inputError(opts)
  if (bad) return { error: bad }
  const wanted: PipelineRunWaitOptions = {
    ...opts,
    run: opts.run !== undefined && opts.run !== '' ? opts.run : undefined,
    pipeline: opts.pipeline !== undefined && opts.pipeline !== '' ? opts.pipeline : undefined,
  }
  const began = opts.now()
  const deadline = began + opts.timeoutMs
  const locked: { run?: string; pipeline?: string } = {}
  let seen: RunView | undefined
  let lastError: string | undefined
  let timedOut = false
  for (;;) {
    try {
      const run = await findRun(wanted, locked)
      lastError = undefined
      if (run !== undefined) {
        seen = run
        locked.run = run.name
        locked.pipeline = run.pipeline
        if (runStatus(run).finished) break
      }
    } catch (err) {
      if (isPermanentReadError(err)) throw err
      lastError = String((err as Error)?.message ?? err)
    }
    const remaining = deadline - opts.now()
    if (remaining <= 0) {
      timedOut = true
      break
    }
    await opts.sleep(Math.min(opts.intervalMs, remaining))
  }
  const waited = Math.round((opts.now() - began) / 1000)
  if (seen === undefined) {
    return {
      found: false,
      run: wanted.run ?? null,
      pipeline: wanted.pipeline ?? null,
      revision: opts.revision ?? null,
      status: 'NotFound',
      reason: '',
      finished: false,
      timed_out: timedOut,
      started_at: null,
      completed_at: null,
      duration_seconds: null,
      tasks: [],
      phases: [],
      waited_seconds: waited,
      note: RUN_NOT_SEEN_NOTE,
      ...(lastError !== undefined ? { last_error: lastError } : {}),
    }
  }
  let tasks: PipelineTaskResult[] = []
  let phases: unknown[] = []
  let taskTimes = true
  try {
    const steps = await opts.get(stepsPath(seen.name))
    tasks = taskResults(steps)
    taskTimes = reportsTaskTimes(steps)
    const got = asObject(steps).phases
    if (Array.isArray(got)) phases = got
  } catch (err) {
    if (isPermanentReadError(err)) throw err
    lastError = String((err as Error)?.message ?? err)
  }
  const state = runStatus(seen)
  return {
    found: true,
    run: seen.name,
    pipeline: seen.pipeline,
    revision: seen.revision !== '' ? seen.revision : null,
    status: state.status,
    reason: seen.reason,
    finished: state.finished,
    timed_out: timedOut,
    started_at: seen.start_time !== '' ? seen.start_time : null,
    completed_at: seen.completion_time !== '' ? seen.completion_time : null,
    duration_seconds: secondsBetween(seen.start_time, seen.completion_time),
    tasks,
    phases,
    waited_seconds: waited,
    ...(tasks.length > 0 && !taskTimes ? { note: TASK_DURATION_NOTE } : {}),
    ...(lastError !== undefined ? { last_error: lastError } : {}),
  }
}
