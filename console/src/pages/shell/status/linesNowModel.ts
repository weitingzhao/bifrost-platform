import type { LineRow } from '@/api/seatStatus'
import type { StatusWord } from '@/lib/shell/stateWords'

/**
 * The dot in the engine's seven words (箭10 2026-11-23, the inventory's first page: seven raw colours and private
 * dot words): waiting on someone is warn, running is info (activity, not a judgement), idle is boundary (asleep on
 * purpose), no heartbeat cannot be judged. What exactly is the note beside the word.
 */
export function lineDot(r: LineRow): { word: StatusWord; note: string } {
  if (r.ownerCards > 0 || r.status?.waiting_on) return { word: 'warn', note: 'waiting' }
  if (r.thread?.status === 'in_turn') return { word: 'info', note: 'running' }
  if (r.thread == null) return { word: 'unknown', note: 'no heartbeat' }
  return { word: 'boundary', note: r.thread.status }
}

/**
 * The section's counts, only those that were read: a count that never arrived is not 0 (design system: the
 * forbidden words; 2026-11-23 the guest view said "0 reporting · 30 not yet" with statuses unread).
 */
export function linesTitle(p: { statusesRead: boolean; cardsRead: boolean; reporting: number; notYet: number; forOwner: number }): string {
  const parts = ['Lines']
  parts.push(p.statusesRead ? `${p.reporting} reporting · ${p.notYet} not yet` : 'statuses not read')
  parts.push(p.cardsRead ? `${p.forOwner} for you` : 'cards not read')
  return parts.join(' · ')
}
