import { authedFetch, pathSegment } from './client'

export type RuleDocument = Record<string, unknown>
export type BoundaryRule = { rule_id: string; in_force: RuleDocument | null; latest: RuleDocument | null; draft: RuleDocument | null; status: string; notes: string[] }
export type BoundarySnapshot = {
  as_of: string
  rules: BoundaryRule[]
  tuning: { key: string; value: number; default: number; min: number; max: number; unit: string; source: string; consumer: string }[]
  landing: Record<string, unknown>[]
  hosts: Record<string, unknown>[]
  pools: Record<string, unknown>[]
  disk_floor_bytes: number
  unknown: string[]
  code_limits: { boundary_id: string; value: number; unit: string; source: string; adjustment: string }[]
}
export type RulePreview = { rule_id: string; rule_version: number; previous_version?: number; apply_via: 'signature' | 'capacity_approval' | 'tighten'; body: RuleDocument; why_not_capacity?: string; capacity_changes?: { field: string; old: unknown; new: unknown }[]; proposed_landing_effects?: Record<string, unknown>[] }
export const readBoundaries = async (signal?: AbortSignal): Promise<BoundarySnapshot> => {
  const value = await (await authedFetch('Delivery boundaries', '/api/v1/agentrt/delivery?view=boundaries', { signal })).json()
  const record = (v: unknown): v is Record<string, unknown> => v !== null && typeof v === 'object' && !Array.isArray(v)
  if (!record(value) || typeof value.as_of !== 'string' || !Number.isFinite(Date.parse(value.as_of)) || typeof value.disk_floor_bytes !== 'number'
    || !['rules', 'tuning', 'landing', 'hosts', 'pools', 'code_limits', 'unknown'].every(key => Array.isArray(value[key]))) throw new Error('Incomplete boundary snapshot; effective policy is unknown')
  const result = value as unknown as BoundarySnapshot
  if (!result.rules.every(row => record(row) && typeof row.rule_id === 'string' && Array.isArray(row.notes) && row.notes.every(n => typeof n === 'string'))
    || !result.tuning.every(row => record(row) && typeof row.key === 'string' && Number.isFinite(row.value))
    || !result.code_limits.every(row => record(row) && typeof row.boundary_id === 'string' && Number.isFinite(row.value))
    || !result.unknown.every(n => typeof n === 'string')
    || ![...result.landing, ...result.hosts, ...result.pools].every(record)) throw new Error('Malformed boundary observations; no eligibility inferred')
  return result
}
export async function ruleRequest<T>(id: string, action: string, body: Record<string, unknown>): Promise<T> {
  return (await authedFetch('Rule', `/api/v1/agentrt/rules/${pathSegment(id)}/${action}`, { method: 'POST', body: JSON.stringify(body) })).json()
}
