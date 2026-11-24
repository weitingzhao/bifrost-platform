import { useMemo, useState } from 'react'
import { RunReportPreview } from './RunReport'
import { useQuery } from '@tanstack/react-query'
import { ObservationReportView, type ObservationReport } from '@bifrost/ui/city/observationReport'
import type { ReadEvidence } from '@bifrost/ui/city/observation'
import { engineReportOptions } from '@/domains/delivery/observation-report/engineRun'
import { queueHistoryReport, queueHistoryReportOptions } from '@/domains/tenant/market-observation/observation-report'

function Reader({report,error,failedAt}:{report?:ObservationReport;error:Error|null;failedAt:number}) {
  const [selected,setSelected]=useState('')
  const failure:ReadEvidence|undefined=error?{source:report?.evidence[0]?.source??'Selected provider',outcome:'error',receivedAt:new Date(failedAt).toISOString(),detail:error.message}:undefined
  return <ObservationReportView report={report} selected={selected} onSelect={setSelected} at={new Date().toISOString()} failure={failure}/>
}
function Pipeline() {
  // The engine's report (engine step 6, kind=pipeline; 2026-11-24 STG check: 24 run lists equal to the direct read).
  const query=useQuery(engineReportOptions({kind:'pipeline',pipeline:'bifrost-ci-platform'}))
  return <Reader report={query.data} error={query.error} failedAt={query.errorUpdatedAt}/>
}
function Queue() {
  const query=useQuery(queueHistoryReportOptions(6))
  const report=useMemo(()=>query.data?queueHistoryReport(query.data,new Date(query.dataUpdatedAt).toISOString()):undefined,[query.data,query.dataUpdatedAt])
  return <Reader report={report} error={query.error} failedAt={query.errorUpdatedAt}/>
}
export function ReportsPreview() {
  const [provider,setProvider]=useState('ci')
  return <main style={{padding:24}}><h1>Shared report reader</h1><p>One reader, provider-supplied fields and evidence. Only the selected source is subscribed. Changing source clears selection.</p>
    <nav aria-label="Report source"><button aria-pressed={provider==='ci'} onClick={()=>setProvider('ci')}>CI runs</button><button aria-pressed={provider==='queue'} onClick={()=>setProvider('queue')}>Plugin queue history</button><button aria-pressed={provider==='run'} onClick={()=>setProvider('run')}>Single run</button></nav>
    {provider==='ci'?<Pipeline/>:provider==='queue'?<Queue/>:<RunReportPreview/>}
  </main>
}
