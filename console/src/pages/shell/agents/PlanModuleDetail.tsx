import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useQuery, type UseQueryResult } from '@tanstack/react-query'
import { fetchRuntimeProgress } from '@/api/agentRuntime'
import { fetchApprovalList } from '@/api/approvals'
import type { PlanNode, PlanSnapshot } from '@/api/planSnapshot'
import { fetchQuestionList } from '@/api/questions'
import { NEEDS_YOU_HREF } from '@/lib/shell/consoleRoutes'
import { useNow } from '../progress/progressClock'
import { StepBadge } from './StepText'
import { actionsLeft, approvalActivity, isStale, mergeActivity, moduleDetailId, moduleScopes, moduleStepRowId, needsNow, needsYouCards, nextInPlanOrder, nowSteps,
  planActivity, questionActivity, reviewWords, runtimeTasks, SHELL_POLL_MS, stepTasks, taskActivity, taskLine, workTasks, type ShellRead, type ShellReads,
  type TaskLine } from './moduleDetailModel'
import { cellLabels, cellStyle, toGoText, type CellState } from './planModuleModel'
import { planNodeHref } from './usePlanSnapshot'

/** Recent activity shows this many rows until the Owner asks for all. */
const ACTIVITY_ROWS = 12

function shellRead<T>(q: UseQueryResult<T>): ShellRead<T> {
  if (q.isError) return { state: 'unknown', reason: q.error instanceof Error ? q.error.message : 'Request failed' }
  if (q.isPaused) return { state: 'unknown', reason: 'Offline' }
  if (q.data !== undefined) return { state: 'ok', data: q.data, at: q.dataUpdatedAt }
  return q.fetchStatus === 'fetching' ? { state: 'reading' } : { state: 'unread' }
}

/**
 * The four lists the shell already polls on every Console page (ConsolePage → useOwnerYours → useNeedsYou: questions
 * and requests every 30 s, the runtime's progress every 5 s), read here from that cache with the same keys and never
 * fetched: enabled is false, so the detail adds no request (the 10-31 PROD platform-api OOM came from Work's reads).
 */
function useShellReads(): ShellReads {
  const questions = useQuery({ queryKey: ['questions', 'all'], queryFn: () => fetchQuestionList('all'), enabled: false, retry: false })
  const open = useQuery({ queryKey: ['approvals', 'open'], queryFn: () => fetchApprovalList('open'), enabled: false, retry: false })
  const all = useQuery({ queryKey: ['approvals', 'all'], queryFn: () => fetchApprovalList('all'), enabled: false, retry: false })
  const progress = useQuery({ queryKey: ['agent-runtime-progress'], queryFn: ({ signal }) => fetchRuntimeProgress(signal), enabled: false, retry: false })
  return { questions: shellRead(questions), open: shellRead(open), all: shellRead(all), progress: shellRead(progress) }
}
const SOURCES: [keyof ShellReads, string][] = [['questions', 'Questions'], ['open', 'Requests'], ['all', 'Request history'], ['progress', 'Runtime progress']]
const readWords = (read: ShellRead<unknown>, stale: boolean) => read.state === 'ok' ? `${new Date(read.at).toISOString()}${stale ? ' · Stale' : ''}`
  : read.state === 'unknown' ? `Unknown · ${read.reason}` : read.state === 'reading' ? 'reading…' : 'not read yet'

