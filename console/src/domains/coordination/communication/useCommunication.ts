import { useBusStream } from '@/domains/foundation/bus/useBusStream'
import { fetchBusContract } from '@/domains/foundation/bus/busNextgen'
import { busFacts } from '@/domains/foundation/bus/communication'
import { operatorToken } from '@/api/client'
import type { Member } from '@bifrost/ui/communication'
import roster from './roster.generated.json'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { fetchLatestEvents, fetchEventsAfter, type RuntimeEventRow } from '@/api/agentRuntimeEvents'
import { fetchAgentThreads } from '@/api/agentThreads'
import { fetchSeatStatuses } from '@/api/seatStatus'
import { fetchLineage } from '@/api/lineage'
import { communicationSnapshot } from './adapter'
import { shellSelfHealthQuery } from '@/domains/foundation/shell/useShellStatusLine'
import { ENGINE_COMMUNICATION_KEY, engineCommunicationOn, engineSnapshot, fetchEngineCommunication } from './engineRead'

const TYPES = ['message_posted', 'seat.mail.arrived', 'message_acked'] as const
const CAP = 10000
type EventWindow = { rows: RuntimeEventRow[]; cursor: string; truncated: boolean; hasOlder: boolean }
let lastIdentity = '', identityVersion = 0
function readIdentityVersion() { const token = operatorToken(); if (token !== lastIdentity) { lastIdentity = token; identityVersion++ } return identityVersion }
/** Shared query state survives look changes; cursor advancement and retained events commit together. */
export function useCommunication() { return useCommunicationObservation().snapshot }
export function useCommunicationObservation() {
  const client = useQueryClient()
  const generation = readIdentityVersion()
  const queryKey = ['communication', 'messages', generation] as const
  // Engine or direct read, by the environment this Console views (engineRead.ts); not known yet → direct.
  const health = useQuery(shellSelfHealthQuery)
  const viewerEnv = health.data?.viewer_env
  const viaEngine = engineCommunicationOn(viewerEnv)
  const bus = useBusStream()
  const contract = useQuery({ queryKey: ['city', 'gen2', 'bus-contract'], queryFn: fetchBusContract, staleTime: 300000 })
  const events = useQuery<EventWindow>({
    queryKey,
    queryFn: async ({ client, queryKey, signal }) => {
      const prior = client.getQueryData<EventWindow>(queryKey)
      const page = prior ? await fetchEventsAfter(prior.cursor, { types: TYPES, limit: 500 }, signal) : await fetchLatestEvents({ types: TYPES, limit: 500 }, signal)
      const current = client.getQueryData<EventWindow>(queryKey) ?? prior
      const unique = new Map((current?.rows ?? []).map(e => [e.event_id, e]))
      for (const event of page.events) unique.set(event.event_id, event)
      const rows = [...unique.values()].sort((a,b) => b.event_id - a.event_id)
      return { rows: rows.slice(0, CAP), cursor: page.next, truncated: Boolean((current?.hasOlder ?? page.events.length === 500) || rows.length >= CAP), hasOlder: current?.hasOlder ?? page.events.length === 500 }
    }, refetchInterval: 15000, enabled: !viaEngine,
  })
  const engine = useQuery({ queryKey: ENGINE_COMMUNICATION_KEY, queryFn: ({ signal }) => fetchEngineCommunication(signal), refetchInterval: 15000, enabled: viaEngine, retry: false })
  const older = useMutation({
    mutationFn: async ({key}: {key: typeof queryKey}) => {
      // Stop the forward refresh before reading its state; history never overwrites the forward cursor.
      await client.cancelQueries({ queryKey: key, exact: true })
      if (key[2] !== readIdentityVersion()) return null
      const prior=client.getQueryData<EventWindow>(key)
      if(!prior?.rows.length) throw new Error('No history cursor available yet.')
      const before=Math.min(...prior.rows.map(e=>e.event_id))
      const page=await fetchLatestEvents({types:TYPES,limit:500,before})
      return {page,key}
    },
    onSuccess: result => {
      // Query cancellation does not cancel mutations. Reject a late old-generation page.
      if (!result || result.key[2] !== readIdentityVersion()) return
      const {page,key}=result
      client.setQueryData<EventWindow>(key, prior => {
      if(!prior)return prior
      const unique=new Map(prior.rows.map(e=>[e.event_id,e]))
      for(const event of page.events) unique.set(event.event_id,event)
      const rows=[...unique.values()].sort((a,b)=>b.event_id-a.event_id)
      const hasOlder=page.events.length===500
      return {...prior,rows:rows.slice(0,CAP),hasOlder,truncated:hasOlder||rows.length>=CAP}
      })
    },
  })
  const historyIsCurrent = older.variables?.key[2] === generation
  const threads = useQuery({ queryKey: ['agent-threads'], queryFn: fetchAgentThreads, refetchInterval: 30000, enabled: !viaEngine })
  const statuses=useQuery({queryKey:['seat-statuses'],queryFn:fetchSeatStatuses,refetchInterval:30000,retry:false,enabled:!viaEngine})
  const lineage=useQuery({queryKey:['lineage-lines',2],queryFn:()=>fetchLineage(2),staleTime:60000,retry:false,enabled:!viaEngine})
  const snapshot = communicationSnapshot(events.data?.rows ?? [], threads.data?.threads ?? [], {
    state: events.error ? 'error' : events.data ? 'ready' : 'loading',
    updatedAt: events.dataUpdatedAt ? new Date(events.dataUpdatedAt).toISOString() : undefined,
    truncated: events.data?.truncated ?? false,
    note: (events.error ? String(events.error) + '. ' : '') + 'Event stream: ' + bus.connection + (bus.receiptsOff ? ' (receipts unavailable)' : '') + '. Messages, inbox arrivals and ACK evidence from the platform ledger. Replies require explicit sender links and matching participants; older letters may lack links. Opening and acceptance are not recorded by this source. Absence of a reply is not agent inactivity. Crew membership is a source snapshot imported ' + roster.importedAt + '; it is not live presence. ' + (threads.error ? 'Member titles could not be refreshed.' : 'Work activity is independent of message activity.') + (statuses.error?' Work reports unavailable: '+String(statuses.error):' Work reports are self-reported, timestamped.') + (lineage.error?' Work records unavailable: '+String(lineage.error):' Commit records cover the last two days.'),
  }, roster.members as Member[], busFacts(contract.data, bus.events, bus.receipts), statuses.data??[],lineage.data?.threads??[])
  if (viaEngine) {
    const facts = busFacts(contract.data, bus.events, bus.receipts)
    const busNote = 'Event stream: ' + bus.connection + (bus.receiptsOff ? ' (receipts unavailable)' : '') + '.'
    return { snapshot: engineSnapshot(engine.data, engine.error, facts, busNote), history: {
      loadOlder: () => {}, loading: false, hasOlder: false,
      note: (engine.data?.coverage.history.note ?? 'Reading through the engine; history coverage is not known yet.') + ' Loading earlier history is not switched to the engine yet.',
    } }
  }
  return { snapshot, history: {
    loadOlder: () => older.mutate({key:queryKey}), loading: historyIsCurrent && older.isPending,
    hasOlder: Boolean(events.data?.hasOlder && events.data.rows.length<CAP),
    error: historyIsCurrent && older.error ? String(older.error) : undefined,
    note: !events.data ? 'Reading the ledger; history coverage is not known yet.' : events.data.rows.length===CAP ? 'Loaded 10,000 records; earlier records remain outside this view.' : events.data?.hasOlder ? 'Earlier ledger records are available. Load earlier history to extend this range.' : 'Beginning of the available filtered ledger reached; retention may limit older records.',
  } }
}
