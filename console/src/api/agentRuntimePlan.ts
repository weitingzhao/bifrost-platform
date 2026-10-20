import { authedFetch, parseError } from '@/api/client'
import { record, records, text } from '@/api/agentRuntime'
import { boardState, importHead, type PlanRequest, type PlanView } from '@/api/agentRuntimePlanView'

/**
 * A step as the plan panes show it. line is the step's ledger line as the import request kept it
 * (read from the runtime only; a step added after the import has none).
 */
export type RuntimePlanStep = { id: string; t: string; state: string; when: string; line?: string }
export type RuntimePlanItem = { key: string; name: string; state: string; done?: number; total?: number; steps: RuntimePlanStep[] }
/**
 * A round, as the board publishes it or as read from the runtime. source 'runtime' means it came from
 * the agent runtime (GET /plan); unknown is the reason the board could not read it, with no items
 * (CUT-plan-import §6): a round that could not be read is never shown as one with no items.
 */
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

/**
 * The import request the plan's import head names (CUT-plan-import §6): the plan's stream read from the
 * start, page by page, for its first legacy_imported event that names a request, and that request read
 * once. null when the stream has no such event: the plan was not imported, and each step shows the time
 * of its state event.
 */
export async function fetchImportRequest(root = PLAN_ROOT, signal?: AbortSignal): Promise<PlanRequest | null> {
  let after = 0
  for (;;) {
    const page = await authedFetch('Plan events', `/api/v1/agentrt/events?${new URLSearchParams({ stream: `plan:${root}`, after_seq: String(after) })}`, { signal })
    const head = importHead(await page.json())
    if (head.artifact !== '') {
      const r = await authedFetch('Plan import request', `/api/v1/agentrt/artifacts/${encodeURIComponent(head.artifact)}`, { signal })
      let body: unknown
      try { body = JSON.parse(await r.text()) } catch { return invalidRequest() }
      return parsePlanRequest(body, root)
    }
    if (head.next === 0) return null
    if (head.next <= after) throw new Error('Plan events: invalid pagination cursor')
    after = head.next
  }
}

const count = (v: unknown): v is number => typeof v === 'number' && Number.isInteger(v) && v >= 0

/**
 * The round as the panes show it: the plan's view, and each item's done, total and state from GET
 * /progress's plan, counted by the runtime's one rule (a dropped step leaves the total). They are two
 * reads; when they do not list the same items the round is not shown.
 */
export function runtimeRound(view: PlanView, plan: unknown[], root: string, request: PlanRequest | null): RuntimeRound {
  const round = records(plan).find(r => r.plan_node_id === root && r.kind === 'round')
  if (round === undefined) throw new Error(`The runtime's progress does not count ${root}.`)
  const counts = new Map(records(round.items).map(i => [text(i.label), record(i.count)]))
  if (counts.size !== view.items.length || view.items.some(i => !counts.has(i.key))) throw new Error(`The plan and the runtime's counts of ${root} list different items; read again.`)
  const lines = new Map((request?.nodes ?? []).map(n => [n.id, n.evidence?.line ?? '']))
  const n = /^R(\d+)$/.exec(root)
  return { n: n ? Number(n[1]) : undefined, source: 'runtime', items: view.items.map(item => {
    const c = counts.get(item.key) ?? {}
    if (!count(c.done) || !count(c.total) || typeof c.state !== 'string') throw new Error(`The runtime's count of ${root}:${item.key} does not read.`)
    return { key: item.key, name: item.name, state: boardState(c.state), done: c.done, total: c.total,
      steps: item.steps.map(s => ({ id: s.id, t: s.t, state: s.state, when: s.when, line: lines.get(`${root}:${s.id}`) || undefined })) }
  }) }
}
