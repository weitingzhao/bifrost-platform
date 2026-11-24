import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ObservationReportView, type ObservationReport } from '@bifrost/ui/city/observationReport'
import { runReportOptions, runReport } from '@/domains/delivery/observation-report/run'
import { engineReportOptions } from '@/domains/delivery/observation-report/engineRun'

function Report({make,error,source,receivedAt}:{make:()=>ObservationReport|undefined;error:Error|null;source:string;receivedAt:number}) {
  const [selected,onSelect]=useState('')
  let report:ObservationReport|undefined, failure=error
  try { report=make() } catch(e) { failure=e instanceof Error?e:new Error(String(e)) }
  return <ObservationReportView report={report} selected={selected} onSelect={onSelect} at={new Date().toISOString()}
    failure={failure?{source,outcome:'error',receivedAt:new Date(receivedAt).toISOString(),detail:failure.message}:undefined}/>
}
function SelectedRun({pipeline,run}:{pipeline:string;run:string}) {
  const timings=useQuery(runReportOptions(pipeline,run))
  const [readLogs,setReadLogs]=useState(false)
  // The log tail is the engine's (engine step 6, kind=logs; 2026-11-24 STG check: text and last log time equal to the direct read).
  const logs=useQuery(engineReportOptions({kind:'logs',run},readLogs))
  return <>
    <Report make={()=>timings.data?runReport(timings.data,pipeline,run,new Date(timings.dataUpdatedAt).toISOString()):undefined} error={timings.error} source="Selected run timings" receivedAt={timings.errorUpdatedAt || timings.dataUpdatedAt}/>
    <button onClick={()=>{if(readLogs)void logs.refetch();else setReadLogs(true)}} disabled={logs.isFetching}>{readLogs ? 'Refresh run log tail' : "Read this run's log tail"}</button>
    {readLogs && <Report make={()=>logs.data} error={logs.error} source="Engine run log report" receivedAt={logs.errorUpdatedAt || logs.dataUpdatedAt}/>}
  </>
}
export function RunReportPreview() {
  const [pipeline,setPipeline]=useState('bifrost-ci-platform'),[run,setRun]=useState('')
  const [request,setRequest]=useState<{pipeline:string;run:string}>()
  return <section aria-label="Single run reader"><form onSubmit={e=>{e.preventDefault();if(pipeline.trim()&&run.trim())setRequest({pipeline:pipeline.trim(),run:run.trim()})}}>
    <label>Pipeline <input value={pipeline} onChange={e=>setPipeline(e.target.value)} required/></label>
    <label>Run name <input value={run} onChange={e=>setRun(e.target.value)} required/></label><button type="submit">Read selected run</button>
    </form><p>Reads one named run from the current host. Logs require a separate request and have independent source times.</p>
    {request && <SelectedRun key={JSON.stringify(request)} {...request}/>}
  </section>
}
