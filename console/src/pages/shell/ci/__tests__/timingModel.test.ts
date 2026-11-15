import { describe, expect, it } from 'vitest'
import { timingRun } from './timingFixture'
import { beforeTests, changeKind, consoleState, elapsed, runStatus, scopeBaseFromLog, seconds, slowTimings, trendRuns } from '../timingModel'

// Origin: benchmark 1999f61b (2026-11-15): 32 s changes, 35 s before tests,
// overlapping test tasks, and a backend file that also requires console tests.
describe('observed CI timing',()=>{
  it('uses timestamps, preserves zero, and keeps missing or invalid durations unknown',()=>{
    expect(elapsed(timingRun())).toBe(199); expect(beforeTests(timingRun())).toBe(35)
    expect(seconds('2026-11-15T20:00:00Z','2026-11-15T20:00:00Z')).toBe(0)
    expect(seconds(undefined,'2026-11-15T20:00:00Z')).toBeNull(); expect(seconds('bad','bad')).toBeNull()
    expect(seconds('2026-11-15T20:00:02Z','2026-11-15T20:00:00Z')).toBeNull()
  })
  it('does not infer touched paths from the modules executed',()=>{
    expect(changeKind(['api/internal/mcp/catalog.go'])).toBe('Backend only')
    expect(changeKind(['console/src/page.tsx'])).toBe('Console only')
    expect(changeKind(['api/a.go','console/a.ts'])).toBe('Both')
    expect(changeKind(['api/a.go','bifrost.units.yaml'])).toBe('Other / shared')
    expect(changeKind(undefined)).toBe('Unknown')
    expect(consoleState(timingRun())).toBe('Skipped by scope')
  })
  it('excludes reused, daily full, failed and duplicate commits from trend medians',()=>{
    const run=timingRun(), reused={...run,name:'reused',results:[{name:'tested-by',value:'ci-one'}]}
    expect(beforeTests(reused)).toBeNull();expect(consoleState(reused)).toMatch(/Reused/)
    expect(trendRuns([reused,{...run,name:'full',trigger:'agentrt-full-ci'},{...run,name:'red',status:'Failed'},run,{...run,name:'duplicate'}]).map(r=>r.name)).toEqual(['ci-one'])
  })
  it('extracts recorded slow packages/tests, ignoring cached and absent durations',()=>{
    const log='ok\tgithub.com/example/api/internal/store\t47.12s\nok\tp/cached\t(cached)\n--- PASS: TestRun (2.20s)\n ✓ src/test.ts (3 tests) 1500ms\nchanges of abc against main (merge base '+ 'b'.repeat(40) + '):'
    expect(slowTimings(log,'ci-one').map(x=>x.seconds)).toEqual([47.12,2.2,1.5]);expect(scopeBaseFromLog(log)).toBe('b'.repeat(40))
    expect(slowTimings('(no pods yet — PipelineRun may still be starting)','ci-one')).toEqual([])
  })
})
// Origin: real ci-platform-5stn2 log read, 2026-11-15: scoped Go tests use
// JSON events, not plain "ok ... Ns". Ignoring them hid the slow backend.
it('reads Go JSON package and test durations without counting cached replays',()=>{
  const lines=[
    {Action:'pass',Package:'p/server',Test:'TestRoute',Elapsed:20.4},
    {Action:'pass',Package:'p/server',Elapsed:34.7},
    {Action:'pass',Package:'p/cached',Test:'TestOld',Elapsed:80},
    {Action:'output',Package:'p/cached',Output:'ok p/cached (cached)\n'},
    {Action:'pass',Package:'p/cached',Elapsed:0},
    {Action:'skip',Package:'p/skipped',Elapsed:0},
  ].map(e=>JSON.stringify(e)).join('\n')
  expect(slowTimings(lines+'\n{"partial"','ci-one').map(r=>[r.kind,r.name,r.seconds])).toEqual([
    ['Package','p/server',34.7],['Test','p/server · TestRoute',20.4],
  ])
})

// Origin: STG browser verification, 2026-11-15: successful runs appeared as
// "True" and every live sample was excluded from trends; fixtures used "Succeeded".
it('includes Tekton success conditions and excludes unsuccessful conditions',()=>{
  const run = {...timingRun(), status:'True', reason:'Succeeded'}
  expect(runStatus(run)).toBe('Succeeded')
  expect(trendRuns([run])).toEqual([run])
  for (const status of ['False','Unknown']) {
    expect(trendRuns([{...run,status}])).toEqual([])
  }
  expect(runStatus({...run,status:'Unknown',reason:'Running'})).toBe('Running')
  expect(runStatus({...run,status:'False',reason:'Cancelled'})).toBe('Failed')
})
