import { Suspense, lazy, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { OpsSection } from '@/components/layout/OpsSection'
import { AGENT_THREADS_REFRESH_MS, fetchAgentThreads, type AgentThread, type AgentThreadStatus } from '@/api/agentThreads'
import { fetchLineage } from '@/api/lineage'
import { fetchBusContract, fetchBusNextgen } from './busNextgen'
import { BUS_LIST_CAP, fetchBusClients, fetchBusReplay, type BusStreamState } from './useBusStream'
import { clockTime, type BusEnvelope } from './busTrace'
import {
  LATE_AFTER_S, OWN_CONSUMER, QUIET_AFTER_H, SPEEDS, STUCK_AFTER_MIN, fetchBusObserve, nerveModel, readerOf, replaySchedule,
  seatLabel, seatMail, seatState, shortSeconds, warmColor,
  type ObserveSeat, type OrganTone,
} from './busNerve'
import { CORE, domainOf } from './busNerveLayout'
import { ContractGraph } from './BusContractSection'
import type { GLink, GNode, NerveApi } from './BusNerveGraph'

// Its own chunk: three.js and the graph libraries load only when the Bus page opens.
const BusNerveGraph = lazy(() => import('./BusNerveGraph'))

/**
 * The observatory's own palette: a dark glass the body lives in, whatever the
 * Console's theme (the Owner's reference, 2026-11-17: a black ground, terminal
 * panels, a living picture in the middle). Fixed on purpose, not theme tokens.
 */
const C = {
  ground: '#020305',
  rule: '#14232c',
  text: '#c7f9d6',
  muted: '#5f7f72',
  blue: '#4cc3ff',
  green: '#39ff8f',
  red: '#ff4d5e',
  amber: '#ffb347',
  grey: '#3b4a52',
  violet: '#b48cff',
  link: '#2a4a5a',
} as const

/** The particles, in the same warm range: an event gold, a receipt white, a seat's letter pink. */
const PULSE = { event: '#ffc46b', receipt: '#ffffff', letter: '#ff8fc7' } as const

const TONE: Record<OrganTone, string> = { live: '#ffd0a8', lost: C.grey, new: C.amber, old: C.grey, plain: C.muted }

/** A seat's breath, from its session's own heartbeat (GET /agent/threads): fast in a turn, slow idle, none when down. */
const BREATH: Record<AgentThreadStatus | 'none', { color: string; dur: number | null; label: string }> = {
  in_turn: { color: '#fff1c9', dur: 1.1, label: 'in a turn' },
  waiting_owner: { color: '#ffb347', dur: 2.2, label: 'waiting for the owner' },
  idle: { color: '#ff9a6b', dur: 4.5, label: 'idle' },
  silent: { color: C.grey, dur: null, label: 'silent mid-turn' },
  host_lost: { color: C.grey, dur: null, label: 'host lost' },
  superseded: { color: C.grey, dur: null, label: 'superseded' },
  none: { color: C.grey, dur: null, label: 'no heartbeat' },
}

/** Hours a session's heartbeat keeps it on the body. */
const SEAT_HOURS = 24
/** Milliseconds from a domain to its topic after the pulse left the publisher. */
const SECOND_HOP_MS = 700

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

function nameOf(id: string, names: Map<string, string>): string {
  return names.get(id) ?? seatLabel(id)
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
  const graph = useMemo((): { nodes: GNode[]; links: GLink[] } | null => {
    if (!model) return null
    const wall = Date.now()
    const nodes: GNode[] = [{ id: CORE, kind: 'core', label: 'bus', color: '#ffe6cc', size: 3, breath: 6, note: 'the bus: ledger and stream' }]
    const links: GLink[] = []
    const byDomain = new Map<string, string[]>()
    for (const t of model.topics) {
      const d = domainOfType.get(t.id) ?? domainOf(t.id)
      byDomain.set(d, [...(byDomain.get(d) ?? []), t.id])
    }
    // Who else lives in a domain: seats in coordination, the owner in owner, the bus's readers and old roads in foundation.
    const seatDomain = (id: string) => (id === 'owner' ? 'owner' : 'coordination')
    const members = new Map<string, number>()
    for (const id of seats) members.set(seatDomain(id), (members.get(seatDomain(id)) ?? 0) + 1)
    members.set('foundation', (members.get('foundation') ?? 0) + model.readers.length)
    for (const d of members.keys()) if (!byDomain.has(d)) byDomain.set(d, [])
    const pubs = new Set<string>()
    for (const [d, types] of [...byDomain.entries()].sort()) {
      const open = expanded.has(d)
      const more = members.get(d) ?? 0
      const hue = warmColor(d)
      const ages = dustByDomain.get(d) ?? []
      nodes.push({ id: `d:${d}`, kind: 'cluster', label: open ? `${d} ▾` : d, color: hue, size: 3, breath: null,
        cloud: { parts: types.length + more, dust: ages },
        note: `domain ${d}: ${types.length} topics${more > 0 ? `, ${more} more inside (seats, readers, old roads)` : ''}, ${ages.length} events in the last day · click to ${open ? 'fold' : 'open'}` })
      links.push({ source: `d:${d}`, target: CORE, color: '#ffd9c2', width: 0.4 })
      if (!open) continue
      for (const t of types) {
        nodes.push({ id: `t:${t}`, kind: 'topic', label: t, color: warmColor(d, 0.78), size: 0.9, breath: null, note: t })
        links.push({ source: `t:${t}`, target: `d:${d}`, color: warmColor(d, 0.5), width: 0.3 })
        const p = model.publisherOf.get(t) ?? 'unknown'
        if (!pubs.has(p)) {
          pubs.add(p)
          nodes.push({ id: `p:${p}`, kind: 'publisher', label: p, color: '#ffe6cc', size: 1.6, breath: null, note: `publisher ${p}` })
          links.push({ source: `p:${p}`, target: CORE, color: C.link, width: 0.5 })
        }
        if (!links.some(l => l.source === `p:${p}` && l.target === `d:${d}`)) links.push({ source: `p:${p}`, target: `d:${d}`, color: C.link, width: 0.5 })
      }
    }
    for (const r of expanded.has('foundation') ? model.readers : []) {
      if (r.tone === 'old') {
        nodes.push({ id: `o:${r.label}`, kind: 'old', label: r.label, color: C.grey, size: 1.4, breath: null, note: `old road, not on the bus yet: ${r.note ?? r.label}` })
        links.push({ source: `o:${r.label}`, target: 'd:foundation', color: C.grey, dashed: true, width: 0.4 })
        continue
      }
      nodes.push({ id: `r:${r.id}`, kind: 'reader', label: r.label.replace(/^gate:events:/, ''), color: TONE[r.tone], size: 2.3, breath: null,
        note: `reader ${r.id} · ${r.tone}` })
      links.push({ source: `r:${r.id}`, target: CORE, color: C.link, width: 0.6 })
    }
    for (const id of seats.filter(i => expanded.has(seatDomain(i)))) {
      const t = beat.get(id)
      const br = BREATH[t?.status ?? 'none']
      const o: ObserveSeat | undefined = observed.get(id)
      const st = o ? seatState(o, team.data?.links ?? [], wall) : 'ok'
      const read = [
        o && o.letters_in > 0 ? `in ${o.letters_in}` : '',
        o?.ack_p50_s != null ? `ack ${shortSeconds(o.ack_p50_s)}` : '',
        o && o.unacked > 0 ? `${o.unacked} open` : '',
        o?.cards_open ? `${o.cards_open} cards open` : '',
        o?.card_wait_p50_s != null ? `cards wait ${shortSeconds(o.card_wait_p50_s)}` : '',
        t && t.status !== 'in_turn' && t.quiet_seconds > 0 ? `quiet ${shortSeconds(t.quiet_seconds)}` : '',
      ].filter(Boolean).join(' · ')
      const name = nameOf(id, names)
      const owner = id === 'owner'
      nodes.push({
        id: `s:${id}`, kind: 'seat', label: name.length > 28 ? `${name.slice(0, 27)}…` : name, color: owner ? '#ff8fc7' : br.color, size: owner ? 2.4 : 1.6,
        breath: owner ? null : br.dur, ring: st === 'stuck' ? C.red : undefined,
        note: `${name} (${id}) · ${owner ? 'the owner' : br.label}${read ? ` · ${read}` : ''}${st === 'quiet' ? ` · no report for ${QUIET_AFTER_H}h` : ''}${st === 'stuck' ? ` · something waits over ${STUCK_AFTER_MIN}m` : ''}`,
      })
      links.push({ source: `s:${id}`, target: `d:${seatDomain(id)}`, color: C.link, width: 0.5 })
    }
    const ids = new Set(nodes.map(n => n.id))
    for (const l of team.data?.links ?? []) {
      const a = l.from === 'seats' ? CORE : `s:${l.from}`
      const b = `s:${l.to}`
      if (!ids.has(a) || !ids.has(b) || a === b) continue
      const openMin = l.oldest_open ? (wall - Date.parse(l.oldest_open)) / 60_000 : 0
      const stuck = l.unacked > 0 && openMin > STUCK_AFTER_MIN
      links.push({
        source: a, target: b, color: stuck ? C.red : l.kind === 'card' ? C.amber : PULSE.letter, dashed: l.kind === 'card',
        width: stuck ? 0.6 : Math.min(2.5, 0.6 + Math.log2(1 + l.count) * 0.5),
        label: `${nameOf(l.from, names)} → ${nameOf(l.to, names)}: ${l.count} ${l.kind === 'card' ? 'cards' : 'letters'}${l.ack_p50_s != null ? ` · ${l.kind === 'card' ? 'wait' : 'ack'} ${shortSeconds(l.ack_p50_s)}` : ''}${l.unacked > 0 ? ` · ${l.unacked} open${stuck ? ` ${Math.round(openMin)}m` : ''}` : ''}`,
      })
    }
    return { nodes, links }
  }, [model, seats, beat, observed, team.data, names, domainOfType, expanded, dustByDomain])

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
  const seenEvents = useRef(new Set<string>())
  const seenReceipts = useRef(new Set<string>())

  const faint = (type: string) => dimCI && type.startsWith('ci.')
  const fireEvent = (e: BusEnvelope) => {
    const a = api.current
    const model = modelRef.current
    if (!a || !model) return
    const d = domainOfType.get(e.type) ?? domainOf(e.type)
    const dom = `d:${d}`
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
      land(reader, faint(p.topic))
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
          for (const r of byEvent.get(event.id) ?? []) {
            const reader = readerOf(r.consumer, modelRef.current?.readers ?? [])
            if (!reader) continue
            const lag = Math.min(1500, Math.max(SECOND_HOP_MS, (Date.parse(r.at) - Date.parse(event.time)) / mode.speed))
            timers.push(window.setTimeout(() => {
              land(reader, faint(event.type))
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
        <TermButton on={expanded.size > 0} onClick={() => setExpanded(e => (e.size > 0 ? new Set() : new Set(domainOfType.values())))}>{expanded.size > 0 ? 'fold domains' : 'open all domains'}</TermButton>
        <span className="mx-1 font-mono text-xs" style={{ color: C.rule }}>|</span>
        {(['3d', '2d', 'list'] as const).map(v => <TermButton key={v} on={view === v} onClick={() => setView(v)}>{v}</TermButton>)}
        {late > 0 && <span className="font-mono text-xs" style={{ color: C.amber }}>{late} events waiting over {LATE_AFTER_S}s for a receipt</span>}
        {stream.receiptsOff && <span className="font-mono text-xs" style={{ color: C.amber }}>receipts off on this gate</span>}
      </div>
      <p className="m-0 min-h-5 truncate font-mono text-xs" data-testid="bus-nerve-last" style={{ color: C.text }}>
        <span style={{ color: C.muted }}>$ </span>
        {last
          ? <button type="button" className="cursor-pointer hover:underline" onClick={() => onSelect(last.event.id)}>
              {clockTime(last.event.time)} {last.event.type} {last.event.subject} → {last.readers.length > 0 ? last.readers.join(', ') : mode.kind === 'live' ? 'awaiting receipt' : 'topic'}
            </button>
          : <span style={{ color: C.muted }}>{mode.kind === 'live' ? 'no event since this page opened · seats breathe with their own heartbeat · ▶ replays the last day' : replayNote}</span>}
      </p>
      {mode.kind === 'live' && team.data?.rate && (
        <p className="m-0 font-mono text-xs" data-testid="bus-nerve-rate" style={{ color: C.muted }}>
          bus now: <span style={{ color: C.text }}>{team.data.rate.per_minute}/min</span> over the last hour ({team.data.rate.events} events
          {Object.keys(team.data.rate.by_type).length > 0 ? `; most ${Object.entries(team.data.rate.by_type).sort((a, b) => b[1] - a[1]).slice(0, 3).map(([t, n]) => `${t} ${n}`).join(', ')}` : ''})
          {' '}· letters between seats mostly go through Desktop sessions, not the bus, so the team's nerves are mostly not on it yet
        </p>
      )}
      {mode.kind === 'replay' && (
        <p className="m-0 font-mono text-xs" data-testid="bus-nerve-replay" style={{ color: C.green }}>
          ▶ replay {mode.speed}× {replayAt ? `· ${clockTime(replayAt.time)} · ${replayAt.i}/${replayAt.n}` : ''} <span style={{ color: C.muted }}>{replayNote}</span>
        </p>
      )}
      {view === 'list'
        ? <div className="rounded-sm bg-[var(--card-fill)] p-2"><ContractGraph contract={contract.data} roster={roster} /></div>
        : (
          <Suspense fallback={<p className="m-0 font-mono text-xs" style={{ color: C.muted }}>loading the body…</p>}>
            <BusNerveGraph nodes={graph.nodes} links={graph.links} flat={view === '2d'} spin={spin} onReady={a => { api.current = a }}
              onNodeClick={id => {
                if (!id.startsWith('d:')) return
                const d = id.slice(2)
                setExpanded(e => { const n = new Set(e); if (n.has(d)) n.delete(d); else n.add(d); return n })
              }} />
          </Suspense>
        )}
      <p className="m-0 font-mono text-[11px]" style={{ color: C.muted }}>
        each domain is a cloud: a light for each of its parts, a dust point for each event of the last day, dimmer with age · gold particle: an event · white: a reader's receipt, the reader lights up · pink: a letter between seats · amber dashed: cards to the owner · seats breathe with their session's heartbeat (fast in a turn, slow idle, still and grey when down; a lost reader is grey too) · thin red ring: a letter or card unanswered over {STUCK_AFTER_MIN}m (the only red) · a seat quiet for {QUIET_AFTER_H}h says so on hover · the map opens at the domains: seats live in coordination, the owner in owner, the bus's readers and old roads in foundation · grey: old roads not on the bus yet · hover a node or link for its readings · drag the background to turn the body, wheel or pinch to zoom, drag a node to move it · click a domain to open or fold it · the Console's own receipt ({OWN_CONSUMER}) is not drawn
      </p>
      {team.data && team.data.unmeasured.length > 0 && (
        <ul className="m-0 flex list-none flex-col gap-0.5 p-0 font-mono text-[11px]" style={{ color: C.muted }}>
          {team.data.unmeasured.map(u => <li key={u.what}><span style={{ color: C.amber }}>not measured</span> {u.what}: {u.why}</li>)}
        </ul>
      )}
      {team.isError && <p className="m-0 font-mono text-[11px]" style={{ color: C.red }}>team readings unreadable: {team.error instanceof Error ? team.error.message : 'request failed'}</p>}
    </div>
  )
  return (
    <OpsSection title="Nerves" bodyPadding="compact">
      <div className="w-full min-w-0 rounded-sm p-2" style={{ background: C.ground, color: C.text }}>{content}</div>
    </OpsSection>
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
      <TermPanel title="backlog · reconcile">
        <p className="m-0" style={{ color: C.muted }}>not wired yet: each subscription's backlog and the nightly reconcile come with the bus health readings; nothing is shown until they are measured</p>
      </TermPanel>
    </div>
  )
}
