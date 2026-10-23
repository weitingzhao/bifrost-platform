import type { RuntimeAttempt, RuntimeExecutor, RuntimeHost } from '@/api/agentRuntime'
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

const short = (executor: string) => executor.replace(/^exec:/, '')

/**
 * What a host's latest claim offered (W-31 3.5), as the Crew card says it: each executor with its model; '' from a
 * platform before 3.5, which does not record it.
 */
export function offeredWords(h: RuntimeHost): string {
  if (h.offered === undefined) return ''
  if (h.offered === null) return 'What it offers is not recorded yet'
  if (h.offered.length === 0) return 'Offers no executor'
  return `Offers ${h.offered.map(o => `${short(o.executor)}${o.model ? ` (${o.model})` : ''}`).join(', ')}`
}

/** An executor as the Crew card's Executors line says it: available, or not with why, or not known with why. */
export function executorWords(e: RuntimeExecutor): string {
  if (e.available === true) return `${short(e.executor)} available`
  return `${short(e.executor)} ${e.available === false ? 'unavailable' : 'not known'}: ${e.why}`
}

/** The jobs no executor can take now, the chief of staff's said for what it means to the Owner. */
export function unservedWords(jobs: readonly string[]): string {
  if (!jobs.length) return ''
  const named = jobs.map(j => j === 'chief_of_staff' ? 'chief_of_staff (the chief of staff cannot answer you)' : j)
  return `No executor can take these jobs now: ${named.join(', ')}.`
}

/** A host the controller reports offline, with when it last polled. */
export function offlineWords(h: RuntimeHost, now: number): string {
  const last = h.last_poll_at ? `its last poll was ${ago(h.last_poll_at, now)}` : 'it has never polled'
  const live = h.live_attempts ? `; ${h.live_attempts} attempt${h.live_attempts === 1 ? '' : 's'} were running on it` : ''
  return `Host ${h.host_id} is offline: ${last}${live}.`
}
