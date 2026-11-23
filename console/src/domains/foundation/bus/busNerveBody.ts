import { STATUS } from '@bifrost/ui/city/world'
import type { AgentThreadStatus } from '@/api/agentThreads'
import { ASK_STUCK_AFTER_MIN, QUIET_AFTER_H, isDeaf, STUCK_AFTER_MIN, seatLabel, seatState, shortSeconds, warmColor, type BusListening, type NerveModel, type ObserveLink, type ObserveSeat, type OrganTone } from './busNerve'
import { CORE, domainOf } from './busNerveLayout'
import type { GLink, GNode } from './BusNerveGraph'
import { arcCurve, arcsOf, cubicAt, funnelOf, halosOf, layerPositions, median, type DomainTier, type Funnel, type Route, type Vec } from './busNerveDirect'

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
  // The colours that mean a state are the engine's (箭10 2026-11-23, the Console's Bus slice): no receipt reads as
  // fail everywhere, nobody listening or late as warn, a lost reader as unknown, information as info, on as ok.
  // The ground, text and the scene's own hues stay the observatory's.
  blue: STATUS.info.ink,
  green: STATUS.ok.ink,
  red: STATUS.fail.ink,
  amber: STATUS.warn.ink,
  grey: STATUS.unknown.fill,
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
      // Display-only topics are watched, not acted on: out of the domain's heard share.
      const acted = types.filter(t => !listen.get(t)?.display_only)
      const pub = acted.reduce((n, t) => n + (listen.get(t)?.published ?? 0), 0)
      const heardN = acted.reduce((n, t) => n + (listen.get(t)?.heard ?? 0), 0)
      const deafN = types.filter(t => isDeaf(listen.get(t))).length
      const hearing = listening && pub > 0 ? ` · ${Math.round((heardN / pub) * 100)}% heard${deafN > 0 ? ` · ${deafN} topics no one listens to` : ''}` : ''
      nodes.push({ id: `d:${d}`, kind: 'cluster', label: open ? `${d} ▾` : d, color: hue, size: 3, breath: null,
        cloud: { parts: types.length + more, dust: ages }, open,
        note: `domain ${d}: ${types.length} topics${more > 0 ? `, ${more} more inside (seats, readers, old roads)` : ''}, ${ages.length} events in the last day${hearing} · click to ${open ? 'fold' : 'open'}` })
      links.push({ source: `d:${d}`, target: CORE, color: '#ffd9c2', width: 0.4 })
      if (!open) continue
      for (const t of types) {
        const heard = listen.get(t)
        // A topic that published and nobody heard: the event dies at the topic. Grey, with its name and why.
        const deaf = isDeaf(heard)
        nodes.push({
          id: `t:${t}`, kind: 'topic', label: deaf ? `${t} · no one listening` : t, color: deaf ? C.grey : warmColor(d, 0.78), size: deaf ? 1.4 : 0.9, breath: null,
          ring: deaf ? C.amber : undefined, named: deaf,
          note: heard ? `${t} · ${heard.published} published in ${listening?.hours ?? 24}h · ${heard.heard} heard · ${heard.followed} followed · ${heard.readers.length} declared readers${heard.display_only ? ' · display only: published to be watched, not counted in heard' : ''}` : t,
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
      // A heartbeat status the map does not know breathes as none: never thrown on (2026-11-17, the roster's idle).
      const br = (BREATH as Record<string, (typeof BREATH)['none']>)[t?.status ?? 'none'] ?? BREATH.none
      const o: ObserveSeat | undefined = observed.get(id)
      const st = o ? seatState(o, teamLinks, wall) : 'ok'
      const read = [
        o && o.letters_in > 0 ? `in ${o.letters_in}` : '',
        o?.ack_p50_s != null ? `ack ${shortSeconds(o.ack_p50_s)}` : '',
        o && o.unacked > 0 ? `${o.unacked} open` : '',
        o?.asks_waiting ? `waits on ${o.asks_waiting} answer${o.asks_waiting === 1 ? '' : 's'}` : '',
        o?.asks_owed ? `owes ${o.asks_owed} answer${o.asks_owed === 1 ? '' : 's'}` : '',
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
        note: `${name} (${id}) · ${owner ? 'the owner' : br.label}${unnamed}${read ? ` · ${read}` : ''}${st === 'quiet' ? ` · no report for ${QUIET_AFTER_H}h` : ''}${st === 'stuck' ? ` · something waits over ${STUCK_AFTER_MIN}m, or an ask over ${ASK_STUCK_AFTER_MIN}m` : ''}`,
      })
      links.push({ source: `s:${id}`, target: `d:${seatDomain(id)}`, color: C.link, width: 0.5 })
    }
    const ids = new Set(nodes.map(n => n.id))
    for (const l of teamLinks) {
      const a = l.from === 'seats' ? CORE : `s:${l.from}`
      const b = `s:${l.to}`
      if (!ids.has(a) || !ids.has(b) || a === b) continue
      const openMin = l.oldest_open ? (wall - Date.parse(l.oldest_open)) / 60_000 : 0
      const askMin = l.oldest_ask ? (wall - Date.parse(l.oldest_ask)) / 60_000 : 0
      const stuck = (l.unacked > 0 && openMin > STUCK_AFTER_MIN) || ((l.open_asks ?? 0) > 0 && askMin > ASK_STUCK_AFTER_MIN)
      links.push({
        source: a, target: b, color: stuck ? C.red : l.kind === 'card' ? C.amber : PULSE.letter, dashed: l.kind === 'card',
        width: stuck ? 0.6 : Math.min(2.5, 0.6 + Math.log2(1 + l.count) * 0.5),
        label: `${nameOf(l.from, names)} → ${nameOf(l.to, names)}: ${l.count} ${l.kind === 'card' ? 'cards' : 'letters'}${l.ack_p50_s != null ? ` · ${l.kind === 'card' ? 'wait' : 'ack'} ${shortSeconds(l.ack_p50_s)}` : ''}${l.unacked > 0 ? ` · ${l.unacked} open${stuck && openMin > STUCK_AFTER_MIN ? ` ${Math.round(openMin)}m` : ''}` : ''}${l.open_asks ? ` · ${l.open_asks} unanswered ask${l.open_asks === 1 ? '' : 's'} ${Math.round(askMin)}m` : ''}`,
      })
    }
    return { nodes, links }
}

