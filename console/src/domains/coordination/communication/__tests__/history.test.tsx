import { cleanup, fireEvent, render, screen, within, renderHook, act, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, it, vi } from 'vitest'
import { AgentWorld, WorkingStudio, exchangesOf } from '@bifrost/ui/communication'
import { useCommunicationObservation } from '../useCommunication'
import type { RuntimeEventRow } from '@/api/agentRuntimeEvents'
const api=vi.hoisted(()=>({latest:vi.fn(),after:vi.fn()}))
vi.mock('@/api/agentRuntimeEvents',()=>({fetchLatestEvents:api.latest,fetchEventsAfter:api.after}))
vi.mock('@/domains/foundation/bus/useBusStream',()=>({useBusStream:()=>({connection:'connected',events:[],receipts:[]})}))
vi.mock('@/domains/foundation/bus/busNextgen',()=>({fetchBusContract:async()=>({})}))
vi.mock('@/domains/foundation/bus/communication',()=>({busFacts:()=>[]}))
vi.mock('@/api/seatStatus',()=>({fetchSeatStatuses:async()=>[]}))
vi.mock('@/api/lineage',()=>({fetchLineage:async()=>({threads:[]})}))
vi.mock('@/api/agentThreads',()=>({fetchAgentThreads:async()=>({threads:[]})}))
vi.mock('@/api/client',()=>({operatorToken:()=>''}))
afterEach(()=>{cleanup();vi.unstubAllGlobals();vi.clearAllMocks()})
it('pauses before ACK, preserves history on a live update, selects evidence and returns to live without writes',()=>{
 const fetch=vi.fn();vi.stubGlobal('fetch',fetch)
 const facts=[{id:'s',communication:'m1',channel:'message' as const,stage:'sent' as const,from:'a',to:'b',label:'Request',evidence:{id:'s',source:'ledger',at:'2026-11-22T12:00:00Z'}},{id:'k',communication:'m1',channel:'message' as const,stage:'acknowledged' as const,from:'a',to:'b',evidence:{id:'k',source:'ledger',at:'2026-11-22T12:05:00Z'}}]
 const snapshot={version:1 as const,members:[{id:'a',label:'Alice',memberships:[{organization:'Platform',domain:'Crew',seat:'crew',role:'owner' as const}]},{id:'b',label:'Bob',memberships:[{organization:'Platform',domain:'Compute',seat:'compute',role:'temporary' as const}]}],exchanges:exchangesOf(facts),coverage:{state:'ready' as const,note:'test',truncated:false,updatedAt:'2026-11-22T12:06:00Z'}}
 const view=render(<AgentWorld snapshot={snapshot} stationImage="/station.png" cityHref="#city" consoleHref="#console"/>)
 fireEvent.change(screen.getByRole('slider',{name:'Communication time'}),{target:{value:'0'}})
 const timeline=within(screen.getByRole('region',{name:'Communication history'}))
 expect(timeline.queryByText('ACK')).toBeNull()
 fireEvent.click(timeline.getByRole('button',{name:/Sent/}))
 expect(within(screen.getByRole('complementary',{name:'Observation details'})).getAllByText('Not observed')).toHaveLength(7)
 view.rerender(<AgentWorld snapshot={{...snapshot,coverage:{...snapshot.coverage,updatedAt:'2026-11-22T12:07:00Z'}}} stationImage="/station.png" cityHref="#city" consoleHref="#console"/>)
 expect(timeline.queryByText('ACK')).toBeNull()
 expect(screen.getByText('History · fixed time')).toBeTruthy()
 fireEvent.click(screen.getByRole('button',{name:'Return to live'}))
 expect(timeline.getByText('ACK')).toBeTruthy()
 expect(screen.getByRole('complementary',{name:'People and departments'}).textContent).toContain('Platform / Compute')
 expect(fetch).not.toHaveBeenCalled()
})
it('loads older pages into the same cache without replacing the forward cursor',async()=>{
 const row=(id:number):RuntimeEventRow=>({event_id:id,event_stream_id:'mbox:b',seq:id,event_type:'message_posted',actor_id:'a',mission_id:'',plan_node_id:'',task_id:'',attempt_id:'',body:{message_id:'m'+id,to:'b',kind:'note'},at:'2026-11-22T12:00:00Z'})
 api.latest.mockResolvedValueOnce({events:Array.from({length:500},(_,i)=>row(1000-i)),next:'100.1000'}).mockResolvedValueOnce({events:[row(500)],next:'1.500'})
 api.after.mockResolvedValue({events:[row(1001)],next:'101.1001'})
 const client=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})
 const wrapper=({children}:{children:React.ReactNode})=><QueryClientProvider client={client}>{children}</QueryClientProvider>
 const {result}=renderHook(()=>useCommunicationObservation(),{wrapper})
 await waitFor(()=>expect(result.current.snapshot.coverage.state).toBe('ready'))
 await act(async()=>result.current.history.loadOlder())
 await waitFor(()=>expect(result.current.snapshot.exchanges.some(x=>x.id==='m500')).toBe(true))
 expect(api.latest.mock.calls[1][0]).toEqual(expect.objectContaining({before:501}))
 await act(async()=>{await client.invalidateQueries({queryKey:['communication','messages']})})
 expect(api.after.mock.calls[0][0]).toBe('100.1000')
 expect(result.current.snapshot.exchanges.some(x=>x.id==='m500')).toBe(true)
 await waitFor(()=>expect(result.current.snapshot.exchanges.some(x=>x.id==='m1001')).toBe(true))
 expect(result.current.history.hasOlder).toBe(false)
 client.clear()
})

it('keeps one host-controlled evidence cutoff across scene and ledger without inventing a snapshot store',()=>{
 const at='2026-11-22T12:00:00Z', later='2026-11-22T12:05:00Z'
 const snapshot={version:1 as const,members:[],coverage:{state:'ready' as const,note:'test',truncated:false,updatedAt:later},exchanges:exchangesOf([{id:'s',communication:'m',channel:'message',stage:'sent',evidence:{id:'s',source:'ledger',at}},{id:'a',communication:'m',channel:'message',stage:'acknowledged',evidence:{id:'a',source:'ledger',at:later}}])}
 const onThrough=vi.fn()
 const view=render(<AgentWorld snapshot={snapshot} stationImage="" cityHref="#city" consoleHref="#console" through={at} onThrough={onThrough}/>)
 expect(within(screen.getByRole('region',{name:'Communication history'})).queryByText('ACK')).toBeNull()
 fireEvent.click(screen.getByRole('button',{name:'Return to live'}))
 expect(onThrough).toHaveBeenLastCalledWith('')
 expect(screen.getByText('History · fixed time')).toBeTruthy()
 view.rerender(<WorkingStudio snapshot={snapshot} stationImage="" ledgerOnly through={at} onThrough={onThrough}/>)
 expect(screen.getAllByText('Not observed')).toHaveLength(7)
 fireEvent.click(screen.getByRole('button',{name:'Return to latest'}))
 expect(onThrough).toHaveBeenLastCalledWith('')
 view.rerender(<WorkingStudio snapshot={snapshot} stationImage="" ledgerOnly through="" onThrough={onThrough}/>)
 expect(screen.getAllByText('Not observed')).toHaveLength(6)
})
