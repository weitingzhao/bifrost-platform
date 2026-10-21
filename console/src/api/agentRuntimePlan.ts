import { authedFetch, parseError } from '@/api/client'
import { serverFetch } from '@/api/serverContact'
import { record, records, text } from '@/api/agentRuntime'
import { boardState, importHead, type PlanRequest, type PlanView } from '@/api/agentRuntimePlanView'

/**
 * A step as the plan panes show it. line is the step's ledger line as the import request kept it; owner
 * and star are the step's Owner (owner_step) and ★ (effect_step) marks. All three are read from the
 * runtime only: the board's round carries none, and a step added after the import has no line.
 */
export type RuntimePlanStep = {
  id: string; t: string; state: string; when: string; line?: string; owner?: boolean; star?: boolean
  /** The approvals its done rule waits on (approval_executed), by number: the request the step is done by. */
  approvals?: number[]
  /** Its done rule's owner_signoff atoms and the sign-offs it holds: what the Owner signs off on the plan page. */
  signoffs?: StepSignoffs
}
/**
 * An item with its count (store/progress.go, the one counting rule): done of total, and of the done ones how many by a
 * manual record (v5 §5.1.2: Progress marks them "manual"); stopped steps count as not done.
 */
export type RuntimePlanItem = { key: string; name: string; state: string; done?: number; total?: number; manual?: number; stopped?: number; steps: RuntimePlanStep[] }
/**
 * A round, as the board publishes it or as read from the runtime. source 'runtime' means it came from
 * the agent runtime (GET /plan); unknown is the reason the board could not read it, with no items
 * (CUT-plan-import §6): a round that could not be read is never shown as one with no items.
 */
/** Of the done steps, those done by a manual record (v5 §5.1.2: Progress marks them "manual"), and the stopped ones; '' for none. */
export function manualAndStopped(item: { manual?: number; stopped?: number }): string {
  return `${item.manual ? ` · ${item.manual} manual` : ''}${item.stopped ? ` · ${item.stopped} stopped` : ''}`
}
export type RuntimeRound = { n?: number; checked?: number; items: RuntimePlanItem[]; source?: string; unknown?: string }
export type RuntimeArchitecture = { title: string; checked?: number; rows: { state: string; what: string; now: string }[] }
const num = (v: unknown) => typeof v === 'number' && Number.isFinite(v) ? v : undefined
export function parseRuntimeRound(value: unknown): RuntimeRound | undefined {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return undefined
  const r = record(value)
  if (typeof r.unknown === 'string') return { unknown: r.unknown || 'the board gave no reason', items: [], source: text(r.source) || undefined }
  return { n: num(r.n), checked: num(r.checked), source: text(r.source) || undefined, items: records(r.items).map(i => ({
    key: text(i.key), name: text(i.name), state: text(i.state), done: num(i.done), total: num(i.total),
    steps: records(i.steps).map(s => ({ id: text(s.id), t: text(s.t), state: text(s.state), when: text(s.when) })),
  })) }
}
export function parseRuntimeArchitecture(value: unknown): RuntimeArchitecture | undefined {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return undefined
  const r = record(value)
  return { title: text(r.title), checked: num(r.checked), rows: records(r.rows).map(row => ({
    state: text(row.state), what: text(row.what), now: text(row.now),
  })) }
}

/**
 * How an architecture row stands: its done rule (gates_passed) evaluated now against what is deployed; nogate when
 * the row has no gate yet (TD-397: not unknown, nothing is unread).
 */
export type ArchTick = 'true' | 'false' | 'nogate' | 'unknown'
/** An architecture row as GET /progress gives it (W-31 2.5 G2): the tick, why, and its latest note, what it actually is now. */
export type RuntimeArchRow = { id: string; title: string; tick: ArchTick; reason: string; note: { text: string; at: string; by: string } | null }
export type RuntimeArchTable = { id: string; title: string; rows: RuntimeArchRow[] }

/** The reason of a row with no done rule (store.ReasonNoGate). */
export const REASON_NO_GATE = 'no gate test yet'

/** The architecture root (planimport.ArchRootID). */
export const ARCH_ROOT = 'ARCH'

/**
 * The architecture table from GET /progress's plan: the arch_root with its rows, each ticked by its done rule
 * evaluated now (store/progressarch.go); null when the runtime holds no architecture root (not imported). A
 * tick the page does not know is unknown, never a pass; a root without its rows list is not a table, and
 * throws (review 431 B1).
 */
