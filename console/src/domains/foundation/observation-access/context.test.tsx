import { afterEach, expect, it, vi } from 'vitest'
import { act, cleanup, renderHook } from '@testing-library/react'
import { useObservationContext } from '@bifrost/ui/city/observationContext'
import { readQuality, readingView, statusView, type ObservationSnapshot } from '@bifrost/ui/city/observation'

const at = '2026-11-23T03:00:00Z'
const make = (id: string, used: number): ObservationSnapshot => ({version:1,id,capturedAt:at,provenance:'fixture',
  content:{tree:{id:'town',kind:'town',shape:'8x8',label:'Town',question:'State?',dashboard:null,children:[{id:'device',kind:'building',shape:'4x4',label:'Device',question:'Read?',dashboard:null,children:[],meter:{used,of:8,unit:'cores',source:'fixture'}}]}},
  evidence:{device:[{source:'fixture',outcome:'read',evaluatedAt:at,receivedAt:at,expiresAt:'2026-11-23T03:01:00Z'}]}})
afterEach(()=>{cleanup();vi.useRealTimers()})

// 2026-11-22 shared-world request: pin and selection must survive a view change while latest input advances.
it('shares snapshot, full selection and observation time between existing projections without changing sources',()=>{
  vi.useFakeTimers(); vi.setSystemTime(new Date('2026-11-23T03:00:20Z'))
  const first=make('first',0), next=make('next',4)
  const {result,rerender}=renderHook(({latest})=>useObservationContext(latest,['device']),{initialProps:{latest:first}})
  act(()=>result.current.pin())
  const before=result.current.context
  const readings=readingView.present(result.current.snapshot,before)
  const status=statusView.present(result.current.snapshot,before)
  rerender({latest:next})
  act(()=>vi.advanceTimersByTime(90000))
  expect(result.current.snapshot).toBe(first)
  expect(result.current.latest).toBe(next)
  expect(result.current.context).toBe(before)
  expect(readingView.present(result.current.snapshot,result.current.context)).toEqual(readings)
  expect(statusView.present(result.current.snapshot,result.current.context)).toEqual(status)
  expect(result.current.context.at).toBe('2026-11-23T03:00:20.000Z')
  expect(first.evidence.device[0].evaluatedAt).toBe(at)
  expect(first.content.tree.children[0].meter?.used).toBe(0)
  act(()=>result.current.resume())
  expect(result.current.snapshot).toBe(next)
  expect(result.current.context.selected).toEqual(['device'])
  expect(result.current.context.at).toBe('2026-11-23T03:01:50.000Z')
  expect(readQuality(next.evidence.device,result.current.context.at)).toBe('Stale')
})
// Same request: missing identity is preserved instead of guessed; caller mutation cannot silently change selection.
it('keeps absent selections and measurement preferences across pin/resume',()=>{
  const selection=['missing/child']
  const {result}=renderHook(()=>useObservationContext(make('a',0),selection))
  selection[0]='changed'
  expect(result.current.context.selected).toEqual(['missing/child'])
  act(()=>{result.current.setMetrics(false);result.current.pin()})
  act(()=>result.current.resume())
  expect(result.current.context.selected).toEqual(['missing/child'])
  expect(result.current.context.metrics).toBe(false)
  const path=['device']; act(()=>result.current.select(path));path[0]='mutated'
  expect(result.current.context.selected).toEqual(['device'])
})
// 2026-11-22 lifecycle boundary: pin/unmount should not leave a clock or a retained history store behind.
it('stops its clock while pinned, cleans up on unmount and cannot resurrect a lost pin',()=>{
  vi.useFakeTimers()
  const first=make('a',0),next=make('b',1)
  const {result,unmount}=renderHook(()=>useObservationContext(first))
  expect(vi.getTimerCount()).toBe(1)
  act(()=>result.current.pin());expect(vi.getTimerCount()).toBe(0)
  act(()=>result.current.resume());expect(vi.getTimerCount()).toBe(1)
  unmount();expect(vi.getTimerCount()).toBe(0)
  const fresh=renderHook(()=>useObservationContext(next))
  expect(fresh.result.current.pinned).toBe(false)
  expect(fresh.result.current.snapshot).toBe(next)
})
