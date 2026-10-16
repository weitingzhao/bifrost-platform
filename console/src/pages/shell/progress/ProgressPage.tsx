import { useEffect, useState, type ReactNode } from 'react'
import { record, text, parseRuntimeTask } from '@/api/agentRuntime'
import { computedAgeLine, generationIsSkewed, publishedAgeSeconds, receivedIsStale, skewSentence, staleSentence } from '@/api/agentProgress'
import { OpsSection } from '@/components/layout/OpsSection'
import { useNeedsYou } from '../needs-you/useNeedsYou'
import { GoalForm } from '../agents/GoalForm'
import { AgentsPlan } from '../agents/AgentsPlan'
import { TaskRow } from './RuntimeWork'
import { ProgressOverview } from './ProgressOverview'
import { NowClock, Tick } from './progressClock'
import { ProgressActivity, ProgressDoneToday } from './ProgressActivity'
import { FinishedWorks, WorkCard } from './ProgressWorks'
import { splitWorks } from './progressModel'
import { workRunning, workWaiting } from './mergedProgressModel'
import { GoalCard } from './GoalCard'
import { missionIds, relative } from './goalCardModel'
import { ProgressNeedsPreview } from './ProgressNeedsPreview'
import { CodeHealthPage } from '@/pages/CodeHealthPage'
import { CommitLineagePage } from '@/pages/CommitLineagePage'

