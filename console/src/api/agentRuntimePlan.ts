import { record, records, text } from '@/api/agentRuntime'

export type RuntimePlanStep = { id: string; t: string; state: string; when: string }
export type RuntimePlanItem = { key: string; name: string; state: string; done?: number; total?: number; steps: RuntimePlanStep[] }
export type RuntimeRound = { n?: number; checked?: number; items: RuntimePlanItem[] }
export type RuntimeArchitecture = { title: string; checked?: number; rows: { state: string; what: string; now: string }[] }
const num = (v: unknown) => typeof v === 'number' && Number.isFinite(v) ? v : undefined
export function parseRuntimeRound(value: unknown): RuntimeRound | undefined {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return undefined
  const r = record(value)
  return { n: num(r.n), checked: num(r.checked), items: records(r.items).map(i => ({
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
