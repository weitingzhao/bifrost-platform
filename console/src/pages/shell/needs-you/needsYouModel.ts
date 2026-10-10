import type { ConsoleNavBadge } from '@/components/shell/ConsoleNavSlotItem'

/**
 * Needs you counts three kinds only: decide, approve, sign off (ADR §6).
 * Approve is wired today; decide and sign off arrive with the D1 transition layer.
 */
export type NeedsYouCount =
  | { state: 'loading' }
  | { state: 'unknown'; reason: string }
  | { state: 'known'; count: number }

/** Decide and sign-off rows turn yellow after 7 days and red after 14. Approvals expire in 24h. */
export const WAIT_YELLOW_DAYS = 7
export const WAIT_RED_DAYS = 14

export type WaitTone = 'normal' | 'yellow' | 'red'

export function waitTone(createdAt: string, now = Date.now()): WaitTone {
  const start = Date.parse(createdAt)
  if (!Number.isFinite(start)) return 'normal'
  const days = (now - start) / 86_400_000
  if (days >= WAIT_RED_DAYS) return 'red'
  if (days >= WAIT_YELLOW_DAYS) return 'yellow'
  return 'normal'
}

export function needsYouCountText(count: NeedsYouCount): string {
  if (count.state === 'loading') return '…'
  if (count.state === 'unknown') return 'Unknown'
  return String(count.count)
}

/** Sidebar pill. Nothing at zero; `?` (titled Unknown) when the count cannot be read. */
export function needsYouBadge(count: NeedsYouCount): ConsoleNavBadge | undefined {
  if (count.state === 'loading') return undefined
  if (count.state === 'unknown') {
    return { text: '?', title: `Needs you: Unknown — ${count.reason}`, tone: 'unknown' }
  }
  if (count.count === 0) return undefined
  return {
    text: count.count > 99 ? '99+' : String(count.count),
    title: `Needs you: ${count.count} waiting`,
    tone: 'attention',
  }
}
