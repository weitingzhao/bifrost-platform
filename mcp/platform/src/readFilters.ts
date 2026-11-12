/**
 * Filters this MCP process applies to two list reads. Neither route takes
 * these as query parameters: GET /api/v1/approvals reads only status
 * (approvals/handler.go HandleList) and GET /api/v1/audit reads none
 * (actuation/audit.go HandleList). The whole list is fetched and narrowed
 * here; matched says how many records passed before the limit.
 */

function asObject(value: unknown): Record<string, unknown> {
  if (value != null && typeof value === 'object' && !Array.isArray(value)) return value as Record<string, unknown>
  return {}
}

function timeOf(record: unknown, key: string): number {
  const at = Date.parse(String(asObject(record)[key] ?? ''))
  return Number.isNaN(at) ? 0 : at
}

/** Newest first by a time field; records without one keep their order, last. */
function newestFirst(records: unknown[], key: string): unknown[] {
  return records
    .map((record, index) => ({ record, index, at: timeOf(record, key) }))
    .sort((a, b) => b.at - a.at || a.index - b.index)
    .map((entry) => entry.record)
}

function clamp(value: number, low: number, high: number): number {
  return Math.min(Math.max(Math.trunc(value), low), high)
}

export const REQUESTS_DEFAULT_LIMIT = 20
export const REQUESTS_MAX_LIMIT = 200

export interface RequestFilter {
  /** Exact action id, e.g. start_pipeline_run. */
  action?: string
  limit?: number
}

/**
 * Narrow GET /api/v1/approvals' {approvals: [...]}. With neither action nor
 * limit the body is returned as it came. Otherwise: only that action, newest
 * created first, at most limit (default 20, cap 200).
 */
export function filterRequests(body: unknown, filter: RequestFilter): unknown {
  const action = filter.action?.trim() ?? ''
  if (action === '' && filter.limit == null) return body
  const doc = asObject(body)
  if (!Array.isArray(doc.approvals)) return body
  const kept = action === '' ? doc.approvals : doc.approvals.filter((a) => asObject(a).action === action)
  const limit = clamp(filter.limit ?? REQUESTS_DEFAULT_LIMIT, 1, REQUESTS_MAX_LIMIT)
  const approvals = newestFirst(kept, 'created_at').slice(0, limit)
  return { ...doc, approvals, matched: kept.length, returned: approvals.length }
}

export const AUDIT_MAX_LIMIT = 500

export interface AuditFilter {
  /** Records whose action starts with this, e.g. release_policy. */
  action_prefix?: string
  /** RFC 3339: records at or after this time. */
  since?: string
  /** Records of the last n minutes. With since too, the later bound holds. */
  since_minutes?: number
  /** Records whose target contains this, ignoring case. */
  target_contains?: string
  limit?: number
}

export const AUDIT_SINCE_ERROR = 'since must be an RFC 3339 date and time, as 2026-11-12T09:00:00-06:00'

function hasAuditFilter(f: AuditFilter): boolean {
  return (
    (f.action_prefix ?? '') !== '' ||
    (f.since ?? '') !== '' ||
    f.since_minutes != null ||
    (f.target_contains ?? '') !== '' ||
    f.limit != null
  )
}

/**
 * Narrow GET /api/v1/audit's {records: [...]} (the platform keeps the newest
 * 500). With no filter the body is returned as it came. Otherwise the records
 * that pass every given filter, newest first, at most limit (cap 500; no
 * limit means all that matched).
 */
export function filterAudit(body: unknown, filter: AuditFilter, nowMs: number): unknown {
  if (!hasAuditFilter(filter)) return body
  let floor = Number.NEGATIVE_INFINITY
  if ((filter.since ?? '') !== '') {
    const since = Date.parse(filter.since as string)
    if (Number.isNaN(since)) return { error: AUDIT_SINCE_ERROR }
    floor = since
  }
  if (filter.since_minutes != null) floor = Math.max(floor, nowMs - filter.since_minutes * 60_000)
  const doc = asObject(body)
  if (!Array.isArray(doc.records)) return body
  const prefix = filter.action_prefix ?? ''
  const needle = (filter.target_contains ?? '').toLowerCase()
  const kept = doc.records.filter((record) => {
    const r = asObject(record)
    if (prefix !== '' && !String(r.action ?? '').startsWith(prefix)) return false
    if (needle !== '' && !String(r.target ?? '').toLowerCase().includes(needle)) return false
    if (floor !== Number.NEGATIVE_INFINITY && timeOf(record, 'at') < floor) return false
    return true
  })
  const sorted = newestFirst(kept, 'at')
  const records = filter.limit == null ? sorted : sorted.slice(0, clamp(filter.limit, 1, AUDIT_MAX_LIMIT))
  return { ...doc, records, matched: kept.length, returned: records.length }
}
