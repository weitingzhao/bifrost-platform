import { authedFetch } from '@/api/client'
import { signInSafeError } from '@/lib/signInError'

export const AGENT_RUNTIME_POLL_MS = 5_000
export type RuntimeRecord = Record<string, unknown>
export const record = (v: unknown): RuntimeRecord => v !== null && typeof v === 'object' && !Array.isArray(v) ? v as RuntimeRecord : {}
export const records = (v: unknown): RuntimeRecord[] => Array.isArray(v) ? v.filter(x => x !== null && typeof x === 'object' && !Array.isArray(x)) as RuntimeRecord[] : []
export const text = (v: unknown): string => typeof v === 'string' ? v : ''
const number = (v: unknown): number | undefined => typeof v === 'number' && Number.isFinite(v) ? v : undefined
const strings = (v: unknown): string[] => Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []

export type RuntimeTask = {
  task_id: string; title: string; role: string; state: string; status: string; reason: string
  since: string; round_no?: number; max_rounds?: number; depends_on: string[]
}
/**
 * When a mission should be done (2.5 G8; store/progresseta.go): its unfinished tasks one after another,
 * each from its usual time to 1.5 times it, with the role and task class each usual time came from;
 * or unknown, with why.
 */
export type EtaBasis = { role: string; task_class: string; usual_s: number; samples: number }
export type MissionEta =
  | { state: 'estimated'; earliest: string; latest: string; basis: EtaBasis[]; reason: string; past_usual: string[] }
  | { state: 'unknown'; reason: string }
export type RuntimeMission = {
  mission_id: string; title: string; goal: string; state: string; status: string; reason: string
  since: string; deadline_at: string; eta: MissionEta; next_steps: unknown[]
  waiting_on: string; waiting_roles: string[]; tasks: RuntimeTask[]
}
export type RuntimeHost = {
  host_id: string; state: string; online?: boolean; poll_age_s?: number; last_poll_at: string; live_attempts?: number
}
export type RuntimeAttempt = {
  attempt_id: string; task_id: string; mission_id: string; host_id: string; executor: string
  model: string; state: string; elapsed_s?: number; heartbeat_age_s?: number; status: string; reason: string
}
export type RuntimeProgress = {
  as_of_event_id?: number; generated_at: string; hosts: RuntimeHost[]; live: RuntimeAttempt[]
  needs_you: { count?: number; items: unknown[]; truncated?: boolean }
  work: { missions: RuntimeMission[] }; recent: { tasks: RuntimeRecord[]; failed_attempts: RuntimeRecord[] }
  controls: RuntimeRecord[]; plan: unknown[]
  /** The answer had a plan list: an empty plan is only one that was read empty (review 431 B1). */
  planRead?: boolean
}
export type RuntimeEvent = {
  seq?: number; event_type: string; at: string; actor_id: string; task_id: string; attempt_id: string; body_json: RuntimeRecord
}
export type GoalResult = {
  mission_id: string; mission_state: string; waiting: string; task_id: string; repo: string; repos: string[]; base_commit: string
  spec_artifact_id: string
}
/**
 * The repositories a goal may name: the platform's `store.Repos` (a Go test,
 * TestConsoleGoalReposAreTheRuntimes, reads this list and holds it to them).
 */
export const GOAL_REPOS = ['bifrost-platform', 'bifrost-trade-infra'] as const

export function parseRuntimeTask(v: unknown): RuntimeTask {
  const r = record(v)
  return { task_id: text(r.task_id), title: text(r.title), role: text(r.role), state: text(r.state),
    status: text(r.status), reason: text(r.reason), since: text(r.since) || text(r.ended_at),
    round_no: number(r.round_no), max_rounds: number(r.max_rounds), depends_on: strings(r.depends_on) }
}
const count = (v: unknown): v is number => typeof v === 'number' && Number.isInteger(v) && v >= 0
/**
 * An estimate whose times or basis do not read is unknown, not a guess (v4 §20.3): a basis with no
 * task class, or with no samples behind its usual time, is not read as another basis (review 411 C2).
 */
