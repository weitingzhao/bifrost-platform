import { record, records, text, number, strings } from '@/domains/foundation/runtime-health/readValues'

export type RuntimeTask = {
  task_id: string; title: string; role: string; state: string; status: string; reason: string
  since: string; round_no?: number; max_rounds?: number; depends_on: string[]
  /** Its latest lost attempt that no later persisted result superseded (TD-352). */
  stopped?: { control_id: string; by: string; reason: string; since: string }
  withdrawn_by?: string; withdraw_reason?: string
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
export function parseRuntimeTask(v: unknown): RuntimeTask {
  const r = record(v)
  const task: RuntimeTask = { task_id: text(r.task_id), title: text(r.title), role: text(r.role), state: text(r.state),
    status: text(r.status), reason: text(r.reason), since: text(r.since) || text(r.ended_at),
    round_no: number(r.round_no), max_rounds: number(r.max_rounds), depends_on: strings(r.depends_on) }
  if (r.stopped !== null && typeof r.stopped === 'object' && !Array.isArray(r.stopped)) {
    const stopped = record(r.stopped)
    task.stopped = { control_id: text(stopped.control_id), by: text(stopped.by), reason: text(stopped.reason), since: text(stopped.since) }
  }
  if (typeof r.withdrawn_by === 'string') task.withdrawn_by = r.withdrawn_by
  if (typeof r.withdraw_reason === 'string') task.withdraw_reason = r.withdraw_reason
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
function parseMissionEta(v: unknown): MissionEta {
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
export function parseRuntimeMission(value: unknown): RuntimeMission {
  const m = record(value)
  return { mission_id: text(m.mission_id),
      title: text(m.title), goal: text(m.goal), state: text(m.state), status: text(m.status), reason: text(m.reason),
      since: text(m.since), deadline_at: text(m.deadline_at), eta: parseMissionEta(m.eta),
      next_steps: Array.isArray(m.next_steps) ? m.next_steps : [], waiting_on: text(m.waiting_on),
      waiting_roles: strings(m.waiting_roles), tasks: records(m.tasks).map(parseRuntimeTask) }
}
