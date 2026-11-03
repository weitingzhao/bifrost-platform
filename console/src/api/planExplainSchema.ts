/**
 * The explain block of GET /plan-view?explain=1 (R5:5.5-19, DESIGN-r5-5.5-19-waiting-chain §7): for each step in
 * progress, what keeps it from done, as record identities only (never whether anyone moves it now: the page judges
 * that from its own snapshot). Its structure is checked closed; a state word the server may add (phase, outcome,
 * batch_state, task_state) is kept as it is and shown unrecognized, never a reason to refuse the block.
 */
type Parser<T> = (value: unknown, path: string) => T
function invalid(path: string): never { throw new Error(`Plan view explanation: unsupported or incomplete response at ${path}`) }
const str: Parser<string> = (v, p) => typeof v === 'string' ? v : invalid(p)
const num: Parser<number> = (v, p) => typeof v === 'number' && Number.isFinite(v) ? v : invalid(p)
const bool: Parser<boolean> = (v, p) => typeof v === 'boolean' ? v : invalid(p)
const dict: Parser<Record<string, unknown>> = (v, p) => v !== null && typeof v === 'object' && !Array.isArray(v) ? v as Record<string, unknown> : invalid(p)
const nullable = <T,>(read: Parser<T>): Parser<T | null> => (v, p) => v === null ? null : read(v, p)
const optional = <T,>(read: Parser<T>): Parser<T | undefined> => (v, p) => v === undefined ? undefined : read(v, p)
const array = <T,>(read: Parser<T>): Parser<T[]> => (v, p) => Array.isArray(v) ? v.map((x, i) => read(x, `${p}[${i}]`)) : invalid(p)
const one = <T extends string>(...values: T[]): Parser<T> => (v, p) => values.includes(v as T) ? v as T : invalid(p)
const timestamp: Parser<string> = (v, p) => { const s = str(v, p); return Number.isFinite(Date.parse(s)) ? s : invalid(p) }
const count: Parser<number> = (v, p) => { const n = num(v, p); return n >= 0 && Number.isInteger(n) ? n : invalid(p) }
const object = <S extends Record<string, Parser<unknown>>>(shape: S): Parser<{ [K in keyof S]: ReturnType<S[K]> }> => (v, p) => {
  const input = dict(v, p)
  if (Object.keys(input).some(k => !Object.hasOwn(shape, k))) invalid(p)
  return Object.fromEntries(Object.entries(shape).map(([k, read]) => [k, read(input[k], `${p}.${k}`)])) as { [K in keyof S]: ReturnType<S[K]> }
}

const card = object({ question_id: nullable(str), prep: str, kind: str, family: str, ask_started: bool, closed: bool, closed_why: nullable(str) })
export type ExplainCard = ReturnType<typeof card>
const cards = { cards: array(card), cards_omitted: optional(count) }
const block = object({ id: str, kind: str, since: timestamp, trigger_seq: count, ...cards })
export type ExplainBlock = ReturnType<typeof block>
const blocks = { blocks: array(block), blocks_omitted: optional(count) }
const rework = object({
  seen: nullable(object({ set_key: str, trigger_seq: count, current: bool })),
  not_asked: bool,
  card: nullable(object({ n: count, prep: str, question_id: nullable(str), ask_started: bool, closed: bool, closed_why: nullable(str),
    outcome: nullable(object({ outcome: str, why: str, seq: count })) })),
})
export type ExplainRework = ReturnType<typeof rework>
const member = { task_id: str, op: str }
const landed = { atom: one('step_landed') }
const stg = { atom: one('step_stg_settled') }