export function runtimeArch(plan: unknown[]): RuntimeArchTable | null {
  const roots = plan.map(record).filter(r => text(r.kind) === 'arch_root')
  const root = roots.find(r => text(r.plan_node_id) === ARCH_ROOT) ?? roots[0]
  if (root == null) return null
  if (!Array.isArray(root.rows)) throw new Error('Agent runtime: the architecture root has no rows list')
  return { id: text(root.plan_node_id), title: text(root.title), rows: records(root.rows).map(row => {
    const tick = text(row.tick)
    const note = row.note === null || typeof row.note !== 'object' ? null : record(row.note)
    const reason = text(row.reason)
    const known: ArchTick = tick === 'true' || tick === 'false' ? tick : 'unknown'
    return { id: text(row.plan_node_id), title: text(row.title), tick: known === 'unknown' && reason === REASON_NO_GATE ? 'nogate' : known, reason,
      note: note && text(note.text) ? { text: text(note.text), at: text(note.at), by: text(note.by) } : null }
  }) }
}

/** An architecture table's rows by tick. */
export function archCounts(table: RuntimeArchTable): { passed: number; notYet: number; noGate: number; unknown: number } {
  const n = (tick: ArchTick) => table.rows.filter(r => r.tick === tick).length
  return { passed: n('true'), notYet: n('false'), noGate: n('nogate'), unknown: n('unknown') }
}

/** The round the plan panes read from the runtime (CUT-round2-2.5-interface §2: GET /plan?root=R2). */
export const PLAN_ROOT = 'R2'
export const PLAN_IMPORT_FORMAT = 'agentrt.plan_import/v1'

/** GET /plan?root=: the answer, or not found when the runtime holds no such root (the plan is not imported). */
export type PlanAnswer = { found: false } | { found: true; doc: unknown }
class PlanNotFound extends Error {}
async function planError(prefix: string, response: Response, token = ''): Promise<Error> {
  // Only the route's own answer for a root it does not hold; a 404 of a platform without the route is an error.
  if (response.status === 404 && record(await response.clone().json().catch(() => ({}))).error === 'not_found') return new PlanNotFound()
  return parseError(prefix, response, token)
}
export async function fetchRuntimePlan(root = PLAN_ROOT, signal?: AbortSignal): Promise<PlanAnswer> {
  try {
    const r = await authedFetch('Plan', `/api/v1/agentrt/plan?${new URLSearchParams({ root })}`, { signal }, planError)
    return { found: true, doc: await r.json() }
  } catch (e) {
    if (e instanceof PlanNotFound) return { found: false }
    throw e
  }
}

const invalidRequest = (): never => { throw new Error('Plan import request: invalid artifact.') }
/** The import request as GET /artifacts/{id} answers it: of this root, each node with its ledger line; anything else does not read. */
export function parsePlanRequest(value: unknown, root: string): PlanRequest {
  const r = record(value)
  if (r.format !== PLAN_IMPORT_FORMAT || record(r.root).id !== root || !Array.isArray(r.nodes)) return invalidRequest()
  return { root, nodes: r.nodes.map(v => {
    const n = record(v)
    if (typeof n.id !== 'string' || n.id === '') return invalidRequest()
    if (n.evidence == null) return { id: n.id, evidence: null }
    const e = record(n.evidence)
    if (e !== n.evidence || (e.marked_at != null && typeof e.marked_at !== 'string') || typeof e.line !== 'string') return invalidRequest()
    return { id: n.id, evidence: { marked_at: text(e.marked_at), line: e.line } }
  }) }
}

/** The events GET /events?stream= answers a page with: its fixed LIMIT (agentrtapi getEvents). */
export const PLAN_STREAM_PAGE = 500
/** How many full pages of the plan's stream are read for its import head. */
export const IMPORT_HEAD_PAGES = 20
/**
 * The import request the plan's import head names (CUT-plan-import §6): the plan's stream read from the
 * start, page by page, for its first legacy_imported event that names a request, and that request read
 * once. null when the stream has no such event: the plan was not imported, and each step shows the time
 * of its state event. The stream ends at a page short of PLAN_STREAM_PAGE (review 418). A stream read
 * IMPORT_HEAD_PAGES full pages on without the head is an error, not a plan without one (review 416 B1):
 * the import writes its head near the start of the plan's stream, after its proposal, its decision and
 * whatever the stream held before.
 */
