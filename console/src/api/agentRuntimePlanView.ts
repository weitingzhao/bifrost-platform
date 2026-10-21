/**
 * The plan as the board shows it, built from a GET /agentrt/plan?root= answer: the TS port of
 * planimport.FromPlanWithRequest, ImportHead and RequestOf (api/internal/agentrt/planimport/view.go,
 * request.go). A test holds it to the Go functions' outputs on the same inputs
 * (__fixtures__/agentrt-plan-view.go.json, written by the Go view from the handlers' answers).
 *
 * It equals Go on the handlers' canonical JSON only (review 416 C1): Go's encoding/json also matches
 * keys case-insensitively and reads a seq as an exact int64, neither of which the handlers' answers
 * call on. Here keys are matched exactly, and a seq that is not a safe integer is refused rather than
 * read inexactly.
 */
export type PlanViewStep = { id: string; t: string; state: string; owner: boolean; star: boolean; when: string }
export type PlanViewItem = { key: string; name: string; steps: PlanViewStep[] }
export type PlanView = { items: PlanViewItem[] }

/** The part of an import request the view reads: each node's ledger line and the date it was marked. */
export type PlanRequestNode = { id: string; evidence: { marked_at: string; line: string } | null }
export type PlanRequest = { root: string; nodes: PlanRequestNode[] }

/** The board's word for a runtime state (planimport.BoardState). */
export function boardState(state: string): string {
  switch (state) {
    case 'done': return 'done'
    case 'active': return 'doing'
    case 'planned': return 'todo'
  }
  return state
}

/** The manual atom an imported done step's rule names (planimport.ManualAtom). */
export const manualAtom = (stepId: string) => `imported from the work ledger: ${stepId}`

const SOURCE_PREFIX = 'plan-import '
const SHA256_ID = /^sha256:[0-9a-f]{64}$/

/** The request artifact a legacy_imported source names, or null (planimport.RequestOf). */
export function requestOf(source: string): string | null {
  if (!source.startsWith(SOURCE_PREFIX)) return null
  const id = source.slice(SOURCE_PREFIX.length)
  return SHA256_ID.test(id) ? id : null
}

type Json = Record<string, unknown>
const isObject = (v: unknown): v is Json => v !== null && typeof v === 'object' && !Array.isArray(v)

/** Reads values as Go's encoding/json does into the view's types: null or absent is the zero value, another type fails. */
function decoder(prefix: string) {
  const fail = (at: string, type: string): never => { throw new Error(`${prefix}: ${at} is not ${type}`) }
  return {
    str: (v: unknown, at: string): string => v == null ? '' : typeof v === 'string' ? v : fail(at, 'a string'),
    ptr: (v: unknown, at: string): string | null => v == null ? null : typeof v === 'string' ? v : fail(at, 'a string'),
    bool: (v: unknown, at: string): boolean => v == null ? false : typeof v === 'boolean' ? v : fail(at, 'a boolean'),
    int: (v: unknown, at: string): number => v == null ? 0 : typeof v === 'number' && Number.isSafeInteger(v) ? v : fail(at, 'a safe integer'),
    obj: (v: unknown, at: string): Json | null => v == null ? null : isObject(v) ? v : fail(at, 'an object'),
    list: (v: unknown, at: string): unknown[] => v == null ? [] : Array.isArray(v) ? v : fail(at, 'a list'),
  }
}

type PlanNode = {
  id: string; parent: string | null; kind: string; label: string; title: string; state: string | null
  owner: boolean; effect: boolean; rule: unknown; event: { type: string; at: string; cause: string } | null
}

function readPlan(doc: unknown): { root: string; nodes: PlanNode[] } {
  // Go decodes the whole answer before it checks anything: one value of the wrong type anywhere fails it.
  const d = decoder('the plan does not read')
  const a = d.obj(doc, 'the answer') ?? {}
  const nodes = d.list(a.nodes, 'nodes').map((value, i): PlanNode => {
    const n = d.obj(value, `nodes[${i}]`) ?? {}
    const e = d.obj(n.state_event, `nodes[${i}].state_event`)
    const body = e === null ? null : d.obj(e.body, `nodes[${i}].state_event.body`) ?? {}
    return {
      id: d.str(n.plan_node_id, `nodes[${i}].plan_node_id`), parent: d.ptr(n.parent_plan_node_id, `nodes[${i}].parent_plan_node_id`),
      kind: d.str(n.node_kind, `nodes[${i}].node_kind`), label: d.str(n.label, `nodes[${i}].label`), title: d.str(n.title, `nodes[${i}].title`),
      state: d.ptr(n.state, `nodes[${i}].state`), owner: d.bool(n.owner_step, `nodes[${i}].owner_step`), effect: d.bool(n.effect_step, `nodes[${i}].effect_step`),
      rule: n.done_rule_json,
      event: e === null ? null : { type: d.str(e.event_type, `nodes[${i}].state_event.event_type`), at: d.str(e.at, `nodes[${i}].state_event.at`),
        cause: d.str(body?.cause, `nodes[${i}].state_event.body.cause`) },
    }
  })
  return { root: d.str(a.root, 'root'), nodes }
}

