import { authedFetch, pathSegment } from '@/api/client'
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
  /** Its latest lost attempt that no later persisted result superseded (TD-352). */
  lost_attempt?: RuntimeAttempt
  /** Work outside the plan, with why (v5 §5.1.1: Progress marks it "off plan"); unset by an answer that does not say. */
  off_plan?: boolean; off_plan_reason?: string
  /** Its latest attempt computed a result the platform does not hold yet (GET /progress only; SPEC §2.2). */
  computed_not_persisted?: boolean
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
/** A tool a host's latest claim offered (W-31 3.5; store/presence.go). */
export type RuntimeOffer = { executor: string; provider: string; model: string }
export type RuntimeHost = {
  host_id: string; state: string; online?: boolean; poll_age_s?: number; last_poll_at: string; live_attempts?: number
  /**
   * What its latest claim offered, null while none is recorded, and whether the controller reports it offline (not online,
   * active, no stop or drain) or its disk low (W-31 3.5). Unset by a platform before 3.5.
   */
  offered?: RuntimeOffer[] | null; offered_at?: string; reported_offline?: boolean; disk_low?: boolean
}
/**
 * An executor some job's candidate names (W-31 3.5; store/presence.go): available, or not with why, or not known (null)
 * while a host that could take work has not said what it offers; the hosts that offer it and its jobs.
 */
export type RuntimeExecutor = { executor: string; available: boolean | null; why: string; hosts: string[]; jobs: string[] }
export type RuntimeAttempt = {
  attempt_id: string; attempt_no?: number; task_id: string; mission_id: string; host_id: string; executor: string
  model: string; state: string; elapsed_s?: number; heartbeat_age_s?: number; status: string; reason: string
  /**
   * What of its work is on the platform (TD-352; store/progresspersist.go): whether its result is persisted, the last
   * checkpoint the platform confirmed, and how long nothing of it was persisted; a lost attempt says when it ended.
   * Unset by a platform before TD-352.
   */
  persisted?: boolean; last_checkpoint_seq?: number; last_checkpoint_at?: string; unpersisted_for_s?: number; ended_at?: string
}
/**
 * The database's agentrt schema against the migrations this platform carries (TD-411; migrate.Check):
 * missing, edited and unknown name the migrations. Unread is a platform before TD-411.
 */
export type RuntimeSchema = {
  state: 'current' | 'behind' | 'ahead' | 'edited' | 'unknown' | 'unread'; missing: string[]; edited: string[]; unknown: number[]; reason: string
}
/** A controller loop that failed the same way several times in a row and has not passed since (TD-411). */
export type RuntimeLoopAlert = { loop: string; since: string; error: string }
/**
 * When the Owner is next needed after what waits for him now (W-31 2.5 gap 1; store/progressnext.go; v4 §20.2's top line,
 * §20.5 scenario 8): the open goal whose next step is his and is due first, its step and when; unknown while any such
 * goal cannot be timed; none when no goal's next step is his. candidates are every such goal, timed ones first.
 */
