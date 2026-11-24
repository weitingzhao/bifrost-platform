import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { runReport, runReportOptions } from './run'
import { engineReportOptions } from './engineRun'
import { RunReportPreview } from '../../../../fixtures/observation-access/RunReport'
import type { PipelineTimings } from '@/api/ciTimings'
const wire=vi.hoisted(()=>({read:vi.fn()}))
vi.mock('@/lib/platformAuth',()=>({getPlatformOperatorToken:()=> 'test-viewer'}))
vi.mock('@/api/serverContact',()=>({serverFetch:(...args:unknown[])=>wire.read(...args)}))
afterEach(()=>{cleanup();wire.read.mockReset()})
const raw:PipelineTimings={as_of:'2026-11-23T06:00:00Z',retained:1,limit:60,runs:[{name:'run-one',pipeline:'bifrost-ci-platform',revision:'input-sha',status:'VendorWaiting',created_at:'2026-11-23T05:50:00Z',params:{},results:[{name:'commit',value:'output-sha'}],skipped:['build'],tasks:[{name:'task-one',task:'check',status:'unknown',results:[{name:'count',value:'0'}],steps:[{name:'check',status:'terminated',exit_code:0}]}]}]}
// The engine's log report (bifrost-engine runLogReport), as /engine/v1/report?kind=logs answers it.
const logReport={version:1,id:'["run-logs","host-cluster","cicd","run-one"]',capturedAt:'2026-11-23T06:01:05Z',scope:'host-cluster / cicd / run-one / log tail',objects:{},
  evidence:[{source:'/api/v1/delivery/runs/run-one/logs?ns=cicd',receivedAt:'2026-11-23T06:01:05Z',evaluatedAt:'2026-11-23T06:01:00Z',outcome:'read'}],
  panels:[{id:'logs',label:'Run log evidence',blocks:[{kind:'facts',rows:[{label:'Run',value:'run-one'},{label:'Last log at',value:'Not recorded'}]},{kind:'note',text:'(unavailable: source denied)'}]}]}
const logEnvelope={version:1,model:'report',capturedAt:'2026-11-23T06:01:05Z',provenance:'live',evidence:logReport.evidence,data:logReport}
// 2026-11-23 real workflow audit: preserve separate input/output revisions, missing steps and independent log timestamps.
it('keeps exact task identities, source values and independent log evidence',()=>{
  const report=runReport(raw,'bifrost-ci-platform','run-one','2026-11-23T06:00:10Z')
  expect(report.evidence[0].evaluatedAt).toBe(raw.as_of)
  expect(report.evidence[0].expiresAt).toBeUndefined()
  const text=JSON.stringify(report)
  for(const value of ['input-sha','output-sha','VendorWaiting','Not recorded','task-one']) expect(text).toContain(value)
  expect(Object.keys(report.objects)).toEqual([JSON.stringify(['task-run','cicd','run-one','task-one'])])
  expect(report.objects[Object.keys(report.objects)[0]].blocks[1]).toMatchObject({rows:[{cells:['check','terminated','Not recorded','Not recorded',0]}]})
  expect(runReport({...raw,runs:[]},'bifrost-ci-platform','run-one',raw.as_of).evidence[0].outcome).toBe('missing')
  expect(()=>runReport(raw,'other','run-one',raw.as_of)).toThrow('identity')
  expect(()=>runReport({...raw,runs:[raw.runs[0],raw.runs[0]]},'bifrost-ci-platform','run-one',raw.as_of)).toThrow('identity')
  expect(()=>runReport({...raw,runs:[{...raw.runs[0],tasks:[raw.runs[0].tasks[0],raw.runs[0].tasks[0]]}]},'bifrost-ci-platform','run-one',raw.as_of)).toThrow('identity')
})
it('shares existing exact-run cache and rejects cancelled late reads without fetching logs',async()=>{
  const client=new QueryClient()
  wire.read.mockResolvedValue(Response.json(raw))
  await Promise.all([client.fetchQuery(runReportOptions('bifrost-ci-platform','run-one')),client.fetchQuery(runReportOptions('bifrost-ci-platform','run-one'))])
  expect(wire.read).toHaveBeenCalledTimes(1)
  expect(client.getQueryData(['ci-timing-run','bifrost-ci-platform','run-one'])).toEqual(raw)
  expect(wire.read.mock.calls[0][0]).toContain('run=run-one')
  expect(new Headers(wire.read.mock.calls[0][1].headers).get('Authorization')).toBe('Bearer test-viewer')
  // 2026-11-24: the log tail is the engine's; read only on request, under the shell's engine key.
  expect(engineReportOptions({kind:'logs',run:'run-one'},false).enabled).toBe(false)
  expect(engineReportOptions({kind:'logs',run:'run-one'}).queryKey).toEqual(['engine','/engine/v1/report',{kind:'logs',run:'run-one'}])
  let finish!:(r:Response)=>void
  wire.read.mockImplementation(()=>new Promise(resolve=>{finish=resolve}))
  const pending=client.fetchQuery(runReportOptions('bifrost-ci-platform','run-two')).catch(e=>e)
  await client.cancelQueries({queryKey:['ci-timing-run','bifrost-ci-platform','run-two']})
  expect(wire.read.mock.calls[1][1].signal.aborted).toBe(true)
  finish(Response.json(raw));await pending
  expect(client.getQueryData(['ci-timing-run','bifrost-ci-platform','run-two'])).toBeUndefined()
  client.clear()
})
it('consumer reads nothing until submit, selects a task and loads only requested logs',async()=>{
  wire.read.mockImplementation(async(url:string)=>Response.json(url.includes('kind=logs')?logEnvelope:raw))
  const client=new QueryClient({defaultOptions:{queries:{retry:false}}})
  render(<QueryClientProvider client={client}><RunReportPreview/></QueryClientProvider>)
  expect(wire.read).not.toHaveBeenCalled()
  fireEvent.change(screen.getByLabelText('Run name'),{target:{value:'run-one'}})
  fireEvent.click(screen.getByRole('button',{name:'Read selected run'}))
  await screen.findByText('VendorWaiting')
  expect(wire.read).toHaveBeenCalledTimes(1)
  fireEvent.click(screen.getByRole('button',{name:'task-one'}))
  expect(screen.getByRole('region',{name:'Selected report object'})).toBeTruthy()
  expect(screen.getByText('terminated')).toBeTruthy()
  fireEvent.click(screen.getByRole('button',{name:"Read this run's log tail"}))
  await screen.findByText('(unavailable: source denied)')
  expect(wire.read).toHaveBeenCalledTimes(2)
  wire.read.mockRejectedValue(new Error('source offline'))
  await client.refetchQueries({queryKey:['ci-timing-run','bifrost-ci-platform','run-one']})
  await waitFor(()=>expect(screen.getByRole('alert').textContent).toContain('Showing the previous report'))
  expect(screen.getByText('VendorWaiting')).toBeTruthy()
  wire.read.mockImplementation(async()=>Response.json(raw))
  await client.refetchQueries({queryKey:['ci-timing-run','bifrost-ci-platform','run-one']})
  await waitFor(()=>expect(screen.queryByRole('alert')).toBeNull())
  fireEvent.change(screen.getByLabelText('Run name'),{target:{value:'run-two'}})
  fireEvent.click(screen.getByRole('button',{name:'Read selected run'}))
  await waitFor(()=>expect(screen.getByRole('alert').textContent).toContain('source identity'))
  expect(screen.queryByRole('region',{name:'Selected report object'})).toBeNull()
  expect(screen.queryByText('(unavailable: source denied)')).toBeNull()
  cleanup();client.clear()
})
