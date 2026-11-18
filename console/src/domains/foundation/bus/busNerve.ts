import { authedFetch } from '@/api/client'
import type { BusEnvelope } from './busTrace'
import type { BusContract, BusNextgen } from './busNextgen'
import type { BusClient } from './useBusStream'

/**
 * The nerve map's model (Bus page, 2026-11-17): organs are the bus's
 * publishers, its topics and its readers; a pulse is one real event, from
 * its publisher to its topic at once, and from the topic to a reader when
 * that reader's receipt comes. Everything is read: the contract, the roster,
 * the generation's source switch, the stream. Nothing is made up to move.
 */

export type OrganTone = 'live' | 'lost' | 'new' | 'old' | 'plain'
export type Organ = { id: string; label: string; col: 'publisher' | 'topic' | 'reader'; tone: OrganTone; note?: string }
export type NerveModel = { publishers: Organ[]; topics: Organ[]; readers: Organ[]; publisherOf: Map<string, string> }

/** A receipt as the stream passes it on (event: rcpt). */
export type BusReceipt = { id: string; consumer: string; at: string }

/** The Console's own reader: every event it shows has this receipt, so it is not a reader on the map. */
export const OWN_CONSUMER = 'gate:stream:console'

const TONE_OF: Record<string, OrganTone> = { active: 'live', lost: 'lost', new: 'new', idle: 'plain' }

export function nerveModel(contract: BusContract, clients: readonly BusClient[], nextgen?: BusNextgen): NerveModel {
  const publisherOf = new Map<string, string>()
  const pubs = new Set<string>()
  for (const t of contract.topics) {
    const p = t.publisher || 'unknown'
    publisherOf.set(t.type, p)
    pubs.add(p)
  }
  const readers: Organ[] = clients
    .filter(c => c.name !== OWN_CONSUMER)
    .map(c => ({ id: c.name, label: c.name, col: 'reader', tone: TONE_OF[c.status] ?? 'plain', note: c.note }))
  // Wake rows name who is woken, not who reads: a woken seat reads through
  // the gate under its own client name, which the roster lists once seen.
  for (const g of nextgen?.readers ?? []) {
    for (const r of g.readers) {
      if (r.status === 'moved' || r.status === 'retired') continue
      readers.push({ id: `old:${r.id}`, label: r.id, col: 'reader', tone: 'old', note: `not on the bus yet: ${r.what}` })
    }
  }
  return {
    publishers: [...pubs].sort().map(p => ({ id: p, label: p, col: 'publisher', tone: 'plain' })),
    topics: contract.topics.map(t => t.type).sort().map(t => ({ id: t, label: t, col: 'topic', tone: 'plain' })),
    readers: readers.sort((a, b) => (a.tone === 'old' ? 1 : 0) - (b.tone === 'old' ? 1 : 0) || a.label.localeCompare(b.label)),
    publisherOf,
  }
}

/** Seconds a pulse waits at its topic for a reader's receipt before it is shown late. */
export const LATE_AFTER_S = 30

/** Replay speeds: one real second plays in 1/speed of a second. */
export const SPEEDS = [60, 600, 3600] as const

/**
 * When each replayed event fires, in milliseconds from the replay's start,
 * at speed (real seconds per played second). Events keep their real order
 * and spacing; a gap longer than maxGapMs once played is shortened to it, so
 * a quiet night does not leave the map still for minutes.
 */
export function replaySchedule(events: readonly BusEnvelope[], speed: number, maxGapMs = 1500): { at: number; event: BusEnvelope }[] {
  const sorted = [...events].sort((a, b) => Date.parse(a.time) - Date.parse(b.time))
  const out: { at: number; event: BusEnvelope }[] = []
  let at = 0
  let prev: number | null = null
  for (const e of sorted) {
    const t = Date.parse(e.time)
    if (prev != null) at += Math.min(maxGapMs, Math.max(0, (t - prev) / speed))
    prev = t
    out.push({ at, event: e })
  }
  return out
}

/** Readers whose name matches a receipt's consumer; a receipt of an undeclared name is still a reader. */
export function readerOf(consumer: string, readers: readonly Organ[]): string | null {
  if (consumer === OWN_CONSUMER) return null
  return readers.find(r => r.id === consumer)?.id ?? consumer
}