/** One project surface, ordered by the Owner's questions. */
export function ProgressPage() {
  const needs = useNeedsYou()
  const [submitted, setSubmitted] = useState<{ id: string; goal: string } | null>(() => {
    const id = new URLSearchParams(window.location.hash.split('?')[1]).get('goal')
    return id && /^M-\d+$/.test(id) ? { id, goal: '' } : null
  })
  useEffect(() => {
    const change = () => {
      const id = new URLSearchParams(window.location.hash.split('?')[1]).get('goal')
      if (id && /^M-\d+$/.test(id)) { setSubmitted(current => current?.id === id ? current : { id, goal: '' }); setHighlighted(id) }
    }
    window.addEventListener('hashchange', change)
    return () => window.removeEventListener('hashchange', change)
  }, [])
  const [highlighted, setHighlighted] = useState('')
  useEffect(() => {
    if (!highlighted) return
    const timer = window.setTimeout(() => setHighlighted(''), 6000)
    return () => window.clearTimeout(timer)
  }, [highlighted])
  const { count, runtimeQuery: runtimeQ, progressQuery: boardQ } = needs
  const runtime = runtimeQ.data, board = boardQ.data?.document
  const ids = missionIds(runtime, submitted?.id)
  const { active, finished } = splitWorks(board?.works ?? [])
  function runtimeState(children: ReactNode, empty: boolean, message: string) {
    return <>
      <p className="text-xs text-muted-foreground">Runtime updated: <time title={runtime?.generated_at}><Tick text={now => relative(runtime?.generated_at || '', now)} /></time></p>
      {runtimeQ.isPending && <p role="status">Loading agent runtime…</p>}
      {runtimeQ.isError && <p role="alert">Could not load agent runtime: {runtimeQ.error.message}</p>}
      {runtime && empty && <p>{message}</p>}
      {runtime && children}
    </>
  }
  function boardState() {
    const env = boardQ.data, now = Date.now()
    const stale = env && (env.stale || receivedIsStale(env.received_at, env.age_seconds, now))
    const skewed = env && generationIsSkewed(board?.at, env.received_at, now)
    return <>
      <p className="text-xs text-muted-foreground">Board updated: <time title={env?.generated_at}><Tick text={now => relative(env?.generated_at || '', now)} /></time>{board?.at && <> · <Tick text={t => computedAgeLine(board.at!, t)} /></>}</p>
      {boardQ.isPending && <p role="status">Loading coordinator board…</p>}
      {boardQ.isError && <p role="alert">Could not load coordinator board: {boardQ.error.message}. Last known data may be shown.</p>}
      {stale && <p>{staleSentence(publishedAgeSeconds(env.received_at, env.age_seconds, now))}</p>}
      {skewed && board?.at && <p>{skewSentence(board.at, now)}</p>}
      {boardQ.isSuccess && !board && <p>No progress has been published.</p>}
    </>
  }
  return <NowClock><div className="flex w-full min-w-0 flex-col gap-4 overflow-x-hidden" data-testid="merged-progress">
    <section aria-label="Needs you"><ProgressNeedsPreview needs={needs} /></section>
    <section aria-label="Goals" className="flex min-w-0 flex-col gap-4">
      <GoalForm onCreated={(result, goal) => { setSubmitted({ id: result.mission_id, goal }); setHighlighted(result.mission_id) }} />
      {runtimeState(null, !ids.length, 'No goals yet.')}
      {ids.slice(0, 8).map(id => <GoalCard key={id} id={id} runtime={runtime} updatedAt={runtimeQ.dataUpdatedAt} questions={needs.questions} highlighted={highlighted === id} submittedGoal={submitted?.id === id ? submitted.goal : ''} />)}
    </section>
    <section aria-label="Overview">{boardState()}<ProgressOverview count={count} runtime={runtime} board={board} failed={runtimeQ.isError || boardQ.isError} silentThreads={needs.silent.length} /></section>
    <section aria-label="Coordinator"><OpsSection title="Coordinator" description="Kept by hand by the coordinator thread · not system records" bodyPadding="compact">
      {boardState()}
      {board?.coordinator && <p className="text-sm">{board.coordinator.doing || board.coordinator.note || board.coordinator.state} {board.coordinator.next && `· Then: ${board.coordinator.next}`}</p>}
      {board && active.length === 0 && <p>No coordinator work running.</p>}
      {active.map((work, i) => <details key={`${work.title}-${i}`} open={workRunning(work) || workWaiting(work)}><summary className="cursor-pointer text-sm">{work.title} {workWaiting(work) && <a href="#needs-you">· Waiting for you</a>}</summary><WorkCard work={work} index={i} at={board?.at} /></details>)}
    </OpsSection></section>
    <section aria-label="Plan and architecture"><AgentsPlan /></section>
    <section aria-label="Hosts"><OpsSection title="Hosts" bodyPadding="compact">{runtimeState(<ul className="flex flex-col gap-2">{runtime?.hosts.map(host => <li key={host.host_id} className="text-sm">{host.host_id} · Online: {host.online === undefined ? 'Unknown' : String(host.online)} · Last poll age: {host.poll_age_s ?? '?'} s · Live attempts: {host.live_attempts ?? '?'} · Last poll: <time title={host.last_poll_at}><Tick text={now => relative(host.last_poll_at, now)} /></time></li>)}</ul>, !runtime?.hosts.length, 'No hosts reported.')}</OpsSection></section>
    <section aria-label="History"><OpsSection title="History" collapsible defaultCollapsed bodyPadding="compact">
      {ids.slice(8).map(id => <p key={id} className="text-sm">{id} · {runtime?.work.missions.find(m => m.mission_id === id)?.title || text(runtime?.recent.tasks.find(t => text(t.mission_id) === id)?.title) || 'Finished goal'} · <a href={`#progress?goal=${encodeURIComponent(id)}`} className="underline" onClick={() => { setSubmitted({ id, goal: '' }); setHighlighted(id) }}>View goal</a></p>)}
      {boardState()}<ProgressDoneToday recent={board?.recent} /><FinishedWorks works={finished} at={board?.at} /><ProgressActivity feed={board?.feed} />
      {runtimeState(<>
        <h4>Recent runtime tasks</h4>{runtime?.recent.tasks.length === 0 && <p>No recent tasks.</p>}
        {runtime?.recent.tasks.map((task, i) => <details key={i}><summary className="cursor-pointer break-words text-sm">{text(task.title) || text(task.task_id)} · {text(task.state)} · {text(task.ended_at)}</summary><TaskRow mission={text(task.mission_id)} task={parseRuntimeTask(task)} /></details>)}
        <h4>Failed attempts</h4>{runtime?.recent.failed_attempts.length === 0 && <p>No failed attempts.</p>}
        {runtime?.recent.failed_attempts.map((value, i) => { const a = record(value); return <details key={i}><summary className="cursor-pointer break-words text-sm">{text(a.attempt_id)} · {text(a.task_id)} · {text(a.ended_at)}</summary><TaskRow mission={text(a.mission_id)} task={parseRuntimeTask(a)} /></details> })}
      </>, !runtime?.recent.tasks.length && !runtime?.recent.failed_attempts.length, 'No runtime history.')}
    </OpsSection></section>
    <section aria-label="Commit lineage" className="flex w-full min-w-0 flex-col"><ProgressReference id="commit-lineage" title="Commit lineage"><CommitLineagePage /></ProgressReference></section>
    <section aria-label="Code health" className="flex w-full min-w-0 flex-col"><ProgressReference id="code-health" title="Code health"><CodeHealthPage /></ProgressReference></section>
  </div></NowClock>
}

// targeted: the location names this section (#<id>, or a redirect's ?section=<id>).
function targeted(id: string): boolean {
  return window.location.hash === `#${id}` || new URLSearchParams(window.location.hash.split('?')[1]).get('section') === id
}

function ProgressReference({ id, title, children }: { id: string; title: string; children: ReactNode }) {
  const [open, setOpen] = useState(() => targeted(id))
  useEffect(() => {
    const change = () => { if (targeted(id)) { setOpen(true); document.getElementById(id)?.scrollIntoView?.({ block: 'start' }) } }
    window.addEventListener('hashchange', change)
    return () => window.removeEventListener('hashchange', change)
  }, [id])
  return <details id={id} open={open} onToggle={event => setOpen(event.currentTarget.open)} className="panel-elevated ops-section"><summary className="cursor-pointer p-3 text-sm font-medium">{title}</summary>{open && children}</details>
}
