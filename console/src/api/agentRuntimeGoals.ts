import { authedFetch } from '@/api/client'
import { fetchRuntimeEvents, parseRuntimeTask, record, records, text, type RuntimeEvent, type RuntimeTask } from './agentRuntime'

export type GoalTask = RuntimeTask & { created_at: string; updated_at: string; subject_task_id: string }
export type GoalData = { mission: Record<string, unknown>; tasks: GoalTask[]; events: RuntimeEvent[] }
export function parseGoalTasks(value: unknown): GoalTask[] {
  return records(record(value).tasks).map(t => ({ ...parseRuntimeTask(t), created_at: text(t.created_at),
    updated_at: text(t.updated_at), subject_task_id: text(t.subject_task_id) }))
}
export async function fetchRuntimeGoal(mission: string, signal?: AbortSignal): Promise<GoalData> {
  const [detail, tasks, events] = await Promise.all([
    authedFetch('Mission', `/api/v1/agentrt/missions/${encodeURIComponent(mission)}`, { signal }).then(r => r.json()),
    authedFetch('Mission tasks', `/api/v1/agentrt/tasks?${new URLSearchParams({ mission })}`, { signal }).then(r => r.json()),
    fetchRuntimeEvents(mission, signal),
  ])
  if (!text(record(detail).mission_id) || !Array.isArray(record(tasks).tasks)) throw new Error('Mission details are incomplete.')
  if (record(tasks).truncated === true) throw new Error('Mission task list is truncated; progress is incomplete.')
  return { mission: record(detail), tasks: parseGoalTasks(tasks), events }
}
