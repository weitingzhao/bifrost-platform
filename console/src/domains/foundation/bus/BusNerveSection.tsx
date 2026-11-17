import { Suspense, lazy, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { OpsSection } from '@/components/layout/OpsSection'
import { AGENT_THREADS_REFRESH_MS, fetchAgentThreads, type AgentThread, type AgentThreadStatus } from '@/api/agentThreads'
import { fetchBusContract, fetchBusNextgen } from './busNextgen'
import { BUS_LIST_CAP, fetchBusClients, fetchBusReplay, type BusStreamState } from './useBusStream'
import { clockTime, type BusEnvelope } from './busTrace'
import {
  LATE_AFTER_S, OWN_CONSUMER, QUIET_AFTER_H, SPEEDS, STUCK_AFTER_MIN, fetchBusObserve, nerveModel, readerOf, replaySchedule,
  seatLabel, seatMail, seatState, shortSeconds,
  type ObserveSeat, type OrganTone,
} from './busNerve'
import { CORE, bodyGraph, domainOf } from './busNerveLayout'
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
  ground: '#04070a',
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

const TONE: Record<OrganTone, string> = { live: C.green, lost: C.red, new: C.amber, old: C.grey, plain: C.muted }

/** A seat's breath, from its session's own heartbeat (GET /agent/threads): fast in a turn, slow idle, none when down. */
const BREATH: Record<AgentThreadStatus | 'none', { color: string; dur: number | null; label: string }> = {
  in_turn: { color: C.green, dur: 1.1, label: 'in a turn' },
  waiting_owner: { color: C.amber, dur: 2.2, label: 'waiting for the owner' },
  idle: { color: C.green, dur: 4.5, label: 'idle' },
  silent: { color: C.red, dur: null, label: 'silent mid-turn' },
  host_lost: { color: C.red, dur: null, label: 'host lost' },
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

/** Seat names, read: each session's title as its heartbeat reports it (the thread titles the seats' hooks send). */
function useSeatNames(): Map<string, string> {
  const threads = useQuery({ queryKey: ['agent-threads'], queryFn: fetchAgentThreads, refetchInterval: AGENT_THREADS_REFRESH_MS, retry: false })
  return useMemo(() => {
    const m = new Map<string, string>()
    for (const t of threads.data?.threads ?? []) if (t.title?.trim()) m.set(seatId(t), t.title.trim())
    return m
  }, [threads.data])
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

  // The body's nodes and links, rebuilt when a read changes; the renderer keeps known nodes where they are.
  const graph = useMemo((): { nodes: GNode[]; links: GLink[] } | null => {
    if (!model) return null
    const wall = Date.now()
    const g = bodyGraph({
      publishers: model.publishers.map(p => p.id),
      topics: model.topics.map(t => ({ type: t.id, publisher: model.publisherOf.get(t.id) ?? 'unknown' })),
      readers: model.readers.filter(r => r.tone !== 'old').map(r => r.id),
      seats,
      old: model.readers.filter(r => r.tone === 'old').map(r => r.label),
      letters: [],
    })
    const tone = new Map(model.readers.map(r => [r.id, r.tone]))
    const domains = new Map<string, number>()
    for (const t of model.topics) domains.set(domainOf(t.id), (domains.get(domainOf(t.id)) ?? 0) + 1)
    const nodes: GNode[] = g.nodes.map(n => {
      switch (n.kind) {
        case 'core': return { id: n.id, kind: n.kind, label: 'bus', color: C.blue, size: 9, breath: 6, note: 'the bus: ledger and stream' }
        case 'publisher': return { id: n.id, kind: n.kind, label: n.label, color: C.blue, size: 5, breath: 6, note: `publisher ${n.label}` }
        case 'cluster': return { id: n.id, kind: n.kind, label: `${n.label}.*`, color: C.blue, size: 3 + Math.sqrt(domains.get(n.label) ?? 1) * 1.4, breath: 5, note: `domain ${n.label}: ${domains.get(n.label) ?? 0} topics` }
        case 'topic': return { id: n.id, kind: n.kind, label: n.label, color: '#2b7fb0', size: 1.4, breath: null, note: n.label }
        case 'reader': {
          const t = tone.get(n.label) ?? 'plain'
          return { id: n.id, kind: n.kind, label: n.label.replace(/^gate:events:/, ''), color: TONE[t], size: 4, breath: t === 'lost' ? null : 3, ring: t === 'lost' ? C.red : undefined, note: `reader ${n.label} · ${t}` }
        }
        case 'seat': {
          const t = beat.get(n.label)
          const br = BREATH[t?.status ?? 'none']
          const o: ObserveSeat | undefined = observed.get(n.label)
          const st = o ? seatState(o, team.data?.links ?? [], wall) : 'ok'
          const read = [
            o && o.letters_in > 0 ? `in ${o.letters_in}` : '',
            o?.ack_p50_s != null ? `ack ${shortSeconds(o.ack_p50_s)}` : '',
            o && o.unacked > 0 ? `${o.unacked} open` : '',
            o?.cards_open ? `${o.cards_open} cards open` : '',
            o?.card_wait_p50_s != null ? `cards wait ${shortSeconds(o.card_wait_p50_s)}` : '',
            t && t.status !== 'in_turn' && t.quiet_seconds > 0 ? `quiet ${shortSeconds(t.quiet_seconds)}` : '',
          ].filter(Boolean).join(' · ')
          const name = nameOf(n.label, names)
          const owner = n.label === 'owner'
          return {
            id: n.id, kind: n.kind, label: name.length > 26 ? `${name.slice(0, 25)}…` : name, color: owner ? C.violet : br.color, size: owner ? 6 : 4.5,
            breath: owner ? 4 : br.dur, ring: st === 'stuck' ? C.red : st === 'quiet' ? C.amber : undefined,
            note: `${name} (${n.label}) · ${owner ? 'the owner' : br.label}${read ? ` · ${read}` : ''}${st === 'quiet' ? ` · no report for ${QUIET_AFTER_H}h` : ''}${st === 'stuck' ? ` · something waits over ${STUCK_AFTER_MIN}m` : ''}`,
          }
        }
        default: return { id: n.id, kind: 'old', label: n.label, color: C.grey, size: 2.5, breath: null, note: `old road, not on the bus yet: ${n.label}` }
      }
    })
    const links: GLink[] = g.links.map(l => ({
      source: l.a, target: l.b,
      color: l.a.startsWith('o:') ? C.grey : C.link,
      dashed: l.a.startsWith('o:'),
      width: l.a.startsWith('t:') ? 0.3 : 0.7,
    }))
    const ids = new Set(nodes.map(n => n.id))
    for (const l of team.data?.links ?? []) {
      const a = l.from === 'seats' ? CORE : `s:${l.from}`
      const b = `s:${l.to}`
      if (!ids.has(a) || !ids.has(b) || a === b) continue
      const openMin = l.oldest_open ? (wall - Date.parse(l.oldest_open)) / 60_000 : 0
      const stuck = l.unacked > 0 && openMin > STUCK_AFTER_MIN
      links.push({
        source: a, target: b, color: stuck ? C.red : l.kind === 'card' ? C.amber : C.violet, dashed: l.kind === 'card',
        width: Math.min(4, 0.8 + Math.log2(1 + l.count)),
        label: `${nameOf(l.from, names)} → ${nameOf(l.to, names)}: ${l.count} ${l.kind === 'card' ? 'cards' : 'letters'}${l.ack_p50_s != null ? ` · ${l.kind === 'card' ? 'wait' : 'ack'} ${shortSeconds(l.ack_p50_s)}` : ''}${l.unacked > 0 ? ` · ${l.unacked} open${stuck ? ` ${Math.round(openMin)}m` : ''}` : ''}`,
      })
    }
    return { nodes, links }
  }, [model, seats, beat, observed, team.data, names])

  const api = useRef<NerveApi | null>(null)
  const [view, setView] = useState<View>(startView)
  const [mode, setMode] = useState<Mode>({ kind: 'live' })
  const [dimCI, setDimCI] = useState(true)
  const [last, setLast] = useState<{ event: BusEnvelope; readers: string[] } | null>(null)
  const [replayNote, setReplayNote] = useState<string | null>(null)
  const pending = useRef(new Map<string, { topic: string; since: number; landed: boolean }>())
  const seenEvents = useRef(new Set<string>())
  const seenReceipts = useRef(new Set<string>())

  const faint = (type: string) => dimCI && type.startsWith('ci.')
  const fireEvent = (e: BusEnvelope) => {
    const a = api.current
    if (!a || !model) return
    const pub = `p:${model.publisherOf.get(e.type) ?? 'unknown'}`
    const dom = `d:${domainOf(e.type)}`
    a.pulse(pub, dom, C.blue, faint(e.type))
    window.setTimeout(() => { a.pulse(dom, `t:${e.type}`, C.blue, faint(e.type)); a.flash(dom) }, SECOND_HOP_MS)
    if (e.type === 'seat.mail.arrived') {
      const w = `s:${e.actor}`
      const r = `s:${e.subject}`
      if (!a.pulse(w, r, C.violet)) {
        a.pulse(w, CORE, C.violet)
        window.setTimeout(() => a.pulse(CORE, r, C.violet), SECOND_HOP_MS)
      }
      window.setTimeout(() => a.flash(r), SECOND_HOP_MS * 2)
    }
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
      api.current?.pulse(CORE, `r:${reader}`, C.green, faint(p.topic))
      window.setTimeout(() => api.current?.flash(`r:${reader}`), SECOND_HOP_MS)
      setLast(l => (l && l.event.id === r.id ? { ...l, readers: [...l.readers, reader] } : l))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- fire per new receipt only
  }, [stream.receipts, model, mode.kind])

  // Replay: the last day from the ledger at speed; receipts are not replayed.
  useEffect(() => {
    if (mode.kind !== 'replay' || !model) return
    let cancelled = false
    const timers: number[] = []
    setLast(null)
    setReplayNote('reading the last 24 hours from the ledger…')
    fetchBusReplay(new Date(Date.now() - 24 * 3600_000).toISOString())
      .then(events => {
        if (cancelled) return
        const plan = replaySchedule(events, mode.speed)
        setReplayNote(`replay: ${events.length} events of the last 24h at ${mode.speed}×; receipts are not replayed`)
        for (const { at: t, event } of plan) {
          timers.push(window.setTimeout(() => { fireEvent(event); setLast({ event, readers: [] }) }, t))
        }
        const end = plan.length > 0 ? plan[plan.length - 1].at + 2 * SECOND_HOP_MS : 0
        timers.push(window.setTimeout(() => setReplayNote(`replay done: ${events.length} events`), end))
      })
      .catch(err => !cancelled && setReplayNote(`replay failed: ${err instanceof Error ? err.message : 'request failed'}`))
    return () => { cancelled = true; timers.forEach(clearTimeout) }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- a replay runs once per start
  }, [mode, model])

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
          : <span style={{ color: C.muted }}>{mode.kind === 'live' ? 'no event since this page opened · seats breathe with their own heartbeat · drag a node · ▶ replays the last day' : replayNote}</span>}
      </p>
      {replayNote && last && <p className="m-0 font-mono text-xs" style={{ color: C.muted }}>{replayNote}</p>}
      {view === 'list'
        ? <div className="rounded-sm bg-[var(--card-fill)] p-2"><ContractGraph contract={contract.data} roster={roster} /></div>
        : (
          <Suspense fallback={<p className="m-0 font-mono text-xs" style={{ color: C.muted }}>loading the body…</p>}>
            <BusNerveGraph nodes={graph.nodes} links={graph.links} flat={view === '2d'} onReady={a => { api.current = a }} />
          </Suspense>
        )}
      <p className="m-0 font-mono text-[11px]" style={{ color: C.muted }}>
        blue particle: an event leaving its publisher through its domain · green: a reader's receipt, the reader lights up · violet: a letter between seats · amber dashed: cards to the owner · seats breathe with their session's heartbeat (fast in a turn, slow idle, still when down) · red ring: a letter or card waiting over {STUCK_AFTER_MIN}m · amber ring: no report for {QUIET_AFTER_H}h · grey: old roads not on the bus yet · hover a node or link for its readings · the Console's own receipt ({OWN_CONSUMER}) is not drawn
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
