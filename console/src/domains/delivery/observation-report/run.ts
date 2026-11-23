import { queryOptions } from '@tanstack/react-query'
import { fetchCITimings, type PipelineTimings, type TimingResult } from '@/api/ciTimings'
import { fetchPipelineRunLogs } from '../api/delivery'
import type { DeliveryRunLogsResponse } from '../api/deliveryTypes'
import type { ObservationReport } from '@bifrost/ui/city/observationReport'
import type { Block } from '@bifrost/ui/city/cards'
import type { ReadEvidence } from '@bifrost/ui/city/observation'

/** Exact run read: keep the existing raw PlatformTimings cache, never the pipeline-list key. */
export function runReportOptions(pipeline: string, run: string) {
  return queryOptions({queryKey:['ci-timing-run',pipeline,run] as const,
    queryFn:({signal})=>fetchCITimings(pipeline,run,signal), enabled:!!pipeline && !!run,
    staleTime:30_000,refetchInterval:30_000,retry:false})
}

/** Existing log readers are scoped to this host's cicd namespace. No eager log fan-out. */
export function runLogOptions(run: string, enabled: boolean) {
  return queryOptions({queryKey:['ci-timing-logs',run] as const,
    queryFn:()=>fetchPipelineRunLogs(run,'cicd'),enabled:enabled && !!run,staleTime:60_000,retry:false})
}
const missing = 'Not recorded'
const resultsBlock = (results: TimingResult[]): Block => ({kind:'table',head:[{label:'Result'},{label:'Value'}],
  rows:results.map((r,i)=>({key:String(i),cells:[r.name,r.value === '' ? '(empty)' : r.value]})),empty:'No results retained.'})

/** Portable report projection only; no status normalization, elapsed-time sum or inferred dependencies. */
export function runReport(data: PipelineTimings, pipeline: string, name: string, receivedAt: string): ObservationReport {
  if (data.runs.length > 1 || data.runs.some(r=>r.name!==name || r.pipeline!==pipeline)) throw new Error('Run report: source identity does not match the requested run')
  const run=data.runs[0]
  const query=new URLSearchParams({limit:'60'})
  if(pipeline==='bifrost-land') query.set('repo','bifrost-platform')
  query.set('run',name)
  const evidence:ReadEvidence[]=[{source:`/api/v1/delivery/pipelines/${encodeURIComponent(pipeline)}/timings?${query}`,receivedAt,
    evaluatedAt:data.as_of,outcome:run?'read':'missing',detail:'Current Platform host / cicd. Source does not supply cluster identity or expiry. Retained run only; missing is not proof it never ran.'}]
  const base={version:1 as const,id:JSON.stringify(['run-report',pipeline,name,data.as_of,receivedAt]),capturedAt:receivedAt,
    scope:`Current Platform host / cicd / ${pipeline} / ${name}`,evidence}
  if(!run) return {...base,panels:[{id:'run',label:'Run record',blocks:[{kind:'note',text:'Requested run not represented in this response. History and current state are unknown.'}]}],objects:{}}
  const seen=new Set<string>()
  for(const task of run.tasks) {
    if(!task.name || seen.has(task.name) || !Array.isArray(task.steps) || !Array.isArray(task.results)) throw new Error('Run report: incomplete or duplicate task identity')
    seen.add(task.name)
  }
  const taskKey=(task:string)=>JSON.stringify(['task-run','cicd',name,task])
  return {...base,panels:[{id:'run',label:'Run record',blocks:[
    {kind:'facts',rows:[['Run',run.name],['Pipeline',run.pipeline],['Input revision',run.revision??missing],['Status',run.status],['Reason',run.reason??missing],['Created at',run.created_at],['Started at',run.start_time??missing],['Completed at',run.completion_time??missing],['Trigger',run.trigger??missing]].map(([label,value])=>({label,value}))},
    {kind:'note',text:'Results and tasks belong to this run. Input revision and output commit remain separate. Task order is not a dependency graph; overlapping durations are not added. A successful run is not evidence of current deployment.'},
    resultsBlock(run.results),
    {kind:'table',head:['Task run','Task','Status','Created at','Finished at'].map(label=>({label})),rows:run.tasks.map(t=>({key:taskKey(t.name),open:taskKey(t.name),cells:[t.name,t.task,t.status,t.created_at??missing,t.finished_at??missing]})),empty:'No task records retained.'},
    {kind:'note',text:`Skipped tasks reported by source: ${run.skipped?.join(', ') || 'None supplied'}`},
  ]}],objects:Object.fromEntries(run.tasks.map(t=>[taskKey(t.name),{label:t.name,evidence,blocks:[
    {kind:'facts',rows:[{label:'Parent run',value:name},{label:'Task',value:t.task},{label:'Status',value:t.status}]},
    {kind:'table',head:['Step','Status','Started at','Finished at','Exit code'].map(label=>({label})),rows:t.steps.map((s,i)=>({key:String(i),cells:[s.name,s.status,s.started_at??missing,s.finished_at??missing,s.exit_code??missing]})),empty:'No step timestamps retained.'},resultsBlock(t.results),
  ]}]))}
}

/** Logs are a separate read with separate source times, never part of the timings snapshot. */
export function runLogReport(data: DeliveryRunLogsResponse, name: string, receivedAt: string): ObservationReport {
  if(data.run_name!==name || data.namespace!=='cicd') throw new Error('Run logs: source identity does not match the requested run')
  const evidence:ReadEvidence[]=[{source:`/api/v1/delivery/runs/${encodeURIComponent(name)}/logs?ns=cicd`,receivedAt,evaluatedAt:data.generated_at,outcome:'read',detail:'Independent log-tail read. Per-container failures may appear in the text. No expiry or full-history guarantee.'}]
  return {version:1,id:JSON.stringify(['run-logs',data.cluster_id,'cicd',name,data.generated_at,receivedAt]),capturedAt:receivedAt,
    scope:`${data.cluster_id} / cicd / ${name} / log tail`,evidence,objects:{},panels:[{id:'logs',label:'Run log evidence',blocks:[
      {kind:'facts',rows:[{label:'Run',value:name},{label:'Last log at',value:data.last_log_at??missing}]},
      {kind:'note',text:data.logs || 'No log text supplied; not proof that the run never executed.'},
    ]}]}
}
