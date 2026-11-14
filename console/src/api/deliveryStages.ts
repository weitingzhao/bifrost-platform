import { authedFetch, parseError } from './client'

/**
 * GET /api/v1/agentrt/delivery?commit= (store/delivery_read.go, store/deliverystages.go, store/delivery_external.go): the
 * seven delivery stages of a commit an external work handed in, each with when it was entered and left. Closed like planSnapshotSchema.ts: a key Go does not write, a stage or state
 * it does not name, or the stages out of Go's order refuse the whole answer. Optional means omitted by Go.
 */
type Parser<T> = (value: unknown, path: string) => T
function invalid(path: string): never { throw new Error(`Delivery: unsupported or incomplete response at ${path}`) }
const str: Parser<string> = (v, p) => typeof v === 'string' ? v : invalid(p)
const dict: Parser<Record<string, unknown>> = (v, p) => v !== null && typeof v === 'object' && !Array.isArray(v) ? v as Record<string, unknown> : invalid(p)
const optional = <T,>(read: Parser<T>): Parser<T | undefined> => (v, p) => v === undefined ? undefined : read(v, p)
const array = <T,>(read: Parser<T>): Parser<T[]> => (v, p) => Array.isArray(v) ? v.map((x, i) => read(x, `${p}[${i}]`)) : invalid(p)
const one = <T extends string>(...values: T[]): Parser<T> => (v, p) => values.includes(v as T) ? v as T : invalid(p)
const object = <S extends Record<string, Parser<unknown>>>(shape: S): Parser<{ [K in keyof S]: ReturnType<S[K]> }> => (v, p) => {
  const input = dict(v, p)
  if (Object.keys(input).some(k => !Object.hasOwn(shape, k))) invalid(p)
  return Object.fromEntries(Object.entries(shape).map(([k, read]) => [k, read(input[k], `${p}.${k}`)])) as { [K in keyof S]: ReturnType<S[K]> }
}
const timestamp: Parser<string> = (v, p) => { const s = str(v, p); return Number.isFinite(Date.parse(s)) ? s : invalid(p) }

/** store.DeliveryStageOrder: the seven stages, always all and in this order. */
export const DELIVERY_ORDER = ['handed_in', 'landing_ci', 'owner_wait', 'on_main', 'stg', 'prod_release', 'on_prod'] as const
const stage = object({
  stage: one(...DELIVERY_ORDER), state: one('done', 'current', 'not_reached', 'unknown'),
  entered_at: optional(timestamp), left_at: optional(timestamp), actor: optional(str), actor_role: optional(one('executor', 'approver', 'pusher', 'releaser')),
  waits_on: optional(str), reason: optional(str), source: optional(one('attempt', 'landing', 'grant', 'release_cycle', 'deploy_seen', 'stg_coverage', 'release_record', 'external_work', 'pipeline_seen')),
})
const delivery = object({
  schema: one('agentrt.delivery_response/v1'), commit: str, task_id: str, attempt_id: str, plan_node_id: str, repo: str, operation: str, external_work_id: optional(str),
  stages: array(stage), warnings: array(str), as_of: timestamp,
})
export type Delivery = ReturnType<typeof delivery>
export type DeliveryStage = Delivery['stages'][number]

export function parseDelivery(value: unknown): Delivery {
  const out = delivery(value, 'delivery')
  if (out.stages.length !== DELIVERY_ORDER.length || out.stages.some((s, i) => s.stage !== DELIVERY_ORDER[i])) invalid('delivery.stages')
  return out
}

/** The delivery of a commit as read: its answer, none (404: no external work names it), or Unknown and why. */
export type DeliveryRead = { state: 'ok'; data: Delivery } | { state: 'none' } | { state: 'unknown'; reason: string }

class NoChange extends Error {}
async function fetchDelivery(query: { commit: string }, signal?: AbortSignal): Promise<DeliveryRead> {
  try {
    const response = await authedFetch('Delivery', `/api/v1/agentrt/delivery?${new URLSearchParams(query)}`, { signal },
      async (prefix, r, token) => r.status === 404 ? new NoChange() : parseError(prefix, r, token))
    return { state: 'ok', data: parseDelivery(await response.json()) }
  } catch (error) {
    if (error instanceof NoChange) return { state: 'none' }
    throw error
  }
}
/** The delivery of the commit an external work handed in. */
export const fetchCommitDelivery = (commit: string, signal?: AbortSignal) => fetchDelivery({ commit }, signal)

/** One stage of a commit's way to PROD: reached at a time, not reached, or Unknown and why. */
export const COMMIT_STAGES = ['handed_in', 'ci', 'on_stg', 'on_prod'] as const
export type CommitStage = typeof COMMIT_STAGES[number]
export type StageReading = { stage: CommitStage } & ({ state: 'reached'; at: string } | { state: 'not_reached'; reason?: string } | { state: 'unknown'; reason: string })

// Which delivery stage says each of the four, and which of its times is when it was reached (ci: when it went green).
const FROM_DELIVERY: Record<CommitStage, { stage: DeliveryStage['stage']; at: 'entered_at' | 'left_at' }> = {
  handed_in: { stage: 'handed_in', at: 'entered_at' }, ci: { stage: 'landing_ci', at: 'left_at' },
  on_stg: { stage: 'stg', at: 'left_at' }, on_prod: { stage: 'on_prod', at: 'entered_at' },
}
/** One stage of an answer. */
function fromDelivery(read: DeliveryRead, stage: CommitStage): StageReading {
  if (read.state === 'none') return { stage, state: 'not_reached' }
  if (read.state === 'unknown') return { stage, state: 'unknown', reason: read.reason }
  const from = FROM_DELIVERY[stage]
  const s = read.data.stages.find(x => x.stage === from.stage)!
  if (s.state === 'unknown') return { stage, state: 'unknown', reason: s.reason || 'Unknown' }
  const at = s[from.at]
  // The answer names the commit, so it was handed in.
  const reached = s.state === 'done' || at !== undefined || (stage === 'handed_in' && s.state === 'current')
  // A stage stopped where it is (a failed run) says why.
  if (!reached) return s.reason ? { stage, state: 'not_reached', reason: s.reason } : { stage, state: 'not_reached' }
  return at === undefined ? { stage, state: 'unknown', reason: `${from.stage} is ${s.state} with no ${from.at}` } : { stage, state: 'reached', at }
}

/**
 * A commit's way to PROD as the delivery answer tells it: handed in, CI green, on STG, on PROD. When it reached main is
 * not shown: the merge happens in Gitea and the platform does not record it, so that stage was always Unknown.
 */
export function commitStages(read: DeliveryRead): StageReading[] {
  return COMMIT_STAGES.map(stage => fromDelivery(read, stage))
}
/** True once the answer says the commit is on PROD: nothing later changes it, so it need not be read again. */
export function deliverySettled(read: DeliveryRead | undefined): boolean {
  return read?.state === 'ok' && read.data.stages.at(-1)?.state === 'done'
}
