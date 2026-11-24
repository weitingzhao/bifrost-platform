import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ObservationReportView, type ObservationReport } from '@bifrost/ui/city/observationReport'
import { runReport, runReportOptions } from '@/domains/delivery/observation-report/run'
import { compareRunReports, engineRunReportOptions, reportSource, type ReportSource } from '@/domains/delivery/observation-report/engineRun'
import { fetchSelfHealth } from '@/api/core'

type Read = { report?: ObservationReport; failure?: Error; at: number }

/** The Console's own read of the run (the way back while the engine's report is checked). */
function usePlatformRead(pipeline: string, run: string, enabled: boolean): Read {
  const query = useQuery({ ...runReportOptions(pipeline, run), enabled: enabled && !!pipeline && !!run })
  let report: ObservationReport | undefined, failure = query.error ?? undefined
  try {
    if (query.data) report = runReport(query.data, pipeline, run, new Date(query.dataUpdatedAt).toISOString())
  } catch (error) {
    failure = error instanceof Error ? error : new Error(String(error))
  }
  return { report, failure, at: query.errorUpdatedAt || query.dataUpdatedAt }
}

/** The engine backend's report of the same run (/engine/v1/report?kind=run). */
function useEngineRead(pipeline: string, run: string, enabled: boolean): Read {
  const query = useQuery(engineRunReportOptions(pipeline, run, enabled))
  return { report: query.data, failure: query.error ?? undefined, at: query.errorUpdatedAt || query.dataUpdatedAt }
}

function ReportView({ read, source }: { read: Read; source: string }) {
  const [selected, onSelect] = useState('')
  return <ObservationReportView report={read.report} selected={selected} onSelect={onSelect} at={new Date().toISOString()}
    failure={read.failure ? { source, outcome: 'error', receivedAt: new Date(read.at).toISOString(), detail: read.failure.message } : undefined} />
}

/** Opt-in exact source evidence; the existing timing analysis and log reader remain independent. */
function SourceRead({ pipeline, run, mode }: { pipeline: string; run: string; mode: ReportSource }) {
  const platform = usePlatformRead(pipeline, run, mode !== 'engine')
  const engine = useEngineRead(pipeline, run, mode !== 'platform')
  if (mode === 'platform') return <ReportView read={platform} source="Selected run timings" />
  if (mode === 'engine') return <ReportView read={engine} source="Engine run report" />
  const differences = platform.report && engine.report ? compareRunReports(platform.report, engine.report) : null
  return <section aria-label="Run report comparison">
    <p role="status">{differences == null
      ? 'Comparison waits for both reports (Console read and engine read).'
      : differences.length === 0 ? 'Console and engine reports are equal (rows, objects, evidence; ids and receive times left out).'
        : `Console and engine reports differ in ${differences.length} place${differences.length === 1 ? '' : 's'}.`}</p>
    {differences != null && differences.length > 0 && <ul aria-label="Report differences">{differences.map(line => <li key={line}>{line}</li>)}</ul>}
    <h4>Console read</h4><ReportView read={platform} source="Selected run timings" />
    <h4>Engine read</h4><ReportView read={engine} source="Engine run report" />
  </section>
}

export function RunSourceReport({ pipeline, run }: { pipeline: string; run: string }) {
  const [open, setOpen] = useState(false)
  // The environment as the platform says it (the same self-health read the frame's badge comes from).
  const self = useQuery({ queryKey: ['platform', 'self-health'], queryFn: fetchSelfHealth, retry: false, staleTime: 30_000, enabled: open })
  const mode = reportSource(self.data?.viewer_env)
  const from = mode === 'engine' ? 'the engine backend' : mode === 'compare' ? 'both the Console and the engine backend' : 'the current Platform host'
  return <div className="min-w-0 px-3 py-2 text-xs [overflow-wrap:anywhere]">
    <button type="button" className="min-h-11 underline" aria-expanded={open} onClick={() => setOpen(value => !value)}>{open ? 'Hide run source report' : 'Read run source report'}</button>
    {open && self.isPending && <p>…</p>}
    {open && !self.isPending && <><p>{`Independent exact-run read from ${from}. Source times may differ from the retained history above. Logs are read separately.`}</p><SourceRead key={JSON.stringify([pipeline, run, mode])} pipeline={pipeline} run={run} mode={mode} /></>}
  </div>
}
