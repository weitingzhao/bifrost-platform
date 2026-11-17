import type { AgentThreadStatus } from '@/api/agentThreads'
import { QUIET_AFTER_H, STUCK_AFTER_MIN, seatLabel, seatState, shortSeconds, warmColor, type BusListening, type NerveModel, type ObserveLink, type ObserveSeat, type OrganTone } from './busNerve'
import { CORE, domainOf } from './busNerveLayout'
import type { GLink, GNode } from './BusNerveGraph'

/**
 * The observatory's own palette: a dark glass the body lives in, whatever the
 * Console's theme (the Owner's reference, 2026-11-17: a black ground, terminal
 * panels, a living picture in the middle). Fixed on purpose, not theme tokens.
 */
export const C = {
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
export const PULSE = { event: '#ffc46b', receipt: '#ffffff', letter: '#ff8fc7' } as const

export const TONE: Record<OrganTone, string> = { live: '#ffd0a8', lost: C.grey, new: C.amber, old: C.grey, plain: C.muted }

/** A seat's breath, from its session's own heartbeat (GET /agent/threads): fast in a turn, slow idle, none when down. */
export const BREATH: Record<AgentThreadStatus | 'none', { color: string; dur: number | null; label: string }> = {
  in_turn: { color: '#fff1c9', dur: 1.1, label: 'in a turn' },
  waiting_owner: { color: '#ffb347', dur: 2.2, label: 'waiting for the owner' },
  idle: { color: '#ff9a6b', dur: 4.5, label: 'idle' },
  silent: { color: C.grey, dur: null, label: 'silent mid-turn' },
  host_lost: { color: C.grey, dur: null, label: 'host lost' },
  superseded: { color: C.grey, dur: null, label: 'superseded' },
  none: { color: C.grey, dur: null, label: 'no heartbeat' },
}

/** A seat's name: its title when one is known, else its short id. */
export function nameOf(id: string, names: Map<string, string>): string {
  return names.get(id) ?? seatLabel(id)
}

/** What the body is built from: all of it read, nothing typed in. */
export type BodyInput = {
  model: NerveModel
  /** Which domain each topic tells of (the register's domain). */
  domainOfType: Map<string, string>
  /** The domains opened to their parts. */
  expanded: Set<string>
  seats: string[]
  /** Each seat's heartbeat. */
  beat: Map<string, { status: AgentThreadStatus; quiet_seconds: number }>
  observed: Map<string, ObserveSeat>
  teamLinks: ObserveLink[]
  names: Map<string, string>
  /** The ages in hours of each domain's events of the last day. */
  dustByDomain: Map<string, number[]>
  /** Who hears each topic over the last day (bus/observe); absent, nothing is said about listening. */
  listening?: BusListening
  now: number
}

/**
 * The body's nodes and links (the Bus page and the bench both draw from this).
 * By default each domain is one cloud; an opened domain shows its topics and
 * their publishers, and its seats, readers or old roads.
 */
export function buildBody({ model, domainOfType, expanded, seats, beat, observed, teamLinks, names, dustByDomain, listening, now }: BodyInput): { nodes: GNode[]; links: GLink[] } {
    const wall = now
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
    const listen = new Map((listening?.topics ?? []).map(t => [t.type, t]))
    const pubs = new Set<string>()
    for (const [d, types] of [...byDomain.entries()].sort()) {
      const open = expanded.has(d)
      const more = members.get(d) ?? 0
      const hue = warmColor(d)
      const ages = dustByDomain.get(d) ?? []
      const pub = types.reduce((n, t) => n + (listen.get(t)?.published ?? 0), 0)
      const heardN = types.reduce((n, t) => n + (listen.get(t)?.heard ?? 0), 0)
      const deafN = types.filter(t => (listen.get(t)?.published ?? 0) > 0 && listen.get(t)?.heard === 0).length
      const hearing = listening && pub > 0 ? ` · ${Math.round((heardN / pub) * 100)}% heard${deafN > 0 ? ` · ${deafN} topics no one listens to` : ''}` : ''
      nodes.push({ id: `d:${d}`, kind: 'cluster', label: open ? `${d} ▾` : d, color: hue, size: 3, breath: null,
        cloud: { parts: types.length + more, dust: ages }, open,
        note: `domain ${d}: ${types.length} topics${more > 0 ? `, ${more} more inside (seats, readers, old roads)` : ''}, ${ages.length} events in the last day${hearing} · click to ${open ? 'fold' : 'open'}` })
      links.push({ source: `d:${d}`, target: CORE, color: '#ffd9c2', width: 0.4 })
      if (!open) continue
      for (const t of types) {
        const heard = listen.get(t)
        // A topic that published and nobody heard: the event dies at the topic. Grey, with its name and why.
        const deaf = heard != null && heard.published > 0 && heard.heard === 0
        nodes.push({
          id: `t:${t}`, kind: 'topic', label: deaf ? `${t} · no one listening` : t, color: deaf ? C.grey : warmColor(d, 0.78), size: deaf ? 1.4 : 0.9, breath: null,
          ring: deaf ? C.amber : undefined, named: deaf,
          note: heard ? `${t} · ${heard.published} published in ${listening?.hours ?? 24}h · ${heard.heard} heard · ${heard.followed} followed · ${heard.readers.length} declared readers` : t,
        })
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
      const st = o ? seatState(o, teamLinks, wall) : 'ok'
      const read = [
        o && o.letters_in > 0 ? `in ${o.letters_in}` : '',
        o?.ack_p50_s != null ? `ack ${shortSeconds(o.ack_p50_s)}` : '',
        o && o.unacked > 0 ? `${o.unacked} open` : '',
        o?.cards_open ? `${o.cards_open} cards open` : '',
        o?.card_wait_p50_s != null ? `cards wait ${shortSeconds(o.card_wait_p50_s)}` : '',
        t && t.status !== 'in_turn' && t.quiet_seconds > 0 ? `quiet ${shortSeconds(t.quiet_seconds)}` : '',
      ].filter(Boolean).join(' · ')
      const name = nameOf(id, names)
      // Why a seat shows its id: neither its heartbeat nor its commits' lineage carries a title for it.
      const unnamed = !names.has(id) && id !== 'owner' ? ' · shown by its id: no title from its heartbeat or from its commits\' lineage' : ''
      const owner = id === 'owner'
      nodes.push({
        id: `s:${id}`, kind: 'seat', label: name.length > 28 ? `${name.slice(0, 27)}…` : name, color: owner ? '#ff8fc7' : br.color, size: owner ? 2.4 : 1.6,
        breath: owner ? null : br.dur, ring: st === 'stuck' ? C.red : undefined,
        note: `${name} (${id}) · ${owner ? 'the owner' : br.label}${unnamed}${read ? ` · ${read}` : ''}${st === 'quiet' ? ` · no report for ${QUIET_AFTER_H}h` : ''}${st === 'stuck' ? ` · something waits over ${STUCK_AFTER_MIN}m` : ''}`,
      })
      links.push({ source: `s:${id}`, target: `d:${seatDomain(id)}`, color: C.link, width: 0.5 })
    }
    const ids = new Set(nodes.map(n => n.id))
    for (const l of teamLinks) {
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
}