export async function fetchImportRequest(root = PLAN_ROOT, signal?: AbortSignal): Promise<PlanRequest | null> {
  let after = 0
  for (let pages = 0; ; pages++) {
    if (pages === IMPORT_HEAD_PAGES) throw new Error(`Plan events: no import head in the first ${IMPORT_HEAD_PAGES} pages of plan:${root}.`)
    const page = await authedFetch('Plan events', `/api/v1/agentrt/events?${new URLSearchParams({ stream: `plan:${root}`, after_seq: String(after) })}`, { signal })
    const events: unknown = await page.json()
    const head = importHead(events)
    if (head.artifact !== '') {
      const r = await authedFetch('Plan import request', `/api/v1/agentrt/artifacts/${encodeURIComponent(head.artifact)}`, { signal })
      let body: unknown
      try { body = JSON.parse(await r.text()) } catch { return invalidRequest() }
      return parsePlanRequest(body, root)
    }
    if (head.next === 0) return null
    if (head.next <= after) throw new Error('Plan events: invalid pagination cursor')
    if (Array.isArray(events) && events.length < PLAN_STREAM_PAGE) return null
    after = head.next
  }
}

const count = (v: unknown): v is number => typeof v === 'number' && Number.isInteger(v) && v >= 0

/**
 * The round as the panes show it: the plan's view, and each item's done, total and state from GET
 * /progress's plan, counted by the runtime's one rule (a dropped step leaves the total). They are two
 * reads; when they do not list the same items the round is not shown.
 */
export function runtimeRound(view: PlanView, plan: unknown[], root: string, request: PlanRequest | null,
  approvals: ReadonlyMap<string, number[]> = new Map(), signoffs: ReadonlyMap<string, StepSignoffs> = new Map()): RuntimeRound {
  const round = records(plan).find(r => r.plan_node_id === root && r.kind === 'round')
  if (round === undefined) throw new Error(`The runtime's progress does not count ${root}.`)
  const counts = new Map(records(round.items).map(i => [text(i.label), record(i.count)]))
  if (counts.size !== view.items.length || view.items.some(i => !counts.has(i.key))) throw new Error(`The plan and the runtime's counts of ${root} list different items; read again.`)
  const lines = new Map((request?.nodes ?? []).map(n => [n.id, n.evidence?.line ?? '']))
  const n = /^R(\d+)$/.exec(root)
  return { n: n ? Number(n[1]) : undefined, source: 'runtime', items: view.items.map(item => {
    const c = counts.get(item.key) ?? {}
    if (!count(c.done) || !count(c.total) || typeof c.state !== 'string') throw new Error(`The runtime's count of ${root}:${item.key} does not read.`)
    // A count without manual or stopped is from a platform before them: unset, never read as none.
    return { key: item.key, name: item.name, state: boardState(c.state), done: c.done, total: c.total,
      manual: count(c.manual) ? c.manual : undefined, stopped: count(c.stopped) ? c.stopped : undefined,
      steps: item.steps.map(s => {
        const waits = approvals.get(`${root}:${s.id}`)
        const signs = signoffs.get(`${root}:${s.id}`)
        return { id: s.id, t: s.t, state: s.state, when: s.when, line: lines.get(`${root}:${s.id}`) || undefined,
          owner: s.owner, star: s.star, ...(waits?.length ? { approvals: waits } : {}), ...(signs ? { signoffs: signs } : {}) }
      }) }
  }) }
}

/** The deepest a done rule nests (plan/rule.go maxDepth). */
const RULE_DEPTH = 8

/**
 * The approvals a done rule waits on: every approval_executed number in it, an atom or nested in all / any as
 * plan/rule.go reads it, each once, in the order the rule names them. Anything else in the rule is passed over. A number
 * past JavaScript's safe integers is not read: it would round to another request's (review 476 C1).
 */
export function approvalNumbers(rule: unknown, depth = 0): number[] {
  if (depth > RULE_DEPTH || rule === null || typeof rule !== 'object' || Array.isArray(rule)) return []
  const r = rule as Record<string, unknown>
  const out: number[] = []
  const n = r.approval_executed
  if (typeof n === 'number' && Number.isSafeInteger(n) && n > 0) out.push(n)
  for (const key of ['all', 'any']) {
    const list = r[key]
    if (Array.isArray(list)) for (const child of list) out.push(...approvalNumbers(child, depth + 1))
  }
  return [...new Set(out)]
}

/**
 * A step of the round whose done rule waits on a request (v4 §20.5 scenario 10: a request says which work and which
 * step it is): the step's id, label and title, whether it is the Owner's, and the item it sits under.
 */
export type ApprovalStep = { id: string; label: string; title: string; owner: boolean; item: string; itemTitle: string }

