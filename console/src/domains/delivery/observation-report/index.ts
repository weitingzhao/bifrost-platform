import { queryOptions } from '@tanstack/react-query'
import type { ObservationReport } from '@bifrost/ui/city/observationReport'
import type { ReadEvidence } from '@bifrost/ui/city/observation'
import { fetchPipelineRuns } from '../api/delivery'
import type { DeliveryPipelineRunsResponse } from '../api/deliveryTypes'

export function pipelineReportOptions(pipeline: string) {
  return queryOptions({
    queryKey: ['delivery', 'runs', pipeline] as const,
    queryFn: async ({signal}) => {
      const data = await fetchPipelineRuns(pipeline, {signal})
      signal.throwIfAborted()
      if (!Array.isArray(data.runs)) throw new Error('Pipeline runs: incomplete source response')
      return data
    },
    staleTime: 30_000, refetchInterval: 30_000, retry: false,
  })
}

/** Host-only projection. Status, SHA and attempt identity are supplied by delivery, never inferred by the engine. */
export function pipelineReport(data: DeliveryPipelineRunsResponse, receivedAt: string): ObservationReport {
  const source = `/api/v1/delivery/pipelines/${encodeURIComponent(data.pipeline)}/runs`
  const evidence: ReadEvidence[] = [{source,receivedAt,evaluatedAt:data.generated_at,
    outcome:data.reachability === 'fail' ? 'error' : data.reachability === 'unknown' ? 'missing' : 'read',
    detail:`Source reachability: ${data.reachability}. ${data.detail}. Refresh deadline not supplied.`}]
  const columns = ['Run', 'Namespace', 'Pipeline', 'Revision', 'Status', 'Reason', 'Started at', 'Completed at']
  const rows = data.runs.map(run => ({
    key:JSON.stringify(['pipeline-run',data.cluster_id,run.namespace,run.name]),
    cells:[run.name,run.namespace,run.pipeline,run.revision ?? 'Not supplied',run.status,run.reason ?? 'Not supplied',run.start_time ?? 'Not supplied',run.completion_time ?? 'Not supplied'],
  }))
  return {version:1,id:JSON.stringify(['pipeline-report',data.cluster_id,data.pipeline,receivedAt]),capturedAt:receivedAt,
    scope:`${data.cluster_id} / ${data.namespace} / ${data.pipeline}; retained runs only`, evidence,
    panels:[{id:'runs',label:'Pipeline runs',blocks:[{kind:'note',text:'Source status is not proof of deployment. Retained records do not establish complete history.'},
      {kind:'table',head:columns.map(label=>({label})),rows:rows.map(row=>({...row,open:row.key})),empty:'No runs supplied; history completeness is unknown.'}]}],
    objects:Object.fromEntries(rows.map(row=>[row.key,{label:row.cells[0],evidence,blocks:[{kind:'facts',rows:columns.map((label,i)=>({label,value:row.cells[i]}))}]}])),
  }
}
