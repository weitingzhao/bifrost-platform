// 2026-11-23 Agents wears the world frame (箭10; World's proposal cockpit/ui-world/frame). The frame must not
// make anything up: an alerts read that failed is "not read", never 0; a request not in the window is named so.
import { expect, it } from 'vitest'
import { exchangesOf, projectCommunication } from '@bifrost/ui/communication'
import { crumbsOf, frameAlerts, frameTime } from '../frame'
const at='2026-11-22T12:00:00Z'
const snapshot={version:1 as const,coverage:{state:'ready' as const,note:'',truncated:false,updatedAt:at},
 members:[{id:'a',label:'Alice',memberships:[]},{id:'b',label:'Bob',memberships:[]}],
 exchanges:exchangesOf([{id:'s',communication:'m1',channel:'message',stage:'sent',from:'a',to:'b',evidence:{id:'s',source:'ledger',at}}])}

it('names the selected letter by its parties and says when the request is not in the window', () => {
 expect(crumbsOf(snapshot, null)).toEqual([{label:'Agents',href:'#collaboration'}])
 expect(crumbsOf(snapshot, projectCommunication(snapshot.exchanges,'',{channel:'message',member:''},'message:m1')).at(-1)).toEqual({label:'Alice → Bob'})
 expect(crumbsOf(snapshot, projectCommunication(snapshot.exchanges,'',{channel:'message',member:''},'message:gone')).at(-1)).toEqual({label:'Not in loaded evidence'})
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
