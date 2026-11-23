// 2026-11-22 Owner: observation must never ACK, accept, reply or approve on a member's behalf.
import { render, screen, fireEvent, cleanup } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { AgentWorld, WorkingStudio, exchangesOf } from '@bifrost/ui/communication'
import { messageFacts } from '../adapter'
afterEach(cleanup)
describe('read-only observation', () => {
  it('switches looks and filters a member without any network write', () => {
    const fetch = vi.fn(); vi.stubGlobal('fetch', fetch)
    render(<WorkingStudio stationImage="/station.png" snapshot={{version:1,members:[{id:'a',label:'A',memberships:[]}],exchanges:exchangesOf([{id:'e1',communication:'m1',channel:'message',stage:'sent',from:'a',to:'b',label:'A request',evidence:{id:'e1',source:'test',at:'2026-11-22T12:00:00Z'}}]),coverage:{state:'ready',note:'Test',truncated:false}}} />)
    fireEvent.click(screen.getByRole('button',{name:'Ledger',exact:true}))
    expect(screen.getByText('message · m1')).toBeTruthy()
    expect(screen.getAllByText('Not observed')).toHaveLength(7)
    expect(screen.getByText('Opened by recipient')).toBeTruthy()
    expect(fetch).not.toHaveBeenCalled(); vi.unstubAllGlobals()
  })
  // 2026-11-22 Owner: the world is the mode; details must not cover it until requested.
  it('opens evidence on demand from the independent Agents mode', () => {
    render(<AgentWorld stationImage="/station.png" cityHref="#city" consoleHref="#collaboration?look=console" snapshot={{version:1,members:[{id:'a',label:'A',memberships:[]}],exchanges:[],coverage:{state:'ready',note:'Test provenance',truncated:false}}}/>)
    expect(screen.queryByRole('complementary',{name:'Observation details'})).toBeNull()
    fireEvent.click(screen.getByRole('button',{name:'A',exact:true}))
    expect(screen.getByRole('complementary',{name:'Observation details'})).toBeTruthy()
    expect(screen.getByText('Activity unknown')).toBeTruthy()
    fireEvent.click(screen.getByRole('button',{name:'Close details'}))
    expect(screen.queryByRole('complementary',{name:'Observation details'})).toBeNull()
    expect(screen.getByRole('link',{name:'Console'}).getAttribute('href')).toBe('#collaboration?look=console')
  })
  it('does not treat handover or answer as completion or reply linkage', () => {
    const base = {event_id:1,event_stream_id:'mbox:b',seq:1,event_type:'message_posted',actor_id:'a',mission_id:'',plan_node_id:'',task_id:'',attempt_id:'',at:'2026-11-22T12:00:00Z',body:{message_id:'m1',to:'b',kind:'handover'}}
    const facts = messageFacts([base]); expect(facts[0].stage).toBe('sent')
    expect(exchangesOf(facts)[0].stages.completed).toBeUndefined()
  })
})