export function parseMissionEta(v: unknown): MissionEta {
  const r = record(v), reason = text(r.reason)
  if (r.state !== 'estimated') return { state: 'unknown', reason: reason || 'the runtime gave no estimate' }
  const basis = Array.isArray(r.basis) ? r.basis.map(record) : []
  const earliest = text(r.earliest), latest = text(r.latest)
  if (!Number.isFinite(Date.parse(earliest)) || !Number.isFinite(Date.parse(latest)) || Date.parse(latest) < Date.parse(earliest)
    || basis.length === 0 || basis.some(b => !text(b.role) || typeof b.task_class !== 'string' || !count(b.usual_s) || !count(b.samples) || b.samples < 1)) {
    return { state: 'unknown', reason: 'the runtime\'s estimate could not be read' }
  }
  return { state: 'estimated', earliest, latest, reason, past_usual: strings(r.past_usual),
    basis: basis.map(b => ({ role: text(b.role), task_class: b.task_class as string, usual_s: b.usual_s as number, samples: b.samples as number })) }
}
export function parseRuntimeProgress(v: unknown): RuntimeProgress {
  const r = record(v), needs = record(r.needs_you), recent = record(r.recent)
  return {
    as_of_event_id: number(r.as_of_event_id), generated_at: text(r.generated_at),
    hosts: records(r.hosts).map(h => ({ host_id: text(h.host_id), state: text(h.state),
      online: typeof h.online === 'boolean' ? h.online : undefined, poll_age_s: number(h.poll_age_s),
      last_poll_at: text(h.last_poll_at), live_attempts: number(h.live_attempts) })),
    live: records(r.live).map(a => ({ attempt_id: text(a.attempt_id), task_id: text(a.task_id),
      mission_id: text(a.mission_id), host_id: text(a.host_id), executor: text(a.executor), model: text(a.model),
      state: text(a.state), elapsed_s: number(a.elapsed_s), heartbeat_age_s: number(a.heartbeat_age_s),
      status: text(a.status), reason: text(a.reason) })),
    needs_you: { count: number(needs.count), items: Array.isArray(needs.items) ? needs.items : [],
      truncated: typeof needs.truncated === 'boolean' ? needs.truncated : undefined },
    work: { missions: records(record(r.work).missions).map(m => ({ mission_id: text(m.mission_id),
      title: text(m.title), goal: text(m.goal), state: text(m.state), status: text(m.status), reason: text(m.reason),
      since: text(m.since), deadline_at: text(m.deadline_at), eta: parseMissionEta(m.eta),
      next_steps: Array.isArray(m.next_steps) ? m.next_steps : [], waiting_on: text(m.waiting_on),
      waiting_roles: strings(m.waiting_roles), tasks: records(m.tasks).map(parseRuntimeTask) })) },
    recent: { tasks: records(recent.tasks), failed_attempts: records(recent.failed_attempts) },
    controls: records(r.controls), plan: Array.isArray(r.plan) ? r.plan : [], planRead: Array.isArray(r.plan),
  }
}
export function parseRuntimeEvents(v: unknown): RuntimeEvent[] {
  return records(v).map(e => ({ seq: number(e.seq), event_type: text(e.event_type), at: text(e.at), actor_id: text(e.actor_id),
    task_id: text(e.task_id), attempt_id: text(e.attempt_id), body_json: record(e.body_json) }))
}
export function parseGoalResult(v: unknown): GoalResult {
  const r = record(v)
  return { mission_id: text(r.mission_id), mission_state: text(r.mission_state), waiting: text(r.waiting),
    task_id: text(r.task_id), repo: text(r.repo), repos: strings(r.repos), base_commit: text(r.base_commit),
    spec_artifact_id: text(r.spec_artifact_id) }
}
export async function fetchRuntimeProgress(signal?: AbortSignal): Promise<RuntimeProgress> {
  const r = await authedFetch('Agent runtime', '/api/v1/agentrt/progress', { signal })
  return parseRuntimeProgress(await r.json())
}
export async function fetchRuntimeEvents(mission: string, signal?: AbortSignal): Promise<RuntimeEvent[]> {
  const events: RuntimeEvent[] = []
  let after = 0
  for (;;) {
    const r = await authedFetch('Agent events', `/api/v1/agentrt/events?${new URLSearchParams({ stream: `mission:${mission}`, after_seq: String(after) })}`, { signal })
    const page = parseRuntimeEvents(await r.json())
    events.push(...page)
    if (page.length < 500) return events
    const last = page.at(-1)?.seq
    if (last === undefined || last <= after) throw new Error('Agent events: invalid pagination cursor')
    after = last
  }
}
/** One task as GET /agentrt/tasks/{id} answers (store.TaskView): what the Crew card names a live attempt by. */
export type RuntimeTaskDetail = { task_id: string; title: string; role: string; mission_id: string; subject_task_id: string }
export function parseRuntimeTaskDetail(v: unknown): RuntimeTaskDetail {
  const r = record(v)
  return { task_id: text(r.task_id), title: text(r.title), role: text(r.role), mission_id: text(r.mission_id), subject_task_id: text(r.subject_task_id) }
}
export async function fetchRuntimeTaskDetail(id: string, signal?: AbortSignal): Promise<RuntimeTaskDetail> {
  const r = await authedFetch('Agent task', `/api/v1/agentrt/tasks/${encodeURIComponent(id)}`, { signal })
  return parseRuntimeTaskDetail(await r.json())
}
export async function fetchRuntimeArtifact(id: string, signal?: AbortSignal): Promise<string> {
  const r = await authedFetch('Agent artifact', `/api/v1/agentrt/artifacts/${encodeURIComponent(id)}`, { signal })
  return r.text()
}
async function goalError(prefix: string, response: Response, token = ''): Promise<Error> {
  let message = `HTTP ${response.status}`
  try {
    const r = record(await response.json())
    message = text(r.message) || text(r.error) || message
  } catch { /* Non-JSON errors retain the HTTP status. */ }
  return new Error(signInSafeError(`${prefix}: ${message}`, token, response.status))
}
/** A goal of one repository is sent as repo, as before; of several, as repos. */
export async function submitRuntimeGoal(goal: string, key: string, repos: readonly string[] = ['bifrost-platform']): Promise<GoalResult> {
  const named = repos.length === 1 ? { repo: repos[0] } : { repos }
  const r = await authedFetch('Goal', '/api/v1/agentrt/goals', {
    method: 'POST', body: JSON.stringify({ idempotency_key: key, goal, ...named }),
  }, goalError)
  const result = parseGoalResult(await r.json())
  if (!result.mission_id) throw new Error('Goal: invalid platform response; retry with the same goal.')
  return result
}
