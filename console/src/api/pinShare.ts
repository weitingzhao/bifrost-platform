import { authedFetch } from './client'

/**
 * GET /api/v1/agentrt/pin-share (R5:5.7-8 slice 1): of the implement tasks proposed in the last days, how many are
 * pinned to the coordinator and how many of those say no reason (pin_reason), counted from the tasks and their creation
 * events. `summary` is the platform's sentence, shown as it is.
 */
export type PinShare = { since: string; readAt: string; days: number; implement: number; pinned: number; unexplained: number; summary: string }

const isObject = (v: unknown): v is Record<string, unknown> => v !== null && typeof v === 'object' && !Array.isArray(v)
const isCount = (v: unknown): v is number => typeof v === 'number' && Number.isInteger(v) && v >= 0

/** Reads the answer, refusing one that lacks what the line shows or whose counts do not fit inside each other. */
export function parsePinShare(value: unknown): PinShare {
  if (!isObject(value)) throw new Error('Pin share: not an object')
  const { since, read_at, days, implement, pinned, unexplained, summary } = value
  if (typeof since !== 'string' || typeof read_at !== 'string' || typeof summary !== 'string' || summary === ''
    || !isCount(days) || !isCount(implement) || !isCount(pinned) || !isCount(unexplained)) {
    throw new Error('Pin share: missing or mistyped fields')
  }
  if (pinned > implement || unexplained > pinned) throw new Error('Pin share: counts do not add up')
  return { since, readAt: read_at, days, implement, pinned, unexplained, summary }
}

export async function fetchPinShare(signal?: AbortSignal): Promise<PinShare> {
  const response = await authedFetch('Pin share', '/api/v1/agentrt/pin-share', { signal })
  return parsePinShare(await response.json())
}