/** Each step's approvals from a GET /plan?root= answer, keyed by the step's node id; steps that wait on none are left out. */
export function approvalsByStep(doc: unknown): Map<string, number[]> {
  const out = new Map<string, number[]>()
  for (const node of records(record(doc).nodes)) {
    if (text(node.node_kind) !== 'step') continue
    const numbers = approvalNumbers(node.done_rule_json)
    if (numbers.length) out.set(text(node.plan_node_id), numbers)
  }
  return out
}

/** The steps of a GET /plan?root= answer whose done rule waits on request n, in the plan's order. */
export function stepsOfApproval(doc: unknown, n: number): ApprovalStep[] {
  const nodes = records(record(doc).nodes)
  const byId = new Map(nodes.map(node => [text(node.plan_node_id), node]))
  return nodes.filter(node => text(node.node_kind) === 'step' && approvalNumbers(node.done_rule_json).includes(n)).map(node => {
    const item = byId.get(text(node.parent_plan_node_id)) ?? {}
    return { id: text(node.plan_node_id), label: text(node.label), title: text(node.title), owner: node.owner_step === true,
      item: text(item.label), itemTitle: text(item.title) }
  })
}

/** A sign-off the Owner gave a step (GET /plan's signoffs): the owner_signoff ref, who signed, and when. */
export type StepSignoff = { ref: string; by: string; at: string }
/**
 * A step the Owner signs off (the Owner 2026-10-21): its node id, the refs of its done rule's owner_signoff atoms in the
 * order the rule names them, and the sign-offs it holds, oldest first.
 */
export type StepSignoffs = { node: string; refs: string[]; signed: StepSignoff[] }

/** The refs of a done rule's owner_signoff atoms, an atom or nested in all / any as plan/rule.go reads it, each once. */
export function signoffRefs(rule: unknown, depth = 0): string[] {
  if (depth > RULE_DEPTH || rule === null || typeof rule !== 'object' || Array.isArray(rule)) return []
  const r = rule as Record<string, unknown>
  const out: string[] = []
  if (typeof r.owner_signoff === 'string' && r.owner_signoff !== '') out.push(r.owner_signoff)
  for (const key of ['all', 'any']) {
    const list = r[key]
    if (Array.isArray(list)) for (const child of list) out.push(...signoffRefs(child, depth + 1))
  }
  return [...new Set(out)]
}

/** Each step's sign-offs from a GET /plan?root= answer, keyed by the step's node id; steps whose rule names no owner_signoff are left out. */
export function signoffsByStep(doc: unknown): Map<string, StepSignoffs> {
  const out = new Map<string, StepSignoffs>()
  for (const node of records(record(doc).nodes)) {
    if (text(node.node_kind) !== 'step') continue
    const refs = signoffRefs(node.done_rule_json)
    if (refs.length === 0) continue
    const signed = records(node.signoffs).map(s => ({ ref: text(s.ref), by: text(s.by), at: text(s.at) })).filter(s => s.ref !== '')
    out.set(text(node.plan_node_id), { node: text(node.plan_node_id), refs, signed })
  }
  return out
}

/** The refs of a step not signed off yet, in the rule's order. */
export function unsignedRefs(s: StepSignoffs): string[] {
  return s.refs.filter(ref => !s.signed.some(signed => signed.ref === ref))
}

/** POST /plan/{id}/signoff's answer: the sign-off, and whether the step held it before the press (nothing was written then). */
export type SignoffAnswer = StepSignoff & { node: string; alreadySigned: boolean }

/** A node id in a path: its colon kept, as the platform's router reads the raw path (mcp runtimeTools id). */
const nodePath = (node: string) => encodeURIComponent(node).replace(/%3A/gi, ':')

/**
 * The Owner signs ref of a step off: POST /plan/{id}/signoff with the admin sign-in. The route takes the admin role
 * only; a second press answers the first sign-off.
 */
export async function postStepSignoff(node: string, ref: string, token: string): Promise<SignoffAnswer> {
  const headers = new Headers()
  headers.set('Content-Type', 'application/json')
  headers.set('Authorization', `Bearer ${token}`)
  const r = await serverFetch(`/api/v1/agentrt/plan/${nodePath(node)}/signoff`, { method: 'POST', headers, body: JSON.stringify({ ref }) })
  if (!r.ok) throw await parseError('Sign off', r, token)
  const body = record(await r.json())
  return { node: text(body.plan_node_id), ref: text(body.ref), by: text(body.by), at: text(body.at), alreadySigned: body.already_signed === true }
}
