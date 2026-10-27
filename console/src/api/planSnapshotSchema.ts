/** Closed PV1/PV2 wire contract. Optional means omitted by Go, never a default zero. */
type Parser<T> = (value: unknown, path: string) => T
function invalid(path: string): never { throw new Error(`Plan view: unsupported or incomplete response at ${path}`) }
const str: Parser<string> = (v, p) => typeof v === 'string' ? v : invalid(p)
const num: Parser<number> = (v, p) => typeof v === 'number' && Number.isFinite(v) ? v : invalid(p)
const bool: Parser<boolean> = (v, p) => typeof v === 'boolean' ? v : invalid(p)
const dict: Parser<Record<string, unknown>> = (v, p) => v !== null && typeof v === 'object' && !Array.isArray(v) ? v as Record<string, unknown> : invalid(p)
const nullable = <T,>(read: Parser<T>): Parser<T | null> => (v, p) => v === null ? null : read(v, p)
const optional = <T,>(read: Parser<T>): Parser<T | undefined> => (v, p) => v === undefined ? undefined : read(v, p)
const array = <T,>(read: Parser<T>): Parser<T[]> => (v, p) => Array.isArray(v) ? v.map((x, i) => read(x, `${p}[${i}]`)) : invalid(p)
const one = <T extends string>(...values: T[]): Parser<T> => (v, p) => values.includes(v as T) ? v as T : invalid(p)
const object = <S extends Record<string, Parser<unknown>>>(shape: S): Parser<{ [K in keyof S]: ReturnType<S[K]> }> => (v, p) => {
  const input = dict(v, p)
  if (Object.keys(input).some(k => !Object.hasOwn(shape, k))) invalid(p)
  return Object.fromEntries(Object.entries(shape).map(([k, read]) => [k, read(input[k], `${p}.${k}`)])) as { [K in keyof S]: ReturnType<S[K]> }
}
const version: Parser<1> = (v, p) => v === 1 ? 1 : invalid(p)
const strings = array(str)
const nonnegative: Parser<number> = (v, p) => { const n = num(v, p); return n >= 0 && Number.isInteger(n) ? n : invalid(p) }
const timestamp: Parser<string> = (v, p) => { const s = str(v, p); return Number.isFinite(Date.parse(s)) ? s : invalid(p) }
const ref = object({ root: str, node: str })
const tri = one('true', 'false', 'unknown')
// PV2: one adopted binding's reading on a plan item (store/planverificationview.go).
const acceptanceStatus = object({
  id: str, binding: str, atom: one('main_effective', 'deployment_verified', 'review_valid', 'external_verified'), value: tri, source_value: tri,
  freshness: one('Fresh', 'Stale', 'Unknown'), source_id: str, source_version: nonnegative, evidence: nullable(strings),
  last_success: str, last_known_value: str, reason: str,
})
const responsible = object({ kind: one('owner', 'crew_seat', 'vendor_executor', 'review'), actor_ref: nullable(str), seat_ref: nullable(str), provider: nullable(str) })
const edgeFields = { milestone: str, required_for: str, relation: one('blocks', 'related'), reason: str, sources: strings }
const edge = object({ from: ref, to: ref, ...edgeFields })
const item = object({
  plan_node_id: str, parent: str, ordinal: nonnegative, title: str, work_id: str,
  lane_kind: one('mainline', 'branch', 'support'), mainline_order: nullable(nonnegative), anchor_node_id: nullable(str), milestones: strings,
  depends_on: array(object({ root: str, node: str, ...edgeFields })), next_responsible: responsible,
  acceptance: array(object({ id: str, milestone: str, criterion: str, rule_ref: str, evidence_required: strings, target_environment: str, sources: strings })),
  estimate: object({ work_days: nullable(array(num)), review_rounds: nonnegative, source: str, confidence: one('low', 'medium'), reason: str }),
  bindings: array(dict), evidence: array(object({ source: str, milestone: str, classification: one('repository_evidence', 'legacy_evidence', 'owner_statement'), assessment: str, observed_at: str, live_verified: bool })),
})
const node = object({
  plan_node_id: str, parent_plan_node_id: nullable(str), node_kind: one('round', 'item', 'step', 'arch_root', 'arch_row'), ordinal: nonnegative,
  label: str, title: str, work_id: nullable(str), mission_id: nullable(str), step_kind: nullable(str), owner_step: bool, effect_step: bool,
  done_rule_json: dict, state: nullable(one('planned', 'active', 'done', 'stopped', 'dropped')), state_event_id: nullable(nonnegative),
  usual_s: nullable(nonnegative), created_by_actor_id: str, created_at: timestamp, version: nonnegative,
  lane_kind: one('mainline', 'branch', 'support', 'Unclassified'), view: optional(item),
  execution_progress: object({ state: one('planned', 'active', 'done', 'stopped'), done: nonnegative, total: nonnegative, stopped: nonnegative, basis: str }),
  acceptance_status: optional(nullable(array(acceptanceStatus))), current_validity: optional(tri),
})
const attempt = object({
  attempt_id: str, attempt_no: nonnegative, task_id: str, mission_id: str, host_id: optional(str), executor: str, model: optional(str),
  state: str, started_at: optional(timestamp), elapsed_s: optional(nonnegative), heartbeat_age_s: num, status: optional(str), reason: str,
  persisted: bool, last_checkpoint_seq: optional(nonnegative), last_checkpoint_at: optional(timestamp), unpersisted_for_s: optional(nonnegative), ended_at: optional(timestamp),
})
const external = object({
  v: version, kind: one('agentrt.external_work/v1'), external_work_id: str, registrant: str, root: str, plan_node_id: str, source_run_key: str,
  artifact_id: str, mission_id: optional(str), task_id: optional(str), seq: nonnegative, type: one('registered', 'started', 'heartbeat', 'submitted', 'cancelled', 'linked'),
  state: one('Registered', 'Running', 'Submitted', 'Cancelled'), request_digest: str, registration_digest: str,
  observed_at: optional(timestamp), registered_at: timestamp, source_started_at: optional(timestamp), started_at: optional(timestamp), heartbeat_at: timestamp, ended_at: optional(timestamp),
  report_path_digest: optional(str), report_digest: optional(str), commit_shas: optional(strings), exit_code: optional(num),
  verification: optional(one('Verified', 'Verification failed', 'Stale', 'Unknown')),
  description: object({ v: version, kind: one('agentrt.external_work/v1'), task: str, executor: str, provider: str, repo: str, branch: str, acceptance_refs: strings, estimate_minutes: array(nonnegative) }),
  recorded_at: timestamp, heartbeat_age_s: num, status: one('Registered', 'Running', 'Submitted', 'Cancelled', 'Unconfirmed'), elapsed_s: optional(nonnegative), backfilled: bool,
})
const landing = object({ stream: str, operation: str, phase: str, outcome: str, reason: str, attempt: str, result: str, main_commit: str, run: str, observed_at: timestamp, basis: str })
const task = object({ task_id: str, plan_node_id: str, mission_id: str, title: str, state: str, attempt: optional(attempt), external_work: optional(array(external)), landing: optional(landing), delivery: optional(str) })
const working: Parser<ReturnType<typeof external> | ReturnType<typeof task>> = (v, p) => 'external_work_id' in dict(v, p) ? external(v, p) : task(v, p)
const finished: Parser<ReturnType<typeof external> | { attempt_id: string; task_id: string; state: string; ended_at: string; meaning: string }> = (v, p) => 'external_work_id' in dict(v, p) ? external(v, p) : object({ attempt_id: str, task_id: str, state: str, ended_at: timestamp, meaning: str })(v, p)
// PV1 entries carry the execution state; PV2 entries (a view with bindings) the verified milestone and current validity.
const mainlineEntry = object({ ref, plan_node_id: str, execution_state: optional(one('planned', 'active', 'done', 'stopped')), milestone_status: one('Unknown', 'true', 'false', 'unknown'), current_validity: optional(tri) })
const signoff = { plan_node_id: str, ref: str, acceptance: str, plan_version: nonnegative, evidence: nullable(strings), reason: str, source: str }
const snapshot = object({
  schema: one('agentrt.plan_view_response/v1'), root: str, plan_version: nonnegative, artifact_id: str, as_of: timestamp,
  nodes: array(node), items: array(item), edges: array(edge),
  mainline: object({ path: array(mainlineEntry), current: nullable(mainlineEntry), basis: str, milestone_status: optional(one('Unknown')) }),
  next: array(object({ plan_node_id: str, action: str, reason: optional(str), responsible: optional(responsible) })),
  waiting_on: array(object({ edge, status: one('Unknown', 'Blocked'), reason: str, responsible })),
  // A runtime task's wait, or (PV2) an acceptance sign-off the Owner can give now.
  owner_waiting: array((v: unknown, p: string) => 'task_id' in dict(v, p)
    ? object({ task_id: str, plan_node_id: str, reason: str, source: str })(v, p)
    : object({ ...signoff, readiness: one('Ready') })(v, p)),
  owner_later: array((v: unknown, p: string) => 'ref' in dict(v, p)
    ? object({ ...signoff, readiness: one('Blocked') })(v, p)
    : object({ plan_node_id: str, title: str, readiness: one('Unknown'), reason: str })(v, p)),
  working: array(working), finished_today: array(finished), day_timezone: str, tasks: array(task), external_work: array(external),
  coverage: object({ unregistered_work: nonnegative, label: str, scope: str, reconciled: bool }),
  source_watermarks: (v: unknown, p: string) => Object.fromEntries(Object.entries(dict(v, p)).map(([k, n]) => [k, nonnegative(n, `${p}.${k}`)])),
  freshness: object({ database: object({ status: one('Fresh', 'Stale', 'Unknown'), observed_at: timestamp }), external_work: str, verification: one('Unknown', 'Fresh', 'Partial', 'Stale'), owner_waiting: str }),
  eta: object({ status: one('Unknown'), reason: str }),
  recent_changes: array(object({ event_id: nonnegative, event_stream_id: str, seq: nonnegative, event_type: str, actor_id: str, at: timestamp, body_json: dict })),
  recent_next: str, recent_as_of: timestamp, warnings: strings,
})
export type PlanSnapshot = ReturnType<typeof snapshot>
export type PlanNode = PlanSnapshot['nodes'][number]
export type PlanItem = PlanSnapshot['items'][number]
export type PlanEdge = PlanSnapshot['edges'][number]
export type PlanWork = PlanSnapshot['working'][number]
export type ExternalWork = PlanSnapshot['external_work'][number]
export function parsePlanSnapshot(value: unknown, root: string): PlanSnapshot {
  const out = snapshot(value, 'snapshot')
  if (out.root !== root || out.nodes.length > 500 || out.edges.length > 2000) invalid('root/limits')
  const nodes = new Map(out.nodes.map(n => [n.plan_node_id, n]))
  if (nodes.size !== out.nodes.length || !nodes.has(root)) invalid('nodes')
  for (const n of out.nodes) {
    if (n.parent_plan_node_id !== null && !nodes.has(n.parent_plan_node_id)) invalid('node.parent')
    if (n.execution_progress.done + n.execution_progress.stopped > n.execution_progress.total) invalid('execution_progress')
  }
  for (const row of [...out.mainline.path, ...out.next, ...out.owner_waiting, ...out.owner_later, ...out.working]) {
    if (!nodes.has(row.plan_node_id)) invalid('node reference')
  }
  const items = new Map(out.nodes.filter(n => n.view).map(n => [n.view!.plan_node_id, n]))
  if (out.items.some(i => !items.has(i.plan_node_id))) invalid('items')
  for (const edge of out.edges) {
    if (edge.from.root === root && !items.has(edge.from.node)) invalid('edge.from')
    if (edge.to.root === root && !items.has(edge.to.node)) invalid('edge.to')
  }
  if (out.mainline.current && !out.mainline.path.some(n => n.plan_node_id === out.mainline.current?.plan_node_id)) invalid('mainline.current')
  for (const work of out.working) if (!('external_work_id' in work) && !work.attempt) invalid('working.attempt')
  return out
}
