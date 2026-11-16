import { parseRuntimeAttempt, parseRuntimeMission, parseOwnerNext } from '@/domains/plan/readings/runtimeWork'
import type { RuntimeAttempt, RuntimeMission, OwnerNext } from '@/domains/plan/readings/runtimeWork'
export { parseRuntimeTask } from '@/domains/plan/readings/runtimeWork'
export type { RuntimeTask, RuntimeAttempt, RuntimeMission, MissionEta, EtaBasis, OwnerNextCandidate, OwnerNext } from '@/domains/plan/readings/runtimeWork'
import { authedFetch } from '@/api/client'

export const AGENT_RUNTIME_POLL_MS = 5_000
import { record, records, text, number, strings } from '@/domains/foundation/runtime-health/readValues'
export { record, records, text } from '@/domains/foundation/runtime-health/readValues'
export type { RuntimeRecord } from '@/domains/foundation/runtime-health/readValues'
import type { RuntimeRecord } from '@/domains/foundation/runtime-health/readValues'

/** A tool a host's latest claim offered (W-31 3.5; store/presence.go). */
export type RuntimeOffer = { executor: string; provider: string; model: string }
export type RuntimeHost = {
  host_id: string; state: string; online?: boolean; poll_age_s?: number; last_poll_at: string; live_attempts?: number
  /**
   * What its latest claim offered, null while none is recorded, and whether the controller reports it offline (not online,
   * active, no stop or drain) or its disk low (W-31 3.5). Unset by a platform before 3.5.
   */
  offered?: RuntimeOffer[] | null; offered_at?: string; reported_offline?: boolean; disk_low?: boolean
  /** Deleting the attempt branches it was handed: silent then for up to agentd's prune budget (review 526 A1). */
  pruning?: boolean
  /**
   * Whether it takes work: its claims recent (true), stopped though it may still be online through its attempts'
   * heartbeats (false), or not known yet (null; review 527 A3).
   */
  claiming?: boolean | null
  /**
   * Elastic slots (R5 phase 2 step 3b; store/hostslots.go): the count the platform's slots pass recorded for it, null while
   * none is (the concurrency rule's per_host, or its slots block's start, then holds), and its latest moves, newest first.
   * Unset by a platform before them.
   */
  effective_slots?: number | null; slot_moves?: RuntimeSlotMove[]
}
/** A move of a host's slots: when, by which policy, from and to how many, and why. */
export type RuntimeSlotMove = { at: string; policy: string; old: number; new: number; reason: string }
/**
 * An executor some job's candidate names (W-31 3.5; store/presence.go): available, or not with why, or not known (null)
 * while a host that could take work has not said what it offers; the hosts that offer it and its jobs.
 */
export type RuntimeExecutor = { executor: string; available: boolean | null; why: string; hosts: string[]; jobs: string[] }
/**
 * The database's agentrt schema against the migrations this platform carries (TD-411; migrate.Check):
 * missing, edited and unknown name the migrations. Unread is a platform before TD-411.
 */
import { parseRuntimeSchema, parseRuntimeLoopAlerts } from '@/domains/foundation/runtime-health/health'
import type { RuntimeSchema, RuntimeLoopAlert } from '@/domains/foundation/runtime-health/health'
export type { RuntimeSchema, RuntimeLoopAlert } from '@/domains/foundation/runtime-health/health'
export type RuntimeProgress = {
  as_of_event_id?: number; generated_at: string; hosts: RuntimeHost[]; live: RuntimeAttempt[]
  /** Unset by a platform before owner_next. */
  owner_next?: OwnerNext
  schema: RuntimeSchema; controller_alerts: RuntimeLoopAlert[]
  /** The executors and the jobs no executor can take now (W-31 3.5); unset by a platform before it. */
  executors?: RuntimeExecutor[]; jobs_without_executor?: string[]
  /** Since when, and why, host presence cannot be judged (the platform not seen taking agentd's calls); null while it can. */
  presence_unjudged?: { since: string; why: string } | null
  /** Since when, and why, hosts silent past two minutes are not judged: the platform's recoveries keep moving their floor. */
  presence_deferred?: { since: string; why: string } | null
  needs_you: { count?: number; items: unknown[]; truncated?: boolean }
  work: { missions: RuntimeMission[] }; recent: { tasks: RuntimeRecord[]; failed_attempts: RuntimeRecord[] }
  controls: RuntimeRecord[]; plan: unknown[]
  /** The answer had a plan list: an empty plan is only one that was read empty (review 431 B1). */
  planRead?: boolean
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
    ...presenceSwitch('presence_unjudged', r.presence_unjudged),
    ...presenceSwitch('presence_deferred', r.presence_deferred),
    live: records(r.live).map(parseRuntimeAttempt),
    needs_you: { count: number(needs.count), items: Array.isArray(needs.items) ? needs.items : [],
      truncated: typeof needs.truncated === 'boolean' ? needs.truncated : undefined },
    work: { missions: records(record(r.work).missions).map(parseRuntimeMission) },
    recent: { tasks: records(recent.tasks), failed_attempts: records(recent.failed_attempts) },
    controls: records(r.controls), plan: Array.isArray(r.plan) ? r.plan : [], planRead: Array.isArray(r.plan),
    schema: parseRuntimeSchema(r.schema),
    controller_alerts: parseRuntimeLoopAlerts(r.controller_alerts),
  }
}
/** A presence switch of /progress: on with since and why, null while off, absent from a platform before it. */
function presenceSwitch<K extends 'presence_unjudged' | 'presence_deferred'>(key: K, v: unknown): Partial<Record<K, { since: string; why: string } | null>> {
  if (v === undefined) return {}
  if (v === null) return { [key]: null } as Partial<Record<K, null>>
  return { [key]: { since: text(record(v).since), why: text(record(v).why) } } as Partial<Record<K, { since: string; why: string }>>
}
function parseRuntimeHost(v: unknown): RuntimeHost {
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
  if (typeof h.pruning === 'boolean') host.pruning = h.pruning
  if (typeof h.claiming === 'boolean' || h.claiming === null) host.claiming = h.claiming
  if (h.effective_slots === null || number(h.effective_slots) !== undefined) host.effective_slots = number(h.effective_slots) ?? null
  if (Array.isArray(h.slot_moves)) {
    host.slot_moves = records(h.slot_moves).map(m => ({ at: text(m.at), policy: text(m.policy), old: number(m.old) ?? 0,
      new: number(m.new) ?? 0, reason: text(m.reason) }))
  }
  return host
}
export async function fetchRuntimeProgress(signal?: AbortSignal): Promise<RuntimeProgress> {
  const r = await authedFetch('Agent runtime', '/api/v1/agentrt/progress', { signal })
  return parseRuntimeProgress(await r.json())
}