export type OwnerNextCandidate = {
  mission_id: string; step: { plan_node_id: string; title: string }; state: 'estimated' | 'unknown'
  earliest?: string; latest?: string; reason: string; after: string[]
  /** Nothing unfinished comes before his step: his turn by the plan. Whether a request is up for him is Needs you's. */
  ready: boolean
}
export type OwnerNext = { state: 'estimated' | 'unknown' | 'none'; reason: string; next?: OwnerNextCandidate; candidates: OwnerNextCandidate[] }
export type RuntimeProgress = {
  as_of_event_id?: number; generated_at: string; hosts: RuntimeHost[]; live: RuntimeAttempt[]
  /** Unset by a platform before owner_next. */
  owner_next?: OwnerNext
  schema: RuntimeSchema; controller_alerts: RuntimeLoopAlert[]
  /** The executors and the jobs no executor can take now (W-31 3.5); unset by a platform before it. */
  executors?: RuntimeExecutor[]; jobs_without_executor?: string[]
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
  const task: RuntimeTask = { task_id: text(r.task_id), title: text(r.title), role: text(r.role), state: text(r.state),
    status: text(r.status), reason: text(r.reason), since: text(r.since) || text(r.ended_at),
    round_no: number(r.round_no), max_rounds: number(r.max_rounds), depends_on: strings(r.depends_on) }
  if (r.lost_attempt !== null && typeof r.lost_attempt === 'object') task.lost_attempt = parseRuntimeAttempt(r.lost_attempt)
  if (typeof r.off_plan === 'boolean') task.off_plan = r.off_plan
  if (r.off_plan === true) task.off_plan_reason = text(r.off_plan_reason)
  if (typeof r.computed_not_persisted === 'boolean') task.computed_not_persisted = r.computed_not_persisted
  return task
}
export function parseRuntimeAttempt(v: unknown): RuntimeAttempt {
  const a = record(v)
  return { attempt_id: text(a.attempt_id), attempt_no: number(a.attempt_no), task_id: text(a.task_id),
    mission_id: text(a.mission_id), host_id: text(a.host_id), executor: text(a.executor), model: text(a.model),
    state: text(a.state), elapsed_s: number(a.elapsed_s), heartbeat_age_s: number(a.heartbeat_age_s),
    status: text(a.status), reason: text(a.reason), persisted: typeof a.persisted === 'boolean' ? a.persisted : undefined,
    last_checkpoint_seq: number(a.last_checkpoint_seq), last_checkpoint_at: text(a.last_checkpoint_at) || undefined,
    unpersisted_for_s: number(a.unpersisted_for_s), ended_at: text(a.ended_at) || undefined }
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
function parseOwnerNextCandidate(v: unknown): OwnerNextCandidate | null {
  const c = record(v), step = record(c.step)
  const earliest = text(c.earliest), latest = text(c.latest)
  // A time that does not read, or a range that runs backwards, is unknown, never a guess (v4 §20.3 rule 7; review 477 C1).
  const timed = c.state === 'estimated' && Number.isFinite(Date.parse(earliest)) && Number.isFinite(Date.parse(latest))
    && Date.parse(latest) >= Date.parse(earliest)
  if (!text(c.mission_id)) return null
  return { mission_id: text(c.mission_id), step: { plan_node_id: text(step.plan_node_id), title: text(step.title) },
    state: timed ? 'estimated' : 'unknown', ...(timed ? { earliest, latest } : {}),
    reason: text(c.reason) || (c.state === 'estimated' ? 'its time does not read' : ''), after: strings(c.after), ready: timed && c.ready === true }
}
export function parseOwnerNext(v: unknown): OwnerNext | undefined {
  if (v === undefined) return undefined
  const r = record(v)
  const candidates = records(r.candidates).map(parseOwnerNextCandidate).filter((c): c is OwnerNextCandidate => c !== null)
  if (r.state === 'none') return { state: 'none', reason: text(r.reason), candidates }
  if (r.state === 'estimated') {
    const next = parseOwnerNextCandidate({ ...r, state: 'estimated' })
    if (next?.state === 'estimated') return { state: 'estimated', reason: text(r.reason), next, candidates }
    return { state: 'unknown', reason: 'the runtime\'s answer could not be read', candidates }
  }
  return { state: 'unknown', reason: text(r.reason) || 'the runtime gave no reason', candidates }
}
export function parseRuntimeProgress(v: unknown): RuntimeProgress {
  const r = record(v), needs = record(r.needs_you), recent = record(r.recent)
  return {
    owner_next: parseOwnerNext(r.owner_next),
    as_of_event_id: number(r.as_of_event_id), generated_at: text(r.generated_at),
    hosts: records(r.hosts).map(parseRuntimeHost),
    ...(Array.isArray(r.executors) ? { executors: records(r.executors).map(e => ({ executor: text(e.executor),
      available: typeof e.available === 'boolean' ? e.available : null, why: text(e.why), hosts: strings(e.hosts), jobs: strings(e.jobs) })) } : {}),
    ...(Array.isArray(r.jobs_without_executor) ? { jobs_without_executor: strings(r.jobs_without_executor) } : {}),
    live: records(r.live).map(parseRuntimeAttempt),
    needs_you: { count: number(needs.count), items: Array.isArray(needs.items) ? needs.items : [],
      truncated: typeof needs.truncated === 'boolean' ? needs.truncated : undefined },
    work: { missions: records(record(r.work).missions).map(m => ({ mission_id: text(m.mission_id),
      title: text(m.title), goal: text(m.goal), state: text(m.state), status: text(m.status), reason: text(m.reason),
      since: text(m.since), deadline_at: text(m.deadline_at), eta: parseMissionEta(m.eta),
      next_steps: Array.isArray(m.next_steps) ? m.next_steps : [], waiting_on: text(m.waiting_on),
      waiting_roles: strings(m.waiting_roles), tasks: records(m.tasks).map(parseRuntimeTask) })) },
    recent: { tasks: records(recent.tasks), failed_attempts: records(recent.failed_attempts) },
    controls: records(r.controls), plan: Array.isArray(r.plan) ? r.plan : [], planRead: Array.isArray(r.plan),
    schema: parseRuntimeSchema(r.schema),
    controller_alerts: records(r.controller_alerts).map(a => ({ loop: text(a.loop), since: text(a.since), error: text(a.error) })),
  }
}
export function parseRuntimeHost(v: unknown): RuntimeHost {
  const h = record(v)
  const host: RuntimeHost = { host_id: text(h.host_id), state: text(h.state),
    online: typeof h.online === 'boolean' ? h.online : undefined, poll_age_s: number(h.poll_age_s),
    last_poll_at: text(h.last_poll_at), live_attempts: number(h.live_attempts) }
  if (h.offered === null) host.offered = null
  else if (Array.isArray(h.offered)) {
    host.offered = records(h.offered).map(o => ({ executor: text(o.executor), provider: text(o.provider), model: text(o.model) }))
  }
  if (text(h.offered_at)) host.offered_at = text(h.offered_at)
  if (typeof h.reported_offline === 'boolean') host.reported_offline = h.reported_offline
  if (typeof h.disk_low === 'boolean') host.disk_low = h.disk_low
  return host
}
const SCHEMA_STATES = ['current', 'behind', 'ahead', 'edited', 'unknown'] as const
export function parseRuntimeSchema(v: unknown): RuntimeSchema {
  const r = record(v)
  const known = (SCHEMA_STATES as readonly string[]).includes(text(r.state))
  return { state: v === undefined ? 'unread' : known ? text(r.state) as RuntimeSchema['state'] : 'unknown',
    missing: strings(r.missing), edited: strings(r.edited),
    unknown: Array.isArray(r.unknown) ? r.unknown.filter((n): n is number => typeof n === 'number') : [],
    reason: text(r.reason) }
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
  const r = await authedFetch('Agent artifact', `/api/v1/agentrt/artifacts/${pathSegment(id)}`, { signal })
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
