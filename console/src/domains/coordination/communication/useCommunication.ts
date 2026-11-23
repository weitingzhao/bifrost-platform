import { useBusStream } from '@/domains/foundation/bus/useBusStream'
import { fetchBusContract } from '@/domains/foundation/bus/busNextgen'
import { busFacts } from '@/domains/foundation/bus/communication'
import { operatorToken } from '@/api/client'
import type { Member } from '@bifrost/ui/communication'
import roster from './roster.generated.json'
import { useQuery } from '@tanstack/react-query'
import { fetchLatestEvents, fetchEventsAfter, type RuntimeEventRow } from '@/api/agentRuntimeEvents'
import { fetchAgentThreads } from '@/api/agentThreads'
import { communicationSnapshot } from './adapter'

const TYPES = ['message_posted', 'seat.mail.arrived', 'message_acked'] as const
const CAP = 1000
let lastIdentity = '', identityVersion = 0
function readIdentityVersion() { const token = operatorToken(); if (token !== lastIdentity) { lastIdentity = token; identityVersion++ } return identityVersion }
/** Shared query state survives look changes; cursor advancement and retained events commit together. */
export function useCommunication() {
  const bus = useBusStream()
  const contract = useQuery({ queryKey: ['city', 'gen2', 'bus-contract'], queryFn: fetchBusContract, staleTime: 300000 })
  const events = useQuery<{ rows: RuntimeEventRow[]; cursor: string; truncated: boolean }>({
    queryKey: ['communication', 'messages', readIdentityVersion()],
    queryFn: async ({ client, queryKey, signal }) => {
      const prior = client.getQueryData<{ rows: RuntimeEventRow[]; cursor: string; truncated: boolean }>(queryKey)
      const page = prior ? await fetchEventsAfter(prior.cursor, { types: TYPES, limit: 500 }, signal) : await fetchLatestEvents({ types: TYPES, limit: 500 }, signal)
      const unique = new Map((prior?.rows ?? []).map(e => [e.event_id, e]))
      for (const event of page.events) unique.set(event.event_id, event)
      const rows = [...unique.values()].sort((a,b) => b.event_id - a.event_id)
      return { rows: rows.slice(0, CAP), cursor: page.next, truncated: Boolean(prior?.truncated || page.events.length === 500 || rows.length > CAP) }
    }, refetchInterval: 15000,
  })
  const threads = useQuery({ queryKey: ['agent-threads'], queryFn: fetchAgentThreads, refetchInterval: 30000 })
  return communicationSnapshot(events.data?.rows ?? [], threads.data?.threads ?? [], {
    state: events.error ? 'error' : events.data ? 'ready' : 'loading',
    updatedAt: events.dataUpdatedAt ? new Date(events.dataUpdatedAt).toISOString() : undefined,
    truncated: events.data?.truncated ?? false,
    note: (events.error ? String(events.error) + '. ' : '') + 'Event stream: ' + bus.connection + (bus.receiptsOff ? ' (receipts unavailable)' : '') + '. Messages, inbox arrivals and ACK evidence from the platform ledger. Replies require explicit sender links and matching participants; older letters may lack links. Opening and acceptance are not recorded by this source. Absence of a reply is not agent inactivity. Crew membership is a source snapshot imported ' + roster.importedAt + '; it is not live presence. ' + (threads.error ? 'Member titles could not be refreshed.' : 'Work activity is independent of message activity.'),
  }, roster.members as Member[], busFacts(contract.data, bus.events, bus.receipts))
}
