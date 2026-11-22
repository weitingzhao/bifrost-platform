import type { CommunicationFact } from '@bifrost/ui/communication'
/** Shared platform translation. A bus delivery is never a letter ACK. */
export const OBSERVER = 'gate:stream:console'
export type CommunicationContract = { topics: { type: string; domain?: string }[]; clients: { name: string; domain?: string }[] }
export function busFacts(contract: CommunicationContract | undefined, events: readonly { id: string; type: string }[], receipts: readonly { id: string; consumer: string; at: string }[]): CommunicationFact[] {
  if (!contract) return []
  const topics = new Map(contract.topics.map(t => [t.type, t.domain]))
  const clients = new Map(contract.clients.map(c => [c.name, c.domain]))
  const types = new Map(events.map(e => [e.id, e.type]))
  return receipts.flatMap(r => {
    const type = types.get(r.id)
    if (r.consumer === OBSERVER || !type) return []
    return [{ id: r.id + '>' + r.consumer + '@' + r.at, communication: r.id + '>' + r.consumer, channel: 'event' as const, stage: 'delivered' as const,
      from: topics.get(type), to: clients.get(r.consumer), label: type + ' → ' + r.consumer,
      evidence: { id: r.id, source: 'bus/stream receipt: ' + r.consumer, at: r.at, actor: r.consumer } }]
  })
}