/** An arc as the renderer draws it: its curve, how thick, and the state of its far half. */
export type GArc = {
  id: string
  from: string
  to: string
  curve: [Vec, Vec, Vec, Vec]
  width: number
  color: string
  /** The far half (the bus to the reader): grey when a reader on it is lost or stopped. */
  back: 'ok' | 'lost'
  label: string
}

/** What the direct view adds to the body's input: where domains stand, and who takes what. */
export type DirectInput = BodyInput & {
  tiers: DomainTier[]
  routes: Route[]
  /** Each reader's roster status (/bus/clients): a lost one greys its arcs' far half. */
  roster: Map<string, string>
  /** Seconds from each event to a reader's receipt, by arc, as seen on this page (live and replay). */
  delays: Map<string, number[]>
}

/**
 * The direct view (the default): domains at their tier's height, in depth, an arc for
 * each pair of domains one's topics reach the other's readers by, and a halo
 * round a domain that sends what nobody takes. No bus node: the bus is the
 * bridge, shown only by the pulse that waits at an arc's middle. An opened
 * domain shows its parts around it, as in the bus view.
 */
export function buildDirectBody(input: DirectInput): { nodes: GNode[]; links: GLink[]; arcs: GArc[]; funnel?: Funnel } {
  const bus = buildBody(input)
  const { domainOfType, listening, tiers, routes, roster, delays } = input
  const clusters = bus.nodes.filter(n => n.kind === 'cluster').map(n => n.id.slice(2))
  // Every domain the table declares has its place in the volume, with or without topics of its own.
  const ids = [...new Set([...tiers.map(t => t.id), ...clusters])]
  const tierOf = new Map(tiers.map(t => [t.id, t.tier]))
  const positions = layerPositions(ids.map(id => ({ id, tier: tierOf.get(id) })))
  const halos = halosOf(routes, domainOfType, listening)
  const maxHalo = Math.max(1, ...[...halos.values()].map(h => h.count))
  const nodes: GNode[] = []
  const have = new Set(bus.nodes.map(n => n.id))
  for (const id of ids) {
    if (!have.has(`d:${id}`)) {
      nodes.push({ id: `d:${id}`, kind: 'cluster', label: id, color: warmColor(id), size: 3, breath: null, cloud: { parts: 0, dust: [] },
        note: `domain ${id}: no topics of its own on the bus` })
    }
  }
  for (const n of [...bus.nodes, ...nodes]) {
    if (n.kind === 'core') continue
    if (n.kind === 'cluster') {
      const d = n.id.slice(2)
      const h = halos.get(d)
      const undeclared = tierOf.get(d) == null ? ' · untiered: the ownership table gives it no tier' : ''
      n.pin = positions.get(d)
      if (h) {
        n.halo = Math.max(0.25, h.count / maxHalo)
        n.note += ` · sends what nobody takes: ${h.count} events in ${listening?.hours ?? 24}h (${h.topics.join(', ')})`
      }
      n.note += undeclared
    }
  }
  const all = [...bus.nodes.filter(n => n.kind !== 'core'), ...nodes]
  const arcs: GArc[] = []
  const maxCount = Math.max(1, ...arcsOf(routes, domainOfType, listening).map(a => a.count))
  const byId = new Map(all.map(n => [n.id, n]))
  for (const a of arcsOf(routes, domainOfType, listening)) {
    // A domain that takes its own topics: no loop over it, a ring inside it, and the topics on its hover.
    if (a.from === a.to) {
      const n = byId.get(`d:${a.from}`)
      if (n) {
        n.self = true
        n.note += ` · hears itself: ${a.topics.join(', ')} read by ${a.readers.join(', ')} (${a.count} events)`
      }
      continue
    }
    const p = positions.get(a.from)
    const q = positions.get(a.to)
    if (!p || !q) continue
    const curve = arcCurve(p, q)
    const lost = a.readers.filter(r => roster.get(r) === 'lost')
    const p50 = median(delays.get(a.id) ?? [])
    const label = `${a.from} → ${a.to}: ${a.count} events in ${listening?.hours ?? 24}h · ${a.topics.join(', ')} · read by ${a.readers.join(', ')}` +
      `${p50 != null ? ` · median ${shortSeconds(p50)} to a receipt` : ' · no receipt timed on this page yet'}${lost.length > 0 ? ` · lost: ${lost.join(', ')}` : ''}`
    arcs.push({ id: a.id, from: `d:${a.from}`, to: `d:${a.to}`, curve, width: 0.35 + 1.6 * Math.sqrt(a.count / maxCount), color: warmColor(a.from, 0.7),
      back: lost.length > 0 ? 'lost' : 'ok', label })
    // The arc's middle: where a pulse waits in the bus; hovering it reads the arc.
    all.push({ id: `m:${a.id}`, kind: 'mid', label: p50 != null ? shortSeconds(p50) : '', color: '#ffe6cc', size: 2.4, breath: null, note: label, pin: cubicAt(curve, 0.5) })
  }
  // Parts of an opened domain hang on their domain; links to the bus are gone with it.
  const links = bus.links.filter(l => l.source !== CORE && l.target !== CORE)
  return { nodes: all, links, arcs, funnel: funnelOf(positions, new Map(ids.map(id => [id, tierOf.get(id)]))) }
}
