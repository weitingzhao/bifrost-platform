import { authedFetch } from './client'

/*
 * Readings the platform computes from its own records (Work > CI). The bodies are the ones under
 * api/internal/server/testdata/contract/readings; the fields absent from some objects there are the optional ones here.
 */

/** One requester's requests of one day. Every list holds request numbers (#n). */
export type ApprovalReadingsGroup = {
  /** An opaque id of who asked. */
  requester: string
  /** The requester's own one-line description. */
  thread?: string
  work_ids: string[]
  /** Made that day. */
  requests: number[]
  pending: number[]
  rejected: number[]
  expired: number[]
  approved: number[]
  /** Median seconds from request to approval, over `approved`. */
  approve_seconds_median?: number
  /** Approved and done. */
  executed: number[]
  /** Approved, ran, failed. */
  failed: number[]
  /** Approved, result unreadable. */
  unknown: number[]
  /** Approved, not finished. */
  waiting: number[]
  /** Test requests, never approved. */
  drills: number[]
}
export type ApprovalReadingsDay = { day: string; groups: ApprovalReadingsGroup[] }
export type ApprovalReadings = {
  as_of: string
  /** The time zone a day is in. */
  zone: string
  /** How many approval records the store still holds. */
  retained: number
  /** Creation time of the oldest retained record; absent when none. Earlier days are not kept. */
  oldest?: string
  /** Newest first. */
  days: ApprovalReadingsDay[]
}

const record = (v: unknown): v is Record<string, unknown> => v !== null && typeof v === 'object' && !Array.isArray(v)
const NUMBER_LISTS = ['requests', 'pending', 'rejected', 'expired', 'approved', 'executed', 'failed', 'unknown', 'waiting', 'drills'] as const

/** What was asked for approval, by day and requester, newest day first. */
export async function fetchApprovalReadings(signal?: AbortSignal): Promise<ApprovalReadings> {
  const value: unknown = await (await authedFetch('Approval readings', '/api/v1/readings/approvals', { signal })).json()
  if (!record(value) || typeof value.as_of !== 'string' || typeof value.zone !== 'string' || typeof value.retained !== 'number'
    || !Array.isArray(value.days)
    || !value.days.every(d => record(d) && typeof d.day === 'string' && Array.isArray(d.groups)
      && d.groups.every(g => record(g) && typeof g.requester === 'string' && Array.isArray(g.work_ids) && NUMBER_LISTS.every(key => Array.isArray(g[key]))))) {
    throw new Error('Approval readings: incomplete answer; how many requests were made is not known')
  }
  return value as unknown as ApprovalReadings
}
