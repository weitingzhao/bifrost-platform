import { useState } from 'react'
import { cleanup, fireEvent, render, screen, within, renderHook, act, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, it, vi } from 'vitest'
import { AgentWorld, WorkingStudio, exchangesOf } from '@bifrost/ui/communication'
import { useCommunicationObservation } from '../useCommunication'
import type { RuntimeEventRow } from '@/api/agentRuntimeEvents'
const api=vi.hoisted(()=>({latest:vi.fn(),after:vi.fn(),identity:'test-A'}))
vi.mock('@/api/agentRuntimeEvents',()=>({fetchLatestEvents:api.latest,fetchEventsAfter:api.after}))
vi.mock('@/domains/foundation/bus/useBusStream',()=>({useBusStream:()=>({connection:'connected',events:[],receipts:[]})}))
vi.mock('@/domains/foundation/bus/busNextgen',()=>({fetchBusContract:async()=>({})}))
vi.mock('@/domains/foundation/bus/communication',()=>({busFacts:()=>[]}))
vi.mock('@/api/seatStatus',()=>({fetchSeatStatuses:async()=>[]}))
vi.mock('@/api/lineage',()=>({fetchLineage:async()=>({threads:[]})}))
vi.mock('@/api/agentThreads',()=>({fetchAgentThreads:async()=>({threads:[]})}))
vi.mock('@/api/client',()=>({operatorToken:()=>api.identity}))
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
it.each(['scene','ledger'])('does not replace a missing requested message in the %s renderer',kind=>{
 const snapshot={version:1 as const,members:[],coverage:{state:'ready' as const,note:'test',truncated:false,updatedAt:'2026-11-22T12:05:00Z'},exchanges:exchangesOf([{id:'s',communication:'existing',channel:'message',stage:'sent',label:'Existing letter',evidence:{id:'s',source:'ledger',at:'2026-11-22T12:00:00Z'}}])}
 const onSelected=vi.fn()
 const props={snapshot,stationImage:'',selectedId:'message:missing',onSelected}
 const view=render(kind==='scene'?<AgentWorld {...props} cityHref="#city" consoleHref="#console"/>:<WorkingStudio {...props} ledgerOnly/>)
 const root=kind==='scene'?screen.getByRole('main'):screen.getByRole('region',{name:'Agent collaboration'})
 expect(root.getAttribute('data-selection-state')).toBe('missing')
 expect(root.getAttribute('data-requested-exchange')).toBe('message:missing')
 expect(root.getAttribute('data-resolved-exchange')).toBeNull()
 expect(screen.getByText(/Requested communication message:missing/)).toBeTruthy()
 expect(onSelected).not.toHaveBeenCalled()
 view.rerender(kind==='scene'?<AgentWorld {...props} selectedId="message:existing" cityHref="#city" consoleHref="#console"/>:<WorkingStudio {...props} selectedId="message:existing" ledgerOnly/>)
 expect(root.getAttribute('data-resolved-exchange')).toBe('message:existing')
 expect(root.getAttribute('data-selection-state')).toBe('resolved')
})

it.each(['message:m','message:missing'])('retains controlled time and request %s after leaving and returning',request=>{
 const fetch=vi.fn();vi.stubGlobal('fetch',fetch)
 const at='2026-11-22T12:00:00Z', later='2026-11-22T12:05:00Z'
 const snapshot={version:1 as const,members:[],coverage:{state:'ready' as const,note:'test',truncated:false,updatedAt:later},exchanges:exchangesOf([{id:'s',communication:'m',channel:'message',stage:'sent',evidence:{id:'s',source:'ledger',at}},{id:'a',communication:'m',channel:'message',stage:'acknowledged',evidence:{id:'a',source:'ledger',at:later}}])}
 function Host(){
  const [place,go]=useState('agents'),[through,setThrough]=useState(at),[selected,setSelected]=useState(request)
  const props={snapshot,stationImage:'',through,onThrough:setThrough,selectedId:selected,onSelected:setSelected}
  return <><button onClick={()=>go('elsewhere')}>Leave scene</button><button onClick={()=>go('ledger')}>Open records</button><button onClick={()=>go('agents')}>Return scene</button>{place==='agents'?<AgentWorld {...props} cityHref="#city" consoleHref="#console"/>:place==='ledger'?<WorkingStudio {...props} ledgerOnly/>:<p>Another place</p>}</>
 }
 render(<Host/>)
 const check=(root:HTMLElement)=>{expect(root.getAttribute('data-requested-exchange')).toBe(request);expect(root.getAttribute('data-resolved-exchange')).toBe(request==='message:m'?request:null)}
 check(screen.getByRole('main'))
 fireEvent.click(screen.getByRole('button',{name:'Leave scene'}))
 expect(screen.queryByRole('main')).toBeNull()
 fireEvent.click(screen.getByRole('button',{name:'Open records'}))
 check(screen.getByRole('region',{name:'Agent collaboration'}))
 if(request==='message:m')expect(screen.getAllByText('Not observed')).toHaveLength(7)
 fireEvent.click(screen.getByRole('button',{name:'Return scene'}))
 check(screen.getByRole('main'))
 expect(screen.getByText('History · fixed time')).toBeTruthy()
 expect(within(screen.getByRole('region',{name:'Communication history'})).queryByText('ACK')).toBeNull()
 expect(fetch).not.toHaveBeenCalled()
})

it.each(['success','failure'])('discards old-identity history %s after switching identity',async outcome=>{
 api.identity='history-A-'+outcome
 const row=(id:number):RuntimeEventRow=>({event_id:id,event_stream_id:'mbox:b',seq:id,event_type:'message_posted',actor_id:'a',mission_id:'',plan_node_id:'',task_id:'',attempt_id:'',body:{message_id:'m'+id,to:'b',kind:'note'},at:'2026-11-22T12:00:00Z'})
 let resolve!:(value:unknown)=>void, reject!:(reason:Error)=>void
 const pending=new Promise((yes,no)=>{resolve=yes;reject=no})
 api.latest.mockReset();api.latest.mockResolvedValueOnce({events:[row(100)],next:'1.100'}).mockReturnValueOnce(pending).mockResolvedValueOnce({events:[row(200)],next:'2.200'})
 const client=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})
 const wrapper=({children}:{children:React.ReactNode})=><QueryClientProvider client={client}>{children}</QueryClientProvider>
 const {result,rerender,unmount}=renderHook(()=>useCommunicationObservation(),{wrapper})
 await waitFor(()=>expect(result.current.snapshot.exchanges.some(x=>x.id==='m100')).toBe(true))
 const oldKey=client.getQueryCache().findAll({queryKey:['communication','messages']})[0].queryKey
 act(()=>result.current.history.loadOlder())
 await waitFor(()=>expect(api.latest).toHaveBeenCalledTimes(2))
 api.identity='history-B-'+outcome;rerender()
 await waitFor(()=>expect(result.current.snapshot.exchanges.some(x=>x.id==='m200')).toBe(true))
 expect(result.current.history.loading).toBe(false)
 await act(async()=>{if(outcome==='success')resolve({events:[row(50)],next:'0.50'});else reject(new Error('old identity failure'))})
 await waitFor(()=>expect(client.getMutationCache().getAll().at(-1)?.state.status).toBe(outcome==='success'?'success':'error'))
 expect(result.current.history.error).toBeUndefined()
 expect(result.current.snapshot.exchanges.some(x=>x.id==='m50')).toBe(false)
 expect((client.getQueryData(oldKey) as {rows:RuntimeEventRow[]}).rows.some(x=>x.event_id===50)).toBe(false)
 unmount();client.clear()
})
