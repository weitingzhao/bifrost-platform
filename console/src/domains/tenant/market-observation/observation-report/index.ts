import { queryOptions } from '@tanstack/react-query'
import type { ObservationReport } from '@bifrost/ui/city/observationReport'
import type { ReadEvidence } from '@bifrost/ui/city/observation'
import { fetchQueueHistory, type QueueHistory } from '../api/marketDataQueueHistory'

export function queueHistoryReportOptions(hours: number) {
  return queryOptions({
    queryKey: ['market-data','ingest','queue-history',hours] as const,
    queryFn: async ({signal}) => {
      const data = await fetchQueueHistory(hours, undefined, {signal})
      signal.throwIfAborted()
      if (!Array.isArray(data.points)) throw new Error('Queue history: incomplete source response')
      return data
    },
    staleTime:30_000,refetchInterval:60_000,retry:1,
  })
}

/** Keeps counts as interval deltas: never silently converts them into rates or judges plugin health. */
export function queueHistoryReport(data: QueueHistory, receivedAt: string): ObservationReport {
  const source = `/api/v1/plugins/market-data/api/market/ingest/queue-history?hours=${data.hours}`
  const evidence:ReadEvidence[]=[{source,receivedAt,outcome:'read',detail:'Historical samples. Response evaluation time and refresh deadline are not supplied; row evidence retains sample time.'}]
  const columns=['Sample time','Kind','Pending (jobs)','Running (jobs)','Created (jobs/interval)','Completions (jobs/interval)','Failed (jobs/interval)','Oldest pending (s)','P50 (s)','P95 (s)']
  const rows=data.points.map(point=>({key:JSON.stringify(['market-data-queue-sample',point.kind,point.sample_ts]),
    cells:[point.sample_ts,point.kind ?? 'Not supplied',point.pending ?? 'Not measured',point.running ?? 'Not measured',point.created_delta,point.done_delta,point.failed_delta,point.oldest_pending_age_sec ?? 'Not measured',point.p50_sec ?? 'Not measured',point.p95_sec ?? 'Not measured'],
    evidence:[{...evidence[0],evaluatedAt:point.sample_ts,detail:'Recorded sample time, not current live state. No freshness deadline supplied.'}],
  }))
  return {version:1,id:JSON.stringify(['queue-history-report',data.hours,data.kind,receivedAt]),capturedAt:receivedAt,
    scope:`${data.hours} hours / ${data.kind ?? 'all kinds'} / source interval ${data.interval_sec} seconds`,evidence,
    panels:[{id:'samples',label:'Queue history samples',blocks:[{kind:'note',text:`Counts are source deltas over ${data.interval_sec} seconds, not jobs per minute. Missing samples are not filled.`},
      {kind:'table',head:columns.map(label=>({label})),rows:rows.map(({key,cells})=>({key,open:key,cells})),empty:'No samples supplied; this does not mean zero work.'}]}],
    objects:Object.fromEntries(rows.map(row=>[row.key,{label:row.cells[0] as string,evidence:row.evidence,blocks:[{kind:'facts',rows:columns.map((label,i)=>({label,value:row.cells[i]}))}]}])),
  }
}
