import { useQuery } from '@tanstack/react-query'
import { useRef } from 'react'
import { fetchCapabilities, type CapabilityScore } from '@/api/agentRuntimeCapabilities'
import { duration } from './crewModel'
import { Bar, Card } from './OwnerParts'
import { ago, answeredAt, lastReadWords, plural, type Tone } from './ownerModel'

const SOURCE: Record<CapabilityScore['source'], string> = { exam: 'exam', benchmark: 'benchmark', in_job: 'in the job' }
const who = (executor: string, model: string) => `${executor.replace(/^exec:/, '')}${model ? ` (${model})` : ''}`
const tone = (score: number): Tone => score >= 0.8 ? 'ok' : score >= 0.5 ? 'warn' : 'fail'

function byJob<T extends { job_id: string }>(rows: readonly T[]): [string, T[]][] {
  const jobs = new Map<string, T[]>()
  for (const row of rows) jobs.set(row.job_id, [...(jobs.get(row.job_id) ?? []), row])
  return [...jobs.entries()]
}

/**
 * Who can do what, as the runtime measured it (2.5 G9): each executor's score of its latest window
 * per job and task class with the attempts behind it, the job candidates and the capability facts in
 * force — from GET /agentrt/capabilities, beside the vendor pools.
 */
export function OwnerCapabilities({ now }: { now: number }) {
  // This card mounts only in the System entry; unmounting removes its polling observer.
  const openedAt = useRef(Date.now())
  const query = useQuery({ queryKey: ['agent-runtime-capabilities'], queryFn: ({ signal }) => fetchCapabilities(signal), refetchInterval: 60_000 })
  // Failed or paused offline: no score, candidate or fact of the last answer is shown as this one's (review 488 A3).
  const unreadable = query.isError || query.isPaused
  const data = unreadable ? undefined : query.data
  return <Card label="Capabilities" title="Capability scores">
    {query.isPending && !query.isPaused && <p className="m-0 text-sm text-muted-foreground">Reading capability scores…</p>}
    {unreadable && <p role="alert" className="m-0 text-sm [overflow-wrap:anywhere]">Capability scores could not be read{query.error ? `: ${query.error.message}` : ' (offline)'}; {lastReadWords(answeredAt(query.dataUpdatedAt), openedAt.current, now)}.</p>}
    {data && data.scores.length === 0 && <p className="m-0 text-sm text-muted-foreground">No scores measured yet.</p>}
    {data && byJob(data.scores).map(([job, scores]) => <section key={job} aria-label={`Scores for ${job}`} className="flex min-w-0 flex-col gap-2">
      <h4 className="m-0 text-xs uppercase tracking-wide text-muted-foreground">{job}</h4>
      <ul className="m-0 flex list-none flex-col gap-2.5 p-0">{scores.map(s => <li key={s.capability_score_id} className="flex min-w-0 flex-col gap-1">
        <div className="flex justify-between gap-2 text-sm"><span className="min-w-0 [overflow-wrap:anywhere]"><strong>{who(s.executor_actor_id, s.model)}</strong> · {s.task_class || 'any class'}{s.core ? ' · core' : ''}</span>
          <span className="font-mono">{Math.round(s.score * 100)}%</span></div>
        <Bar percent={s.score * 100} tone={tone(s.score)} />
        <span className="min-w-0 text-xs text-muted-foreground [overflow-wrap:anywhere]" title={`${s.window_start} to ${s.window_end}; computed ${s.computed_at}`}>
          {SOURCE[s.source]} · {plural(s.attempts, 'attempt')}: {s.first_pass} first pass, {s.accepted} accepted, {s.rework} rework
          {s.median_wall_s !== null && ` · median ${duration(s.median_wall_s)}`} · window ended {ago(s.window_end, now)}</span>
      </li>)}</ul>
    </section>)}
    {data && data.candidates.length > 0 && <details className="min-w-0 text-sm"><summary className="min-h-11 cursor-pointer py-3">Candidates · {data.candidates.length}</summary>
      <ul className="m-0 flex list-none flex-col gap-1 p-0">{byJob(data.candidates).map(([job, rows]) => <li key={job} className="min-w-0 [overflow-wrap:anywhere]">
        <strong>{job}</strong>: {rows.map(c => `${who(c.executor_actor_id, c.model_pattern === '*' ? c.model_provider : `${c.model_provider} ${c.model_pattern}`)} ${c.status}`).join(', ')}</li>)}</ul>
    </details>}
    {data && data.facts.length > 0 && <details className="min-w-0 text-sm"><summary className="min-h-11 cursor-pointer py-3">Capability facts · {data.facts.length}</summary>
      <ul className="m-0 flex list-none flex-col gap-1 p-0">{data.facts.map(f => <li key={f.capability_fact_id} className="min-w-0 [overflow-wrap:anywhere]">
        {who(f.executor_actor_id, f.model_provider)} · {f.fact_kind.replace('_', ' ')} · {f.subject}
        <span className="text-xs text-muted-foreground"> · {f.source_kind === 'owner' ? 'Owner' : 'measured'}{f.rule_id ? `, rule ${f.rule_id}` : ''}</span></li>)}</ul>
    </details>}
    {data?.truncated && <p className="m-0 text-xs text-muted-foreground">Each list shows its first 1000 rows; more are recorded.</p>}
  </Card>
}