const quote = (s: string) => JSON.stringify(s)

/**
 * The view of a GET /plan?root= answer for the round root, each step with what the board shows
 * beside it: for a step whose state is still the one the import gave it (done by its imported manual
 * rule, active with cause legacy_imported, or planned since it was created), the date its ledger line
 * was marked, from the import request; for any other step the time of its state event. The answer
 * must be one round: the root once, each node's id <root>:<label>, no id twice, every item under the
 * root and every step under an item before it. Throws with the Go function's reasons.
 */
export function fromPlanWithRequest(doc: unknown, root: string, req: PlanRequest | null, ledgerUnread = false): PlanView {
  const a = readPlan(doc)
  if (a.root !== root) throw new Error(`the plan is of root ${quote(a.root)}, not ${quote(root)}`)
  const marked = new Map<string, string>()
  for (const n of req?.nodes ?? []) if (n.evidence !== null) marked.set(n.id, n.evidence.marked_at)
  const view: PlanView = { items: [] }
  const at = new Map<string, number>()
  const seen = new Set<string>()
  let roots = 0
  for (const n of a.nodes) {
    const parent = n.parent ?? ''
    if (seen.has(n.id)) throw new Error(`node ${n.id} is listed twice`)
    seen.add(n.id)
    if (n.kind !== 'round' && n.id !== `${root}:${n.label}`) throw new Error(`node ${n.id} is not ${root}:${n.label}`)
    switch (n.kind) {
      case 'round':
        if (n.id !== root || parent !== '') throw new Error(`round ${n.id} inside the plan of ${root}`)
        roots++
        break
      case 'item':
        if (parent !== root) throw new Error(`item ${n.id} sits under ${quote(parent)}, not the root`)
        at.set(n.id, view.items.length)
        view.items.push({ key: n.label, name: n.title, steps: [] })
        break
      case 'step': {
        const i = at.get(parent)
        if (i === undefined || n.state === null) throw new Error(`step ${n.id} sits under ${quote(parent)}, which is not an item listed before it`)
        view.items[i].steps.push({ id: n.label, t: n.title, state: boardState(n.state), owner: n.owner, star: n.effect, when: when(n, n.state, marked, ledgerUnread) })
        break
      }
      default:
        throw new Error(`node ${n.id} is a ${n.kind}, which a round does not hold`)
    }
  }
  if (roots !== 1) throw new Error(`the plan holds its root ${roots} times`)
  return view
}

/**
 * A step's date: the ledger's mark for a step the import brought in, else its state event's time. While the import
 * request cannot be read (ledgerUnread), an imported step's date is not known: the import's own moment is not when
 * the work was done, so none is told rather than that one (review 416/418's concern, kept when the plan is shown).
 */
function when(n: PlanNode, state: string, marked: Map<string, string>, ledgerUnread = false): string {
  if (n.event === null) return ''
  const m = marked.get(n.id)
  if (m !== undefined || ledgerUnread) {
    // Go reads the rule into a map of strings: every key counts, a value that is not a string reads as "".
    const rule = isObject(n.rule) ? n.rule : {}
    let imported = false
    switch (state) {
      case 'done': imported = Object.keys(rule).length === 1 && rule.manual === manualAtom(n.id) && n.event.cause === 'done_rule_manual'; break
      case 'active': imported = n.event.cause === 'legacy_imported'; break
      case 'planned': imported = n.event.type === 'plan_node_created'; break
    }
    if (imported) return m ?? ''
  }
  return n.event.at
}

/**
 * One page of GET /events?stream=plan:<root> (events in seq order) read for the plan's import head:
 * the first legacy_imported event whose source names a request artifact. Not on this page: next is
 * the page's last seq to read after, and 0 for an empty page, which means the plan was never
 * imported (planimport.ImportHead).
 */
export function importHead(page: unknown): { artifact: string; next: number } {
  // As in Go, the whole page is read before any event is looked at.
  const d = decoder('the events do not read')
  const events = d.list(page, 'the page').map((value, i) => {
    const e = d.obj(value, `events[${i}]`) ?? {}
    return { seq: d.int(e.seq, `events[${i}].seq`), type: d.str(e.event_type, `events[${i}].event_type`),
      source: d.str((d.obj(e.body_json, `events[${i}].body_json`) ?? {}).source, `events[${i}].body_json.source`) }
  })
  let next = 0
  for (const e of events) {
    const id = requestOf(e.source)
    if (id !== null && e.type === 'legacy_imported') return { artifact: id, next: 0 }
    next = e.seq
  }
  return { artifact: '', next }
}
