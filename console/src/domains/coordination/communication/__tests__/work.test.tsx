import { describe, expect, it } from 'vitest'
import { communicationSnapshot } from '../adapter'
import type { AgentThread } from '@/api/agentThreads'
import type { SeatStatus } from '@/api/seatStatus'
import type { LineageThread } from '@/api/lineage'
const coverage={state:'ready' as const,truncated:false,note:'test',updatedAt:'2026-11-22T12:30:00Z'}
const thread={thread:'sidebar-1',session:'vendor-1',title:'Shipping Agents',vendor:'codex',status:'in_turn',at:'2026-11-22T12:20:00Z',event:'after_tool',tool:'build',binding:{state:'linked',root:'R5',plan_node_id:'R5:task',external_work_id:'ew-1'}} as AgentThread
const status={seat:'thread:vendor-1',message_id:'m-status',at:'2026-11-22T12:10:00Z',goal:'Deliver working observation',doing:'Validate timeline',percent:60,percent_basis:'3 / 5 checks',waiting_on:'One external receipt',next:'Run browser QA',charter:'Cockpit'} as SeatStatus
describe('member work evidence',()=>{
 it('joins only explicit session identity and keeps self-reported work separate from membership and permissions',()=>{
  const lineage={session:'vendor-1',repos:[{repo:'platform',commits:[{sha:'abc',subject:'Implement history',at:'2026-11-22T12:00:00Z',ref:'ready/agents',landed:false}]}]} as LineageThread
  const s=communicationSnapshot([], [thread],coverage,[],[],[status],[lineage])
  const m=s.members.find(m=>m.id==='thread:sidebar-1')!
  expect(m.work).toMatchObject({line:'Shipping Agents',goal:status.goal,waitingOn:status.waiting_on,declaredCharter:'Cockpit',binding:'linked · R5 · R5:task · ew-1'})
  expect(m.memberships).toEqual([])
  expect(m.records?.map(r=>r.evidence.source)).toEqual(['agent/threads','agentrt/seats','lineage'])
  expect(m.activity?.label).toBe('in turn')
 })
 it('does not guess across duplicate session bindings or equal display names',()=>{
  const s=communicationSnapshot([], [thread,{...thread,thread:'sidebar-2'}],coverage,[],[],[status])
  expect(s.members.find(m=>m.id==='thread:vendor-1')?.work?.goal).toBe(status.goal)
  expect(s.members.find(m=>m.id==='thread:sidebar-1')?.work?.goal).toBeUndefined()
 })
 it('keeps the newest source report and shows parse problems without fabricated progress',()=>{
  const s=communicationSnapshot([], [thread],coverage,[],[],[{...status,at:'2026-11-22T12:25:00Z',problem:'missing goal'},status])
  const work=s.members.find(m=>m.id==='thread:sidebar-1')?.work
  expect(work?.problem).toBe('missing goal');expect(work?.progress).toBeUndefined()
 })
})
