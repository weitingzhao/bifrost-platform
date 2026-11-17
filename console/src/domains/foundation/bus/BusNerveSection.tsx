import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { OpsSection } from '@/components/layout/OpsSection'
import { fetchBusContract, fetchBusNextgen } from './busNextgen'
import { BUS_LIST_CAP, fetchBusClients, fetchBusReplay, type BusStreamState } from './useBusStream'
import { clockTime, type BusEnvelope } from './busTrace'
import {
  LATE_AFTER_S, OWN_CONSUMER, QUIET_AFTER_H, SPEEDS, STUCK_AFTER_MIN, fetchBusObserve, nerveModel, readerOf, replaySchedule,
  seatLabel, seatMail, seatState, shortSeconds,
  type BusObserve, type NerveModel, type ObserveLink, type Organ, type OrganTone, type SeatState,
} from './busNerve'

/**
 * The observatory's own palette: a dark glass the map lives in, whatever the
 * Console's theme (the Owner's reference, 2026-11-17: a black ground, terminal
 * panels, a moving picture in the middle). Fixed on purpose, not theme tokens.
 */
const C = {
  ground: '#05080b',
  panel: '#0a1116',
  rule: '#14232c',
  text: '#c7f9d6',
  muted: '#5f7f72',
  blue: '#4cc3ff',
  green: '#39ff8f',
  red: '#ff4d5e',
  amber: '#ffb347',
  grey: '#3b4a52',
} as const

const VIEW_W = 960
const ROW = 22
const TOP = 26
const COL_X = { publisher: 95, topic: 480, reader: 865 } as const
const BOX_W = 170
const HOP_MS = 900
const RING_R = 150

const TONE: Record<OrganTone, string> = { live: C.green, lost: C.red, new: C.amber, old: C.grey, plain: C.muted }
const SEAT: Record<SeatState, string> = { ok: C.green, stuck: C.red, quiet: C.amber }

type Pulse = { key: string; x1: number; y1: number; x2: number; y2: number; start: number; color: string; faint: boolean; bend?: Point }
type Point = { x: number; y: number }

function layout(model: NerveModel, team: BusObserve | undefined): { at: Map<string, Point>; eventsH: number; height: number; center: Point } {
  const at = new Map<string, Point>()
  const place = (organs: Organ[], x: number) => organs.forEach((o, i) => at.set(`${o.col}:${o.id}`, { x, y: TOP + i * ROW }))
  place(model.publishers, COL_X.publisher)
  place(model.topics, COL_X.topic)
  place(model.readers, COL_X.reader)
  const rows = Math.max(model.publishers.length, model.topics.length, model.readers.length, 4)
  const eventsH = TOP + rows * ROW
  const center = { x: VIEW_W / 2, y: eventsH + 60 + RING_R }
  const seats = team?.seats ?? []
  seats.forEach((s, i) => {
    const a = (2 * Math.PI * i) / Math.max(1, seats.length) - Math.PI / 2
    at.set(`seat:${s.id}`, { x: center.x + RING_R * Math.cos(a), y: center.y + RING_R * Math.sin(a) })
  })
  return { at, eventsH, height: seats.length > 0 ? center.y + RING_R + 70 : eventsH + 10, center }
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
        const left = ps.filter(p => t - p.start < HOP_MS)
        return left.length === ps.length ? ps : left
      })
      raf = requestAnimationFrame(tick)
    }
    raf = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(raf)
  }, [pulses.length])
  const fire = (p: Omit<Pulse, 'start'>) => setPulses(ps => [...ps.slice(-200), { ...p, start: performance.now() }])
  return { pulses, now, fire }
}

