// 2026-11-24 箭10: Agents prepares to read the engine (step 5) with the direct read kept as the way back, per environment, off by default.
import { expect, it } from 'vitest'
import { ENGINE_COMMUNICATION, engineCommunicationOn, engineSnapshot, engineCommunicationOf, ENGINE_COMMUNICATION_KEY, type EngineCommunication } from '../engineRead'
const at='2026-11-24T04:00:00Z'
const read: EngineCommunication = {
 exchanges:[{id:'m1',channel:'message',from:'thread:a',to:'thread:b',stages:{sent:{id:'1',source:'agentrt/events',at}},facts:[]} as never],
 members:[{id:'thread:a',label:'A',memberships:[]},{id:'thread:b',label:'B',memberships:[]}],
 coverage:{state:'ready',note:'Engine note.',updatedAt:at,truncated:false,history:{hasOlder:true,note:'Earlier letters exist.'}},
}

it('reads the engine on STG only and treats an unknown or unread environment as direct', () => {
 // 2026-11-24: STG on for the comparison; PROD has no engine yet.
 expect(ENGINE_COMMUNICATION).toEqual({dev:false,stg:true,prod:false})
 expect(engineCommunicationOn('stg')).toBe(true)
 expect(engineCommunicationOn(' STG ')).toBe(true)
 for (const env of ['prod','dev',undefined,null,'','qa','constructor']) expect(engineCommunicationOn(env)).toBe(false)
})

// 2026-11-24 UI / Engine: every view reads the engine through api/engine.ts (readEngine, engineQueryKey). The first
// version parsed the envelope itself and read evidence fields the engine does not send (route/ok/error).
it('takes the data of a reached answer and says a failed or unreached read as failed, never as empty', () => {
 const reached=(data:EngineCommunication|null, evidence:{source:string;outcome:'read'|'missing'|'error';receivedAt:string;detail?:string}[]=[])=>({reached:true as const,model:'communication',capturedAt:at,identity:null,evidence,data})
 expect(engineCommunicationOf(reached(read)).members).toHaveLength(2)
 expect(()=>engineCommunicationOf(reached(null,[{source:'/api/v1/agentrt/events',outcome:'error',receivedAt:at,detail:'HTTP 502'}]))).toThrow(/agentrt\/events: HTTP 502/)
 expect(()=>engineCommunicationOf({reached:false,reason:'HTTP 404',status:404,lastReachedAt:null})).toThrow(/engine unreachable: HTTP 404/)
 // The read sits under ['engine', …] so the shell drops it when the identity changes.
 expect(ENGINE_COMMUNICATION_KEY[0]).toBe('engine')
})

it('keeps the event channel from the bus facts in engine mode and marks the source', () => {
 const event={id:'bus:1',communication:'e1',channel:'event' as const,stage:'delivered' as const,from:'coordination',to:'console',evidence:{id:'1',source:'bus',at}}
 const s=engineSnapshot(read,null,[event],'Event stream: live.')
 expect(s.exchanges.map(x=>x.channel+':'+x.id)).toEqual(['message:m1','event:e1'])
 expect(s.members.map(m=>m.id)).toEqual(expect.arrayContaining(['thread:a','thread:b','coordination','console']))
 expect(s.coverage).toEqual(expect.objectContaining({state:'ready',updatedAt:at}))
 expect(s.coverage.note).toContain('/engine/v1/communication')
 // Control: no engine answer yet is loading, an engine error is an error.
 expect(engineSnapshot(undefined,null,[],'').coverage.state).toBe('loading')
 expect(engineSnapshot(undefined,new Error('engine communication: HTTP 404'),[],'').coverage.state).toBe('error')
})
