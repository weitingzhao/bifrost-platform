import { NEEDS_YOU_HREF, hashQueryParam } from '@/lib/shell/consoleRoutes'
import { canonicalQuery } from '@/lib/shell/useHashSection'
import { AttemptActions } from './AttemptActions'
import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { StatusLamp } from '@bifrost/ui'
import { record, text, type RuntimeProgress } from '@/api/agentRuntime'
import { fetchRuntimeGoal, type GoalData } from '@/api/agentRuntimeGoals'
import type { Question } from '@/api/questions'
import { questionHref } from '@/lib/shell/consoleRoutes'
import { ArtifactPanel, TaskTimeline } from '../agents/TaskTimeline'
import { useNow } from './progressClock'
import { unpersistedWords } from '../owner/crewModel'
import { stalledWords } from '../owner/attemptActivity'
import { OffPlanMark } from '../owner/OwnerParts'
import { currentVerdict, droppedByRule, droppedTasks, duration, goalDone, goalQuestions, goalStages, goalStatus, goalTaskId, goalWaitingLinks, latestLease, relative, stageMarks, stalledStage, taskTiming, taskWho, terminalMission } from './goalCardModel'

/** What a card's answer added to the budget: the Owner's, or a STG rule's for him (TD-390), so not called his. */
const budgetLabels: Record<string, string> = { owner_granted_attempts: 'attempts granted on its cards', owner_granted_wall_hours: 'hours granted on its cards' }

