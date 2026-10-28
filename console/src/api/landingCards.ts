import { authHeaders, parseError } from '@/api/client'
import { serverFetch } from '@/api/serverContact'
import { isQuestionId } from '@/lib/shell/consoleRoutes'

export const LANDING_KIND_LABELS = {
  root_failed: 'Landing failed',
  floor: 'Cannot land',
  ok_missing: 'Push missing from main',
  late_landing: 'Landed late',
  repo_stopped: 'Repository landings stopped',
  ci_stuck: 'CI running long',
  parked_long: 'Landing parked',
  landed_unverified: 'Landing not verified',
  in_main: 'Change already in main',
  late_revert: 'Landing reverted after close',
} as const

const OUTCOMES = ['applied', 'moot', 'refused', 'held', 'acknowledged'] as const
type Outcome = typeof OUTCOMES[number]
export type LandingOutcome = {
  outcome: Outcome; why: string; choice: string; by: string; answered_at: string; remind_at?: string
}
export type LandingCard = {
  question_id: string; prep: string; kind: keyof typeof LANDING_KIND_LABELS; root: string; op: string
  trigger_seq: number; stop_generation: number; acked: boolean; closed: boolean; closed_why: string
  options: Record<string, string>; outcome: LandingOutcome | null; next_step: string
}

const object = (v: unknown): v is Record<string, unknown> => v !== null && typeof v === 'object' && !Array.isArray(v)
const time = (v: unknown): v is string => typeof v === 'string' && Number.isFinite(Date.parse(v))
const count = (v: unknown): v is number => typeof v === 'number' && Number.isSafeInteger(v) && v >= 0

function isOutcome(v: unknown): v is LandingOutcome {
  return object(v) && OUTCOMES.some(value => value === v.outcome)
    && ['why', 'choice', 'by'].every(key => typeof v[key] === 'string') && time(v.answered_at)
    && (!('remind_at' in v) || time(v.remind_at)) && (v.outcome !== 'held' || time(v.remind_at))
}

/** Missing or mistyped fields are unreadable; new, unrelated fields are allowed. */
export function parseLandingCard(v: unknown): LandingCard | null {
  if (!object(v) || typeof v.question_id !== 'string' || !isQuestionId(v.question_id)
    || !['prep', 'root', 'op', 'closed_why', 'next_step'].every(key => typeof v[key] === 'string')
    || typeof v.kind !== 'string' || !Object.hasOwn(LANDING_KIND_LABELS, v.kind)
    || !count(v.trigger_seq) || !count(v.stop_generation)
    || typeof v.acked !== 'boolean' || typeof v.closed !== 'boolean'
    || !object(v.options) || !Object.values(v.options).every(option => typeof option === 'string')
    || (v.outcome !== null && !isOutcome(v.outcome))) return null
  return v as LandingCard
}

/** 404 means an ordinary question. Other failures remain independent of reading or answering it. */
export async function fetchLandingCard(id: string, signal?: AbortSignal): Promise<LandingCard | null> {
  const r = await serverFetch(`/api/v1/agentrt/landing/cards/${encodeURIComponent(id)}`, { headers: authHeaders(), signal })
  if (r.status === 404) return null
  if (!r.ok) throw await parseError('Landing outcome', r)
  const card = parseLandingCard(await r.json())
  if (card === null || card.question_id !== id) throw new Error('Landing outcome: invalid platform response.')
  return card
}
