import { authedFetch } from './client'

/**
 * GET /api/v1/agentrt/plan-pace?root= (R5:5.5-13 slice 1): a round's steps left, its steps done on each of the last
 * days in the Owner's time zone, and at that pace how many days remain. `summary` is the one sentence every page and
 * report shows as it is; no page builds its own (Design v3.16 §4.3).
 */
export type PlanPaceDay = { date: string; done: number; partial: boolean }
export type PlanPaceETA = { status: 'ok' | 'unknown' | 'done'; days?: number; date?: string; reason?: string }
export type PlanPace = {
  root: string; readAt: string; timezone: string; remaining: number; days: PlanPaceDay[]; average: number; eta: PlanPaceETA; summary: string
  /** R5:5.5-17 slice 1: how long no registered work ran in the last day, as the platform words it; absent from an older server. */
  quietMinutes?: number; quietSummary?: string
}

const isObject = (v: unknown): v is Record<string, unknown> => v !== null && typeof v === 'object' && !Array.isArray(v)
const isNumber = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v)

/** Reads the answer, refusing one that lacks what the page shows (its own error, never a half-read pace). */
export function parsePlanPace(value: unknown): PlanPace {
  if (!isObject(value)) throw new Error('Plan pace: not an object')
  const { root, read_at, timezone, remaining, days, average, eta, summary, quiet_minutes, quiet_summary } = value
  if (typeof root !== 'string' || typeof read_at !== 'string' || typeof timezone !== 'string' || typeof summary !== 'string' || summary === ''
    || !isNumber(remaining) || !isNumber(average) || !Array.isArray(days) || !isObject(eta)) {
    throw new Error('Plan pace: missing or mistyped fields')
  }
  if (eta.status !== 'ok' && eta.status !== 'unknown' && eta.status !== 'done') throw new Error(`Plan pace: unknown ETA status ${String(eta.status)}`)
  const parsedDays = days.map(d => {
    if (!isObject(d) || typeof d.date !== 'string' || !isNumber(d.done)) throw new Error('Plan pace: a day is malformed')
    return { date: d.date, done: d.done, partial: d.partial === true }
  })
  return {
    root, readAt: read_at, timezone, remaining, average, summary, days: parsedDays,
    ...(isNumber(quiet_minutes) ? { quietMinutes: quiet_minutes } : {}),
    ...(typeof quiet_summary === 'string' && quiet_summary !== '' ? { quietSummary: quiet_summary } : {}),
    eta: {
      status: eta.status,
      ...(isNumber(eta.days) ? { days: eta.days } : {}),
      ...(typeof eta.date === 'string' ? { date: eta.date } : {}),
      ...(typeof eta.reason === 'string' ? { reason: eta.reason } : {}),
    },
  }
}

export async function fetchPlanPace(root: string, signal?: AbortSignal): Promise<PlanPace> {
  const response = await authedFetch('Plan pace', `/api/v1/agentrt/plan-pace?${new URLSearchParams({ root })}`, { signal })
  return parsePlanPace(await response.json())
}
