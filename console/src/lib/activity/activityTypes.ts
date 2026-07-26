/** Shell Activity Feed — glanceable ops timeline (Audit remains SSOT). */

export type ActivityKind = 'actuation' | 'agent' | 'pipeline' | 'signal-transition'

export type ActivityPhase =
  | 'requested'
  | 'applying'
  | 'settled'
  | 'completed'
  | 'failed'

export type ActivitySettledOutcome =
  | 'resolved'
  | 'signal-unchanged'
  | 'timeout'
  | 'error'

export type ActivityEvent = {
  id: string
  kind: ActivityKind
  phase: ActivityPhase
  title: string
  target?: string
  detail?: string
  settledOutcome?: ActivitySettledOutcome
  ts: number
  /** Console tab id or hash fragment for navigate. */
  linkTo?: string
  /** Optional chip / deployment key for settle correlation. */
  correlateKey?: string
}

export const ACTIVITY_DROPDOWN_MAX = 25
/** Settled / terminal events age out of the dropdown after this window; Audit keeps history. */
export const ACTIVITY_SETTLED_TTL_MS = 30 * 60 * 1000
