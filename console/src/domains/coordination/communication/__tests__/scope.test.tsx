// 2026-11-23 Agents scope/projection (the former B proposal), folded into the engine backend's step 5 by the
// UI lead (箭10): the scope words are the same ones the engine serves (channel, member; through stays separate).
// Before this, channel/member were private to each renderer, so a host could neither restore them nor learn
// which window a request resolved in (World had to guess from raw snapshot.exchanges).
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { AgentWorld, WorkingStudio, exchangesOf, projectCommunication, type CommunicationProjection } from '@bifrost/ui/communication'
afterEach(()=>{cleanup();vi.unstubAllGlobals()})
const at='2026-11-22T12:00:00Z', later='2026-11-22T12:05:00Z'
const snapshot={version:1 as const,coverage:{state:'ready' as const,note:'test',truncated:false,updatedAt:later},
 members:[{id:'a',label:'Alice',memberships:[]},{id:'b',label:'Bob',memberships:[]},{id:'c',label:'Cara',memberships:[]}],
 exchanges:exchangesOf([
  {id:'s1',communication:'m1',channel:'message',stage:'sent',from:'a',to:'b',label:'ab',evidence:{id:'s1',source:'ledger',at}},
  {id:'k1',communication:'m1',channel:'message',stage:'acknowledged',from:'a',to:'b',evidence:{id:'k1',source:'ledger',at:later}},
  {id:'s2',communication:'m2',channel:'message',stage:'sent',from:'c',to:'a',label:'ca',evidence:{id:'s2',source:'ledger',at}},
  {id:'e1',communication:'d1',channel:'event',stage:'delivered',from:'b',to:'c',label:'ev',evidence:{id:'e1',source:'bus',at}},
 ])}

it('evaluates time, channel and member once and resolves the request inside that window only', () => {
 const all=projectCommunication(snapshot.exchanges,'',{channel:'message',member:''},'message:m1')
 expect(all.exchanges.map(x=>x.id).sort()).toEqual(['m1','m2'])
 expect(all.state).toBe('resolved')
 // Member scope excluding the request: missing, and never another letter.
 const outside=projectCommunication(snapshot.exchanges,'',{channel:'message',member:'c'},'message:m1')
 expect(outside).toEqual(expect.objectContaining({requested:'message:m1',resolved:null,state:'missing'}))
 // Control for the case above: the same request back in a scope that holds it resolves.
 expect(projectCommunication(snapshot.exchanges,'',{channel:'message',member:'b'},'message:m1').state).toBe('resolved')
 // A cutoff before the ACK keeps the letter without the later ACK.
 const past=projectCommunication(snapshot.exchanges,at,{channel:'message',member:'a'},'message:m1')
 expect(past.exchange?.stages.acknowledged).toBeUndefined()
 expect(projectCommunication(snapshot.exchanges,'',{channel:'event',member:''}).exchanges.map(x=>x.id)).toEqual(['d1'])
})

it('lets the host own scope in both renderers, tells it the evaluated window once per change, and never fetches', () => {
 const fetch=vi.fn();vi.stubGlobal('fetch',fetch)
 for (const kind of ['world','studio'] as const) {
  const seen:CommunicationProjection[]=[], asked:unknown[]=[]
  const props={snapshot,stationImage:'',selectedId:'message:m1',scope:{channel:'message' as const,member:'c'},onScopeChange:(s:unknown)=>asked.push(s),onProjection:(p:CommunicationProjection)=>seen.push(p)}
  const view=kind==='world'?render(<AgentWorld {...props} cityHref="#city" consoleHref="#console"/>):render(<WorkingStudio {...props} ledgerOnly/>)
  expect(seen).toHaveLength(1)
  expect(seen[0]).toEqual(expect.objectContaining({scope:{channel:'message',member:'c'},requested:'message:m1',resolved:null,state:'missing'}))
  expect(seen[0].exchanges.map(x=>x.id)).toEqual(['m2'])
  // A user filter in controlled mode is a request to the host, not a local change.
  fireEvent.change(screen.getByRole('combobox',{name:'Communication channel'}),{target:{value:'event'}})
  expect(asked).toEqual([{channel:'event',member:''}])
  expect(seen).toHaveLength(1)
  // The host decides; returning the request's scope resolves it, and the notice comes once.
  const next={...props,scope:{channel:'message' as const,member:'b'}}
  if(kind==='world')view.rerender(<AgentWorld {...next} cityHref="#city" consoleHref="#console"/>);else view.rerender(<WorkingStudio {...next} ledgerOnly/>)
  expect(seen).toHaveLength(2)
  expect(seen[1]).toEqual(expect.objectContaining({resolved:'message:m1',state:'resolved'}))
  if(kind==='world')view.rerender(<AgentWorld {...next} cityHref="#city" consoleHref="#console"/>);else view.rerender(<WorkingStudio {...next} ledgerOnly/>)
  expect(seen).toHaveLength(2)
  cleanup()
 }
 expect(fetch).not.toHaveBeenCalled()
})

it('keeps the renderers\' own filters when no scope is given', () => {
 render(<WorkingStudio snapshot={snapshot} stationImage="" ledgerOnly selectedId="event:d1"/>)
 expect((screen.getByRole('combobox',{name:'Communication channel'}) as HTMLSelectElement).value).toBe('event')
 fireEvent.change(screen.getByRole('combobox',{name:'Communication channel'}),{target:{value:'message'}})
 expect(screen.getByText('ab')).toBeTruthy()
})