const stageColor = { done: 'var(--color-success)', running: 'var(--color-info)', waiting: 'var(--color-warning)', failed: 'var(--color-danger)', future: 'var(--border)', skipped: 'var(--border)' }
function Lines({ value }: { value: unknown }) {
  const lines = Array.isArray(value) ? value.filter((v): v is string => typeof v === 'string') : []
  return lines.length ? <ul className="list-disc space-y-1 pl-5">{lines.map((line, i) => <li key={i}>{line}</li>)}</ul> : <p className="text-muted-foreground">Not recorded yet.</p>
}
function GoalResult({ data }: { data: GoalData }) {
  const [artifact, setArtifact] = useState<string | null>(null)
  const accepted = new Set(data.tasks.filter(t => t.role === 'implement' && t.state === 'accepted').map(t => t.task_id))
  const persisted = data.events.filter(e => e.event_type === 'attempt_persisted' && accepted.has(e.task_id) && e.attempt_id === latestLease(data, data.tasks.find(t => t.task_id === e.task_id)!)?.attempt_id && text(e.body_json.pushed_commit))
  const reviewed = data.tasks.filter(t => t.role === 'implement' && t.state === 'accepted').map(t => currentVerdict(data, t, 'review')).filter(Boolean)
  const reports = data.events.filter(e => e.event_type === 'attempt_persisted' && data.tasks.some(t => t.task_id === e.task_id && ['implement', 'review'].includes(t.role)))
    .flatMap(e => Array.isArray(e.body_json.artifacts) ? e.body_json.artifacts.filter((id): id is string => typeof id === 'string').map((id, i) => ({ id, label: `${data.tasks.find(t => t.task_id === e.task_id)?.role === 'review' ? 'Review' : 'Implementation'} report ${i + 1}` })) : [])
  return <section aria-label="Result" className="rounded border border-[var(--card-border)] p-3 text-sm">
    <h4 className="font-semibold">Result</h4>
    {persisted.map((e, i) => <div key={i}><p>Commit <code>{text(e.body_json.pushed_commit).slice(0, 7)}</code> · Branch {text(e.body_json.pushed_ref).replace(/t_[^/]+/g, '…')}</p>
      {/* Runtime branches contain task identifiers, so keep the full ref in Details. */}
      <details><summary>Details · branch and commit</summary><p className="break-all">Branch: {text(e.body_json.pushed_ref)}</p><p className="break-all">{text(e.body_json.pushed_commit)}</p></details>
    </div>)}
    {!persisted.length && <p>No implementation commit recorded.</p>}
    <p>Review verdict: {reviewed.join(', ') || 'Not recorded'}</p>
    <div className="flex flex-wrap gap-3">{reports.map((r, i) => <button key={`${r.id}-${i}`} className="underline" onClick={() => setArtifact(r.id)}>{r.label}</button>)}</div>
    {artifact && <ArtifactPanel id={artifact} label="Report" close={() => setArtifact(null)} />}
  </section>
}
function ChiefOfStaff({ data, questions, done }: { data: GoalData; questions: Question[]; done: boolean }) {
  const amendment = data.events.filter(e => e.event_type === 'mission_amended').at(-1)?.body_json
  const success = record(data.mission.success_json).criteria ?? amendment?.success
  const scope = record(data.mission.scope_json).out_of_scope ?? amendment?.out_of_scope
  const budget = record(data.mission.budget_json)
  const decisions = data.events.filter(e => e.event_type === 'decision_recorded')
  const asked = goalQuestions(data, questions)
  const dropped = droppedTasks(data), byRule = droppedByRule(data)
  return <details key={done ? 'closed' : 'open'} open={!done} className="text-sm">
    <summary className="cursor-pointer font-semibold">What the chief of staff wrote</summary>
    <div className="mt-3 grid gap-4 md:grid-cols-2">
      <section aria-label="Done when"><h4 className="font-medium">Done when</h4><Lines value={success} /></section>
      <section aria-label="Not doing"><h4 className="font-medium">Not doing</h4><Lines value={scope} /></section>
      <section aria-label="Budget"><h4 className="font-medium">Budget</h4><p>{String(budget.max_attempts ?? amendment?.max_attempts ?? '?')} attempts · {String(budget.max_wall_hours ?? amendment?.max_wall_hours ?? '?')} h maximum</p>
        <dl>{Object.entries(budget).filter(([k]) => !['max_attempts', 'max_wall_hours'].includes(k)).map(([k, v]) => <div key={k}><dt>{budgetLabels[k] ?? k.replaceAll('_', ' ')}</dt><dd>{JSON.stringify(v)}</dd></div>)}</dl></section>
      <section aria-label="Implementation tasks"><h4 className="font-medium">Implementation tasks</h4><ul className="space-y-2">{data.tasks.filter(t => t.role === 'implement').map(t => <li key={t.task_id}>{t.title} · {t.state === 'accepted' ? 'Done' : dropped.has(t.task_id) ? (byRule.has(t.task_id) ? 'Dropped by rule' : 'Dropped by you') : t.state === 'verifying' ? 'Checking' : t.state === 'ready' ? 'Queued' : t.state} · {taskWho(data, t)}</li>)}</ul></section>
      <section aria-label="Decisions" className="md:col-span-2"><h4 className="font-medium">Decisions</h4><ol className="list-decimal space-y-3 pl-5">{decisions.map((e, i) => <li key={i}><strong>{text(e.body_json.decision)}</strong><p>Why: {text(e.body_json.why)}</p>{e.body_json.dropped != null && <p>Dropped: {text(e.body_json.dropped)}</p>}{e.body_json.reverse != null && <p>How to reverse: {text(e.body_json.reverse)}</p>}</li>)}</ol>{!decisions.length && <p>No decisions recorded yet.</p>}</section>
      <section aria-label="Questions asked"><h4 className="font-medium">Questions asked</h4><ul>{asked.map(q => <li key={q.id}><a href={questionHref(q.id)} className="underline">Q{q.number} · {q.title}</a> · {q.status}</li>)}</ul>{!asked.length && <p>No questions recorded.</p>}</section>
    </div>
  </details>
}
/** The task of goal the address points at (`#work?goal=M-n&task=t_…`, T36: a commit's task on Commit lineage), or ''. */
function taskInHash(goal: string): string {
  const q = canonicalQuery(window.location.hash), task = hashQueryParam(q, 'task')
  return hashQueryParam(q, 'goal') === goal && task && /^t_[0-9A-Za-z]+$/.test(task) ? task : ''
}
export function GoalCard({ id, runtime, updatedAt, questions, highlighted = false, submittedGoal = '' }: {
  id: string; runtime?: RuntimeProgress; updatedAt: number; questions: Question[]; highlighted?: boolean; submittedGoal?: string
}) {
  const query = useQuery({ queryKey: ['agent-runtime-goal', id], queryFn: ({ signal }) => fetchRuntimeGoal(id, signal), retry: false,
    staleTime: Infinity, refetchOnWindowFocus: false })
  const previousPoll = useRef(updatedAt)
  const data = query.data
  const finished = data && (goalDone(data) || terminalMission(text(data.mission.state)))
  const refetch = query.refetch
  useEffect(() => {
    if (previousPoll.current === updatedAt) return
    previousPoll.current = updatedAt
    if (!finished) void refetch()
  }, [updatedAt, finished, refetch])
  const now = useNow() || Date.now()
  // The task the address points at: its details open and come into view once the card has its tasks.
  const [target, setTarget] = useState(() => taskInHash(id))
  useEffect(() => {
    const change = () => setTarget(taskInHash(id))
    window.addEventListener('hashchange', change)
    return () => window.removeEventListener('hashchange', change)
  }, [id])
  const targeted = !!target && !!data?.tasks.some(t => t.task_id === target)
  useEffect(() => {
    if (targeted) document.getElementById(goalTaskId(id, target))?.scrollIntoView?.({ block: 'center' })
  }, [targeted, id, target])
  const summary = runtime?.work.missions.find(m => m.mission_id === id)
  const goal = text(data?.mission.goal) || submittedGoal || summary?.goal || summary?.title || 'Loading goal…'
  const links = data ? goalWaitingLinks(id, data, questions, runtime) : []
  if (data?.mission.state === 'awaiting_owner' && !links.length) links.push({ title: 'Review the mission request', href: NEEDS_YOU_HREF })
  const stages = data ? goalStages(data) : []
  // A stage none of whose work the runtime can tell is moving says the runtime's words, not running (T24, review 471 B2).
  const live = runtime?.live ?? []
  const activeStalled = stalledStage(stages.find(s => s.state === 'running') ?? stages[0] ?? { label: '', state: 'future', tasks: [], completed: 0, dropped: 0 }, live)
  const status = (data ? goalStatus(data, links.length > 0) : 'Planning') + (activeStalled ? ` · ${activeStalled}` : '')
  const done = data ? goalDone(data) : false
  const start = text(data?.mission.created_at)
  const accepted = data?.events.filter(e => e.event_type === 'task_state_changed' && e.body_json.to === 'accepted').at(-1)?.at
  const end = done ? accepted : finished ? data?.events.at(-1)?.at : undefined
  return <article id={`goal-${id}`} aria-label={`Goal ${id}`} className={`min-w-0 scroll-mt-4 rounded-[var(--card-radius)] border bg-[var(--card-fill)] p-4 ${highlighted ? 'border-[var(--color-info)] ring-2 ring-[var(--color-info)]' : 'border-[var(--card-border)]'}`}>
    <header className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-3"><span className="rounded bg-[var(--control-fill)] px-2 py-1 font-mono text-sm font-semibold">{id}</span><StatusLamp value={status === 'Done' ? 'ok' : status === 'Blocked' ? 'fail' : 'degraded'} /><strong className="text-lg">{status}</strong></div>
      <h3 className="break-words text-base font-semibold">{goal.split('\n')[0]}</h3>
      <p className="text-xs text-muted-foreground">Given by {data?.mission.created_by_actor_id === 'owner' ? 'You' : text(data?.mission.created_by_actor_id) || 'Unknown'} · <time title={start}>started {relative(start, now)}</time>{start && <span title={[start, end].filter(Boolean).join(' → ')}> · {end ? 'took' : 'elapsed'} {duration(((end ? Date.parse(end) : now) - Date.parse(start)) / 1000)}</span>}</p>
      <details><summary className="cursor-pointer text-sm">Full goal</summary><p className="whitespace-pre-wrap break-words text-sm">{goal}</p></details>
    </header>
    {query.isPending && <p role="status">Loading mission details…</p>}
    {query.isError && <p role="alert">Could not load {id}: {query.error.message}. {data ? 'Showing last known details.' : ''}<button onClick={() => void refetch()} className="ml-2 underline">Retry</button></p>}
    {links.length > 0 && <ul className="my-3 text-sm">{links.map((l, i) => <li key={i}><a className="underline" href={l.href}>Waiting for you · {l.title}</a></li>)}</ul>}
    {data && <div className="mt-4 flex flex-col gap-4">
      <ol aria-label="Goal stages" className="flex flex-wrap gap-2">{stages.map(stage => { const stalled = stalledStage(stage, live), shown = stalled ? 'waiting' : stage.state
        return <li key={stage.label} aria-label={`${stage.label}: ${stalled || stage.state}`} className="min-w-32 flex-1 rounded border border-[var(--card-border)] border-t-4 p-2 text-sm" style={{ borderTopColor: stageColor[shown] }}>
        <div className="flex items-center gap-2"><span aria-hidden="true" style={{ color: stageColor[shown] }}>{stageMarks[shown]}</span><strong>{stage.label}</strong>{stage.tasks.length > 1 && <span>{stage.completed}/{stage.tasks.length}</span>}</div>
        {stalled && <p className="text-xs font-semibold text-[var(--color-warning)] [overflow-wrap:anywhere]">{stalled}: not confirmed running</p>}
        {stage.state === 'skipped' && !stage.dropped && <p className="text-xs text-muted-foreground">Skipped</p>}
        {stage.dropped - (stage.droppedByRule ?? 0) > 0 && <p className="text-xs text-muted-foreground">{stage.dropped - (stage.droppedByRule ?? 0)} dropped by you</p>}
        {(stage.droppedByRule ?? 0) > 0 && <p className="text-xs text-muted-foreground">{stage.droppedByRule} dropped by rule</p>}
        {stage.tasks.map(t => { const timing = taskTiming(data, t, now), lease = latestLease(data, t)
          const live = runtime?.live.find(a => a.task_id === t.task_id)
          // TD-352: an attempt stale or lost with its result not persisted says so, and a lost one is not done.
          const lost = live ? undefined : summary?.tasks.find(x => x.task_id === t.task_id)?.lost_attempt
          const unpersisted = live ? unpersistedWords(live, now) : lost ? unpersistedWords(lost, now) : ''
          // T24: an attempt on a lost host, or maybe stuck, says so; a result computed on the host and not yet on the platform too.
          const stalled = live ? stalledWords(live) : ''
          const computed = summary?.tasks.find(x => x.task_id === t.task_id)?.computed_not_persisted === true
          return <div key={t.task_id} className="mt-1 text-xs text-muted-foreground"><p className="flex flex-wrap items-center gap-1">{taskWho(data, t)}{t.off_plan === true && <OffPlanMark reasons={[t.off_plan_reason ?? '']} />}</p>
            {stalled && <p className="font-semibold text-[var(--color-warning)] [overflow-wrap:anywhere]">{stalled}</p>}
            {computed && <p className="font-semibold text-[var(--color-warning)] [overflow-wrap:anywhere]">Result computed on the host, not yet persisted on the platform</p>}
            {unpersisted && <p className="font-semibold text-[var(--color-warning)]">{lost ? `Attempt ${lost.attempt_no ?? '?'} lost on ${lost.host_id || 'no host'}, not done. ` : ''}{unpersisted}</p>}<p title={timing.title}>{timing.label}</p>{stage.state === 'running' && <p>{live?.host_id || text(lease?.body_json.host_id)}{live?.elapsed_s !== undefined && ` · elapsed ${duration(live.elapsed_s)}`}{live?.heartbeat_age_s !== undefined && ` · heartbeat ${duration(live.heartbeat_age_s)} ago`}</p>}{live?.attempt_id && <AttemptActions key={live.attempt_id} id={live.attempt_id} />}</div>
        })}
      </li> })}</ol>
      {done && <GoalResult data={data} />}
      <ChiefOfStaff data={data} questions={questions} done={done} />
      <details open={targeted || undefined}><summary className="cursor-pointer text-sm">Details</summary><p className="break-words text-xs">Mission state: {text(data.mission.state)} · {summary?.reason}</p>{data.tasks.map(t => <details key={t.task_id} id={goalTaskId(id, t.task_id)} open={(targeted && t.task_id === target) || undefined} className="scroll-mt-4"><summary className="break-all text-xs">{t.role} · {t.task_id} · {t.state}{t.off_plan === true ? ' · off plan' : ''}</summary><TaskTimeline mission={id} task={t.task_id} events={data.events} finished={!!finished} /></details>)}</details>
    </div>}
  </article>
}
