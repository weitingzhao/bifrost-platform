import { authedFetch } from '@/api/client'
import { record, text } from '@/api/agentRuntime'

/**
 * GET /api/v1/agentrt/capabilities (2.5 G9; agentrtapi/capabilities.go): each executor's score of its
 * latest window per job and task class, the job candidates, and the capability facts not retired.
 * Rows are `to_jsonb` of agentrt.capability_score, job_candidate and capability_fact.
 */
export type CapabilityScore = {
  capability_score_id: string; executor_actor_id: string; model_provider: string; model: string; tool_version: string
  job_id: string; task_class: string; core: boolean; source: 'exam' | 'benchmark' | 'in_job'
  window_start: string; window_end: string; attempts: number; first_pass: number; accepted: number; rework: number
  median_wall_s: number | null; score: number; computed_at: string
}
export type JobCandidate = {
  job_candidate_id: string; job_id: string; executor_actor_id: string; model_provider: string; model_pattern: string
  status: 'qualifying' | 'active' | 'probation' | 'demoted' | 'retired'; rule_id: string; qualified_at: string | null
}
export type CapabilityFact = {
  capability_fact_id: string; executor_actor_id: string; model_provider: string; fact_kind: 'cannot' | 'must_not' | 'only' | 'prefer'
  subject: string; source_kind: 'measured' | 'owner'; source: string; rule_id: string; recorded_at: string
}
export type Capabilities = { scores: CapabilityScore[]; candidates: JobCandidate[]; facts: CapabilityFact[]; truncated: boolean }

const invalid = (): never => { throw new Error('Capabilities: invalid platform response.') }
const count = (v: unknown): v is number => typeof v === 'number' && Number.isInteger(v) && v >= 0
const time = (v: unknown): v is string => typeof v === 'string' && Number.isFinite(Date.parse(v))
const named = (v: unknown): v is string => typeof v === 'string' && v !== ''
const list = (v: unknown) => Array.isArray(v) ? v.map(record) : invalid()

/** Reject a malformed row rather than show a score nobody measured. */
export function parseCapabilities(value: unknown): Capabilities {
  const r = record(value)
  if (typeof r.truncated !== 'boolean') return invalid()
  const scores = list(r.scores).map(s => {
    if (!named(s.capability_score_id) || !named(s.executor_actor_id) || !named(s.job_id) || typeof s.task_class !== 'string'
      || typeof s.core !== 'boolean' || !['exam', 'benchmark', 'in_job'].includes(text(s.source))
      || typeof s.score !== 'number' || !(s.score >= 0 && s.score <= 1) || !time(s.window_start) || !time(s.window_end) || !time(s.computed_at)
      || ![s.attempts, s.first_pass, s.accepted, s.rework].every(count) || (s.median_wall_s !== null && !count(s.median_wall_s))) return invalid()
    return { capability_score_id: s.capability_score_id, executor_actor_id: s.executor_actor_id, model_provider: text(s.model_provider),
      model: text(s.model), tool_version: text(s.tool_version), job_id: s.job_id, task_class: s.task_class, core: s.core,
      source: s.source as CapabilityScore['source'], window_start: s.window_start, window_end: s.window_end, attempts: s.attempts as number,
      first_pass: s.first_pass as number, accepted: s.accepted as number, rework: s.rework as number,
      median_wall_s: s.median_wall_s as number | null, score: s.score, computed_at: s.computed_at }
  })
  const candidates = list(r.candidates).map(c => {
    if (!named(c.job_candidate_id) || !named(c.job_id) || !named(c.executor_actor_id)
      || !['qualifying', 'active', 'probation', 'demoted', 'retired'].includes(text(c.status)) || (c.qualified_at !== null && !time(c.qualified_at))) return invalid()
    return { job_candidate_id: c.job_candidate_id, job_id: c.job_id, executor_actor_id: c.executor_actor_id, model_provider: text(c.model_provider),
      model_pattern: text(c.model_pattern), status: c.status as JobCandidate['status'], rule_id: text(c.rule_id), qualified_at: c.qualified_at as string | null }
  })
  const facts = list(r.facts).map(f => {
    if (!named(f.capability_fact_id) || !named(f.executor_actor_id) || !['cannot', 'must_not', 'only', 'prefer'].includes(text(f.fact_kind))
      || typeof f.subject !== 'string' || !['measured', 'owner'].includes(text(f.source_kind)) || !time(f.recorded_at)) return invalid()
    return { capability_fact_id: f.capability_fact_id, executor_actor_id: f.executor_actor_id, model_provider: text(f.model_provider),
      fact_kind: f.fact_kind as CapabilityFact['fact_kind'], subject: f.subject, source_kind: f.source_kind as CapabilityFact['source_kind'],
      source: text(f.source), rule_id: text(f.rule_id), recorded_at: f.recorded_at }
  })
  return { scores, candidates, facts, truncated: r.truncated }
}

export async function fetchCapabilities(signal?: AbortSignal): Promise<Capabilities> {
  const r = await authedFetch('Capabilities', '/api/v1/agentrt/capabilities', { signal })
  return parseCapabilities(await r.json())
}