function OrganBox({ organ, at, flash, late }: { organ: Organ; at: Point; flash: boolean; late: number }) {
  return (
    <g transform={`translate(${at.x - BOX_W / 2},${at.y - 9})`}>
      <title>{organ.note ? `${organ.label} · ${organ.note}` : organ.label}</title>
      <rect width={BOX_W} height={18} rx={3} fill={C.panel} stroke={late > 0 ? C.amber : TONE[organ.tone]} strokeWidth={flash ? 2.5 : 1}
        opacity={organ.tone === 'old' ? 0.5 : 1} />
      {flash && <rect width={BOX_W} height={18} rx={3} fill={C.green} opacity={0.22} />}
      <text x={6} y={13} fontSize={10.5} fontFamily="ui-monospace, monospace" fill={organ.tone === 'old' ? C.muted : C.text}>
        {organ.label.length > 26 ? `${organ.label.slice(0, 25)}…` : organ.label}
      </text>
      {late > 0 && <text x={BOX_W - 6} y={13} fontSize={10} textAnchor="end" fill={C.amber}>{late} late</text>}
    </g>
  )
}

/** A curve between two seats, bowed away from the ring's centre so A→B and B→A do not overlap. */
function bend(a: Point, b: Point, center: Point): Point {
  const mx = (a.x + b.x) / 2
  const my = (a.y + b.y) / 2
  const dx = b.x - a.x
  const dy = b.y - a.y
  const n = Math.hypot(dx, dy) || 1
  const k = 0.18 * n
  const px = mx - (dy / n) * k
  const py = my + (dx / n) * k
  return Math.hypot(px - center.x, py - center.y) < Math.hypot(mx - center.x, my - center.y) ? { x: mx + (dy / n) * k, y: my - (dx / n) * k } : { x: px, y: py }
}

function TeamLink({ l, a, b, center, now }: { l: ObserveLink; a: Point; b: Point; center: Point; now: number }) {
  const q = bend(a, b, center)
  const openMin = l.oldest_open ? (now - Date.parse(l.oldest_open)) / 60_000 : 0
  const stuck = l.unacked > 0 && openMin > STUCK_AFTER_MIN
  const color = stuck ? C.red : l.kind === 'card' ? C.amber : C.blue
  const label = `${l.count}${l.ack_p50_s != null ? ` · ${l.kind === 'card' ? 'wait' : 'ack'} ${shortSeconds(l.ack_p50_s)}` : ''}${l.unacked > 0 ? ` · ${l.unacked} open${stuck ? ` ${Math.round(openMin)}m` : ''}` : ''}`
  return (
    <g>
      <title>{`${l.from} → ${l.to}: ${l.count} ${l.kind === 'card' ? 'cards' : 'letters'}; ${label}`}</title>
      <path d={`M${a.x},${a.y} Q${q.x},${q.y} ${b.x},${b.y}`} fill="none" stroke={color} strokeWidth={Math.min(4, 0.8 + Math.log2(1 + l.count))}
        opacity={0.65} strokeDasharray={l.kind === 'card' ? '4 3' : undefined} />
      <text x={(a.x + 2 * q.x + b.x) / 4} y={(a.y + 2 * q.y + b.y) / 4} fontSize={9.5} textAnchor="middle" fontFamily="ui-monospace, monospace" fill={color}>{label}</text>
    </g>
  )
}

type Mode = { kind: 'live' } | { kind: 'replay'; speed: number }

function TermButton({ on, children, onClick }: { on: boolean; children: React.ReactNode; onClick: () => void }) {
  return (
    <button type="button" onClick={onClick} className="cursor-pointer rounded-sm border px-2 py-0.5 font-mono text-xs"
      style={{ borderColor: on ? C.green : C.rule, color: on ? C.green : C.muted, background: on ? '#0d2116' : 'transparent' }}>
      {children}
    </button>
  )
}

/**
 * Bus → Nerves: a watch on the team at work. Above, every real event is a
 * pulse from its publisher to its topic, and from the topic to each reader
 * when that reader's receipt comes (a receipt is the arrival). Below, the
 * seats on a ring: who wrote to whom, how fast it was taken, what waits too
 * long (red), who has gone quiet (amber), how long cards wait on the owner.
 * Replay plays the last day from the ledger. Nothing is drawn that was not read.
 */