/** Each blocker state's closed shape (§7). */
const shapes = {
  tasks_short: object({ atom: one('step_tasks'), state: one('tasks_short'), have: count, need: count,
    candidates: array(object({ task_id: str, task_state: str })), omitted: optional(count) }),
  no_task: object({ atom: one('step_tasks'), state: one('no_task') }),
  no_implementation: object({ ...landed, state: one('no_implementation') }),
  no_landing_evidence: object({ ...landed, state: one('no_landing_evidence'), task_id: str }),
  landing_unclaimed: object({ ...landed, state: one('landing_unclaimed'), task_id: str, accepted_at: nullable(timestamp) }),
  landing_pending: object({ ...landed, state: one('landing_pending'), ...member, phase: str, last_record_at: nullable(timestamp),
    grant: nullable(object({ approval_id: str, number: count, last_status: str, recorded_at: timestamp })), ...cards }),
  landing_main_unsettled: object({ ...landed, state: one('landing_main_unsettled'), ...member, phase: str, last_record_at: nullable(timestamp),
    grant: nullable(object({ approval_id: str, number: count, last_status: str, recorded_at: timestamp })), ...cards }),
  landing_failed: object({ ...landed, state: one('landing_failed'), ...member, outcome: str, ...cards }),
  landing_declined: object({ ...landed, state: one('landing_declined'), ...member, rework }),
  landing_unmapped: object({ ...landed, state: one('landing_unmapped'), ...member, phase: str, outcome: str }),
  stg_needs_landing: object({ ...stg, state: one('stg_needs_landing') }),
  stg_not_supported: object({ ...stg, state: one('stg_not_supported'), repo: str }),
  stg_withheld: object({ ...stg, state: one('stg_withheld'), ...member, kind: str, ...cards, ...blocks }),
  stg_batch_open: object({ ...stg, state: one('stg_batch_open'), ...member, batch_id: str, batch_state: str,
    try: nullable(object({ n: count, snapshot_at: timestamp, started_at: nullable(timestamp), bound_uid: nullable(str), run: nullable(str) })),
    quarantine: nullable(object({ reason: str, trigger_seq: count, ...cards })), ...blocks }),
  stg_not_admitted: object({ ...stg, state: one('stg_not_admitted'), ...member, to: nullable(str), ...blocks,
    repo_blocks: array(block), repo_blocks_omitted: optional(count), unexplained: array(one('budget', 'policy', 'repo_cards')) }),
  stg_exhausted: object({ ...stg, state: one('stg_exhausted'), ...member, ...cards, ...blocks }),
  stg_disposed: object({ ...stg, state: one('stg_disposed'), ...member, ...blocks }),
}
type Shapes = typeof shapes
export type AtomBlocker = { [K in keyof Shapes]: ReturnType<Shapes[K]> }[keyof Shapes]
export type OneOf = { atom: 'one_of'; branches: ExplainBlocker[][] }
export type ExplainBlocker = AtomBlocker | OneOf

const blocker: Parser<ExplainBlocker> = (v, p) => {
  const input = dict(v, p)
  if (input.atom === 'one_of') {
    if (Object.keys(input).some(k => k !== 'atom' && k !== 'branches')) invalid(p)
    return { atom: 'one_of', branches: array(array(blocker))(input.branches, `${p}.branches`) }
  }
  const state = str(input.state, `${p}.state`)
  if (!Object.hasOwn(shapes, state)) invalid(`${p}.state`)
  return shapes[state as keyof Shapes](v, p)
}

const version = { node_version: count }
const step: Parser<StepExplain> = (v, p) => {
  const kind = str(dict(v, p).kind, `${p}.kind`)
  switch (kind) {
    case 'explained': return object({ ...version, kind: one('explained'), rule: one('true', 'false', 'unknown'), blockers: array(blocker), omitted: optional(count) })(v, p)
    case 'no_done_rule': case 'not_started': return object({ ...version, kind: one('no_done_rule', 'not_started') })(v, p)
    case 'rule_not_supported': return object({ ...version, kind: one('rule_not_supported'), atoms: array(str) })(v, p)
    case 'not_explained': return object({ ...version, kind: one('not_explained'), reason: one('over_cap') })(v, p)
    case 'incomplete': return object({ ...version, kind: one('incomplete'), reason: one('input_too_large', 'timeout'), which: optional(str) })(v, p)
    case 'explain_error': return object({ ...version, kind: one('explain_error'), code: str })(v, p)
  }
  return invalid(`${p}.kind`)
}
export type StepExplain = { node_version: number } & (
  | { kind: 'explained'; rule: 'true' | 'false' | 'unknown'; blockers: ExplainBlocker[]; omitted?: number }
  | { kind: 'no_done_rule' | 'not_started' }
  | { kind: 'rule_not_supported'; atoms: string[] }
  | { kind: 'not_explained'; reason: 'over_cap' }
  | { kind: 'incomplete'; reason: 'input_too_large' | 'timeout'; which?: string }
  | { kind: 'explain_error'; code: string }
)

const envelope = object({
  status: one('ok', 'pending', 'failed'), reason: optional(one('input_too_large', 'timeout', 'error')), read_at: optional(timestamp),
  last_attempt_at: optional(timestamp), last_error: optional(str),
  steps: (v: unknown, p: string) => Object.fromEntries(Object.entries(dict(v, p)).map(([k, s]) => [k, step(s, `${p}.${k}`)])),
})
export type PlanExplain = ReturnType<typeof envelope>

/** The explain block as read: the block, or why it could not be read (it affects this block alone). */
export type ExplainRead = { ok: true; explain: PlanExplain } | { ok: false; error: string }

export function parsePlanExplain(value: unknown): ExplainRead {
  try {
    const out = envelope(value, 'explain')
    if (out.status === 'ok' && !out.read_at) invalid('explain.read_at')
    if (out.status === 'failed' && !out.reason) invalid('explain.reason')
    return { ok: true, explain: out }
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : String(e) }
  }
}
