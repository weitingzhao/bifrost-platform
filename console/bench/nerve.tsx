/**
 * The nerve map bench (2026-11-17, the coordinator: stop drawing blind).
 * Draws BusNerveGraph exactly as the Bus page does (buildBody), from a real
 * snapshot exported by export-snapshot.py, in the Vite dev server only (never
 * built into the Console). Query: ?view=2d · ?open=coordination,foundation ·
 * ?speed=3600 replays the snapshot's events · ?spin=0 · ?bus=1 the view
 * through the bus (the default is who listens to whom, buildDirectBody).
 * The snapshot has no receipts (they live in the bus's stream, which the
 * bench does not read): a replayed pulse waits mid-arc a fixed 400 ms.
 */
import { createRoot } from 'react-dom/client'
import { Suspense, lazy, useEffect, useMemo, useRef } from 'react'
import { nerveModel, replaySchedule, type ObserveLink } from '../src/domains/foundation/bus/busNerve'
import { PULSE, buildBody, buildDirectBody } from '../src/domains/foundation/bus/busNerveBody'
import { arcId, routesOf } from '../src/domains/foundation/bus/busNerveDirect'
import type { BusContract } from '../src/domains/foundation/bus/busNextgen'
import { CORE, domainOf } from '../src/domains/foundation/bus/busNerveLayout'
import type { NerveApi } from '../src/domains/foundation/bus/BusNerveGraph'
import type { BusEnvelope } from '../src/domains/foundation/bus/busTrace'

const BusNerveGraph = lazy(() => import('../src/domains/foundation/bus/BusNerveGraph'))

type Snapshot = {
  topics: { type: string; publisher: string; domain: string; consumers?: string[]; display_only?: boolean }[]
  rows?: { id: string; on: string[]; deliver: string; domain: string }[]
  clients?: { name: string; kind: string; domain: string }[]
  tiers?: { id: string; tier?: number }[]
  events: { type: string; time: string }[]
  letters: { from: string; to: string; count: number; unacked: number }[]
  seats: string[]
  names?: Record<string, string>
}

const q = new URLSearchParams(location.search)
const byBus = q.get('bus') === '1'

function contractOf(snap: Snapshot): BusContract {
  return {
    topics: snap.topics.map(t => ({ tenant: 'rocket', type: t.type, version: 1, publisher: t.publisher, domain: t.domain, guarantee: '', consumers: t.consumers ?? [] })),
    subscriptions: (snap.rows ?? []).map(r => ({ tenant: 'rocket', id: r.id, on: r.on, deliver: r.deliver, by: 'dispatcher', domain: r.domain || undefined })),
    clients: (snap.clients ?? []).map(c => ({ tenant: 'rocket', name: c.name, kind: c.kind, domain: c.domain || undefined })),
  }
}

function Bench({ snap }: { snap: Snapshot }) {
  const api = useRef<NerveApi | null>(null)
  const domainOfType = useMemo(() => new Map(snap.topics.map(t => [t.type, t.domain || domainOf(t.type)])), [snap])
  const routes = useMemo(() => routesOf(contractOf(snap)), [snap])
  const body = useMemo(() => {
    const contract = contractOf(snap)
    // The roster as STG had it on 11-17: a dispatcher row with nothing yet is "idle" (it turned the Bus page white once).
    const model = nerveModel(contract, [
      { name: 'headrelease', declared: true, receipts: 0, status: 'active' as const },
      { name: 'sub-card-created-to-owner', declared: true, receipts: 0, status: 'idle' as const },
    ])
    const now = Date.now()
    const dust = new Map<string, number[]>()
    for (const e of snap.events) {
      const d = domainOfType.get(e.type) ?? domainOf(e.type)
      dust.set(d, [...(dust.get(d) ?? []), (now - Date.parse(e.time)) / 3600_000])
    }
    const teamLinks: ObserveLink[] = snap.letters.map(l => ({ from: l.from, to: l.to, kind: 'letter', count: l.count, unacked: l.unacked }))
    const published = new Map<string, number>()
    for (const e of snap.events) published.set(e.type, (published.get(e.type) ?? 0) + 1)
    const listening = { hours: 24, topics: snap.topics.map(t => ({ type: t.type, published: published.get(t.type) ?? 0, readers: [], heard: 0, followed: 0, display_only: t.display_only })) }
    const input = {
      model, domainOfType, expanded: new Set((q.get('open') ?? '').split(',').filter(Boolean)), seats: snap.seats,
      beat: new Map(), observed: new Map(), teamLinks, names: new Map(Object.entries(snap.names ?? {})), dustByDomain: dust, now, listening,
    }
    if (byBus) return buildBody(input)
    return buildDirectBody({ ...input, tiers: snap.tiers ?? [], routes, roster: new Map(), delays: new Map() })
  }, [snap, domainOfType, routes])

  // ?focus=d:coordination flies into a node once the body has settled (as a click on a cloud does).
  useEffect(() => {
    const id = q.get('focus')
    if (!id) return
    const t = window.setTimeout(() => api.current?.focus(id), 9000)
    return () => clearTimeout(t)
  }, [])

  // ?speed= replays the snapshot's real events through the body, as the page's replay does.
  useEffect(() => {
    const speed = Number(q.get('speed') ?? 0)
    if (!speed) return
    const timers: number[] = []
    const plan = replaySchedule(snap.events.map((e, i) => ({ ...e, id: `ev-${i}` }) as unknown as BusEnvelope), speed)
    for (const { at, event } of plan) {
      timers.push(window.setTimeout(() => {
        const dom = domainOfType.get(event.type) ?? domainOf(event.type)
        const d = `d:${dom}`
        if (byBus) {
          api.current?.pulse(CORE, d, PULSE.event)
          window.setTimeout(() => api.current?.flash(d), 700)
          return
        }
        api.current?.flash(d)
        const rs = routes.filter(r => r.topic === event.type)
        if (rs.length === 0) api.current?.halo(d)
        for (const r of rs) {
          const key = `${event.id}|${r.reader}`
          if (api.current?.launch(key, arcId(dom, r.domain), PULSE.event)) window.setTimeout(() => api.current?.arrive(key), 1050)
        }
      }, at + 2500))
    }
    return () => timers.forEach(clearTimeout)
  }, [snap, domainOfType, routes])

  return (
    <Suspense fallback={<p>loading…</p>}>
      <BusNerveGraph nodes={body.nodes} links={body.links} arcs={'arcs' in body ? body.arcs : undefined} flat={q.get('view') === '2d'} spin={q.get('spin') !== '0'} onReady={a => { api.current = a }} />
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
