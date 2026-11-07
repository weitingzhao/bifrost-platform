import { authedFetch } from './client'

/**
 * GET /api/v1/agentrt/plan-timeline (store/steptimeline_read.go): a round's steps, or one step of it, with their moves
 * in time and their cards. Closed like planSnapshotSchema.ts: a key Go does not write, a kind it does not name or a
 * missing field refuses the whole answer. Optional means omitted by Go (omitempty, omitzero), never a default zero.
 */
type Parser<T> = (value: unknown, path: string) => T
function invalid(path: string): never { throw new Error(`Plan timeline: unsupported or incomplete response at ${path}`) }
const str: Parser<string> = (v, p) => typeof v === 'string' ? v : invalid(p)
const num: Parser<number> = (v, p) => typeof v === 'number' && Number.isFinite(v) ? v : invalid(p)
const dict: Parser<Record<string, unknown>> = (v, p) => v !== null && typeof v === 'object' && !Array.isArray(v) ? v as Record<string, unknown> : invalid(p)
const optional = <T,>(read: Parser<T>): Parser<T | undefined> => (v, p) => v === undefined ? undefined : read(v, p)
const array = <T,>(read: Parser<T>): Parser<T[]> => (v, p) => Array.isArray(v) ? v.map((x, i) => read(x, `${p}[${i}]`)) : invalid(p)
const one = <T extends string>(...values: T[]): Parser<T> => (v, p) => values.includes(v as T) ? v as T : invalid(p)
const object = <S extends Record<string, Parser<unknown>>>(shape: S): Parser<{ [K in keyof S]: ReturnType<S[K]> }> => (v, p) => {
  const input = dict(v, p)
  if (Object.keys(input).some(k => !Object.hasOwn(shape, k))) invalid(p)
  return Object.fromEntries(Object.entries(shape).map(([k, read]) => [k, read(input[k], `${p}.${k}`)])) as { [K in keyof S]: ReturnType<S[K]> }
}
const nonnegative: Parser<number> = (v, p) => { const n = num(v, p); return n >= 0 && Number.isInteger(n) ? n : invalid(p) }
const timestamp: Parser<string> = (v, p) => { const s = str(v, p); return Number.isFinite(Date.parse(s)) ? s : invalid(p) }

// StepEvent (store/steptimeline.go): every field always written.
const event = object({
  kind: one('created', 'active', 'done_by_rule', 'task_leased', 'coordinator_work', 'submitted', 'accepted', 'landed', 'on_stg', 'on_prod', 'external_work'),
  at: timestamp, reason: str, source: one('agentrt.event', 'note.landing', 'deploy_seen', 'external_work', 'coordinator_work'),
  event_id: nonnegative, plan_node_id: str, task_id: str, attempt_id: str, actor: str, waited_on: str, wait_s: nonnegative,
})
// StepCard and CardAttempt (store/stepcards.go).
const card = object({
  kind: one('approval', 'question', 'signoff', 'landing_card', 'stg_card'), ref: str, plan_node_id: str,
  via: optional(one('binding', 'task', 'landing_op', 'signoff')), task_id: optional(str),
  attempts: array(object({
    n: nonnegative, question_id: optional(str), opened_at: optional(timestamp), closed_at: optional(timestamp),
    result: optional(object({ status: optional(str), option: optional(str), choice: optional(str), answered_by: optional(str), answered_at: optional(str) })),
    void_reason: optional(str), event_id: optional(nonnegative),
  })),
})
const step = object({
  plan_node_id: str, label: str, title: str, state: one('', 'planned', 'active', 'done', 'stopped', 'dropped'),
  node_kind: one('round', 'item', 'step', 'arch_root', 'arch_row'), events: array(event), cards: array(card),
})
const timeline = object({
  schema: one('agentrt.plan_timeline_response/v1'), root: str, as_of: timestamp, steps: array(step), unbound_cards: array(card), warnings: array(str),
})
export type PlanTimeline = ReturnType<typeof timeline>
export type TimelineStep = PlanTimeline['steps'][number]
export type TimelineEvent = TimelineStep['events'][number]
export type TimelineCard = TimelineStep['cards'][number]

/** Reads an answer of root (or of its one step node); one of another round, or a step it was not asked for, is refused. */
export function parsePlanTimeline(value: unknown, root: string, node = ''): PlanTimeline {
  const out = timeline(value, 'timeline')
  if (out.root !== root) invalid('root')
  if (node !== '' && (out.steps.length !== 1 || out.steps[0]!.plan_node_id !== node)) invalid('steps')
  if (new Set(out.steps.map(s => s.plan_node_id)).size !== out.steps.length) invalid('steps')
  return out
}

export async function fetchPlanTimeline(root: string, node = '', signal?: AbortSignal): Promise<PlanTimeline> {
  const query = new URLSearchParams({ root })
  if (node) query.set('node', node)
  const response = await authedFetch('Plan timeline', `/api/v1/agentrt/plan-timeline?${query}`, { signal })
  return parsePlanTimeline(await response.json(), root, node)
}