function Tile({ label, children }: { label: string; children: ReactNode }) {
  return <section aria-label={label} className="flex min-w-0 flex-col gap-1 rounded-[var(--control-radius)] border border-[var(--card-border)] bg-[var(--control-fill)] p-3 text-sm [overflow-wrap:anywhere]">
    <h4 className="m-0 text-xs font-semibold uppercase tracking-wide text-muted-foreground">{label}</h4>
    {children}
  </section>
}
function Big({ children }: { children: ReactNode }) {
  return <strong className="text-2xl font-bold leading-tight">{children}</strong>
}
function StatePill({ state }: { state: CellState }) {
  return <span data-step-state={state} className="inline-flex shrink-0 items-center gap-1 rounded-full border border-[var(--card-border)] px-2 text-xs font-semibold">
    <span aria-hidden="true" className="inline-block h-2 w-4 rounded-full" style={cellStyle[state]} />{cellLabels[state]}</span>
}
function Who({ owner }: { owner: boolean }) {
  return <span className="shrink-0 rounded-full bg-[var(--card-fill)] px-1.5 text-xs font-semibold" title={owner ? "The Owner's step (owner_step)" : 'Not an Owner step: the crew does it'}>{owner ? 'Owner' : 'Agent'}</span>
}
function Task({ task }: { task: TaskLine }) {
  return <span className="min-w-0">
    {task.role} · <a href={task.href} className="underline-offset-2 hover:underline pointer-coarse:inline-flex pointer-coarse:min-h-11 pointer-coarse:items-center">{task.title}</a>
    {' · '}{task.state}{task.round && ` · ${task.round}`}{task.attempt && ` · ${task.attempt}`}{task.detail && ` · ${task.detail}`}
  </span>
}
const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`

/**
 * Work → Plan: one module's detail (R5:5.1-7; Design v3.6 ItemDetail), opened by the module's link or by an address
 * naming the module or one of its steps. What is left, what needs the Owner, what runs now and next, the steps, and
 * what happened lately — each from a field the Console already reads, Unknown where none says.
 */
export function PlanModuleDetail({ view, item, step, stale }: { view: PlanSnapshot; item: PlanNode; step: string; stale: boolean }) {
  const now = useNow()
  const reads = useShellReads()
  const heading = useRef<HTMLHeadingElement>(null)
  const [allActivity, setAllActivity] = useState(false)
  const scopes = useMemo(() => moduleScopes(view), [view])
  const scope = scopes.get(item.plan_node_id)!
  const row = scope.row
  const runtime = runtimeTasks(reads.progress)
  const cards = needsYouCards(reads.progress)?.cards ?? new Map()
  const needs = needsNow(view, scopes, scope, reads, now)
  const actions = actionsLeft(scope, needs)
  const current = nowSteps(scope, runtime)
  const next = nextInPlanOrder(scope)
  const plan = planActivity(view, scope, scopes, cards)
  const activity = mergeActivity(plan.rows, questionActivity(view, scope, scopes, reads.questions, cards), approvalActivity(view, scope, scopes, reads.all), taskActivity(scope, runtime))
  const shown = allActivity ? activity : activity.slice(0, ACTIVITY_ROWS)
  const stales = SOURCES.filter(([key]) => isStale(reads[key], SHELL_POLL_MS[key], now)).map(([, name]) => name)
  // Arriving at a module, or at one of its steps, puts the keyboard there: the step's row, else the module's heading.
  useEffect(() => {
    const target = step ? document.getElementById(moduleStepRowId(step)) : heading.current
    target?.focus({ preventScroll: true })
  }, [item.plan_node_id, step])
  return <section id={moduleDetailId(item.plan_node_id)} aria-label={`Module ${item.label}`} data-module-detail={item.label}
    className="flex min-w-0 scroll-mt-4 flex-col gap-3 rounded-[var(--card-radius)] border border-[var(--color-info)] bg-[var(--card-fill)] p-4 [overflow-wrap:anywhere]">
    <header className="flex min-w-0 flex-wrap items-start gap-2">
      <div className="flex min-w-0 flex-[1_1_16rem] flex-col gap-1">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <StepBadge label={item.label} />
          <span className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{row.lane}</span>
          {row.current && <span className="rounded-full border border-[var(--color-info)] px-2 text-xs font-semibold">Current</span>}
        </div>
        <h3 ref={heading} tabIndex={-1} className="m-0 break-words text-base font-semibold focus:outline-none">{row.title || 'Untitled module'}</h3>
      </div>
      <a href={planNodeHref(view.root)} className="ml-auto inline-flex min-h-11 shrink-0 items-center rounded-[var(--control-radius)] border border-[var(--card-border)] px-3 text-sm">All modules</a>
    </header>
    <dl aria-label="Read as of" className="m-0 flex min-w-0 flex-wrap gap-x-4 gap-y-0.5 text-xs text-muted-foreground">
      <div className="flex min-w-0 flex-wrap gap-1"><dt>Plan snapshot</dt><dd className="m-0"><time dateTime={view.as_of}>{view.as_of}</time> · {stale ? 'Stale' : 'Fresh'}</dd></div>
      {SOURCES.map(([key, name]) => <div key={key} className="flex min-w-0 flex-wrap gap-1"><dt>{name}</dt>
        <dd className="m-0" data-source={key}>{readWords(reads[key], isStale(reads[key], SHELL_POLL_MS[key], now))}</dd></div>)}
    </dl>

    <div className="grid min-w-0 grid-cols-1 gap-2 sm:grid-cols-2 xl:grid-cols-3">
      <Tile label="Steps to go">
        {row.total === 0 || row.toGo === 0 ? <Big>{toGoText(row)}</Big>
          : <p className="m-0" data-to-go><Big>{row.toGo}</Big> {row.toGo === 1 ? 'step' : 'steps'} to go</p>}
        <span>of {row.total} · {row.done} done{row.active > 0 && ` · ${row.active} in progress`}{row.stopped > 0 && ` · ${row.stopped} stopped`}</span>
        {row.unknown > 0 && <span>{plural(row.unknown, 'step')} without a state: Unknown</span>}
        {row.dropped > 0 && <span className="text-xs text-muted-foreground">{row.dropped} dropped, not counted</span>}
        <span className="text-xs text-muted-foreground">Execution progress from the controller's step states. Not acceptance, not deployment.</span>
      </Tile>

      <Tile label="Your actions found">
        <p className="m-0" data-actions-found><Big>{actions.total}{actions.floor && <span aria-hidden="true">+</span>}</Big>
          {actions.floor && <span className="sr-only"> or more</span>}</p>
        <span>{actions.items} counted under Needs you now{actions.notCounted > 0 && ` · ${actions.notCounted} listed, not counted`}</span>
        <span>{plural(actions.ownerSteps, 'Owner step')} not done, {actions.bareOwnerSteps} with nothing counted</span>
        {actions.stopped > 0 && <span>{plural(actions.stopped, 'Owner step')} stopped, not counted</span>}
        <span className="text-xs text-muted-foreground">Owner steps not done, plus what Needs you now counts; cards without evidence here are not counted (see Needs you).</span>
        {actions.floor && <span className="text-xs text-muted-foreground">At least: a list is not read or was cut (Needs you now says which).</span>}
        {stales.length > 0 && <span className="text-xs text-muted-foreground">Stale: {stales.join(', ')} not refreshed for over twice their poll.</span>}
      </Tile>

      <Tile label="Now">
        {current.length === 0
          ? <p className="m-0">{row.unknown > 0 ? `No step in progress by the runtime's states; ${plural(row.unknown, 'step')} without a state (Unknown).` : 'No step in progress.'}</p>
          : <ul className="m-0 flex list-none flex-col gap-2 p-0">{current.map(s => <li key={s.cell.id} className="flex min-w-0 flex-col gap-1">
            <span className="flex min-w-0 flex-wrap items-center gap-2"><StepBadge label={s.cell.label} /><span className="min-w-0 break-words">{s.cell.title || 'Untitled step'}</span></span>
            {s.tasks.length === 0 ? <span className="text-muted-foreground">No work task on this step yet (checks are under Review)</span>
              : s.tasks.map(t => <Task key={t.id} task={t} />)}
          </li>)}</ul>}
      </Tile>

      <Tile label="Next in plan order">
        {next ? <>
          <span className="flex min-w-0 flex-wrap items-center gap-2"><StepBadge label={next.label} /><Who owner={next.owner} /><span className="min-w-0 break-words">{next.title || 'Untitled step'}</span></span>
          <span className="text-xs text-muted-foreground">Plan order only, not a commitment.</span>
        </> : <p className="m-0">{row.unknown > 0 ? `Unknown · ${plural(row.unknown, 'step')} without a state` : 'No step left to start'}</p>}
      </Tile>

      <Tile label="Needs you now">
        {needs.items.length === 0 && <p className="m-0">Nothing linked to {item.label} found.</p>}
        {needs.items.length > 0 && <ul className="m-0 flex list-none flex-col gap-1 p-0">{needs.items.map(n => <li key={n.key} data-need={n.kind} className="flex min-w-0 flex-col">
          <a href={n.href} className="inline-flex min-h-11 items-center underline-offset-2 hover:underline">{n.text}</a>
          {n.note && <span className="text-xs text-muted-foreground">{n.note}{!n.counted && ' · not counted'}</span>}
        </li>)}</ul>}
        {needs.gaps.map(g => <span key={g} className="text-xs text-muted-foreground">{g}</span>)}
        <span className="text-xs text-muted-foreground">Listed only with direct evidence: its task is on a step of {item.label}, a step's done rule names it, or the plan change it decides touches {item.label} (seen only while its proposal is among the plan's latest {view.recent_changes.length} events). Not listed: cards and requests with no such evidence here, including those that only share a mission with {item.label}; so a card of {item.label} can be missing from this list and from Your actions found.</span>
        <a href={NEEDS_YOU_HREF} data-needs-you className="inline-flex min-h-11 items-center text-xs underline-offset-2 hover:underline">Cards with no evidence here for a module of {view.root} are on Needs you</a>
      </Tile>

      <Tile label="ETA">
        <Big>{view.eta.status}</Big>
        <span>{view.eta.reason}</span>
        <span className="text-xs text-muted-foreground">as of <time dateTime={view.as_of}>{view.as_of}</time></span>
      </Tile>
    </div>

    <section aria-label={`Steps of ${item.label}`} className="flex min-w-0 flex-col gap-1">
      <h4 className="m-0 text-sm font-semibold">Steps · plan order</h4>
      {row.cells.length === 0 ? <p className="m-0 text-sm">No steps in this module.</p>
        : <ol className="m-0 flex min-w-0 list-none flex-col p-0">{row.cells.map(cell => {
          const work = workTasks(scope, cell.id, runtime)
          const checks = stepTasks(scope, cell.id).length - work.length
          const latest = work[0] && taskLine(work[0], runtime)
          const selected = cell.id === step
          return <li key={cell.id} id={moduleStepRowId(cell.id)} tabIndex={-1} data-step-row={cell.label} aria-current={selected ? 'step' : undefined}
            className={`flex min-w-0 scroll-mt-4 flex-col gap-1 border-b border-[var(--table-rule)] px-1 py-2 text-sm focus:outline-none focus-visible:outline-2 focus-visible:outline-[var(--ring)] ${selected ? 'bg-[var(--control-fill)]' : ''}`}>
            <div className="flex min-w-0 flex-wrap items-center gap-2">
              <a href={planNodeHref(view.root, cell.id)} aria-label={`Open step ${cell.label}`} className="inline-flex min-h-6 items-center pointer-coarse:min-h-11"><StepBadge label={cell.label} /></a>
              <StatePill state={cell.state} />
              <Who owner={cell.owner} />
              <span className="min-w-0 flex-[1_1_12rem] break-words">{cell.title || 'Untitled step'}</span>
            </div>
            <dl className="m-0 flex min-w-0 flex-wrap gap-x-4 gap-y-1 text-xs">
              <div className="flex min-w-0 flex-wrap gap-1"><dt className="text-muted-foreground">Latest task</dt>
                <dd className="m-0 min-w-0">{latest ? <Task task={latest} /> : 'None'}{work.length > 1 && ` · ${work.length - 1} earlier`}{checks > 0 && ` · ${plural(checks, 'check')}`}</dd></div>
              <div className="flex min-w-0 flex-wrap gap-1"><dt className="text-muted-foreground">Review</dt><dd className="m-0">{reviewWords(scope, view, cell.id, runtime)}</dd></div>
            </dl>
          </li>
        })}</ol>}
      {row.dropped > 0 && <p className="m-0 text-xs text-muted-foreground">{plural(row.dropped, 'dropped step')} not listed.</p>}
      <p className="m-0 text-xs text-muted-foreground">Latest task is the step's newest own work, not its checks: a task's role is the runtime's while it lists the task (unfinished, or among its latest 20 ended), else the title the controller gives a check, else Unknown. Review is the newest check of that task only (by the task its title names); its verdict is its attempt's conclusion (done passes) while the runtime lists it among its latest 20 ended tasks. A check the controller closed without using its result is not told apart.</p>
    </section>

    <section aria-label={`Recent activity on ${item.label}`} className="flex min-w-0 flex-col gap-1">
      <h4 className="m-0 text-sm font-semibold">Recent activity</h4>
      {activity.length === 0 ? <p className="m-0 text-sm">No activity on {item.label} in what was read.</p>
        : <ol className="m-0 flex min-w-0 list-none flex-col p-0">{shown.map(a => <li key={a.key} data-activity={a.source} className="flex min-w-0 flex-wrap gap-x-2 border-b border-[var(--table-rule)] py-1.5 text-sm">
          <time dateTime={a.at} className="shrink-0 font-mono text-xs text-muted-foreground">{a.at}</time>
          <strong className="shrink-0">{a.kind}</strong>
          {a.href ? <a href={a.href} className="min-w-0 break-words underline-offset-2 hover:underline">{a.text}</a> : <span className="min-w-0 break-words">{a.text}</span>}
        </li>)}</ol>}
      {activity.length > ACTIVITY_ROWS && <button type="button" onClick={() => setAllActivity(v => !v)} aria-expanded={allActivity}
        className="min-h-11 self-start rounded-[var(--control-radius)] border border-[var(--card-border)] px-3 text-sm">{allActivity ? 'Show fewer' : `Show all ${activity.length}`}</button>}
      <p className="m-0 text-xs text-muted-foreground">Plan events: only the latest {view.recent_changes.length} of {view.root} up to <time dateTime={view.recent_as_of}>{view.recent_as_of}</time> (older ones under Recent changes); a step's earlier moves are not tied to it there. A plan change appears here only when one of its events in that window names a node of {item.label} or decides a question of it; the others are under Recent changes.</p>
      <p className="m-0 text-xs text-muted-foreground">Ended tasks and checks: only those among the runtime's latest 20 ended tasks of every goal. Questions: the question store keeps a bounded history of answered ones. Requests: as the Console last read them.</p>
    </section>
  </section>
}
