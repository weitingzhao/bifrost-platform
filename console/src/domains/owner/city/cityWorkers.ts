import type { SeatStatus } from '@/api/seatStatus'

/**
 * The workers (the Owner, 2026-11-18): each seat that reported a status is a
 * worker. It lives in the dorm and walks to the building of its line: the
 * charter it says it works under names its domain (a charter's file name is
 * its domain's directory name, agent-config/charters/README.md). It holds a
 * sign when it waits on something, and goes home when its status is stale.
 * Only seats that reported are drawn; a status read that failed draws none
 * and says unknown.
 */

export type WorkerState = 'working' | 'waiting' | 'home'
/** Where a worker goes: the first building of this kind, the one bound to `binds` first when given. */
export type Target = { kind: string; binds?: string }
export type Worker = { seat: string; name: string; state: WorkerState; target: Target; bubble: string; lines: string[] }

/** A status older than this sends its worker home (as the Lines page calls it stale). */
export const HOME_AFTER_MINUTES = 120

const HOME: Target = { kind: 'dorm', binds: 'domain:coordination' }

/** The building of a line, by the domain its charter names. A metaphor (one meaning each, dictionary v4), not a fact about the line. */
export function targetOfCharter(charter: string | undefined): Target {
  const c = (charter ?? '').toLowerCase().replace(/^.*\//, '').replace(/\.md$/, '')
  if (!c) return HOME
  if (/verify|release|\bci\b|-ci$/.test(c)) return { kind: 'hif', binds: 'stage:rocket-ci' }
  if (/tenant/.test(c)) return { kind: 'satellites', binds: 'stage:satellites' }
  if (/lineage/.test(c)) return { kind: 'archive', binds: 'domain:lineage' }
  if (/compute|foundation|decouple/.test(c)) return { kind: 'power' }
  if (/owner|plan|coordinator|cockpit/.test(c)) return { kind: 'control', binds: 'domain:owner' }
  return HOME
}

const short = (s: string, n: number) => s.length > n ? `${s.slice(0, n - 1)}…` : s

export function workersOf(statuses: readonly SeatStatus[], now: number): Worker[] {
  return statuses.map(st => {
    const age = Math.round((now - Date.parse(st.at)) / 60_000)
    const stale = !Number.isFinite(age) || age > HOME_AFTER_MINUTES
    const state: WorkerState = stale ? 'home' : st.waiting_on ? 'waiting' : 'working'
    const name = (st.charter ?? '').replace(/^.*\//, '').replace(/\.md$/, '') || st.seat.replace(/^thread:local_/, '').slice(0, 8)
    const bubble = state === 'home' ? 'zzz' : state === 'waiting' ? short(`waiting: ${st.waiting_on}`, 30) : short(st.doing || st.summary || '…', 30)
    return {
      seat: st.seat, name, state, target: state === 'home' ? HOME : targetOfCharter(st.charter), bubble,
      lines: [
        `${name}: ${st.doing || 'no "doing" given'}`,
        ...(st.waiting_on ? [`waiting on ${st.waiting_on}`] : []),
        `${st.percent}% (${st.percent_basis}); next: ${st.next}`,
        `status ${Number.isFinite(age) ? `${age} min` : 'at an unreadable time'} ago${stale ? ': stale, gone home' : ''}`,
      ],
    }
  })
}

/** The seats in one chip: how many work, wait, sleep; unknown when the read failed. */
export function workersGlance(ws: readonly Worker[] | undefined): { text: string; tone: 'calm' | 'busy' | 'unknown'; href: string } {
  if (!ws) return { text: 'Seats: unknown', tone: 'unknown', href: '#command?section=lines' }
  const n = (s: WorkerState) => ws.filter(w => w.state === s).length
  const parts = [`${n('working')} working`, ...(n('waiting') ? [`${n('waiting')} waiting`] : []), ...(n('home') ? [`${n('home')} home`] : [])]
  return { text: `Seats: ${parts.join(', ')}`, tone: n('waiting') ? 'busy' : 'calm', href: '#command?section=lines' }
}
