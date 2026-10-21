import { useQuery } from '@tanstack/react-query'
import { useRef } from 'react'
import { POOLS_QUERY_KEY, fetchPools } from '@/api/agentRuntimePools'
import { Bar } from './OwnerParts'
import { ago, answeredAt, clockTime, lastReadWords, poolRows } from './ownerModel'

/**
 * Each vendor pool's last reading with when it was read, an old one marked (2.5 G7 A): read from
 * GET /agentrt/pools, the same rows as System's quota card, where the Owner records a reading.
 */
export function PoolReadings({ now }: { now: number }) {
  const openedAt = useRef(Date.now())
  const query = useQuery({ queryKey: POOLS_QUERY_KEY, queryFn: ({ signal }) => fetchPools(signal), refetchInterval: 30_000 })
  const rows = poolRows(query.data ?? [], now)
  // Failed or paused offline: no reading from the last answer is shown as this one (review 488 A3).
  if (query.isError || query.isPaused) {
    return <p role="alert" className="m-0 text-sm [overflow-wrap:anywhere]">Quota could not be read{query.error ? `: ${query.error.message}` : ' (offline)'}; {lastReadWords(answeredAt(query.dataUpdatedAt), openedAt.current, now)}.</p>
  }
  if (query.isPending) return <p className="m-0 text-sm text-muted-foreground">Reading quota…</p>
  if (rows.length === 0) return <p className="m-0 text-sm text-muted-foreground">No pools reported yet.</p>
  return <ul aria-label="Quota readings" className="m-0 flex list-none flex-col gap-2.5 p-0">{rows.map(row => <li key={row.id} className="flex min-w-0 flex-col gap-1">
    <div className={`flex justify-between gap-2 text-sm ${row.old ? 'text-muted-foreground' : ''}`}><span className="min-w-0 break-words">{row.name}</span>
      <span className="font-mono">{row.used === null ? '?' : `${row.used}%`}</span></div>
    <Bar percent={row.used ?? 0} tone={row.tone} />
    <span className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
      <span title={row.readAt ?? undefined}>{row.used === null ? 'no reading yet' : `read ${ago(row.readAt ?? undefined, now)}`}</span>
      {row.old && <span title={row.old} className="rounded-full bg-[var(--control-fill)] px-2 font-bold text-[var(--color-warning)]">old</span>}
      {row.cooling && <span className="font-semibold text-[var(--color-danger)]">cooling until {row.cooldownUntil ? clockTime(Date.parse(row.cooldownUntil), now) : 'unknown'}</span>}
    </span>
  </li>)}</ul>
}
