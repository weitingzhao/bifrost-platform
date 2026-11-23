import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { QueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { ObservationReportView, type ObservationReport } from '@bifrost/ui/city/observationReport'
import { pipelineReport, pipelineReportOptions } from '@/domains/delivery/observation-report'
import { queueHistoryReport, queueHistoryReportOptions } from '@/domains/tenant/market-observation/observation-report'
import type { DeliveryPipelineRunsResponse } from '@/api/deliveryTypes'
import type { QueueHistory } from '@/api/marketDataQueueHistory'
const wire=vi.hoisted(()=>({fetch:vi.fn()}))
vi.mock('@/api/serverContact',()=>({serverFetch:(...args:unknown[])=>wire.fetch(...args)}))
afterEach(()=>{cleanup();wire.fetch.mockReset()})
const received='2026-11-23T05:00:00Z'
const runs:DeliveryPipelineRunsResponse={cluster_id:'test',namespace:'cicd',pipeline:'checks',reachability:'ok',detail:'retained sample',generated_at:'2026-11-23T04:59:00Z',runs:[{name:'run-one',namespace:'cicd',pipeline:'checks',status:'VendorPaused',revision:'abcdef',reason:'waiting on source',start_time:'2026-11-23T04:50:00Z'}]}
const history:QueueHistory={hours:6,kind:null,interval_sec:300,points:[{sample_ts:'2026-11-23T04:55:00Z',kind:null,pending:0,running:null,created_delta:12,done_delta:0,failed_delta:2,oldest_pending_age_sec:null,p95_sec:null}]}
function Reader({report}:{report:ObservationReport}) {
  const [selected,onSelect]=useState('')
  return <ObservationReportView report={report} selected={selected} onSelect={onSelect} at={received}/>
}
// Owner: one provider-independent reader must preserve both CI and plugin facts.
it('reads distinct provider schemas with original values, times and unknowns',()=>{
  const ci=pipelineReport(runs,received)
  const view=render(<Reader report={ci}/>)
  expect(screen.getByText('VendorPaused')).toBeTruthy()
  fireEvent.click(screen.getByRole('button',{name:'run-one'}))
  expect(screen.getByRole('region',{name:'Selected report object'})).toBeTruthy()
  expect(screen.getAllByText('abcdef')).toHaveLength(2)
  expect(ci.objects[Object.keys(ci.objects)[0]].evidence[0].evaluatedAt).toBe(runs.generated_at)
  view.unmount()
  const plugin=queueHistoryReport(history,received)
  render(<Reader report={plugin}/>)
  fireEvent.click(screen.getByRole('button',{name:history.points[0].sample_ts}))
  expect(screen.getAllByText('0').length).toBeGreaterThan(0)
  expect(screen.getAllByText('Not measured').length).toBeGreaterThan(0)
  expect(screen.getAllByText('12')).toHaveLength(2)
  expect(screen.getAllByText(/source interval 300 seconds/).length).toBeGreaterThan(0)
  expect(plugin.evidence[0].evaluatedAt).toBeUndefined()
  expect(plugin.objects[Object.keys(plugin.objects)[0]].evidence[0].evaluatedAt).toBe(history.points[0].sample_ts)
  expect(plugin.panels[0].blocks[1]).toMatchObject({rows:[{cells:[history.points[0].sample_ts,'Not supplied',0,'Not measured',12,0,2,'Not measured','Not measured','Not measured']}]})
})
it('retains missing selection and old evidence on failure without choosing a replacement',()=>{
  const report=pipelineReport(runs,received)
  render(<ObservationReportView report={report} selected="__proto__" onSelect={()=>{throw new Error('unexpected intent')}} at={received} failure={{source:'/test',outcome:'error',receivedAt:'2026-11-23T05:01:00Z',detail:'offline'}}/>)
  expect(screen.getByRole('alert').textContent).toContain('previous report')
  expect(screen.getByText('Object not represented')).toBeTruthy()
  expect(screen.getByText(/Requested: __proto__/)).toBeTruthy()
  expect(report.capturedAt).toBe(received)
})
it('deduplicates raw scoped reads and cancels late publication',async()=>{
  const client=new QueryClient()
  wire.fetch.mockResolvedValue({ok:true,json:async()=>runs})
  const [a,b]=await Promise.all([client.fetchQuery(pipelineReportOptions('checks')),client.fetchQuery(pipelineReportOptions('checks'))])
  expect(a).toBe(b);expect(a).toEqual(runs);expect(wire.fetch).toHaveBeenCalledTimes(1)
  expect(wire.fetch.mock.calls[0][0]).toBe('/api/v1/delivery/pipelines/checks/runs')
  expect(pipelineReportOptions('checks').queryKey).toEqual(['delivery','runs','checks'])
  expect(queueHistoryReportOptions(6).queryKey).toEqual(['market-data','ingest','queue-history',6])
  let finish!:(v:unknown)=>void
  wire.fetch.mockImplementation(()=>new Promise(resolve=>{finish=resolve}))
  const pending=client.fetchQuery(queueHistoryReportOptions(6)).catch(error=>error)
  await client.cancelQueries({queryKey:queueHistoryReportOptions(6).queryKey})
  expect(wire.fetch.mock.calls[1][1].signal.aborted).toBe(true)
  finish({ok:true,json:async()=>({ok:true,data:history})});await pending
  expect(client.getQueryData(queueHistoryReportOptions(6).queryKey)).toBeUndefined()
  client.clear()
})
