import { useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { StatusLamp } from '@bifrost/ui'
import {
  AGENT_RUNTIME_POLL_MS, fetchRuntimeProgress, parseRuntimeTask, text,
  type RuntimeMission, type RuntimeProgress, type RuntimeRecord, type RuntimeTask,
} from '@/api/agentRuntime'
import { OpsSection } from '@/components/layout/OpsSection'
import { GoalForm } from './GoalForm'
import { TaskTimeline } from './TaskTimeline'
import { AgentsPlan } from './AgentsPlan'

function State({ state, status }: { state: string; status?: string }) {
  const value = ['failed', 'blocked', 'lost'].includes(state) || status === 'blocked' ? 'fail'
    : ['accepted', 'done', 'running'].includes(state) ? 'ok' : 'unknown'
  return <span className="inline-flex items-center gap-1"><StatusLamp value={value} />{state || 'Unknown'}{status && ` / ${status}`}</span>
}
function TaskRow({ mission, task }: { mission: string; task: RuntimeTask }) {
  const [open, setOpen] = useState(false)
  return <div className="min-w-0 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)]">
    <button type="button" aria-expanded={open} onClick={() => setOpen(!open)}
      className="flex w-full flex-wrap items-center gap-x-3 gap-y-1 p-2 text-left text-sm">
      <span>{open ? '▾' : '▸'} {task.role || 'Unknown role'}</span>
      <strong>{task.title || task.task_id || 'Untitled task'}</strong>
      <State state={task.state} status={task.status} />
      <span>Round {task.round_no ?? '?'}/{task.max_rounds ?? '?'}</span>
      <span className="break-all text-xs text-muted-foreground">{task.task_id}</span>
      <span className="w-full break-words">{task.reason || 'No reason reported'} · Since {task.since || 'unknown'}</span>
    </button>
    {open && <TaskTimeline mission={mission} task={task.task_id} />}
  </div>
}
function missionTasks(mission: RuntimeMission, recent: RuntimeRecord[]): RuntimeTask[] {
  const ids = new Set(mission.tasks.map(t => t.task_id))
  return [...mission.tasks, ...recent.filter(t => text(t.mission_id) === mission.mission_id && !ids.has(text(t.task_id))).map(parseRuntimeTask)]
}
function Missions({ data }: { data: RuntimeProgress }) {
  // Mission numbers represent creation order; `since` is a state-change time.
  const missions = [...data.work.missions].sort((a, b) => b.mission_id.localeCompare(a.mission_id, undefined, { numeric: true }))
  return <div className="flex flex-col gap-4">
    {missions.map((mission, i) => {
      const tasks = missionTasks(mission, data.recent.tasks)
      const controls = data.controls.filter(c => c.scope === 'mission' && c.scope_ref === mission.mission_id && !c.cleared_at)
      return <article key={`${mission.mission_id}-${i}`} className="flex min-w-0 flex-col gap-2">
        <h4 className="text-sm font-medium">{mission.mission_id} · {mission.title || 'Untitled mission'}</h4>
        <State state={mission.state} status={mission.status} />
        <p className="break-words text-sm">{mission.reason || 'No reason reported'}</p>
        {mission.goal && <p className="break-words text-sm text-muted-foreground">{mission.goal}</p>}
        {controls.map((control, j) => <p key={j} className="text-sm">Control: {text(control.control_kind)} · {text(control.reason)}</p>)}
        {tasks.length === 0 && <p>No tasks reported.</p>}
        {tasks.map((task, j) => <TaskRow key={`${task.task_id}-${j}`} mission={mission.mission_id} task={task} />)}
      </article>
    })}
  </div>
}
function display(value: unknown): string {
  if (value === undefined || value === null) return 'Unknown'
  return typeof value === 'string' ? value : JSON.stringify(value)
}

export function AgentsPage() {
  const query = useQuery({ queryKey: ['agent-runtime-progress'],
    queryFn: ({ signal }) => fetchRuntimeProgress(signal), refetchInterval: AGENT_RUNTIME_POLL_MS })
  const data = query.data
  function section(title: string, count: number | undefined, empty: string, children: ReactNode) {
    return <OpsSection title={title} bodyPadding="compact" actions={<span className="text-xs text-muted-foreground">Generated: {data?.generated_at || 'Unknown'}</span>}>
      {query.isPending && <p role="status">Loading {title.toLowerCase()}…</p>}
      {query.isError && <p role="alert">Could not load {title.toLowerCase()}: {query.error.message}</p>}
      {data && count === 0 && <p>{empty}</p>}
      {data && children}
    </OpsSection>
  }
  return <div className="flex w-full min-w-0 flex-col gap-4 overflow-x-hidden">
    <GoalForm />
    {section('Missions', data?.work.missions.length, 'No missions reported.', data && <Missions data={data} />)}
    {section('Running now', data?.live.length, 'No attempts running.', <div className="flex flex-col gap-2">
      {data?.live.map((attempt, i) => <div key={`${attempt.attempt_id}-${i}`} className="break-words rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] p-2 text-sm">
        <strong>{attempt.task_id} · {attempt.host_id}</strong> · {attempt.executor} / {attempt.model || 'Model unknown'}
        <p>Elapsed: {attempt.elapsed_s ?? '?'} s · Heartbeat age: {attempt.heartbeat_age_s ?? '?'} s</p>
        <State state={attempt.state} status={attempt.status} /><p>{attempt.reason || 'No reason reported'}</p>
      </div>)}
    </div>)}
    {section('Hosts', data?.hosts.length, 'No hosts reported.', <div className="flex flex-col gap-2">
      {data?.hosts.map((host, i) => <div key={`${host.host_id}-${i}`} className="break-words rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] p-2 text-sm">
        <strong>{host.host_id}</strong> · Online: {display(host.online)} · Last poll age: {host.poll_age_s ?? '?'} s
        <p>Last poll: {host.last_poll_at || 'Unknown'} · Live attempts: {host.live_attempts ?? '?'}</p>
        <State state={host.state} />
      </div>)}
    </div>)}
    {section('Needs you', data?.needs_you.items.length, 'No items reported.', <>
      {data && <p>Count: {data.needs_you.count ?? 'Unknown'}{data.needs_you.truncated && ' · List truncated'}</p>}
      <ul className="flex flex-col gap-2">{data?.needs_you.items.map((item, i) => <li key={i} className="whitespace-pre-wrap break-words text-sm">{display(item)}</li>)}</ul>
    </>)}
    <AgentsPlan />
  </div>
}
