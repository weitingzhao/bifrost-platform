import type { StepSignoff } from '@/api/agentRuntimePlan'

/** A press that failed may still have reached the platform; the route answers a second press with the first sign-off. */
export const SIGNOFF_PRESS_AGAIN = 'Pressing again is safe: a sign-off already recorded is answered, not written twice.'

/** When, to the minute in UTC; the text as given when it is not a time. */
function minute(at: string): string {
  const t = Date.parse(at)
  return Number.isNaN(t) ? at : `${new Date(t).toISOString().slice(0, 16).replace('T', ' ')}Z`
}

/** "Signed off exam-2.4-1 by the Owner, 2026-10-21 15:04Z". */
export function signedLine(s: StepSignoff): string {
  return `Signed off ${s.ref} by ${s.by === 'owner' ? 'the Owner' : s.by}, ${minute(s.at)}`
}
