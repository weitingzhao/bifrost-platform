// 2026-11-23 Agents wears the world frame (箭10; World's proposal cockpit/ui-world/frame). The frame must not
// make anything up: an alerts read that failed is "not read", never 0; a request not in the window is named so.
import { expect, it } from 'vitest'
import { exchangesOf, projectCommunication } from '@bifrost/ui/communication'
import { cityLanding, crumbsOf, frameAlerts, frameTime, signalsPin } from '../frame'
const at='2026-11-22T12:00:00Z'
const snapshot={version:1 as const,coverage:{state:'ready' as const,note:'',truncated:false,updatedAt:at},
 members:[{id:'a',label:'Alice',memberships:[]},{id:'b',label:'Bob',memberships:[]}],
 exchanges:exchangesOf([{id:'s',communication:'m1',channel:'message',stage:'sent',from:'a',to:'b',evidence:{id:'s',source:'ledger',at}}])}

it('names the selected letter by its parties and says when the request is not in the window', () => {
 expect(crumbsOf(snapshot, null)).toEqual([{label:'Agents',href:'#collaboration'}])
 expect(crumbsOf(snapshot, projectCommunication(snapshot.exchanges,'',{channel:'message',member:''},'message:m1')).at(-1)).toEqual({label:'Alice → Bob'})
 expect(crumbsOf(snapshot, projectCommunication(snapshot.exchanges,'',{channel:'message',member:''},'message:gone')).at(-1)).toEqual({label:'Letter not in view'})
 // 2026-11-23 World's rescore: the person in focus belongs in the crumbs too.
 expect(crumbsOf(snapshot, projectCommunication(snapshot.exchanges,'',{channel:'message',member:'a'},'message:m1')).map(c=>c.label)).toEqual(['Agents','Alice','Alice → Bob'])
})

it('pins the time at the cutoff, follows the last read live, and never turns an unread alerts count into 0', () => {
 expect(frameTime(snapshot,'')).toEqual({mode:'live',at})
 expect(frameTime(snapshot,'2026-11-22T11:00:00Z')).toEqual({mode:'pinned',at:'2026-11-22T11:00:00Z'})
 expect(frameAlerts(undefined, new Error('telemetry alerts: HTTP 502')).count).toBeNull()
 expect(frameAlerts(undefined, null).count).toBeNull()
 const firing=(severity:string)=>({labels:{severity},state:'firing'})
 // Control: a read with alerts counts them by the shared rule (severity none is the chain's heartbeat, not counted).
 expect(frameAlerts({alerts:[firing('warning'),firing('critical'),firing('none')],generated_at:at} as never, null)).toEqual(expect.objectContaining({count:2,worst:'fail'}))
})

// 2026-11-23 World's gap table item 9: Agents had its own time only; a Signals pin was invisible from here.
it('says the Signals pin only when the shared context is ready and pinned', () => {
 const observation=(pinned:boolean)=>({pinned,snapshot:{capturedAt:'2026-11-23T21:02:00Z'}}) as never
 expect(signalsPin({state:'ready',observation:observation(true)})).toBe('2026-11-23T21:02:00Z')
 // Controls: live Signals, Signals not opened in this tab, and a context still reading say nothing.
 expect(signalsPin({state:'ready',observation:observation(false)})).toBeNull()
 expect(signalsPin({state:'off'})).toBeNull()
 expect(signalsPin({state:'reading'})).toBeNull()
})

// 2026-11-23 World's gap table item 8: going to City from Agents always landed on the whole city (#city).
it('lands City on the domain of the person in focus, else the sender, and says nothing it does not know', () => {
 const seat=(domain:string|null)=>({seat:'s',organization:'Platform',domain,role:'owner' as const})
 const s={...snapshot,members:[{id:'a',label:'Alice',memberships:[seat(null),seat('coordination')]},{id:'thread:local_x',label:'X',memberships:[]},{id:'b',label:'Bob',memberships:[]}]}
 const p=(member:string,req?:string)=>projectCommunication(s.exchanges,'',{channel:'message',member},req)
 expect(cityLanding(s,null)).toBe('#city')
 expect(cityLanding(s,p('a'))).toBe('#city?at=coordination')
 // The selected letter's sender when nobody is in focus.
 expect(cityLanding(s,p('','message:m1'))).toBe('#city?at=coordination')
 // No domain known: a thread lands with its line picked; anyone else on the whole city, never a guessed district.
 expect(cityLanding(s,p('thread:local_x'))).toBe('#city?sel=local_x')
 expect(cityLanding(s,p('b'))).toBe('#city')
})
