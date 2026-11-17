import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
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
import { CORE, bodyGraph, domainOf, settle, type Placed } from './busNerveLayout'

/**
 * The observatory's own palette: a dark glass the body lives in, whatever the
 * Console's theme (the Owner's reference, 2026-11-17: a black ground, terminal
 * panels, a living picture in the middle). Fixed on purpose, not theme tokens.
 */
const C = {
  ground: '#04070a',
  panel: '#0a1116',
  rule: '#14232c',
  text: '#c7f9d6',
  muted: '#5f7f72',
  blue: '#4cc3ff',
  green: '#39ff8f',
  red: '#ff4d5e',
  amber: '#ffb347',
  grey: '#3b4a52',
  violet: '#b48cff',
} as const

const W = 960
const H = 660
const HOP_MS = 1100
const TRAIL = 7

const TONE: Record<OrganTone, string> = { live: C.green, lost: C.red, new: C.amber, old: C.grey, plain: C.muted }

/** A seat's breath, from its session's own heartbeat (GET /agent/threads): fast in a turn, slow idle, none when down. */
type Breath = { color: string; dur: number | null; label: string }
const BREATH: Record<AgentThreadStatus | 'none', Breath> = {
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

type Pulse = { key: string; from: { x: number; y: number }; to: { x: number; y: number }; via?: { x: number; y: number }; start: number; color: string; faint: boolean }

function seatId(t: AgentThread): string {
  return `thread:${(t.session || t.thread).replace(/^thread:/, '')}`
}

/** Pulses in flight and the frame clock that moves them; frames run only while one is in flight. */
function usePulses() {
  const [pulses, setPulses] = useState<Pulse[]>([])
  const [now, setNow] = useState(() => performance.now())
  useEffect(() => {
    if (pulses.length === 0) return
    let raf = 0
    const tick = () => {
      const t = performance.now()
      setNow(t)
      setPulses(ps => {
        const left = ps.filter(p => t - p.start < HOP_MS * 1.3)
        return left.length === ps.length ? ps : left
      })
      raf = requestAnimationFrame(tick)
    }
    raf = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(raf)
  }, [pulses.length])
  const fire = (p: Omit<Pulse, 'start'>) => setPulses(ps => [...ps.slice(-240), { ...p, start: performance.now() }])
  return { pulses, now, fire }
}

function at(p: Pulse, k: number): { x: number; y: number } {
  const e = k < 0.5 ? 2 * k * k : 1 - (-2 * k + 2) ** 2 / 2
  if (!p.via) return { x: p.from.x + (p.to.x - p.from.x) * e, y: p.from.y + (p.to.y - p.from.y) * e }
  const u = 1 - e
  return { x: u * u * p.from.x + 2 * u * e * p.via.x + e * e * p.to.x, y: u * u * p.from.y + 2 * u * e * p.via.y + e * e * p.to.y }
}

/** A glowing organ: a soft halo that breathes at dur seconds (none, still), the body, its label. */
function Organ({ n, color, dur, ring, label, sub, dim, title, hot }: {
  n: Placed; color: string; dur: number | null; ring?: string; label?: string; sub?: string; dim?: boolean; title: string; hot?: boolean
}) {
  const r = n.r
  return (
    <g opacity={dim ? 0.45 : 1}>
      <title>{title}</title>
      <circle cx={n.x} cy={n.y} r={r * 1.9} fill={color} opacity={hot ? 0.35 : 0.12} filter="url(#nerve-soft)">
        {dur != null && <animate attributeName="r" values={`${r * 1.5};${r * 2.4};${r * 1.5}`} dur={`${dur}s`} repeatCount="indefinite" />}
        {dur != null && <animate attributeName="opacity" values="0.08;0.3;0.08" dur={`${dur}s`} repeatCount="indefinite" />}
      </circle>
      <circle cx={n.x} cy={n.y} r={r} fill={C.panel} stroke={ring ?? color} strokeWidth={ring ? 2.5 : 1.6} />
      <circle cx={n.x} cy={n.y} r={Math.max(1.5, r * 0.35)} fill={color} opacity={0.9} />
      {label && <text x={n.x} y={n.y + r + 11} fontSize={10} textAnchor="middle" fontFamily="ui-monospace, monospace" fill={C.text}>{label}</text>}
      {sub && <text x={n.x} y={n.y + r + 22} fontSize={8.5} textAnchor="middle" fontFamily="ui-monospace, monospace" fill={ring && ring !== color ? ring : C.muted}>{sub}</text>}
    </g>
  )
}

type Mode = { kind: 'live' } | { kind: 'replay'; speed: number }

function TermButton({ on, children, onClick }: { on: boolean; children: ReactNode; onClick: () => void }) {
  return (
    <button type="button" onClick={onClick} className="cursor-pointer rounded-sm border px-2 py-0.5 font-mono text-xs"
      style={{ borderColor: on ? C.green : C.rule, color: on ? C.green : C.muted, background: on ? '#0d2116' : 'transparent' }}>
      {children}
    </button>
  )
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
  const n = names.get(id)
  return n ?? seatLabel(id)
}

/**
 * Bus → Nerves: a watch on the team at work. The bus is the spine; topics
 * gather by domain around it, fed by their publishers; readers and seats
 * are organs. Every real event is a pulse with a trail from its publisher
 * through its domain to its topic, and on to a reader when that reader's
 * receipt comes. Seats breathe with their own heartbeat (fast in a turn,
 * slow idle, still when down); a letter crosses between seats; red is
 * something waiting too long. Replay plays the last day from the ledger.
 * Nothing is drawn that was not read.
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

  // The seats on the body: every session with a heartbeat in the last day, and every actor the letters name.
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

  const body = useMemo(() => {
    if (!model) return null
    const g = bodyGraph({
      publishers: model.publishers.map(p => p.id),
      topics: model.topics.map(t => ({ type: t.id, publisher: model.publisherOf.get(t.id) ?? 'unknown' })),
      readers: model.readers.filter(r => r.tone !== 'old').map(r => r.id),
      seats,
      old: model.readers.filter(r => r.tone === 'old').map(r => r.label),
      letters: (team.data?.links ?? []).filter(l => l.kind === 'letter').map(l => ({ from: l.from, to: l.to })),
    })
    const placed = settle(g.nodes, g.links, W, H)
    return { placed, byId: new Map(placed.map(p => [p.id, p])) }
  }, [model, seats, team.data])

  const { pulses, now, fire } = usePulses()
  const [mode, setMode] = useState<Mode>({ kind: 'live' })
  const [dimCI, setDimCI] = useState(true)
  const [hot, setHot] = useState<Map<string, number>>(new Map())
  const [last, setLast] = useState<{ event: BusEnvelope; readers: string[] } | null>(null)
  const [replayNote, setReplayNote] = useState<string | null>(null)
  const pending = useRef(new Map<string, { topic: string; since: number; landed: boolean }>())
  const seenEvents = useRef(new Set<string>())
  const seenReceipts = useRef(new Set<string>())

  const heat = (ids: string[]) => setHot(h => { const m = new Map(h); for (const id of ids) m.set(id, Date.now()); return m })
  const faint = (type: string) => dimCI && type.startsWith('ci.')
  const toTopic = (e: BusEnvelope) => {
    if (!body || !model) return
    const pub = body.byId.get(`p:${model.publisherOf.get(e.type) ?? 'unknown'}`)
    const cl = body.byId.get(`d:${domainOf(e.type)}`)
    const tp = body.byId.get(`t:${e.type}`)
    if (pub && tp) fire({ key: `${e.id}:t`, from: pub, to: tp, via: cl, color: C.blue, faint: faint(e.type) })
    heat([`p:${model.publisherOf.get(e.type) ?? 'unknown'}`, `d:${domainOf(e.type)}`])
    if (e.type === 'seat.mail.arrived') {
      const w = body.byId.get(`s:${e.actor}`)
      const r = body.byId.get(`s:${e.subject}`)
      if (w && r) {
        fire({ key: `${e.id}:s`, from: w, to: r, via: body.byId.get(CORE), color: C.violet, faint: false })
        heat([`s:${e.subject}`])
      }
    }
  }

  // Live: a new event pulses to its topic and waits there for receipts.
  useEffect(() => {
    if (mode.kind !== 'live' || !body) return
    for (const e of [...stream.events].reverse()) {
      if (seenEvents.current.has(e.id)) continue
      seenEvents.current.add(e.id)
      pending.current.set(e.id, { topic: e.type, since: Date.now(), landed: false })
      // The list keeps the newest BUS_LIST_CAP events; so does this.
      if (pending.current.size > BUS_LIST_CAP) pending.current.delete(pending.current.keys().next().value as string)
      toTopic(e)
      setLast({ event: e, readers: [] })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- fire per new event only
  }, [stream.events, body, mode.kind])

  // Live: a receipt carries its pulse from the topic to the reader that took it.
  useEffect(() => {
    if (mode.kind !== 'live' || !body || !model) return
    for (const r of [...stream.receipts].reverse()) {
      const k = `${r.id}|${r.consumer}`
      if (seenReceipts.current.has(k)) continue
      seenReceipts.current.add(k)
      const reader = readerOf(r.consumer, model.readers)
      const p = pending.current.get(r.id)
      if (!reader || !p) continue
      p.landed = true
      const a = body.byId.get(`t:${p.topic}`)
      const b = body.byId.get(`r:${reader}`)
      if (a && b) fire({ key: k, from: a, to: b, color: C.green, faint: faint(p.topic) })
      heat([`r:${reader}`])
      setLast(l => (l && l.event.id === r.id ? { ...l, readers: [...l.readers, reader] } : l))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- fire per new receipt only
  }, [stream.receipts, body, mode.kind])

  // Replay: play the last day from the ledger at speed; receipts are not replayed.
  useEffect(() => {
    if (mode.kind !== 'replay' || !body) return
    let cancelled = false
    const timers: number[] = []
    setLast(null)
    setReplayNote('reading the last 24 hours from the ledger…')
    fetchBusReplay(new Date(Date.now() - 24 * 3600_000).toISOString())
      .then(events => {
        if (cancelled) return
        const plan = replaySchedule(events, mode.speed)
        setReplayNote(`replay: ${events.length} events of the last 24h at ${mode.speed}×; receipts are not replayed, pulses stop at their topic`)
        for (const { at: t, event } of plan) {
          timers.push(window.setTimeout(() => { toTopic(event); setLast({ event, readers: [] }) }, t))
        }
        const end = plan.length > 0 ? plan[plan.length - 1].at + HOP_MS : 0
        timers.push(window.setTimeout(() => setReplayNote(`replay done: ${events.length} events`), end))
      })
      .catch(err => !cancelled && setReplayNote(`replay failed: ${err instanceof Error ? err.message : 'request failed'}`))
    return () => { cancelled = true; timers.forEach(clearTimeout) }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- a replay runs once per start
  }, [mode, body])

  const wall = Date.now()
  const isHot = (id: string) => (hot.get(id) ?? 0) > wall - 1500
  const lateByTopic = new Map<string, number>()
  if (mode.kind === 'live') {
    for (const p of pending.current.values()) {
      if (!p.landed && wall - p.since > LATE_AFTER_S * 1000) lateByTopic.set(p.topic, (lateByTopic.get(p.topic) ?? 0) + 1)
    }
  }
  const lateByDomain = new Map<string, number>()
  for (const [t, n] of lateByTopic) lateByDomain.set(domainOf(t), (lateByDomain.get(domainOf(t)) ?? 0) + n)

  let content
  if (contract.isError) content = <p className="m-0 font-mono text-xs" style={{ color: C.red }}>{`contract unreadable: ${contract.error instanceof Error ? contract.error.message : 'request failed'}`}</p>
  else if (!model || !body) content = <p className="m-0 font-mono text-xs" style={{ color: C.muted }}>loading…</p>
  else {
    const toneOf = new Map(model.readers.map(r => [r.id, r.tone]))
    const domainSize = new Map<string, number>()
    for (const t of model.topics) domainSize.set(domainOf(t.id), (domainSize.get(domainOf(t.id)) ?? 0) + 1)
    const core = body.byId.get(CORE)!
    content = (
      <div className="flex w-full min-w-0 flex-col gap-2">
        <div className="flex flex-wrap items-center gap-1.5">
          <TermButton on={mode.kind === 'live'} onClick={() => { setMode({ kind: 'live' }); setReplayNote(null) }}>● live</TermButton>
          {SPEEDS.map(s => (
            <TermButton key={s} on={mode.kind === 'replay' && mode.speed === s} onClick={() => setMode({ kind: 'replay', speed: s })}>▶ 24h {s}×</TermButton>
          ))}
          <TermButton on={dimCI} onClick={() => setDimCI(d => !d)}>dim ci.*</TermButton>
          {stream.receiptsOff && <span className="font-mono text-xs" style={{ color: C.amber }}>receipts off on this gate</span>}
        </div>
        <p className="m-0 min-h-5 truncate font-mono text-xs" data-testid="bus-nerve-last" style={{ color: C.text }}>
          <span style={{ color: C.muted }}>$ </span>
          {last
            ? <button type="button" className="cursor-pointer hover:underline" onClick={() => onSelect(last.event.id)}>
                {clockTime(last.event.time)} {last.event.type} {last.event.subject} → {last.readers.length > 0 ? last.readers.join(', ') : mode.kind === 'live' ? 'awaiting receipt' : 'topic'}
              </button>
            : <span style={{ color: C.muted }}>{mode.kind === 'live' ? 'no event since this page opened · the seats breathe with their own heartbeat · ▶ replays the last day' : replayNote}</span>}
        </p>
        {replayNote && last && <p className="m-0 font-mono text-xs" style={{ color: C.muted }}>{replayNote}</p>}
        <svg viewBox={`0 0 ${W} ${H}`} className="w-full rounded-sm" style={{ background: C.ground, border: `1px solid ${C.rule}` }} role="img"
          aria-label="Bus nerve map: the bus as a spine, topics by domain, publishers, readers and seats as organs, pulses of real events" data-testid="bus-nerve-map">
          <defs>
            <filter id="nerve-soft" x="-100%" y="-100%" width="300%" height="300%"><feGaussianBlur stdDeviation="5" /></filter>
            <filter id="nerve-glow" x="-100%" y="-100%" width="300%" height="300%"><feGaussianBlur stdDeviation="2" result="b" /><feMerge><feMergeNode in="b" /><feMergeNode in="SourceGraphic" /></feMerge></filter>
            <radialGradient id="nerve-core"><stop offset="0%" stopColor={C.blue} stopOpacity="0.5" /><stop offset="100%" stopColor={C.blue} stopOpacity="0" /></radialGradient>
          </defs>
          {/* nerves: the springs of the body, faint */}
          {body.placed.filter(n => n.kind !== 'core').map(n => {
            const anchor = n.kind === 'topic' ? body.byId.get(`d:${n.group}`) : core
            if (!anchor) return null
            return <line key={`n-${n.id}`} x1={anchor.x} y1={anchor.y} x2={n.x} y2={n.y} stroke={n.kind === 'old' ? C.grey : C.rule} strokeWidth={n.kind === 'topic' ? 0.6 : 0.9}
              strokeDasharray={n.kind === 'old' ? '2 4' : undefined} opacity={n.kind === 'topic' ? 0.7 : 0.9} />
          })}
          {/* letters between seats: who wrote to whom, how fast it was taken, what waits */}
          {(team.data?.links ?? []).map(l => {
            const a = l.from === 'seats' ? core : body.byId.get(`s:${l.from}`)
            const b = body.byId.get(`s:${l.to}`)
            if (!a || !b) return null
            const openMin = l.oldest_open ? (wall - Date.parse(l.oldest_open)) / 60_000 : 0
            const stuck = l.unacked > 0 && openMin > STUCK_AFTER_MIN
            const color = stuck ? C.red : l.kind === 'card' ? C.amber : C.violet
            const mx = (a.x + b.x) / 2 + (core.x - (a.x + b.x) / 2) * 0.35
            const my = (a.y + b.y) / 2 + (core.y - (a.y + b.y) / 2) * 0.35
            const text = `${l.count}${l.ack_p50_s != null ? ` · ${l.kind === 'card' ? 'wait' : 'ack'} ${shortSeconds(l.ack_p50_s)}` : ''}${l.unacked > 0 ? ` · ${l.unacked} open${stuck ? ` ${Math.round(openMin)}m` : ''}` : ''}`
            return (
              <g key={`l-${l.from}-${l.to}`}>
                <title>{`${nameOf(l.from, names)} → ${nameOf(l.to, names)}: ${l.count} ${l.kind === 'card' ? 'cards' : 'letters'} · ${text}`}</title>
                <path d={`M${a.x},${a.y} Q${mx},${my} ${b.x},${b.y}`} fill="none" stroke={color} strokeWidth={Math.min(4, 0.8 + Math.log2(1 + l.count))} opacity={0.55}
                  strokeDasharray={l.kind === 'card' ? '5 4' : undefined} />
                <text x={(a.x + 2 * mx + b.x) / 4} y={(a.y + 2 * my + b.y) / 4 - 3} fontSize={9} textAnchor="middle" fontFamily="ui-monospace, monospace" fill={color}>{text}</text>
              </g>
            )
          })}
          {/* the spine */}
          <circle cx={core.x} cy={core.y} r={60} fill="url(#nerve-core)">
            <animate attributeName="r" values="48;66;48" dur="6s" repeatCount="indefinite" />
          </circle>
          <circle cx={core.x} cy={core.y} r={core.r} fill={C.panel} stroke={C.blue} strokeWidth={2} filter="url(#nerve-glow)" />
          <text x={core.x} y={core.y + 3.5} fontSize={10} textAnchor="middle" fontFamily="ui-monospace, monospace" fill={C.blue}>bus</text>
          {body.placed.map(n => {
            switch (n.kind) {
              case 'topic': {
                const late = lateByTopic.get(n.label) ?? 0
                return (
                  <circle key={n.id} cx={n.x} cy={n.y} r={n.r} fill={late > 0 ? C.amber : C.blue} opacity={isHot(n.id) ? 1 : 0.55}>
                    <title>{`${n.label}${late > 0 ? ` · ${late} waiting over ${LATE_AFTER_S}s for a receipt` : ''}`}</title>
                  </circle>
                )
              }
              case 'cluster': {
                const late = lateByDomain.get(n.label) ?? 0
                return <Organ key={n.id} n={n} color={late > 0 ? C.amber : C.blue} dur={isHot(n.id) ? 0.8 : 5} hot={isHot(n.id)} label={`${n.label}.*`}
                  sub={late > 0 ? `${late} late` : `${domainSize.get(n.label) ?? 0} topics`} title={`domain ${n.label}: ${domainSize.get(n.label) ?? 0} topics`} />
              }
              case 'publisher':
                return <Organ key={n.id} n={n} color={C.blue} dur={isHot(n.id) ? 0.9 : 6} hot={isHot(n.id)} label={n.label} title={`publisher ${n.label}`} />
              case 'reader': {
                const tone = toneOf.get(n.label) ?? 'plain'
                return <Organ key={n.id} n={n} color={TONE[tone]} dur={tone === 'lost' ? null : 3} hot={isHot(n.id)}
                  label={n.label.replace(/^gate:events:/, '')} sub={tone === 'lost' ? 'lost' : tone === 'new' ? 'undeclared' : undefined}
                  ring={tone === 'lost' ? C.red : undefined} title={`reader ${n.label} · ${tone}`} />
              }
              case 'seat': {
                const t = beat.get(n.label)
                const br = BREATH[t?.status ?? 'none']
                const o: ObserveSeat | undefined = observed.get(n.label)
                const st = o ? seatState(o, team.data?.links ?? [], wall) : 'ok'
                const ring = st === 'stuck' ? C.red : st === 'quiet' ? C.amber : undefined
                const read = [
                  o && o.letters_in > 0 ? `in ${o.letters_in}` : '',
                  o?.ack_p50_s != null ? `ack ${shortSeconds(o.ack_p50_s)}` : '',
                  o && o.unacked > 0 ? `${o.unacked} open` : '',
                  o?.cards_open ? `${o.cards_open} cards open` : '',
                  t && t.status !== 'in_turn' && t.quiet_seconds > 0 ? `quiet ${shortSeconds(t.quiet_seconds)}` : '',
                ].filter(Boolean).join(' · ')
                const name = nameOf(n.label, names)
                return <Organ key={n.id} n={n} color={br.color} dur={br.dur} ring={ring} hot={isHot(n.id)}
                  label={name.length > 24 ? `${name.slice(0, 23)}…` : name} sub={read || br.label}
                  title={`${name} (${n.label}) · ${br.label}${read ? ` · ${read}` : ''}${st === 'quiet' ? ` · no report for ${QUIET_AFTER_H}h` : ''}`} />
              }
              case 'old':
                return <Organ key={n.id} n={n} color={C.grey} dur={null} dim label={n.label} title={`old road, not on the bus yet: ${n.label}`} />
              default:
                return null
            }
          })}
          {pulses.map(p => {
            const k = Math.min(1, (now - p.start) / HOP_MS)
            return (
              <g key={p.key} opacity={p.faint ? 0.3 : 1}>
                {Array.from({ length: TRAIL }, (_, i) => {
                  const q = at(p, Math.max(0, k - i * 0.035))
                  return <circle key={i} cx={q.x} cy={q.y} r={(p.faint ? 2.2 : 3.6) * (1 - i / TRAIL)} fill={p.color} opacity={(1 - i / TRAIL) * 0.9}
                    filter={i === 0 && !p.faint ? 'url(#nerve-glow)' : undefined} />
                })}
              </g>
            )
          })}
        </svg>
        <p className="m-0 font-mono text-[11px]" style={{ color: C.muted }}>
          blue pulse: an event leaving its publisher through its domain · green: a reader's receipt, the event arriving · violet: a letter between seats · seats breathe with their session's heartbeat (fast in a turn, slow idle, still when down) · red ring: a letter or card waiting over {STUCK_AFTER_MIN}m · amber ring: no report for {QUIET_AFTER_H}h · grey dashed: old roads not on the bus yet · the Console's own receipt ({OWN_CONSUMER}) is not drawn
        </p>
        {team.data && team.data.unmeasured.length > 0 && (
          <ul className="m-0 flex list-none flex-col gap-0.5 p-0 font-mono text-[11px]" style={{ color: C.muted }}>
            {team.data.unmeasured.map(u => <li key={u.what}><span style={{ color: C.amber }}>not measured</span> {u.what}: {u.why}</li>)}
          </ul>
        )}
        {team.isError && <p className="m-0 font-mono text-[11px]" style={{ color: C.red }}>team readings unreadable: {team.error instanceof Error ? team.error.message : 'request failed'}</p>}
      </div>
    )
  }
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
