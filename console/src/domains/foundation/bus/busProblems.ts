import { authedFetch } from '@/api/client'

/**
 * GET /api/v1/bus/problems: the problem loop (OWNER_LAW #93), each case a
 * letter of a row that asks for a verdict, the counts computed from those
 * cases by the platform. Nothing here is typed in on the page.
 */

export type ProblemCase = {
  kind: 'alert' | 'delivery'
  subject: string
  event: string
  row: string
  receiver: string
  letter_at: string
  acked_at: string | null
  verdict: '' | 'real' | 'false'
  outcome: string
  ref: string
  verdict_by: string
  verdict_at: string | null
  closed: boolean
  state: 'sent' | 'seen' | 'judged' | 'closed'
}
export type ProblemCounts = {
  raised: number; seen: number; judged: number; real: number; false: number; closed: number; open: number
  ack_median_minutes: number | null; verdict_median_minutes: number | null
  /** Letter to the verdict that closed it, over the closed cases; absent from an older API. */
  closed_median_minutes?: number | null
}
export type ProblemInvalid = { message_id: string; sender: string; at: string; summary?: string; problem: string }
export type BusProblems = { since: string; counts: ProblemCounts; cases: ProblemCase[]; invalid: ProblemInvalid[] }

export async function fetchBusProblems(): Promise<BusProblems> {
  const r = await authedFetch('bus problems', '/api/v1/bus/problems')
  const b = (await r.json()) as BusProblems
  return { ...b, cases: b.cases ?? [], invalid: b.invalid ?? [] }
}

/** Minutes as the reader counts them: under an hour in minutes, then hours, then days. */
export function minutesText(m: number | null): string {
  if (m == null) return 'unknown'
  if (m < 60) return `${Math.round(m)}m`
  if (m < 60 * 48) return `${Math.round(m / 60)}h`
  return `${Math.round(m / 1440)}d`
}

/** The counts in one line. */
export function countsLine(c: ProblemCounts): string {
  return `raised ${c.raised} · seen ${c.seen} · judged ${c.judged} · real ${c.real} · false ${c.false} · closed ${c.closed} · open ${c.open}`
    + ` · median to seen ${minutesText(c.ack_median_minutes)} · to verdict ${minutesText(c.verdict_median_minutes)}`
    + ` · to closed ${minutesText(c.closed_median_minutes ?? null)} (${c.closed} case${c.closed === 1 ? '' : 's'})`
}

/** The cases not closed, oldest letter first: the ones that wait longest are on top. */
export function openCases(cases: readonly ProblemCase[]): ProblemCase[] {
  return cases.filter(c => !c.closed).sort((a, b) => Date.parse(a.letter_at) - Date.parse(b.letter_at))
}

/** How long ago a letter was sent, at `now`. */
export function ageText(letterAt: string, now: number): string {
  return minutesText(Math.max(0, (now - Date.parse(letterAt)) / 60000))
}
