import { expect, it } from 'vitest'
import fixture from '@/api/__fixtures__/agentrt-progress-owner-next.testdb.json'
import { parseOwnerNext, parseRuntimeMission } from './runtimeWork'
it('retains recorded candidate order and dependency chain', () => {
 const next=parseOwnerNext(fixture.owner_next)
 expect(next?.state).toBe('estimated')
 expect(next?.candidates.map(c=>[c.mission_id,c.after,c.ready])).toEqual([['M-1',[],false],['M-2',['M-1'],false]])
 expect(next?.next?.step).toEqual(fixture.owner_next.step)
})
it('unreadable next-step times remain unknown and never ready', () => {
 const next=parseOwnerNext({...fixture.owner_next, earliest:'bad', ready:true, candidates:[{...fixture.owner_next.candidates[0], earliest:'bad',ready:true}]})
 expect(next?.state).toBe('unknown')
 expect(next?.candidates[0]).toMatchObject({state:'unknown',ready:false})
})
it('a mission estimate without a supported sample remains unknown', () => {
 const mission=parseRuntimeMission({eta:{state:'estimated',earliest:'2026-11-16T00:00:00Z',latest:'2026-11-16T01:00:00Z',basis:[{role:'ops',task_class:'check',usual_s:10,samples:0}]}})
 expect(mission.eta).toEqual({state:'unknown',reason:"the runtime's estimate could not be read"})
})
