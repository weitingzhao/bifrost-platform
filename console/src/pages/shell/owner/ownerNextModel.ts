import type { OwnerNext, OwnerNextCandidate } from '@/api/agentRuntime'
import { clockTime } from './ownerModel'

function within(ms: number): string {
  const m = Math.max(1, Math.round(ms / 60_000))
  return m < 60 ? `${m} min` : `${Math.floor(m / 60)} h${m % 60 ? ` ${m % 60} min` : ''}`
}

/**
 * When a timed goal is due: "14:20–14:40, in 20 to 40 min". Its turn by the plan when nothing unfinished comes before
 * it; a time that has passed is only that, never "now": the work before it is not done (review 477 B1).
 */
export function dueWords(c: OwnerNextCandidate, now: number): string {
  const lo = Date.parse(c.earliest ?? ''), hi = Date.parse(c.latest ?? '')
  if (c.ready) return 'your turn by the plan (nothing unfinished before it); Needs you lists any request'
  if (hi <= now) return `expected by ${clockTime(hi, now)}; that time has passed, waiting for progress`
  const range = clockTime(lo, now) === clockTime(hi, now) ? clockTime(hi, now) : `${clockTime(lo, now)}–${clockTime(hi, now)}`
  return lo <= now ? `by ${clockTime(hi, now)}, within ${within(hi - now)}` : `${range}, in ${within(lo - now)}${hi - lo >= 60_000 ? ` to ${within(hi - now)}` : ''}`
}

/** One goal: its id, its step, and when ("after M-2" when it waits on other goals). */
export function candidateWords(c: OwnerNextCandidate, now: number): string {
  const head = `${c.mission_id} · ${c.step.title || c.step.plan_node_id || 'its next step'}`
  if (c.state !== 'estimated') return `${head} — time unknown`
  return `${head} — ${dueWords(c, now)}${c.after.length ? `, after ${c.after.join(', ')}` : ''}`
}

/**
 * The line For you shows (v4 §20.2's top line): when the Owner is next needed after what waits now, or why it cannot be
 * told and the earliest goal that can be timed; null for a platform before owner_next.
 */
export function ownerNextLine(next: OwnerNext | undefined, now: number): { text: string; mission: string | null } | null {
  if (next === undefined) return null
  if (next.state === 'none') return { text: 'No open goal\'s next step is yours.', mission: null }
  if (next.state === 'estimated' && next.next) return { text: candidateWords(next.next, now), mission: next.next.mission_id }
  const timed = next.candidates.find(c => c.state === 'estimated')
  return { text: `Unknown — ${next.reason}${timed ? `. The earliest that can be timed: ${candidateWords(timed, now)}` : ''}`, mission: null }
}
