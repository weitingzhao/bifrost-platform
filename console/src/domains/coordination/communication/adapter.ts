import { exchangesOf, membersOf, type CommunicationFact, type CommunicationSnapshot, type Member } from '@bifrost/ui/communication'
import type { RuntimeEventRow } from '@/api/agentRuntimeEvents'
import type { SeatStatus } from '@/api/seatStatus'
import type { LineageThread } from '@/api/lineage'
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
export function communicationSnapshot(events: readonly RuntimeEventRow[], threads: readonly AgentThread[], coverage: CommunicationSnapshot['coverage'], roster: readonly Member[] = [], bus: readonly CommunicationFact[] = [], statuses: readonly SeatStatus[] = [], lineage: readonly LineageThread[] = []): CommunicationSnapshot {
  const exchanges = exchangesOf([...messageFacts(events), ...bus])
  const identities = new Map(roster.map(m => [m.id, m]))
  for (const t of threads) {
    const id = t.thread.startsWith('thread:') ? t.thread : 'thread:' + t.thread
    const old = identities.get(id)
    const recent=Date.parse(coverage.updatedAt??'')-Date.parse(t.at)<86400000
    if (!old && !recent && !exchanges.some(x => x.from === id || x.to === id)) continue
    identities.set(id, { id, label: t.title || old?.label || t.vendor + ' · ' + t.thread.slice(0, 8), memberships: old?.memberships ?? [], work: {line:t.title||t.thread, doing:t.work, binding:t.binding ? [t.binding.state,t.binding.root,t.binding.plan_node_id,t.binding.external_work_id].filter(Boolean).join(' · ') : undefined,evidence:{id:t.thread,source:'agent/threads',at:t.at}}, records:[{label:'Last reported thread event',detail:[t.event,t.tool,t.reason].filter(Boolean).join(' · '),evidence:{id:t.thread+':'+t.at,source:'agent/threads',at:t.at}}], activity: { label: t.status.replaceAll('_', ' '), evidence: { id: t.thread, source: 'agent/threads', at: t.at } } })
  }
  // Exact identifiers only. Vendor session aliases are accepted only when uniquely bound by the thread source.
  const resolve=(seat:string)=>{
    if(identities.has(seat))return seat
    const session=seat.replace(/^thread:/,'')
    const matches=threads.filter(t=>t.thread===session||t.session===session)
    const ids=[...new Set(matches.map(t=>t.thread.startsWith('thread:')?t.thread:'thread:'+t.thread))]
    return ids.length===1?ids[0]:seat
  }
  for(const status of [...statuses].sort((a,b)=>a.at.localeCompare(b.at))){
    const id=resolve(status.seat),old=identities.get(id)
    const evidence={id:status.message_id,source:'agentrt/seats',at:status.at}
    identities.set(id,{id,label:old?.label??id,memberships:old?.memberships??[],...old,
      work:{line:old?.work?.line??id,goal:status.goal,doing:status.doing,waitingOn:status.waiting_on,next:status.next,progress:status.problem?undefined:status.percent,progressBasis:status.percent_basis,declaredCharter:status.charter,binding:old?.work?.binding,problem:status.problem,evidence},
      records:[...(old?.records??[]),{label:'Latest work report',detail:[status.goal,status.doing,status.waiting_on?'Waiting: '+status.waiting_on:'',status.next?'Next: '+status.next:''].filter(Boolean).join(' · '),evidence}]})
  }
  for(const line of lineage){
    if(!line.session)continue
    const id=resolve('thread:'+line.session),old=identities.get(id)
    if(!old)continue
    const records=line.repos.flatMap(r=>r.commits.map(c=>({label:c.subject,detail:r.repo+' · '+c.sha+' · '+(c.landed?'On main':'On branch '+c.ref)+(c.reached?.length?' · Releases: '+c.reached.map(e=>e.env).join(', '):''),evidence:{id:c.sha,source:'lineage',at:c.at},href:'#commit-lineage'})))
    identities.set(id,{...old,records:[...(old.records??[]),...records]})
  }
  return { version: 1, exchanges, members: membersOf(exchanges, [...identities.values()]), coverage }
}
