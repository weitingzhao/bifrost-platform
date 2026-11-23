import { useState } from 'react'
import { RunSourceReport } from './RunSourceReport'
import { CommitJourney } from './CommitJourney'
import { useQuery } from '@tanstack/react-query'
import { DenseTag } from '@bifrost/ui'
import { fetchPipelineRunLogs } from '@/api/delivery'
import type { TimingRun, TimingTask } from '@/api/ciTimings'
import { OpsSection } from '@/components/layout/OpsSection'
import { beforeTests, consoleState, elapsed, result, reusedBy, runCommit, runStatus, scopeBaseFromLog, scopeResult, seconds, secondsText, slowTimings, taskSeconds, taskWait } from './timingModel'

export const READ_BUTTON = 'min-h-11 rounded-[var(--control-radius)] border border-transparent bg-[var(--control-fill)] px-3 text-sm font-medium hover:bg-[var(--accent)]'
const missing = 'Not recorded; older runs may predate this result'

function TaskTiming({ task, start, total }: { task: TimingTask; start?: string; total: number | null }) {
  const offset = seconds(start, task.created_at), duration = taskSeconds(task)
  const span = total != null && total > 0 ? total : null
  return <details className="min-w-0 border-b border-[var(--table-rule)] px-3 py-2">
    <summary className="cursor-pointer list-none">
      <span className="flex min-h-8 flex-wrap items-center justify-between gap-2">
        <span className="break-all font-medium">{task.task} <span className="text-xs text-muted-foreground">▸ steps</span></span>
        <span className="font-mono text-xs">{secondsText(duration)} · {task.status}</span>
      </span>
      <div className="my-1 h-2 overflow-hidden rounded bg-[var(--muted)]" aria-hidden="true">
        {duration != null && offset != null && span != null && <div className={`h-2 rounded ${task.status === 'failed' ? 'bg-[var(--destructive)]' : 'bg-[var(--primary)]'}`} style={{ marginLeft:`${Math.min(100, offset/span*100)}%`, width:`${Math.max(.5, Math.min(100, duration/span*100))}%` }} />}
      </div>
      <span className="text-xs text-muted-foreground">{`Starts +${secondsText(offset)} · before first step ${secondsText(taskWait(task))}`}</span>
    </summary>
    <div className="overflow-x-auto py-2" tabIndex={0} role="region" aria-label={`${task.task} steps`}>
      <table className="w-full min-w-[420px] text-left text-xs">
        <thead><tr>{['Step', 'Seconds', 'Status', 'Started (UTC)'].map(h => <th className="py-2 pr-3 font-medium" key={h}>{h}</th>)}</tr></thead>
        <tbody>{task.steps.map(step => <tr key={step.name}>
          <td className="py-2 pr-3">{step.name}</td><td className="font-mono">{secondsText(seconds(step.started_at,step.finished_at))}</td>
          <td>{step.status}{step.exit_code != null ? ` (${step.exit_code})` : ''}</td><td className="font-mono">{step.started_at || 'Not recorded'}</td>
        </tr>)}</tbody>
      </table>
      {task.steps.length === 0 && <p>No step timestamps retained.</p>}
    </div>
    {task.results.length > 0 && <dl className="grid gap-2 text-xs">{task.results.map(r => <div key={r.name}><dt className="font-medium">{r.name}</dt><dd className="m-0 whitespace-pre-wrap break-all text-muted-foreground">{r.value || '(empty)'}</dd></div>)}</dl>}
  </details>
}