export function BusNerveSection({ stream, onSelect }: { stream: BusStreamState; onSelect: (id: string) => void }) {
  const contract = useQuery({ queryKey: ['bus-contract'], queryFn: fetchBusContract, staleTime: 300_000, retry: false })
  const clients = useQuery({ queryKey: ['bus', 'clients'], queryFn: fetchBusClients, refetchInterval: 60_000 })
  const nextgen = useQuery({ queryKey: ['bus-nextgen'], queryFn: fetchBusNextgen, refetchInterval: 60_000, retry: false })
  const team = useQuery({ queryKey: ['bus-observe'], queryFn: fetchBusObserve, refetchInterval: 60_000, retry: false })
  const model = useMemo(
    () => (contract.data ? nerveModel(contract.data, clients.data?.clients ?? [], nextgen.data) : null),
    [contract.data, clients.data, nextgen.data],
  )
  const { at, eventsH, height, center } = useMemo(
    () => (model ? layout(model, team.data) : { at: new Map<string, Point>(), eventsH: 120, height: 120, center: { x: 0, y: 0 } }),
    [model, team.data],
  )
  const { pulses, now, fire } = usePulses()
  const [mode, setMode] = useState<Mode>({ kind: 'live' })
  const [dimCI, setDimCI] = useState(true)
  const [flash, setFlash] = useState<Map<string, number>>(new Map())
  const [last, setLast] = useState<{ event: BusEnvelope; readers: string[] } | null>(null)
  const [replayNote, setReplayNote] = useState<string | null>(null)
  const pending = useRef(new Map<string, { topic: string; since: number; landed: boolean }>())
  const seenEvents = useRef(new Set<string>())
  const seenReceipts = useRef(new Set<string>())

  const faint = (type: string) => dimCI && type.startsWith('ci.')
  const toTopic = (e: BusEnvelope) => {
    const pub = model?.publisherOf.get(e.type) ?? 'unknown'
    const a = at.get(`publisher:${pub}`)
    const b = at.get(`topic:${e.type}`)
    if (a && b) fire({ key: `${e.id}:t`, x1: a.x + BOX_W / 2, y1: a.y, x2: b.x - BOX_W / 2, y2: b.y, color: C.blue, faint: faint(e.type) })
    // A seat's letter also crosses the ring, writer to recipient, when both are on it.
    if (e.type === 'seat.mail.arrived') {
      const w = at.get(`seat:${e.actor}`)
      const r = at.get(`seat:${e.subject}`)
      if (w && r) fire({ key: `${e.id}:s`, x1: w.x, y1: w.y, x2: r.x, y2: r.y, color: C.green, faint: false, bend: bend(w, r, center) })
    }
  }

  // Live: a new event pulses to its topic and waits there for receipts.
  useEffect(() => {
    if (mode.kind !== 'live' || !model) return
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
  }, [stream.events, model, mode.kind])

  // Live: a receipt carries its pulse from the topic to the reader that took it.
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
      const a = at.get(`topic:${p.topic}`)
      const b = at.get(`reader:${reader}`)
      if (a && b) fire({ key: k, x1: a.x + BOX_W / 2, y1: a.y, x2: b.x - BOX_W / 2, y2: b.y, color: C.green, faint: faint(p.topic) })
      setFlash(f => new Map(f).set(reader, Date.now()))
      setLast(l => (l && l.event.id === r.id ? { ...l, readers: [...l.readers, reader] } : l))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- fire per new receipt only
  }, [stream.receipts, model, mode.kind])

  // Replay: play the last day from the ledger at speed; receipts are not replayed.
  useEffect(() => {
    if (mode.kind !== 'replay' || !model) return
    let cancelled = false
    const timers: number[] = []
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
  }, [mode, model])

  // Late: events still waiting at their topic for any reader past LATE_AFTER_S.
  const lateByTopic = new Map<string, number>()
  if (mode.kind === 'live') {
    for (const p of pending.current.values()) {
      if (!p.landed && Date.now() - p.since > LATE_AFTER_S * 1000) lateByTopic.set(p.topic, (lateByTopic.get(p.topic) ?? 0) + 1)
    }
  }
  const flashing = (id: string) => (flash.get(id) ?? 0) > Date.now() - 1200
  const wall = Date.now()

  let body
  if (contract.isError) body = <p className="m-0 font-mono text-xs" style={{ color: C.red }}>{`contract unreadable: ${contract.error instanceof Error ? contract.error.message : 'request failed'}`}</p>
  else if (!model) body = <p className="m-0 font-mono text-xs" style={{ color: C.muted }}>loading…</p>
  else body = (
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
              {clockTime(last.event.time)} {last.event.type} {last.event.subject} → {last.readers.length > 0 ? last.readers.join(', ') : 'awaiting receipt'}
            </button>
          : <span style={{ color: C.muted }}>{mode.kind === 'live' ? 'no event since this page opened; ▶ replays the last day' : replayNote}</span>}
      </p>
      {replayNote && last && <p className="m-0 font-mono text-xs" style={{ color: C.muted }}>{replayNote}</p>}
      <svg viewBox={`0 0 ${VIEW_W} ${height}`} className="w-full rounded-sm" style={{ background: C.ground, border: `1px solid ${C.rule}` }} role="img"
        aria-label="Bus nerve map: publishers, topics and readers with pulses of live events; below, the seats and their letters" data-testid="bus-nerve-map">
        <defs>
          <filter id="nerve-glow" x="-50%" y="-50%" width="200%" height="200%"><feGaussianBlur stdDeviation="2.2" result="b" /><feMerge><feMergeNode in="b" /><feMergeNode in="SourceGraphic" /></feMerge></filter>
        </defs>
        {[['publishers', COL_X.publisher, model.publishers.length], ['topics', COL_X.topic, model.topics.length], ['readers', COL_X.reader, model.readers.length]].map(([t, x, n]) => (
          <text key={String(t)} x={Number(x)} y={13} fontSize={10} textAnchor="middle" fontFamily="ui-monospace, monospace" fill={C.muted}>{`${t} (${n})`}</text>
        ))}
        {model.topics.map(t => {
          const a = at.get(`publisher:${model.publisherOf.get(t.id) ?? 'unknown'}`)
          const b = at.get(`topic:${t.id}`)
          return a && b ? <line key={t.id} x1={a.x + BOX_W / 2} y1={a.y} x2={b.x - BOX_W / 2} y2={b.y} stroke={C.rule} strokeWidth={0.7} /> : null
        })}
        {model.publishers.map(o => <OrganBox key={o.id} organ={o} at={at.get(`publisher:${o.id}`)!} flash={false} late={0} />)}
        {model.topics.map(o => <OrganBox key={o.id} organ={o} at={at.get(`topic:${o.id}`)!} flash={false} late={lateByTopic.get(o.id) ?? 0} />)}
        {model.readers.map(o => <OrganBox key={o.id} organ={o} at={at.get(`reader:${o.id}`)!} flash={flashing(o.id)} late={0} />)}
        {team.data && team.data.seats.length > 0 && (
          <g>
            <line x1={20} y1={eventsH + 20} x2={VIEW_W - 20} y2={eventsH + 20} stroke={C.rule} strokeDasharray="2 4" />
            <text x={VIEW_W / 2} y={eventsH + 40} fontSize={10} textAnchor="middle" fontFamily="ui-monospace, monospace" fill={C.muted}>
              {`team · last ${team.data.hours}h · platform letters and cards · red waits over ${STUCK_AFTER_MIN}m · amber no report for ${QUIET_AFTER_H}h`}
            </text>
            {team.data.links.map(l => {
              const a = at.get(`seat:${l.from}`) ?? (l.from === 'seats' ? center : undefined)
              const b = at.get(`seat:${l.to}`)
              return a && b ? <TeamLink key={`${l.from}>${l.to}`} l={l} a={a} b={b} center={center} now={wall} /> : null
            })}
            {team.data.seats.map(s => {
              const p = at.get(`seat:${s.id}`)!
              const st = seatState(s, team.data.links, wall)
              const right = p.x >= center.x
              const read = [
                s.letters_in > 0 ? `in ${s.letters_in}` : '',
                s.ack_p50_s != null ? `ack ${shortSeconds(s.ack_p50_s)}` : '',
                s.unacked > 0 ? `${s.unacked} open` : '',
                s.cards_open ? `${s.cards_open} cards open` : '',
                s.card_wait_p50_s != null ? `cards ${shortSeconds(s.card_wait_p50_s)}` : '',
                s.last_status ? `report ${shortSeconds((wall - Date.parse(s.last_status)) / 1000)} ago` : '',
              ].filter(Boolean).join(' · ')
              return (
                <g key={s.id}>
                  <title>{`${s.id}\nout ${s.letters_out} · ${read}`}</title>
                  <circle cx={p.x} cy={p.y} r={7} fill={C.panel} stroke={SEAT[st]} strokeWidth={2} filter="url(#nerve-glow)" />
                  <text x={p.x + (right ? 12 : -12)} y={p.y - 2} fontSize={10.5} textAnchor={right ? 'start' : 'end'} fontFamily="ui-monospace, monospace" fill={C.text}>{seatLabel(s.id)}</text>
                  <text x={p.x + (right ? 12 : -12)} y={p.y + 10} fontSize={9} textAnchor={right ? 'start' : 'end'} fontFamily="ui-monospace, monospace" fill={SEAT[st] === C.green ? C.muted : SEAT[st]}>{read}</text>
                </g>
              )
            })}
          </g>
        )}
        {pulses.map(p => {
          const k = Math.min(1, (now - p.start) / HOP_MS)
          const e = k < 0.5 ? 2 * k * k : 1 - (-2 * k + 2) ** 2 / 2
          const x = p.bend ? (1 - e) ** 2 * p.x1 + 2 * (1 - e) * e * p.bend.x + e ** 2 * p.x2 : p.x1 + (p.x2 - p.x1) * e
          const y = p.bend ? (1 - e) ** 2 * p.y1 + 2 * (1 - e) * e * p.bend.y + e ** 2 * p.y2 : p.y1 + (p.y2 - p.y1) * e
          return <circle key={p.key} cx={x} cy={y} r={p.faint ? 2.5 : 4} fill={p.color} opacity={p.faint ? 0.3 : 1} filter={p.faint ? undefined : 'url(#nerve-glow)'} />
        })}
      </svg>
      <p className="m-0 font-mono text-[11px]" style={{ color: C.muted }}>
        blue: an event leaving its publisher · green: a reader's receipt, the event arriving · box green live, red lost, amber undeclared or events waiting over {LATE_AFTER_S}s, grey an old road not on the bus yet · the Console's own receipt ({OWN_CONSUMER}) is not drawn · click the $ line to trace the event
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
      <div className="w-full min-w-0 rounded-sm p-2" style={{ background: C.ground, color: C.text }}>{body}</div>
    </OpsSection>
  )
}

function TermPanel({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0 rounded-sm p-2 font-mono text-xs" style={{ background: C.ground, border: `1px solid ${C.rule}`, color: C.text }}>
      <p className="m-0 mb-1" style={{ color: C.muted }}>{`┌ ${title}`}</p>
      {children}
    </div>
  )
}

/** The panels under the map: seats' letters, the newest events (each opens its journey), and what is not wired yet. */
export function BusNervePanels({ stream, onSelect }: { stream: BusStreamState; onSelect: (id: string) => void }) {
  const mail = seatMail(stream.events).slice(0, 8)
  const recent = stream.events.slice(0, 8)
  const row = (e: BusEnvelope) => (
    <li key={e.id}>
      <button type="button" onClick={() => onSelect(e.id)} className="w-full min-w-0 cursor-pointer truncate text-left hover:underline" title={`${e.type} · ${e.subject}`}>
        <span style={{ color: C.muted }}>{clockTime(e.time)}</span> {e.type === 'seat.mail.arrived' ? `${seatLabel(e.actor)} → ${seatLabel(e.subject)}` : `${e.type} ${e.subject}`}
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
