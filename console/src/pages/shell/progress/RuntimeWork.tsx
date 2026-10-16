import { useState } from 'react'
import { StatusLamp } from '@bifrost/ui'
import {
  parseRuntimeTask, text,
  type RuntimeMission, type RuntimeProgress, type RuntimeRecord, type RuntimeTask,
} from '@/api/agentRuntime'
import { TaskTimeline } from '../agents/TaskTimeline'

function State({ state, status }: { state: string; status?: string }) {
  const value = ['failed', 'blocked', 'lost'].includes(state) || status === 'blocked' ? 'fail'
    : ['accepted', 'done', 'running'].includes(state) ? 'ok' : 'unknown'
  return <span className="inline-flex items-center gap-1"><StatusLamp value={value} />{state || 'Unknown'}{status && ` / ${status}`}</span>
}
export function TaskRow({ mission, task }: { mission: string; task: RuntimeTask }) {
  const [open, setOpen] = useState(['running', 'awaiting_owner', 'waiting_owner'].includes(task.state) || task.status === 'waiting_owner')
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
    {(task.state === 'awaiting_owner' || task.status === 'waiting_owner') && <a href="#needs-you" className="text-sm underline">Waiting for you · See Needs you</a>}
    {open && <TaskTimeline mission={mission} task={task.task_id} />}
  </div>
}
function missionTasks(mission: RuntimeMission, recent: RuntimeRecord[]): RuntimeTask[] {
  const ids = new Set(mission.tasks.map(t => t.task_id))
  return [...mission.tasks, ...recent.filter(t => text(t.mission_id) === mission.mission_id && !ids.has(text(t.task_id))).map(parseRuntimeTask)]
}
export function Missions({ data, history = false }: { data: RuntimeProgress; history?: boolean }) {
  // Mission numbers represent creation order; `since` is a state-change time.
  const missions = [...data.work.missions].sort((a, b) => b.mission_id.localeCompare(a.mission_id, undefined, { numeric: true }))
  return <div className="flex flex-col gap-4">
    {missions.map((mission, i) => {
      const tasks = missionTasks(mission, data.recent.tasks)
      const controls = data.controls.filter(c => c.scope === 'mission' && c.scope_ref === mission.mission_id && !c.cleared_at)
      const active = tasks.some(t => ['running', 'awaiting_owner', 'waiting_owner'].includes(t.state)) || data.live.some(a => a.mission_id === mission.mission_id) || ['owner', 'you'].includes(mission.waiting_on)
      if (!history && ['done', 'stopped', 'accepted', 'failed'].includes(mission.state)) return null
      return <details open={active} key={`${mission.mission_id}-${i}`} className="flex min-w-0 flex-col gap-2">
        <summary className="cursor-pointer text-sm font-medium">{mission.mission_id} · {mission.title || 'Untitled mission'}</summary>
        <State state={mission.state} status={mission.status} />
        {mission.waiting_on === 'owner' && <a href="#needs-you" className="text-sm underline">Waiting for you · See Needs you</a>}
        <p className="break-words text-sm">{mission.reason || 'No reason reported'}</p>
        {mission.goal && <p className="break-words text-sm text-muted-foreground">{mission.goal}</p>}
        {controls.map((control, j) => <p key={j} className="text-sm">Control: {text(control.control_kind)} · {text(control.reason)}</p>)}
        {tasks.length === 0 && <p>No tasks reported.</p>}
        {(history ? tasks : tasks.filter(t => !['done', 'accepted', 'failed', 'cancelled'].includes(t.state))).map((task, j) => <TaskRow key={`${task.task_id}-${j}`} mission={mission.mission_id} task={task} />)}
      </details>
    })}
  </div>
}
