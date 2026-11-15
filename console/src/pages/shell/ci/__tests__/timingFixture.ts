import type { TimingRun } from '@/api/ciTimings'

export const timingRun = (): TimingRun => ({
  name:'ci-one', pipeline:'bifrost-ci-platform', revision:'a'.repeat(40), status:'Succeeded',
  created_at:'2026-11-15T20:00:00Z', start_time:'2026-11-15T20:00:00Z', completion_time:'2026-11-15T20:03:19Z',
  params:{}, results:[{name:'api-scope',value:'incremental'},{name:'base-commit',value:'b'.repeat(40)},{name:'api-packages',value:'internal/server internal/mcp'}], skipped:[],
  tasks:[{name:'changes',task:'changes',status:'succeeded',created_at:'2026-11-15T20:00:00Z',finished_at:'2026-11-15T20:00:32Z',results:[{name:'run-modules',value:' api repo '},{name:'scope-why',value:'console keys match base'}],steps:[]},
    {name:'test-api',task:'test-api',status:'succeeded',created_at:'2026-11-15T20:00:32Z',finished_at:'2026-11-15T20:01:29Z',results:[],steps:[{name:'go-test',status:'succeeded',started_at:'2026-11-15T20:00:35Z',finished_at:'2026-11-15T20:01:29Z'}]}],
})
