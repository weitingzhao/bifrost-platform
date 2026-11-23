import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ObservationReportView, type ObservationReport } from '@bifrost/ui/city/observationReport'
import { runReport, runReportOptions } from '@/domains/delivery/observation-report/run'

/** Opt-in exact source evidence; the existing timing analysis and log reader remain independent. */
function SourceRead({ pipeline, run }: { pipeline: string; run: string }) {
  const query = useQuery(runReportOptions(pipeline, run))
  const [selected, onSelect] = useState('')
  let report: ObservationReport | undefined, failure = query.error
  try {
    if (query.data) report = runReport(query.data, pipeline, run, new Date(query.dataUpdatedAt).toISOString())
  } catch (error) {
    failure = error instanceof Error ? error : new Error(String(error))
  }
  return <ObservationReportView report={report} selected={selected} onSelect={onSelect} at={new Date().toISOString()}
    failure={failure ? { source: 'Selected run timings', outcome: 'error', receivedAt: new Date(query.errorUpdatedAt || query.dataUpdatedAt).toISOString(), detail: failure.message } : undefined} />
}

export function RunSourceReport({ pipeline, run }: { pipeline: string; run: string }) {
  const [open, setOpen] = useState(false)
  return <div className="min-w-0 px-3 py-2 text-xs [overflow-wrap:anywhere] [&_table]:w-full [&_th]:text-left [&_td]:p-2 [&_td]:[overflow-wrap:anywhere] [&_th]:[overflow-wrap:anywhere] [&_th]:p-2 [&_tr]:border-b [&_.ledger-controls]:flex [&_.ledger-controls]:flex-wrap [&_.ledger-controls]:gap-2">
    <button type="button" className="min-h-11 underline" aria-expanded={open} onClick={() => setOpen(value => !value)}>{open ? 'Hide run source report' : 'Read run source report'}</button>
    {open && <><p>Independent exact-run read from the current Platform host. Source times may differ from the retained history above. Logs are read separately.</p><SourceRead key={JSON.stringify([pipeline, run])} pipeline={pipeline} run={run} /></>}
  </div>
}
