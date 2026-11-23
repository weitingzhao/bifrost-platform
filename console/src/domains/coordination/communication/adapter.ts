import { exchangesOf, membersOf, type CommunicationFact, type CommunicationSnapshot, type Member } from '@bifrost/ui/communication'
import type { RuntimeEventRow } from '@/api/agentRuntimeEvents'
import type { AgentThread } from '@/api/agentThreads'
const string = (v: unknown) => typeof v === 'string' ? v : undefined
/** Platform vocabulary ends here. The engine receives evidence, never platform envelopes. */
export function messageFacts(events: readonly RuntimeEventRow[]): CommunicationFact[] {
  return events.flatMap(e => {
    const id = string(e.body.message_id)
    if (!id || !['message_posted', 'seat.mail.arrived', 'message_acked'].includes(e.event_type)) return []
    return [{ id: 'agentrt:' + e.event_id, communication: id, channel: 'message' as const,
      stage: e.event_type === 'message_posted' ? 'sent' as const : e.event_type === 'seat.mail.arrived' ? 'delivered' as const : 'acknowledged' as const,
      ...(e.event_type === 'message_posted' ? { from: e.actor_id, to: string(e.body.to), label: string(e.body.kind), replyTo: string(e.body.reply_to) } : {}),
      evidence: { id: String(e.event_id), source: 'agentrt/events', at: e.at, actor: e.actor_id } }]
  })
}
export function communicationSnapshot(events: readonly RuntimeEventRow[], threads: readonly AgentThread[], coverage: CommunicationSnapshot['coverage'], roster: readonly Member[] = [], bus: readonly CommunicationFact[] = []): CommunicationSnapshot {
  const exchanges = exchangesOf([...messageFacts(events), ...bus])
  const identities = new Map(roster.map(m => [m.id, m]))
  for (const t of threads) {
    const id = t.thread.startsWith('thread:') ? t.thread : 'thread:' + t.thread
    const old = identities.get(id)
    if (!old && !exchanges.some(x => x.from === id || x.to === id)) continue
    identities.set(id, { id, label: t.title || old?.label || t.vendor + ' · ' + t.thread.slice(0, 8), memberships: old?.memberships ?? [], activity: { label: t.status.replaceAll('_', ' '), evidence: { id: t.thread, source: 'agent/threads', at: t.at } } })
  }
  return { version: 1, exchanges, members: membersOf(exchanges, [...identities.values()]), coverage }
}
