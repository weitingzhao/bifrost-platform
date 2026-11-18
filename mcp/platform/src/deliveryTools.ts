import { platformGet } from './platformClient.js'
import { BUS_RECHECK_MS, busPause } from './busWait.js'
import { pollPipelineRun } from './pollPipelineRun.js'

// Read-only delivery tools: the release policy's judgement of one run
// request, the wait for a PipelineRun, and a change's delivery progress.
// Each takes its GET as an argument so the tests need no HTTP.

type Get = (path: string) => Promise<unknown>

export interface PipelineRunRequest {
  name: string
  revision?: string
  tag?: string
  who?: string
  params?: Record<string, string>
}

function trimmed(value: string | undefined): string {
  return value == null ? '' : value.trim()
}

/**
 * GET /api/v1/delivery/pipelines/{name}/policy-check. Empty query keys are
 * left out; params is the JSON object, URL-encoded.
 */
export function policyCheckPath(input: PipelineRunRequest): string {
  const q: string[] = []
  for (const [key, value] of [
    ['revision', trimmed(input.revision)],
    ['tag', trimmed(input.tag)],
    ['who', trimmed(input.who)],
  ] as const) {
    if (value !== '') q.push(`${key}=${encodeURIComponent(value)}`)
  }
  if (input.params != null && Object.keys(input.params).length > 0) {
    q.push(`params=${encodeURIComponent(JSON.stringify(input.params))}`)
  }
  const base = `/api/v1/delivery/pipelines/${encodeURIComponent(input.name)}/policy-check`
  return q.length > 0 ? `${base}?${q.join('&')}` : base
}

/** Judge one run request against the signed release policy. Starts nothing. */
export async function checkReleasePolicy(input: PipelineRunRequest, get: Get = platformGet): Promise<unknown> {
  return get(policyCheckPath(input))
}

export const DELIVERY_ONE_OF_ERROR = 'name exactly one of commit or task'

/** GET /api/v1/agentrt/delivery: the delivery stages of one commit or one task. */
export async function getDelivery(input: { commit?: string; task?: string }, get: Get = platformGet): Promise<unknown> {
  const commit = trimmed(input.commit)
  const task = trimmed(input.task)
  if ((commit === '') === (task === '')) return { error: DELIVERY_ONE_OF_ERROR }
  const query = commit !== '' ? `commit=${encodeURIComponent(commit)}` : `task=${encodeURIComponent(task)}`
  return get(`/api/v1/agentrt/delivery?${query}`)
}

export const RUN_WAIT_DEFAULT_SECONDS = 600
export const RUN_WAIT_MAX_SECONDS = 900
export const RUN_POLL_DEFAULT_SECONDS = 10
export const RUN_POLL_MIN_SECONDS = 5

function clamp(value: number, low: number, high: number): number {
  return Math.min(Math.max(value, low), high)
}

export interface WaitForPipelineRunInput {
  run?: string
  pipeline?: string
  revision?: string
  timeout_seconds?: number
  poll_seconds?: number
}

export async function waitForPipelineRun(input: WaitForPipelineRunInput, get: Get = platformGet): Promise<unknown> {
  const timeout = clamp(input.timeout_seconds ?? RUN_WAIT_DEFAULT_SECONDS, 1, RUN_WAIT_MAX_SECONDS)
  const poll = clamp(input.poll_seconds ?? RUN_POLL_DEFAULT_SECONDS, RUN_POLL_MIN_SECONDS, RUN_WAIT_MAX_SECONDS)
  const run = trimmed(input.run)
  const revision = trimmed(input.revision)
  const sleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms))
  return pollPipelineRun({
    run,
    pipeline: trimmed(input.pipeline),
    revision,
    timeoutMs: timeout * 1000,
    // Between reads it holds a long poll for the run's events (subject <run>@<sha>); poll_seconds is the pace without the bus.
    intervalMs: Math.max(poll * 1000, BUS_RECHECK_MS),
    now: () => Date.now(),
    sleep: busPause({
      types: 'ci.run.started,ci.run.finished', match: () => run || revision, fallbackMs: poll * 1000,
      get, now: () => Date.now(), sleep,
    }),
    get,
  })
}
