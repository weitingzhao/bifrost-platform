import { Suspense, lazy, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { OpsSection } from '@/components/layout/OpsSection'
import { AGENT_THREADS_REFRESH_MS, fetchAgentThreads, type AgentThread } from '@/api/agentThreads'
import { fetchLineage } from '@/api/lineage'
import { fetchCiDomains } from '@/api/ciDomains'
import { fetchBusContract, fetchBusNextgen } from './busNextgen'
import { BUS_LIST_CAP, fetchBusClients, fetchBusReplay, type BusStreamState } from './useBusStream'
import { clockTime, type BusEnvelope } from './busTrace'
import {
  LATE_AFTER_S, OWN_CONSUMER, QUIET_AFTER_H, SPEEDS, STUCK_AFTER_MIN, fetchBusObserve, nerveModel, readerOf, replaySchedule,
  isDeaf, seatMail,
} from './busNerve'
import { C, PULSE, buildBody, buildDirectBody, nameOf, type GArc } from './busNerveBody'
import { arcId, halosOf, routesOf, type Funnel, type Route } from './busNerveDirect'
import { CORE, domainOf } from './busNerveLayout'
import { ContractGraph } from './BusContractSection'
import type { GLink, GNode, NerveApi } from './BusNerveGraph'

// Its own chunk: three.js and the graph libraries load only when the Bus page opens.
const BusNerveGraph = lazy(() => import('./BusNerveGraph'))

/** Hours a session's heartbeat keeps it on the body. */
const SEAT_HOURS = 24
/** Milliseconds from a domain to its topic after the pulse left the publisher. */
const SECOND_HOP_MS = 700
/** The repository whose ownership table gives the domains' tiers (this platform's own). */
const TIER_REPO = 'bifrost-platform'
/** Delays kept per arc for its median. */
const DELAYS_KEPT = 200

function seatId(t: AgentThread): string {
  return `thread:${(t.session || t.thread).replace(/^thread:/, '')}`
}

/** Days of commit lineage read for seat names: the Lines section's read, so they share it. */
const LINEAGE_DAYS = 2

/**
 * Seat names, read: each session's title as its heartbeat reports it, else
 * the title its commits' lineage carries (the order Work › Lines uses). On
 * an environment whose seats report elsewhere (STG: the seats beat to PROD)
 * the heartbeat has none, so the lineage title is what names them.
 */
function useSeatNames(): Map<string, string> {
  const threads = useQuery({ queryKey: ['agent-threads'], queryFn: fetchAgentThreads, refetchInterval: AGENT_THREADS_REFRESH_MS, retry: false })
  const lineage = useQuery({ queryKey: ['lineage-lines', LINEAGE_DAYS], queryFn: () => fetchLineage(LINEAGE_DAYS), staleTime: 60_000, retry: false })
  return useMemo(() => {
    const m = new Map<string, string>()
    for (const l of lineage.data?.threads ?? []) if (l.session && l.title?.trim()) m.set(`thread:${l.session.replace(/^thread:/, '')}`, l.title.trim())
    for (const t of threads.data?.threads ?? []) if (t.title?.trim()) m.set(seatId(t), t.title.trim())
    return m
  }, [threads.data, lineage.data])
}


type Mode = { kind: 'live' } | { kind: 'replay'; speed: number }
type View = '3d' | '2d' | 'list'

function TermButton({ on, children, onClick }: { on: boolean; children: ReactNode; onClick: () => void }) {
  return (
    <button type="button" onClick={onClick} className="cursor-pointer rounded-sm border px-2 py-0.5 font-mono text-xs"
      style={{ borderColor: on ? C.green : C.rule, color: on ? C.green : C.muted, background: on ? '#0d2116' : 'transparent' }}>
      {children}
    </button>
  )
}

function startView(): View {
  try {
    return window.matchMedia('(max-width: 767px)').matches ? '2d' : '3d'
  } catch {
    return '3d'
  }
}

/**
 * Bus → Nerves: a watch on the team at work. One body: the bus at its heart,
 * topics gathered by domain, publishers, readers, seats and the owner as
 * organs that find their own place, can be dragged and drift. Every real
 * event is a particle from its publisher through its domain to its topic;
 * a reader's receipt is a particle from the bus to that reader, which lights
 * up; a seat's letter runs between the seats. Seats breathe with their own
 * heartbeat. Red is something waiting too long. Replay plays the last day
 * from the ledger. Nothing is drawn that was not read.
 */
export function BusNerveSection({ stream, onSelect }: { stream: BusStreamState; onSelect: (id: string) => void }) {
  const contract = useQuery({ queryKey: ['bus-contract'], queryFn: fetchBusContract, staleTime: 300_000, retry: false })
  const clients = useQuery({ queryKey: ['bus', 'clients'], queryFn: fetchBusClients, refetchInterval: 60_000 })
  const nextgen = useQuery({ queryKey: ['bus-nextgen'], queryFn: fetchBusNextgen, refetchInterval: 60_000, retry: false })
  const team = useQuery({ queryKey: ['bus-observe'], queryFn: fetchBusObserve, refetchInterval: 60_000, retry: false })
  const threads = useQuery({ queryKey: ['agent-threads'], queryFn: fetchAgentThreads, refetchInterval: AGENT_THREADS_REFRESH_MS, retry: false })
  const names = useSeatNames()
  const model = useMemo(
    () => (contract.data ? nerveModel(contract.data, clients.data?.clients ?? [], nextgen.data) : null),
    [contract.data, clients.data, nextgen.data],
  )
  const roster = useMemo(() => new Map((clients.data?.clients ?? []).map(c => [c.name, c.status])), [clients.data])

  // The seats: every session with a heartbeat in the last day, and every actor the letters and cards name.
  const beat = useMemo(() => {
    const m = new Map<string, AgentThread>()
    const since = Date.now() - SEAT_HOURS * 3600_000
    for (const t of threads.data?.threads ?? []) {
      if (t.status === 'superseded' || Date.parse(t.at) < since) continue
      const id = seatId(t)
      const prev = m.get(id)
      if (!prev || prev.at < t.at) m.set(id, t)
    }
    return m
  }, [threads.data])
  const seats = useMemo(() => {
    const ids = new Set<string>(beat.keys())
    for (const s of team.data?.seats ?? []) ids.add(s.id)
    return [...ids].sort()
  }, [beat, team.data])
  const observed = useMemo(() => new Map((team.data?.seats ?? []).map(s => [s.id, s])), [team.data])

  // Which domain each topic tells of (the register's domain, else its prefix).
  const domainOfType = useMemo(() => new Map((contract.data?.topics ?? []).map(t => [t.type, t.domain || domainOf(t.type)])), [contract.data])
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [spin, setSpin] = useState(true)
  const [help, setHelp] = useState(false)
  // The default is who listens to whom, straight (the Owner, 2026-11-17); the old view through the bus stays behind "?".
  const [byBus, setByBus] = useState(false)
  const domains = useQuery({ queryKey: ['ci-domains', TIER_REPO], queryFn: () => fetchCiDomains(TIER_REPO), staleTime: 300_000, retry: false })
  const tiers = useMemo(() => (domains.data?.domains ?? []).map(d => ({ id: d.id, tier: d.tier })), [domains.data])
  // Only the readers that run here: head-release reads in PROD's workers alone, so on STG its topics are heard by nobody (11-18).
  const routes = useMemo((): Route[] => (contract.data ? routesOf(contract.data, clients.data?.env) : []), [contract.data, clients.data])
  const routesByTopic = useMemo(() => {
    const m = new Map<string, Route[]>()
    for (const r of routes) m.set(r.topic, [...(m.get(r.topic) ?? []), r])
    return m
  }, [routes])
  const haloTopics = useMemo(() => new Set([...halosOf(routes, domainOfType, team.data?.listening).values()].flatMap(h => h.topics)), [routes, domainOfType, team.data])
  // Seconds from each event to a reader's receipt, by arc, as this page saw them; the arcs' medians, refreshed at most every 10 seconds.
  const delays = useRef(new Map<string, number[]>())
  const [delayTick, setDelayTick] = useState(0)
  const lastDelayTick = useRef(0)
  const noteDelay = (arc: string, seconds: number) => {
    if (!(seconds >= 0)) return
    const xs = delays.current.get(arc) ?? []
    xs.push(seconds)
    if (xs.length > DELAYS_KEPT) xs.shift()
    delays.current.set(arc, xs)
    if (Date.now() - lastDelayTick.current > 10_000) { lastDelayTick.current = Date.now(); setDelayTick(t => t + 1) }
  }

  // Dust: every event of the last day, by the domain it tells of, read once when the page opens (no receipts asked), then the live ones.
  const [dust, setDust] = useState<{ type: string; time: string }[]>([])
  useEffect(() => {
    let cancelled = false
    fetchBusReplay(new Date(Date.now() - 24 * 3600_000).toISOString(), false)
      .then(({ events }) => { if (!cancelled) setDust(d => [...events.map(e => ({ type: e.type, time: e.time })), ...d]) })
      .catch(() => { /* no dust: the clouds keep their parts */ })
    return () => { cancelled = true }
  }, [])
  const dustByDomain = useMemo(() => {
    const m = new Map<string, number[]>()
    const now = Date.now()
    const seen = new Set<string>()
    for (const e of [...dust, ...stream.events.map(x => ({ type: x.type, time: x.time }))]) {
      const k = `${e.type}|${e.time}`
      if (seen.has(k)) continue
      seen.add(k)
      const age = (now - Date.parse(e.time)) / 3600_000
      if (!(age >= 0 && age <= 24)) continue
      const d = domainOfType.get(e.type) ?? domainOf(e.type)
      m.set(d, [...(m.get(d) ?? []), Math.round(age * 4) / 4])
    }
    return m
  }, [dust, stream.events, domainOfType])

  // The body's nodes and links, rebuilt when a read changes; the renderer keeps known nodes where they are.
  // By default each domain is one node; an expanded domain shows its topics and their publishers.
  const graph = useMemo((): { nodes: GNode[]; links: GLink[]; arcs?: GArc[]; funnel?: Funnel } | null => {
    if (!model) return null
    const input = { model, domainOfType, expanded, seats, beat, observed, teamLinks: team.data?.links ?? [], names, dustByDomain, listening: team.data?.listening, now: Date.now() }
    if (byBus) return buildBody(input)
    return buildDirectBody({ ...input, tiers, routes, roster, delays: delays.current })
    // eslint-disable-next-line react-hooks/exhaustive-deps -- delays is a ref; delayTick says when it changed
  }, [model, seats, beat, observed, team.data, names, domainOfType, expanded, dustByDomain, byBus, tiers, routes, roster, delayTick])

  const api = useRef<NerveApi | null>(null)
  // The latest model, for a replay that must not restart when a read refreshes it.
  const modelRef = useRef(model)
  modelRef.current = model
  const [view, setView] = useState<View>(startView)
  const [mode, setMode] = useState<Mode>({ kind: 'live' })
  const [dimCI, setDimCI] = useState(true)
  const [last, setLast] = useState<{ event: BusEnvelope; readers: string[] } | null>(null)
  const [replayNote, setReplayNote] = useState<string | null>(null)
  const [replayAt, setReplayAt] = useState<{ i: number; n: number; time: string } | null>(null)
  const pending = useRef(new Map<string, { topic: string; since: number; landed: boolean }>())
  /** The direct view's pulses waiting in the bus: event|reader → its arc and when the event was. */
  const waiting = useRef(new Map<string, { arc: string; time: string; since: number }>())
  const seenEvents = useRef(new Set<string>())
  const seenReceipts = useRef(new Set<string>())

  const faint = (type: string) => dimCI && type.startsWith('ci.')
  const fireEvent = (e: BusEnvelope) => {
    const a = api.current
    const model = modelRef.current
    if (!a || !model) return
    const d = domainOfType.get(e.type) ?? domainOf(e.type)
    const dom = `d:${d}`
    if (!byBus) {
      // Direct: the sender lights; each reader's pulse runs to its arc's middle and waits there for that reader's receipt.
      a.flash(dom)
      const rs = routesByTopic.get(e.type) ?? []
      for (const r of rs) {
        const key = `${e.id}|${r.reader}`
        const arc = arcId(d, r.domain)
        if (a.launch(key, arc, PULSE.event, faint(e.type))) waiting.current.set(key, { arc, time: e.time, since: Date.now() })
        // Its own reader (no arc: a ring inside the cloud): the domain lights again.
        else if (r.domain === d) window.setTimeout(() => a.flash(dom), SECOND_HOP_MS)
      }
      // Sent and nobody takes it: the sender's halo lights.
      if (rs.length === 0 && haloTopics.has(e.type)) a.halo(dom)
      return
    }
    if (expanded.has(d)) {
      a.pulse(`p:${model.publisherOf.get(e.type) ?? 'unknown'}`, dom, PULSE.event, faint(e.type))
      window.setTimeout(() => { a.pulse(dom, `t:${e.type}`, PULSE.event, faint(e.type)); a.flash(dom) }, SECOND_HOP_MS)
    } else {
      a.pulse(CORE, dom, PULSE.event, faint(e.type))
      window.setTimeout(() => a.flash(dom), SECOND_HOP_MS)
    }
    if (e.type === 'seat.mail.arrived') {
      const w = `s:${e.actor}`
      const r = `s:${e.subject}`
      // Folded, the letter is a pulse into its domain; open, it runs between the seats.
      if (!a.pulse(w, r, PULSE.letter) && !a.pulse(w, 'd:coordination', PULSE.letter)) a.pulse(CORE, 'd:coordination', PULSE.letter)
      window.setTimeout(() => a.flash(r), SECOND_HOP_MS * 2)
    }
  }

  // Direct: a receipt takes its waiting pulse on from the bus to the reader; the wait is timed for the arc's median.
  const arrive = (eventId: string, consumer: string, at: string) => {
    const key = `${eventId}|${consumer}`
    const w = waiting.current.get(key)
    if (!w) return false
    waiting.current.delete(key)
    api.current?.arrive(key)
    noteDelay(w.arc, (Date.parse(at) - Date.parse(w.time)) / 1000)
    return true
  }
  // Direct, live: a pulse whose receipt did not come in time turns red at the middle, and its arc's far half with it.
  useEffect(() => {
    if (byBus || mode.kind !== 'live') return
    const t = window.setInterval(() => {
      for (const [key, w] of waiting.current) {
        if (Date.now() - w.since < LATE_AFTER_S * 1000) continue
        waiting.current.delete(key)
        api.current?.fail(key)
      }
    }, 2000)
    return () => window.clearInterval(t)
  }, [byBus, mode.kind])

  // A receipt lands on its reader; with the bus's domain folded, on that domain.
  const land = (reader: string, dim: boolean) => {
    const a = api.current
    if (!a) return
    if (a.pulse(CORE, `r:${reader}`, PULSE.receipt, dim)) window.setTimeout(() => a.flash(`r:${reader}`), SECOND_HOP_MS)
    else a.pulse(CORE, 'd:foundation', PULSE.receipt, dim)
  }

  // Live: a new event leaves its publisher for its topic and waits there for receipts.
  useEffect(() => {
    if (mode.kind !== 'live' || !model) return
    for (const e of [...stream.events].reverse()) {
      if (seenEvents.current.has(e.id)) continue
      seenEvents.current.add(e.id)
      pending.current.set(e.id, { topic: e.type, since: Date.now(), landed: false })
      // The list keeps the newest BUS_LIST_CAP events; so does this.
      if (pending.current.size > BUS_LIST_CAP) pending.current.delete(pending.current.keys().next().value as string)
      fireEvent(e)
      setLast({ event: e, readers: [] })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- fire per new event only
  }, [stream.events, model, mode.kind])

  // Live: a receipt runs from the bus to the reader that took it, which lights up.
  useEffect(() => {
    if (mode.kind !== 'live' || !model) return
    for (const r of [...stream.receipts].reverse()) {
      const k = `${r.id}|${r.consumer}`
      if (seenReceipts.current.has(k)) continue
      seenReceipts.current.add(k)
      const reader = readerOf(r.consumer, model.readers)
      const p = pending.current.get(r.id)
      if (!reader || !p) continue
      p.landed = true
      if (!byBus) arrive(r.id, r.consumer, r.at)
      else land(reader, faint(p.topic))
      setLast(l => (l && l.event.id === r.id ? { ...l, readers: [...l.readers, reader] } : l))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- fire per new receipt only
  }, [stream.receipts, model, mode.kind])

  // Replay: the last day from the ledger at speed, each event's receipts landing after it as they did.
  useEffect(() => {
    if (mode.kind !== 'replay' || !modelRef.current) return
    let cancelled = false
    const timers: number[] = []
    setLast(null)
    setReplayAt(null)
    setReplayNote('reading the last 24 hours from the ledger…')
    fetchBusReplay(new Date(Date.now() - 24 * 3600_000).toISOString())
      .then(({ events, receipts, receiptsNote }) => {
        if (cancelled) return
        const plan = replaySchedule(events, mode.speed)
        const byEvent = new Map<string, typeof receipts>()
        for (const r of receipts) if (r.consumer !== OWN_CONSUMER) byEvent.set(r.id, [...(byEvent.get(r.id) ?? []), r])
        setReplayNote(`replay: ${events.length} events of the last 24h at ${mode.speed}×; ${receipts.length > 0 ? `${receipts.length} receipts replayed with them` : `receipts not replayed: ${receiptsNote ?? 'none found in the receipt stream for these events'}`}`)
        plan.forEach(({ at: t, event }, i) => {
          timers.push(window.setTimeout(() => { fireEvent(event); setLast({ event, readers: [] }); setReplayAt({ i: i + 1, n: plan.length, time: event.time }) }, t))
          // Direct: a reader whose receipt is not among the replayed ones turns its pulse red, once the others would have landed.
          if (!byBus && receipts.length > 0) {
            for (const rt of routesByTopic.get(event.type) ?? []) {
              if ((byEvent.get(event.id) ?? []).some(x => x.consumer === rt.reader)) continue
              const key = `${event.id}|${rt.reader}`
              timers.push(window.setTimeout(() => { if (waiting.current.delete(key)) api.current?.fail(key) }, t + 1600))
            }
          }
          for (const r of byEvent.get(event.id) ?? []) {
            const reader = readerOf(r.consumer, modelRef.current?.readers ?? [])
            if (!reader) continue
            const lag = Math.min(1500, Math.max(SECOND_HOP_MS, (Date.parse(r.at) - Date.parse(event.time)) / mode.speed))
            timers.push(window.setTimeout(() => {
              if (!byBus) arrive(event.id, r.consumer, r.at)
              else land(reader, faint(event.type))
              setLast(l => (l && l.event.id === event.id ? { ...l, readers: [...l.readers, reader] } : l))
            }, t + lag))
          }
        })
        const end = plan.length > 0 ? plan[plan.length - 1].at + 2 * SECOND_HOP_MS : 0
        timers.push(window.setTimeout(() => setReplayNote(`replay done: ${events.length} events`), end))
      })
      .catch(err => !cancelled && setReplayNote(`replay failed: ${err instanceof Error ? err.message : 'request failed'}`))
    return () => { cancelled = true; timers.forEach(clearTimeout) }
    // A replay starts once per press: the model refreshing every minute must not restart it (STG, 2026-11-17: it never got going).
    // eslint-disable-next-line react-hooks/exhaustive-deps -- a replay runs once per start
  }, [mode])

  const late = mode.kind === 'live'
    ? [...pending.current.values()].filter(p => !p.landed && Date.now() - p.since > LATE_AFTER_S * 1000).length
    : 0

  let content
  if (contract.isError) content = <p className="m-0 font-mono text-xs" style={{ color: C.red }}>{`contract unreadable: ${contract.error instanceof Error ? contract.error.message : 'request failed'}`}</p>
  else if (!model || !graph || !contract.data) content = <p className="m-0 font-mono text-xs" style={{ color: C.muted }}>loading…</p>
  else content = (
    <div className="flex w-full min-w-0 flex-col gap-2">
      <div className="flex flex-wrap items-center gap-1.5">
        <TermButton on={mode.kind === 'live'} onClick={() => { setMode({ kind: 'live' }); setReplayNote(null) }}>● live</TermButton>
        {SPEEDS.map(s => (
          <TermButton key={s} on={mode.kind === 'replay' && mode.speed === s} onClick={() => setMode({ kind: 'replay', speed: s })}>▶ 24h {s}×</TermButton>
        ))}
        <TermButton on={dimCI} onClick={() => setDimCI(d => !d)}>dim ci.*</TermButton>
        <TermButton on={spin} onClick={() => setSpin(v => !v)}>spin</TermButton>
        <TermButton on={false} onClick={() => api.current?.home()}>home</TermButton>
        <TermButton on={expanded.size > 0} onClick={() => setExpanded(e => (e.size > 0 ? new Set() : new Set(domainOfType.values())))}>{expanded.size > 0 ? 'fold domains' : 'open all domains'}</TermButton>
        <span className="mx-1 font-mono text-xs" style={{ color: C.rule }}>|</span>
        {(['3d', '2d', 'list'] as const).map(v => <TermButton key={v} on={view === v} onClick={() => setView(v)}>{v}</TermButton>)}
        {late > 0 && <span className="font-mono text-xs" style={{ color: C.amber }}>{late} events waiting over {LATE_AFTER_S}s for a receipt</span>}
        {stream.receiptsOff && <span className="font-mono text-xs" style={{ color: C.amber }}>receipts off on this gate</span>}
        <span className="ml-auto" />
        <TermButton on={help} onClick={() => setHelp(h => !h)}>?</TermButton>
      </div>
      <p className="m-0 min-h-5 truncate font-mono text-xs" data-testid="bus-nerve-last" style={{ color: C.text }}>
        <span style={{ color: C.muted }}>$ </span>
        {last
          ? <button type="button" className="cursor-pointer hover:underline" onClick={() => onSelect(last.event.id)}>
              {clockTime(last.event.time)} {last.event.type} {last.event.subject} → {last.readers.length > 0 ? last.readers.join(', ') : mode.kind === 'live' ? 'awaiting receipt' : 'topic'}
            </button>
          : <span style={{ color: C.muted }}>{mode.kind === 'live' ? 'no event since this page opened' : replayNote}</span>}
      </p>
      {mode.kind === 'live' && team.data?.rate && (
        <p className="m-0 font-mono text-xs" data-testid="bus-nerve-rate" style={{ color: C.muted }}>
          <span style={{ color: C.text }}>{team.data.rate.per_minute}/min</span> · {team.data.rate.events} in 1h
          {team.data.listening?.heard_share != null && <> · <span style={{ color: C.text }}>{Math.round(team.data.listening.heard_share * 100)}%</span> heard</>}
          {team.data.listening?.closed_share != null && <> · <span style={{ color: C.text }}>{Math.round(team.data.listening.closed_share * 100)}%</span> loops closed</>}
          {team.data.listening?.caused_share != null && <> · <span style={{ color: C.text }}>{Math.round(team.data.listening.caused_share * 100)}%</span> with a cause</>}
          {Object.keys(team.data.rate.by_type).length > 0 ? ` · ${Object.entries(team.data.rate.by_type).sort((a, b) => b[1] - a[1]).slice(0, 3).map(([t, n]) => `${t} ${n}`).join(' · ')}` : ''}
        </p>
      )}
      {mode.kind === 'replay' && (
        <p className="m-0 font-mono text-xs" data-testid="bus-nerve-replay" style={{ color: C.green }}>
          ▶ replay {mode.speed}× {replayAt ? `· ${clockTime(replayAt.time)} · ${replayAt.i}/${replayAt.n}` : ''} <span style={{ color: C.muted }}>{replayNote}</span>
        </p>
      )}
      <div className="relative w-full min-w-0">
      {help && <HelpPanel byBus={byBus} onByBus={() => setByBus(b => !b)}
        unmeasured={[
          ...(team.data?.unmeasured ?? []),
          ...(team.data?.listening?.notes ?? []).map(n => ({ what: n.split(':')[0], why: n.includes(':') ? n.slice(n.indexOf(':') + 1).trim() : '' })),
          ...(!byBus && domains.isError ? [{ what: 'tiers', why: `the ownership table could not be read (${domains.error instanceof Error ? domains.error.message : 'request failed'}); every domain is untiered, at one height` }] : []),
          ...(!byBus && domains.data && tiers.every(t => t.tier == null) ? [{ what: 'tiers', why: 'the ownership table this platform serves declares no tiers yet; every domain is untiered, at one height' }] : []),
          ...(!byBus ? [{ what: 'red near half', why: 'a sender that failed to publish is not reported to this page yet; the arc\'s near half is never red' }] : []),
        ]} onClose={() => setHelp(false)} />}
      {view === 'list'
        ? <div className="rounded-sm bg-[var(--card-fill)] p-2"><ContractGraph contract={contract.data} roster={roster} /></div>
        : (
          <Suspense fallback={<p className="m-0 font-mono text-xs" style={{ color: C.muted }}>loading the body…</p>}>
            <BusNerveGraph nodes={graph.nodes} links={graph.links} arcs={graph.arcs} funnel={graph.funnel} flat={view === '2d'} spin={spin} onReady={a => { api.current = a }}
              onNodeClick={id => {
                if (!id.startsWith('d:')) return
                const d = id.slice(2)
                // Opening a cloud flies into it; folding it goes back to the whole body.
                if (expanded.has(d)) api.current?.home()
                else api.current?.focus(id)
                setExpanded(e => { const n = new Set(e); if (n.has(d)) n.delete(d); else n.add(d); return n })
              }} />
          </Suspense>
        )}
      </div>
      {team.isError && <p className="m-0 font-mono text-[11px]" style={{ color: C.red }}>team readings unreadable: {team.error instanceof Error ? team.error.message : 'request failed'}</p>}
    </div>
  )
  return (
    <OpsSection title="Nerves" bodyPadding="compact">
      <div className="w-full min-w-0 rounded-sm p-2" style={{ background: C.ground, color: C.text }}>{content}</div>
    </OpsSection>
  )
}

/** A legend row: a mark and two or three words. */
function Key({ mark, children }: { mark: ReactNode; children: ReactNode }) {
  return <li className="flex items-center gap-2"><span className="inline-flex w-4 justify-center">{mark}</span><span>{children}</span></li>
}
const dot = (color: string, ring = false) => (
  <span className="inline-block size-2 rounded-full" style={ring ? { border: `1px solid ${color}` } : { background: color, boxShadow: `0 0 4px ${color}` }} />
)

/**
 * Everything that explains the map, kept off the page until asked (the Owner,
 * 2026-11-17: explanations go behind a "?"): the legend, how to move it, and
 * what is not measured and why.
 */
function HelpPanel({ unmeasured, onClose, byBus, onByBus }: { unmeasured: { what: string; why: string }[]; onClose: () => void; byBus: boolean; onByBus: () => void }) {
  return (
    <div className="absolute top-2 right-2 z-10 max-w-80 rounded-sm p-2 font-mono text-[11px]" role="dialog" aria-label="Nerve map help"
      style={{ background: 'rgba(4,7,10,0.92)', border: `1px solid ${C.rule}`, color: C.text }}>
      <div className="mb-1 flex items-center justify-between" style={{ color: C.muted }}>
        <span>┌ legend</span>
        <button type="button" className="cursor-pointer" onClick={onClose} aria-label="Close help">×</button>
      </div>
      <ul className="m-0 flex list-none flex-col gap-0.5 p-0">
        <Key mark={<span style={{ color: '#ffc46b' }}>✦</span>}>cloud = a domain</Key>
        {!byBus && (
          <>
            <Key mark={<span style={{ color: '#c9a38a' }}>◠</span>}>funnel = the body opening out from its core, the base</Key>
            <Key mark={<span style={{ color: '#c9a38a' }}>◯</span>}>brighter ring on it = a tier: 0 base and compute, 1 coordination, 2 the work (verification, delivery, lineage, tenant), 3 plan and the owner</Key>
            <Key mark={<span style={{ color: '#ffc46b' }}>⌒</span>}>arc = one domain's topics taken by another's reader</Key>
            <Key mark={dot('#ffc46b')}>pulse waits mid-arc = in the bus, until the receipt</Key>
            <Key mark={<span style={{ color: C.red }}>⌒</span>}>far half red = no receipt in {LATE_AFTER_S}s</Key>
            <Key mark={<span style={{ color: C.grey }}>⌒</span>}>far half grey = reader lost</Key>
            <Key mark={dot(C.amber, true)}>halo = sends what nobody takes</Key>
          </>
        )}
        <Key mark={dot(PULSE.event)}>event</Key>
        <Key mark={dot(PULSE.receipt)}>receipt</Key>
        <Key mark={dot(PULSE.letter)}>seat letter</Key>
        <Key mark={<span style={{ color: C.amber }}>┄</span>}>card to owner</Key>
        <Key mark={dot(C.red, true)}>waiting &gt;{STUCK_AFTER_MIN}m</Key>
        <Key mark={dot(C.grey)}>old road · down</Key>
        <Key mark={dot(C.amber, true)}>no one listening</Key>
      </ul>
      <p className="m-0 mt-2 mb-1" style={{ color: C.muted }}>┌ move</p>
      <ul className="m-0 flex list-none flex-col gap-0.5 p-0">
        <li>drag: turn · wheel: zoom</li>
        <li>click a cloud: open · again: fold</li>
        <li>hover: readings</li>
        <li>home: back to where it was</li>
        {!byBus && <li>hover an arc's middle: its topics, readers and median wait</li>}
      </ul>
      <p className="m-0 mt-2 mb-1" style={{ color: C.muted }}>┌ view</p>
      <button type="button" className="cursor-pointer underline" onClick={onByBus}>{byBus ? 'back to who listens to whom' : 'view by bus (every line through the bus)'}</button>
      {unmeasured.length > 0 && (
        <>
          <p className="m-0 mt-2 mb-1" style={{ color: C.muted }}>┌ {unmeasured.length} not measured</p>
          <ul className="m-0 flex list-none flex-col gap-0.5 p-0">
            {unmeasured.map(u => <li key={u.what}><span style={{ color: C.amber }}>{u.what}</span> · {u.why}</li>)}
          </ul>
        </>
      )}
      <p className="m-0 mt-2" style={{ color: C.muted }}>each subscription's backlog and the nightly reconcile are not wired yet (they come with the bus health readings) · heard: an event someone took (a receipt from anyone but this page), as a share of the events of every topic but the display-only ones (published to be watched, like ci.run.started; the register marks them) · loops closed: an event something did next names as its cause · with a cause: an event that names the event it answers · dust: each event of the last day, dimmer with age · seats breathe with their heartbeat; quiet {QUIET_AFTER_H}h shows on hover · the Console's own receipt ({OWN_CONSUMER}) is not drawn</p>
    </div>
  )
}

function TermPanel({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="min-w-0 rounded-sm p-2 font-mono text-xs" style={{ background: C.ground, border: `1px solid ${C.rule}`, color: C.text }}>
      <p className="m-0 mb-1" style={{ color: C.muted }}>{`┌ ${title}`}</p>
      {children}
    </div>
  )
}

/** The panels under the map: seats' letters, the newest events (each opens its journey), and what is not wired yet. */
export function BusNervePanels({ stream, onSelect }: { stream: BusStreamState; onSelect: (id: string) => void }) {
  const names = useSeatNames()
  // The topics that published and nobody took, most first: what the change of generation must decide (drop the publisher, or add a reader).
  const team = useQuery({ queryKey: ['bus-observe'], queryFn: fetchBusObserve, refetchInterval: 60_000, retry: false })
  const contract = useQuery({ queryKey: ['bus-contract'], queryFn: fetchBusContract, staleTime: 300_000, retry: false })
  const publisherOf = new Map((contract.data?.topics ?? []).map(t => [t.type, t.publisher]))
  const deaf = (team.data?.listening?.topics ?? []).filter(t => isDeaf(t)).sort((a, b) => b.published - a.published)
  const mail = seatMail(stream.events).slice(0, 8)
  const recent = stream.events.slice(0, 8)
  const row = (e: BusEnvelope) => (
    <li key={e.id}>
      <button type="button" onClick={() => onSelect(e.id)} className="w-full min-w-0 cursor-pointer truncate text-left hover:underline" title={`${e.type} · ${e.subject}`}>
        <span style={{ color: C.muted }}>{clockTime(e.time)}</span> {e.type === 'seat.mail.arrived' ? `${nameOf(e.actor, names)} → ${nameOf(e.subject, names)}` : `${e.type} ${e.subject}`}
      </button>
    </li>
  )
  return (
    <div className="grid min-w-0 gap-2 md:grid-cols-3">
      <TermPanel title="seat mail">
        {mail.length === 0 ? <p className="m-0" style={{ color: C.muted }}>no letter since this page opened</p> : <ul className="m-0 flex list-none flex-col gap-0.5 p-0">{mail.map(row)}</ul>}
      </TermPanel>
      <TermPanel title="ledger">
        {recent.length === 0 ? <p className="m-0" style={{ color: C.muted }}>no event since this page opened</p> : <ul className="m-0 flex list-none flex-col gap-0.5 p-0">{recent.map(row)}</ul>}
      </TermPanel>
      <TermPanel title={`no one listening · ${team.data?.listening?.hours ?? 24}h`}>
        {deaf.length === 0
          ? <p className="m-0" style={{ color: C.muted }}>—</p>
          : (
            <ul className="m-0 flex list-none flex-col gap-0.5 p-0">
              {deaf.map(t => (
                <li key={t.type} className="flex min-w-0 justify-between gap-2" title={`${t.type} · published by ${publisherOf.get(t.type) ?? '?'} · ${t.published} events, none taken`}>
                  <span className="min-w-0 truncate">{t.type}</span>
                  <span className="shrink-0" style={{ color: C.muted }}>{publisherOf.get(t.type) ?? '?'} · <span style={{ color: C.amber }}>{t.published}</span></span>
                </li>
              ))}
            </ul>
          )}
      </TermPanel>
    </div>
  )
}
