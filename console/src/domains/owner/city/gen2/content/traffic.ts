import { busFacts, OBSERVER } from '@/domains/foundation/bus/communication'
import type { Block } from '@bifrost/ui/city/cards'
import type { Panel } from '@bifrost/ui/city/content'
import type { Trip } from '@bifrost/ui/city/traffic'

/**
 * The bus's deliveries as the City's traffic (ISSUE-9: the communication line first, the first
 * kind of vehicle). Everything is read from the communication line's contract and its stream, nothing guessed:
 *   from   the topic's domain, as the bus registry declares it (GET /api/v1/bus/contract, topics[].domain)
 *   to     the domain of the client whose receipt came (clients[].domain): the reader that took it
 *   when   a receipt on the stream (GET /api/v1/bus/stream?receipts=1): the delivery happened
 * The City's own reader (the Console's stream) is not a trip: it is the picture reading, not the system talking.
 * A delivery whose topic or reader declares no domain, or a domain the map does not serve, is counted, not drawn
 * (it is coupling nobody has placed yet: the comm line's to place, the map's to see).
 */

/** The shapes read here, as the comm line serves them (a subset of the Bus page's own types). */
export type TrafficContract = {
  topics: { type: string; domain?: string }[]
  clients: { name: string; domain?: string }[]
}
export type TrafficEvent = { id: string; type: string }
export type TrafficReceipt = { id: string; consumer: string; at: string }

/** The Console's own stream reader (busNerve.ts OWN_CONSUMER): never a trip. */
export const PICTURE_READER = OBSERVER

export type Traffic = { trips: Trip[]; unplaced: number }

export function tripsOf(contract: TrafficContract | undefined, events: readonly TrafficEvent[], receipts: readonly TrafficReceipt[], places: ReadonlySet<string>): Traffic {
  const trips: Trip[] = []
  let unplaced = 0
  const seen = new Set<string>()
  for (const fact of busFacts(contract, events, receipts)) {
    if (seen.has(fact.communication)) continue
    seen.add(fact.communication)
    const from = fact.from ?? '', to = fact.to ?? ''
    if (!places.has(from) || !places.has(to)) { unplaced++; continue }
    trips.push({ id: fact.communication, kind: 'event', from: [from], to: [to], what: fact.label ?? fact.communication })
  }
  return { trips, unplaced }
}

/** How the stream stands, as the Bus page's reader says it (useBusStream). */
export type TrafficRead = Traffic & { connection: string; detail: string | null }

/** The town's traffic card: what the roads show and what they do not (never an empty road read as quiet). */
export function trafficPanel(read: TrafficRead): Panel {
  const live = read.connection === 'live'
  const stream: Block = {
    kind: 'facts',
    rows: [
      { label: 'Stream', value: { text: read.connection, tone: live ? 'ok' : read.connection === 'connecting' ? 'pending' : 'unknown' }, note: read.detail ?? '' },
      { label: 'Deliveries seen', value: read.trips.length, note: 'since this page opened, from a reader the bus registry places; one inside a single domain is that district’s own business, not driven on the town' },
      { label: 'Not placed', value: read.unplaced ? { text: read.unplaced, tone: 'warn' } : 0, note: 'a topic or reader that declares no domain the map serves' },
    ],
  }
  const note: Block = { kind: 'note', text: 'A coral bus is one event taken by one reader, from the topic’s domain to the reader’s. The stream starts when the page opens; nothing before it is replayed.' }
  return { id: 'traffic', title: 'Traffic', dock: 'left', cards: [stream, note] }
}
