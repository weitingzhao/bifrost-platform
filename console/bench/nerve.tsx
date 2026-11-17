/**
 * The nerve map bench (2026-11-17, the coordinator: stop drawing blind).
 * Draws BusNerveGraph exactly as the Bus page does (buildBody), from a real
 * snapshot exported by export-snapshot.py, in the Vite dev server only (never
 * built into the Console). Query: ?view=2d · ?open=coordination,foundation ·
 * ?speed=3600 replays the snapshot's events · ?spin=0.
 */
import { createRoot } from 'react-dom/client'
import { Suspense, lazy, useEffect, useMemo, useRef } from 'react'
import { nerveModel, replaySchedule, type ObserveLink } from '../src/domains/foundation/bus/busNerve'
import { PULSE, buildBody } from '../src/domains/foundation/bus/busNerveBody'
import { CORE, domainOf } from '../src/domains/foundation/bus/busNerveLayout'
import type { NerveApi } from '../src/domains/foundation/bus/BusNerveGraph'
import type { BusEnvelope } from '../src/domains/foundation/bus/busTrace'

const BusNerveGraph = lazy(() => import('../src/domains/foundation/bus/BusNerveGraph'))

type Snapshot = {
  topics: { type: string; publisher: string; domain: string }[]
  events: { type: string; time: string }[]
  letters: { from: string; to: string; count: number; unacked: number }[]
  seats: string[]
}

const q = new URLSearchParams(location.search)

function Bench({ snap }: { snap: Snapshot }) {
  const api = useRef<NerveApi | null>(null)
  const domainOfType = useMemo(() => new Map(snap.topics.map(t => [t.type, t.domain || domainOf(t.type)])), [snap])
  const body = useMemo(() => {
    const contract = { topics: snap.topics.map(t => ({ tenant: 'rocket', type: t.type, version: 1, publisher: t.publisher, domain: t.domain, guarantee: '', consumers: [] })), subscriptions: [], clients: [] }
    const model = nerveModel(contract, [{ name: 'headrelease', declared: true, receipts: 0, status: 'active' as const }])
    const now = Date.now()
    const dust = new Map<string, number[]>()
    for (const e of snap.events) {
      const d = domainOfType.get(e.type) ?? domainOf(e.type)
      dust.set(d, [...(dust.get(d) ?? []), (now - Date.parse(e.time)) / 3600_000])
    }
    const teamLinks: ObserveLink[] = snap.letters.map(l => ({ from: l.from, to: l.to, kind: 'letter', count: l.count, unacked: l.unacked }))
    return buildBody({
      model, domainOfType, expanded: new Set((q.get('open') ?? '').split(',').filter(Boolean)), seats: snap.seats,
      beat: new Map(), observed: new Map(), teamLinks, names: new Map(), dustByDomain: dust, now,
    })
  }, [snap, domainOfType])

  // ?speed= replays the snapshot's real events through the body, as the page's replay does.
  useEffect(() => {
    const speed = Number(q.get('speed') ?? 0)
    if (!speed) return
    const timers: number[] = []
    const plan = replaySchedule(snap.events.map((e, i) => ({ ...e, id: `ev-${i}` }) as unknown as BusEnvelope), speed)
    for (const { at, event } of plan) {
      timers.push(window.setTimeout(() => {
        const d = `d:${domainOfType.get(event.type) ?? domainOf(event.type)}`
        api.current?.pulse(CORE, d, PULSE.event)
        window.setTimeout(() => api.current?.flash(d), 700)
      }, at + 2500))
    }
    return () => timers.forEach(clearTimeout)
  }, [snap, domainOfType])

  return (
    <Suspense fallback={<p>loading…</p>}>
      <BusNerveGraph nodes={body.nodes} links={body.links} flat={q.get('view') === '2d'} spin={q.get('spin') !== '0'} onReady={a => { api.current = a }} />
    </Suspense>
  )
}

void fetch('./snapshot.local.json').then(async r => {
  const bar = document.getElementById('bar')!
  if (!r.ok) { bar.textContent = 'no snapshot: run python3 console/bench/export-snapshot.py'; return }
  const snap = (await r.json()) as Snapshot
  bar.textContent = `${snap.topics.length} topics · ${snap.events.length} events of the last day · ${snap.letters.length} letter pairs · ${snap.seats.length} seats`
  createRoot(document.getElementById('root')!).render(<Bench snap={snap} />)
})
