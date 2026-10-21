import type { RuntimeAttempt, RuntimeHost } from '@/api/agentRuntime'
import { ago } from './ownerModel'

export function duration(s: number | undefined): string {
  if (s == null) return 'not started'
  if (s < 60) return `${s} s`
  const m = Math.floor(s / 60)
  return m < 60 ? `${m} min` : `${Math.floor(m / 60)} h ${m % 60} min`
}

export type CrewRow = { host: RuntimeHost | undefined; id: string; attempts: RuntimeAttempt[]; lost: RuntimeAttempt[] }

/**
 * Each agent host of this platform with the attempts it runs now and the lost attempts whose result is not persisted
 * (TD-352), hosts with either first.
 */
export function crewRows(hosts: RuntimeHost[], live: RuntimeAttempt[], lost: RuntimeAttempt[] = []): CrewRow[] {
  const rows = new Map<string, CrewRow>()
  for (const h of hosts) if (h.state !== 'retired') rows.set(h.host_id, { host: h, id: h.host_id, attempts: [], lost: [] })
  const row = (a: RuntimeAttempt) => {
    const id = a.host_id || 'no host'
    const r = rows.get(id) ?? { host: undefined, id, attempts: [], lost: [] }
    rows.set(id, r)
    return r
  }
  for (const a of live) row(a).attempts.push(a)
  for (const a of lost) if (a.persisted === false) row(a).lost.push(a)
  const busy = (r: CrewRow) => r.attempts.length + r.lost.length > 0 ? 1 : 0
  return [...rows.values()].sort((a, b) => busy(b) - busy(a) || a.id.localeCompare(b.id))
}

/**
 * What of an attempt's work is not on the platform (TD-352, design T34 ⑤): for a stale or lost attempt, or one on a lost
 * host, whose result is not persisted, the last checkpoint the platform confirmed and how long nothing was persisted;
 * '' for any other (a running attempt's result is not persisted until it ends, which is no news).
 */
export function unpersistedWords(a: RuntimeAttempt, now: number): string {
  if (a.persisted !== false || !(a.state === 'stale' || a.state === 'lost' || a.status === 'host_lost')) return ''
  const checkpoint = a.last_checkpoint_seq != null && a.last_checkpoint_at
    ? `last checkpoint ${a.last_checkpoint_seq} ${ago(a.last_checkpoint_at, now)}` : 'no checkpoint'
  const span = a.unpersisted_for_s != null ? `, nothing persisted for ${duration(a.unpersisted_for_s)}` : ''
  return `Result not persisted: ${checkpoint}${span}`
}

/** The first line of a goal or title, capped in UTF-16 units without splitting a surrogate pair. */
export function firstLine(s: string, max = 80): string {
  const line = s.split('\n')[0].trim()
  if (line.length <= max) return line
  let end = max - 1
  const last = line.charCodeAt(end - 1)
  if (last >= 0xD800 && last <= 0xDBFF) end--
  return `${line.slice(0, end)}…`
}