export function RunTimingDetail({ run, showJourney = true }: { run: TimingRun; showJourney?: boolean }) {
  const [readLogs, setReadLogs] = useState(false)
  const logs = useQuery({ queryKey:['ci-timing-logs',run.name], queryFn:() => fetchPipelineRunLogs(run.name,'cicd'), enabled:readLogs, staleTime:60_000, retry:false })
  const text = logs.data?.logs ?? '', slow = slowTimings(text,run.name).slice(0,10)
  const changes = run.tasks.find(t => t.task === 'changes')
  const reuse = reusedBy(run), base = scopeResult(run,'base-commit')
  const isCI = run.pipeline === 'bifrost-ci-platform'
  const active = run.tasks.filter(t => ['running', 'pending'].includes(t.status.toLowerCase()))
  const facts: [string,string][] = [
    ['Commit', runCommit(run) || 'Not recorded'],
    ['Total elapsed', secondsText(elapsed(run))],
    ...(isCI ? [
    ['Before test tasks', reuse ? 'Not applicable: reused' : secondsText(beforeTests(run))],
    ['Test base', base || run.params.base || missing],
    ['Scope comparison base', scopeBaseFromLog(text) || (readLogs ? 'Not retained in the available logs' : 'Read logs below for the merge-base recorded by changes')],
    ['Scope task request', result(changes?.results ?? [],'api-scope') || missing],
    ['Scope task reason', result(changes?.results ?? [],'why') || missing],
    ['API scope actually tested', scopeResult(run,'api-scope') || missing],
    ['API scope reason', scopeResult(run,'api-scope-why') || missing],
    ['Selected API packages', scopeResult(run,'api-packages') || (scopeResult(run,'api-scope') === 'full' ? 'Full suite; package timings are in test logs' : missing)],
    ['Modules required', result(changes?.results ?? [],'run-modules') ?? missing],
    ['Module scope reason', result(changes?.results ?? [],'scope-why') || result(changes?.results ?? [],'why') || missing],
    ['Console tests', consoleState(run)],
    ['Reused verdict', reuse || 'No whole-run reuse reported; module reuse is described by the scope reason'],
    ] as [string,string][] : []),
  ]
  return <OpsSection title="Selected run" description={run.name} bodyPadding="none">
    {active.length > 0 && <div role="status" className="m-3 rounded border border-[var(--table-rule)] p-3">
      <strong className="text-sm">In progress · {active.length} active tasks</strong>
      <ul className="mb-0 space-y-2 pl-5 text-sm">{active.map(t => <li key={t.name}><span className="font-medium">{t.task}</span> · {t.status}
        {t.steps.filter(step => ['running', 'waiting', 'pending'].includes(step.status.toLowerCase())).map(step => <span key={step.name} className="ml-2 break-all">→ {step.name}: {step.status}</span>)}
      </li>)}</ul><p className="mb-0 text-xs text-muted-foreground">Refreshes every 30 seconds. Parallel tasks may run together; no completion percentage is inferred.</p>
    </div>}
    <div className="px-3 py-2"><DenseTag variant={runStatus(run) === 'Succeeded' ? 'success' : runStatus(run) === 'Failed' ? 'danger' : 'neutral'}>{runStatus(run)}</DenseTag></div>
    <dl className="grid min-w-0 gap-x-6 gap-y-3 px-3 pb-3 text-xs sm:grid-cols-2">{facts.map(([label,value]) => <div key={label} className="min-w-0"><dt className="font-medium">{label}</dt><dd className="m-0 mt-1 whitespace-pre-wrap [overflow-wrap:anywhere] text-muted-foreground">{value}</dd></div>)}</dl>
    {!isCI && <p className="m-0 px-3 py-2 text-xs text-muted-foreground">This pipeline does not select test scope. Inspect the CI of the same commit for package selection and test reuse.</p>}
    {showJourney && runCommit(run) && <CommitJourney key={runCommit(run)} commit={runCommit(run)} />}
    <RunSourceReport key={JSON.stringify([run.pipeline, run.name])} pipeline={run.pipeline} run={run.name} />
    {run.results.length > 0 && <details className="px-3 py-2 text-xs"><summary className="cursor-pointer py-2">Pipeline results</summary><dl className="space-y-2">{run.results.map(r => <div key={r.name}><dt className="font-medium">{r.name}</dt><dd className="m-0 whitespace-pre-wrap break-all text-muted-foreground">{r.value || '(empty)'}</dd></div>)}</dl></details>}
    <p className="m-0 px-3 py-2 text-xs text-muted-foreground">Task time is creation to completion; step time is script start to finish. Parallel tasks overlap: their seconds must not be added. “Before test tasks” ends when the first test TaskRun is created, matching the CI benchmark. Task startup and script setup are additional costs.</p>
    {run.tasks.length === 0 && <p className="px-3 text-sm">No TaskRuns retained for this run. Outside retention there is no history to display.</p>}
    {run.tasks.map(task => <TaskTiming key={task.name} task={task} start={run.start_time} total={elapsed(run)} />)}
    {run.skipped.length > 0 && <p className="px-3 text-xs">Skipped by pipeline: {run.skipped.join(', ')}</p>}
    <div className="space-y-2 p-3">
      <button className={READ_BUTTON} onClick={() => setReadLogs(true)} disabled={readLogs}>{isCI ? 'Read scope logs and slow tests' : 'Read source logs'}</button>
      {readLogs && logs.isPending && <p className="text-sm">Reading retained log tails…</p>}
      {logs.error && <p role="alert" className="text-sm text-destructive">Logs could not be read: {logs.error.message}</p>}
      {logs.data && <>
        {isCI && <p className="text-xs text-muted-foreground">Slowest observed tests/packages in this run. Log tails only (up to 5,000 lines per container); cached packages and missing timings are excluded. Pods expire before run records.</p>}
        {isCI && (slow.length ? <ol className="space-y-2 pl-5 text-xs">{slow.map((row,i) => <li key={`${row.name}-${i}`}><span className="font-mono">{secondsText(row.seconds)}</span> · {row.kind} · <span className="break-all">{row.name}</span></li>)}</ol> : <p className="text-sm">No test/package durations in the retained logs. Outside pod retention there are no logs.</p>)}
        <details><summary className="cursor-pointer py-2 text-sm">Source logs</summary><pre className="max-h-96 overflow-auto whitespace-pre text-xs" tabIndex={0}>{text}</pre></details>
      </>}
    </div>
  </OpsSection>
}