/** "seat.mail.arrived" events: the seats' letters, for the mail panel. */
export function seatMail(events: readonly BusEnvelope[]): BusEnvelope[] {
  return events.filter(e => e.type === 'seat.mail.arrived')
}

/** GET /api/v1/bus/observe: the team at work (bus/observe). */
export type ObserveLink = { from: string; to: string; kind: 'letter' | 'card'; count: number; ack_p50_s?: number; unacked: number; oldest_open?: string }
export type ObserveSeat = {
  id: string; letters_out: number; letters_in: number; ack_p50_s?: number; unacked: number
  last_status?: string; cards_open?: number; card_wait_p50_s?: number
}
export type BusRate = { minutes: number; events: number; per_minute: number; by_type: Record<string, number> }
export type TopicListen = { type: string; published: number; readers: string[]; heard: number; followed: number; display_only?: boolean }
export type BusListening = { hours: number; topics: TopicListen[]; heard_share?: number; closed_share?: number; caused_share?: number; counted?: number; watched?: number; notes?: string[] }

/** A topic that published and nobody heard. A display-only topic (published to be watched, registry display_only) is never deaf. */
export function isDeaf(t: TopicListen | undefined): boolean {
  return t != null && !t.display_only && t.published > 0 && t.heard === 0
}
export type BusObserve = {
  hours: number; generated_at: string; seats: ObserveSeat[]; links: ObserveLink[]; unmeasured: { what: string; why: string }[]
  rate?: BusRate; listening?: BusListening
}

/** Minutes an unacked letter or open card may wait before it is drawn as stuck. */
export const STUCK_AFTER_MIN = 10
/** Hours without a status report before a seat is drawn as quiet. */
export const QUIET_AFTER_H = 2

export type SeatState = 'ok' | 'stuck' | 'quiet'

/** A seat is stuck while a letter to it, or a card on it, waits past STUCK_AFTER_MIN; quiet when its last report is old. */
export function seatState(s: ObserveSeat, links: readonly ObserveLink[], now: number): SeatState {
  const waiting = links.some(l => l.to === s.id && l.oldest_open != null && now - Date.parse(l.oldest_open) > STUCK_AFTER_MIN * 60_000)
  if (waiting) return 'stuck'
  if (s.last_status != null && now - Date.parse(s.last_status) > QUIET_AFTER_H * 3600_000) return 'quiet'
  return 'ok'
}

/** "thread:local_ab850773-…" → "ab850773"; "seat:chief_of_staff" → "chief_of_staff"; others as they are. */
export function seatLabel(id: string): string {
  const m = /^thread:local_([0-9a-f]{8})/.exec(id)
  if (m) return m[1]
  return id.replace(/^(seat|thread):/, '')
}

/** "4.8s", "2.2m", "9.1h". */
export function shortSeconds(s: number): string {
  if (s < 90) return `${s.toFixed(s < 10 ? 1 : 0)}s`
  if (s < 5400) return `${(s / 60).toFixed(1)}m`
  return `${(s / 3600).toFixed(1)}h`
}

/** GET /api/v1/bus/observe?hours=24: the team at work, computed from the platform's records when asked. */
export async function fetchBusObserve(): Promise<BusObserve> {
  const r = await authedFetch('bus observe', '/api/v1/bus/observe?hours=24')
  return (await r.json()) as BusObserve
}

/**
 * A warm colour for a domain (the Owner's reference, 2026-11-17: nebulae of
 * small lights in orange, pink and white): a hue between magenta and amber
 * fixed by the id, so domains differ but share one warm range.
 */
export function warmColor(id: string, light = 0.66): string {
  let h = 2166136261
  for (let i = 0; i < id.length; i++) h = Math.imul(h ^ id.charCodeAt(i), 16777619)
  const hue = ((320 + ((h >>> 0) % 85)) % 360) / 360
  const sat = 0.9
  const f = (n: number) => {
    const k = (n + hue * 12) % 12
    const a = sat * Math.min(light, 1 - light)
    const c = light - a * Math.max(-1, Math.min(k - 3, 9 - k, 1))
    return Math.round(c * 255).toString(16).padStart(2, '0')
  }
  return `#${f(0)}${f(8)}${f(4)}`
}

/** How bright a dust point of an event this many hours old is: full when new, a tenth after a day. */
export function dustBrightness(ageHours: number): number {
  return Math.max(0.1, 1 - (0.9 * Math.min(24, Math.max(0, ageHours))) / 24)
}
