// The counter rooms on the bench (箭10 2026-11-23): each building's packages as the City draws them, from the
// map's domains (an archived read, counters.local.json: the bench has no viewer token for /catalog/domain) and
// the live request readings (counterQueries.json, through the platform's PromQL read). Nothing is drawn from
// made-up numbers: a route with no reading is shuttered.
import { StrictMode, useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { counterRoom, counterSummaryOf, queueStep } from '@bifrost/ui/city/units/counterRoom'
import { spriteUrl } from '@bifrost/ui/city/sprites'
import { buildingFlow, COUNTER_QUERIES, counterRooms, readingsOf, routesByPackage, type CounterReads } from '../src/domains/owner/city/gen2/content/counters'
import type { DomainHoldings } from '../src/domains/owner/city/gen2/content/domainOwns'

type Snap = { read_at: string; holdings: Record<string, DomainHoldings> }
const snap = Object.values(import.meta.glob('./counters.local.json', { eager: true, import: 'default' }))[0] as Snap | undefined

async function readLive(): Promise<CounterReads & { at: string }> {
  const q = (query: string) => fetch(`/api/v1/telemetry/promql?q=${encodeURIComponent(query)}`).then(r => r.ok ? r.json() : null).then(v => v?.points ?? null, () => null)
  const [a, b] = await Promise.all([q(COUNTER_QUERIES.perMin), q(COUNTER_QUERIES.failPerMin)])
  return { ...readingsOf(a, b), at: new Date().toISOString() }
}

/** What the caption says, nothing it did not read: no routes, not read, or the readings. */
function captionOf(routes: number, sum: ReturnType<typeof counterSummaryOf>): string {
  if (routes === 0) return 'no routes in the contract'
  if (sum.perMin == null) return `${routes} routes · not read`
  return `${routes} routes · ${sum.perMin.toFixed(2)} req/min · ${sum.failing} with 5xx (${sum.measured} of ${routes} read)`
}

function App() {
  const [reads, setReads] = useState<(CounterReads & { at: string }) | null>(null)
  useEffect(() => { readLive().then(setReads) }, [])
  if (!snap) return <p>No counters.local.json: read the domains first (MCP get_domain).</p>
  const routes = routesByPackage(snap.holdings, reads ?? { perMin: null, failPerMin: null })
  return <div className="page">
    <h1>Counter rooms</h1>
    <p>Domains: {snap.read_at}. Readings: {reads ? `live, read ${reads.at}` : 'reading…'}.</p>
    {Object.values(snap.holdings).map(h => <section key={h.id}>
      <h2>{h.id}</h2>
      {(h.buildings ?? []).map(b => <div key={b.id} className="bldg">
        <h3>{b.id} · door: {(() => { const f = buildingFlow(b.packages, routes); return !b.packages.some(p => (routes.get(p) ?? []).length) ? 'no routes (no cars)' : f.perMin == null ? 'not read (no cars)' : `${f.perMin.toFixed(2)} req/min, ${queueStep(f.perMin)} car${queueStep(f.perMin) === 1 ? '' : 's'}` })()}</h3>
        <div className="rooms">{counterRooms(b.packages, routes, 'online').map(r => {
          const rs = routes.get(r.id) ?? []
          const sum = counterSummaryOf({ routes: rs })
          return <figure key={r.id}>
            {r.picture ? <img src={spriteUrl(counterRoom({ routes: rs }))} alt={`counter of ${r.id}`} /> : <div className="none">{r.label}</div>}
            <figcaption><b>{r.label}</b> · {captionOf(rs.length, sum)}</figcaption>
          </figure>
        })}</div>
      </div>)}
    </section>)}
  </div>
}
createRoot(document.getElementById('root')!).render(<StrictMode><App /></StrictMode>)
