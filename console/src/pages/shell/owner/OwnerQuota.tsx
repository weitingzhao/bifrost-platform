import { useContext, useRef, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button } from '@bifrost/ui'
import { fetchPools, recordPoolReading } from '@/api/agentRuntimePools'
import { PlatformAuthContext } from '@/hooks/platformAuthContext'
import { Card } from './OwnerParts'
import { ago } from './ownerModel'

const POOLS_KEY = ['agent-runtime-pools']
const KNOWN_POOLS = ['codex:owner:short', 'codex:owner:weekly', 'claude:owner:short', 'claude:owner:weekly', 'cursor:owner:monthly']
const localTime = (at: string | null) => at ? new Date(at).toLocaleString() : 'unknown'
const inputClass = 'min-h-11 min-w-0 w-full max-w-full rounded border border-[var(--card-border)] bg-[var(--control-fill)] px-2 text-base'

export function OwnerQuota({ now }: { now: number }) {
  const auth = useContext(PlatformAuthContext)
  const owner = auth != null && !auth.capsLoading && !auth.capsError && auth.canAdmin
  const client = useQueryClient()
  // This card mounts only in the System entry; unmounting removes its polling observer.
  const query = useQuery({ queryKey: POOLS_KEY, queryFn: ({ signal }) => fetchPools(signal), refetchInterval: 30_000 })
  const [pool, setPool] = useState(KNOWN_POOLS[0])
  const [percent, setPercent] = useState('')
  const [reset, setReset] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)
  const busy = useRef(false)
  const pools = query.data ?? []
  const choices = [...new Set([...KNOWN_POOLS, ...pools.map(p => p.vendor_pool_id)])]
  async function submit(event: FormEvent) {
    event.preventDefault()
    if (!owner || busy.current) return
    const fraction = Number(percent) / 100
    if (!percent.trim() || !Number.isFinite(fraction) || fraction < 0 || fraction > 1 || (fraction === 1 && !reset)) return
    busy.current = true; setPending(true); setError(''); setSaved(false)
    try {
      await recordPoolReading(pool, fraction, reset ? new Date(reset).toISOString() : undefined)
      // a form left open overnight must not send yesterday's reset again (review 281 C1)
      setPercent(''); setReset(''); setSaved(true)
      await client.invalidateQueries({ queryKey: POOLS_KEY })
    } catch (e) { setError(e instanceof Error ? e.message : 'Could not record reading.') }
    finally { busy.current = false; setPending(false) }
  }
  return <Card label="Quota" title="Quota">
    {query.isPending && <p className="m-0 text-sm text-muted-foreground">Reading quota…</p>}
    {query.isError && <p role="alert" className="m-0 text-sm [overflow-wrap:anywhere]">Quota could not be read: {query.error.message}</p>}
    {query.isSuccess && pools.length === 0 && <p className="m-0 text-sm text-muted-foreground">No pools reported yet.</p>}
    <ul className="m-0 flex min-w-0 list-none flex-col gap-2 p-0 text-sm">
      {pools.map(p => {
        // A reading whose window has reset since is the previous window's, not this one's (review 281 B1).
        const stale = p.resets_at !== null && Date.parse(p.resets_at) <= now
        return <li key={p.vendor_pool_id} className="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1 rounded-[var(--control-radius)] bg-[var(--control-fill)] p-2.5 [overflow-wrap:anywhere]" style={p.cooling ? { color: 'var(--color-danger)' } : undefined}>
        <strong className="min-w-0">{p.tool} · {p.account} · {p.window}</strong>
        <span>{p.used_fraction === null ? 'not read yet' : stale ? `last reading ${(p.used_fraction * 100).toFixed(1)}% is from the window before` : `${(p.used_fraction * 100).toFixed(1)}% used`}</span>
        <span>{stale ? `Reset at ${localTime(p.resets_at)} passed` : `Resets ${localTime(p.resets_at)}`}</span>
        <span style={!p.cooling && p.pace !== null && p.pace > 0.1 ? { color: 'var(--color-warning)' } : undefined}>Pace {p.pace === null ? 'unknown' : `${p.pace >= 0 ? '+' : ''}${(p.pace * 100).toFixed(1)}%`}</span>
        <span title={p.measured_at ?? undefined}>{p.measure_source || 'no source'} · {p.measured_at ? ago(p.measured_at, now) : 'not read yet'}</span>
        <span title={`Since ${localTime(p.used_since)}`}>This window: {p.used.tokens?.toLocaleString() ?? '—'} tokens · {p.used.usd === undefined ? '—' : `$${p.used.usd.toFixed(2)}`} USD · {p.used.wall_s === undefined ? '—' : (p.used.wall_s / 3600).toFixed(2)} hours</span>
        {p.cooling && <strong>Cooling until {localTime(p.cooldown_until)}</strong>}
      </li>
      })}
    </ul>
    {auth?.capsLoading && <p className="m-0 text-sm text-muted-foreground">Checking your sign-in…</p>}
    {!auth?.capsLoading && auth?.capsError && <p className="m-0 text-sm text-muted-foreground">Your role could not be read. Reading entry is unavailable.</p>}
    {owner && <form aria-label="Record quota reading" onSubmit={submit} className="grid min-w-0 gap-2 [overflow-wrap:anywhere] sm:grid-cols-2">
      <label className="min-w-0 text-sm">Pool<select className={inputClass} disabled={pending} value={pool} onChange={e => { setPool(e.target.value); setSaved(false) }}>{choices.map(id => <option key={id} value={id}>{id}</option>)}</select></label>
      <label className="min-w-0 text-sm">Used percent<input className={inputClass} disabled={pending} type="number" min="0" max="100" step="any" required value={percent} onChange={e => { setPercent(e.target.value); setSaved(false) }} /></label>
      <label className="min-w-0 text-sm">Reset (local time)<input className={inputClass} disabled={pending} type="datetime-local" required={Number(percent) === 100} value={reset} onChange={e => { setReset(e.target.value); setSaved(false) }} /></label>
      <div className="flex min-w-0 items-end"><Button type="submit" disabled={pending} className="min-h-11">{pending ? 'Submitting…' : 'Submit'}</Button></div>
      {error && <p role="alert" className="m-0 text-sm sm:col-span-2">{error}</p>}
      {saved && <p role="status" className="m-0 text-sm">Reading recorded.</p>}
    </form>}
  </Card>
}
