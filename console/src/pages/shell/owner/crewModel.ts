import type { RuntimeAttempt, RuntimeHost } from '@/api/agentRuntime'

export function duration(s: number | undefined): string {
  if (s == null) return 'not started'
  if (s < 60) return `${s} s`
  const m = Math.floor(s / 60)
  return m < 60 ? `${m} min` : `${Math.floor(m / 60)} h ${m % 60} min`
}

export type CrewRow = { host: RuntimeHost | undefined; id: string; attempts: RuntimeAttempt[] }

/** Each agent host of this platform with the attempts it runs now, hosts that run something first. */
export function crewRows(hosts: RuntimeHost[], live: RuntimeAttempt[]): CrewRow[] {
  const rows = new Map<string, CrewRow>()
  for (const h of hosts) if (h.state !== 'retired') rows.set(h.host_id, { host: h, id: h.host_id, attempts: [] })
  for (const a of live) {
    const id = a.host_id || 'no host'
    const row = rows.get(id) ?? { host: undefined, id, attempts: [] }
    row.attempts.push(a)
    rows.set(id, row)
  }
  return [...rows.values()].sort((a, b) => (b.attempts.length > 0 ? 1 : 0) - (a.attempts.length > 0 ? 1 : 0) || a.id.localeCompare(b.id))
}
