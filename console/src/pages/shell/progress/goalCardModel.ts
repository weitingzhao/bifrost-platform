import { record, text, type RuntimeEvent, type RuntimeProgress } from '@/api/agentRuntime'
import type { GoalData, GoalTask } from '@/api/agentRuntimeGoals'
import type { Question } from '@/api/questions'
import { questionHref } from '@/lib/shell/consoleRoutes'

export type StageState = 'done' | 'running' | 'waiting' | 'failed' | 'future' | 'skipped'
export type GoalStage = { label: string; state: StageState; tasks: GoalTask[]; completed: number }
export const stageMarks: Record<StageState, string> = { done: '✓', running: '●', waiting: '⏸', failed: '✗', future: '○', skipped: '—' }
export const terminalMission = (state: string) => ['done', 'accepted', 'stopped', 'failed', 'cancelled'].includes(state)
export const executorName = (id: string) => ({ 'exec:claude': 'Claude', 'exec:codex': 'Codex', 'exec:cursor': 'Cursor', 'exec:deterministic': 'Checks' })[id] || id || 'Unassigned'
export function duration(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds))
  return s < 60 ? `${s} s` : s < 3600 ? `${Math.floor(s / 60)} min` : `${Math.floor(s / 3600)} h ${Math.floor(s % 3600 / 60)} min`
}
export function relative(at: string, now: number): string {
  const ms = Date.parse(at)
  return Number.isFinite(ms) ? `${duration((now - ms) / 1000)} ago` : 'Time unknown'
}
export function taskEvents(data: GoalData, task: GoalTask): RuntimeEvent[] { return data.events.filter(e => e.task_id === task.task_id) }
export function latestLease(data: GoalData, task: GoalTask) { return taskEvents(data, task).filter(e => e.event_type === 'attempt_leased').at(-1) }
export function taskWho(data: GoalData, task: GoalTask): string {
  const lease = latestLease(data, task)
  const started = [...taskEvents(data, task)].reverse().find(e => e.event_type === 'attempt_started' && e.attempt_id === lease?.attempt_id)
  return [executorName(text(lease?.body_json.executor)), text(started?.body_json.model) || text(lease?.body_json.model)].filter(Boolean).join(' · ')
}
export function taskTiming(data: GoalData, task: GoalTask, now: number): { label: string; title: string } {
  const events = taskEvents(data, task), lease = latestLease(data, task)
  const attempt = events.filter(e => e.attempt_id === lease?.attempt_id)
  const start = attempt.find(e => e.event_type === 'attempt_started')?.at || lease?.at || task.created_at
  const end = attempt.find(e => e.event_type === 'attempt_submitted')?.at || (task.state === 'accepted' ? task.updated_at : '')
  if (!start) return { label: 'Time unknown', title: '' }
  return { label: `${end ? 'took' : 'elapsed'} ${duration(((end ? Date.parse(end) : now) - Date.parse(start)) / 1000)}`, title: [start, end].filter(Boolean).join(' → ') }
}
function verdicts(data: GoalData, method: string) {
  // Verification events refer to the implementation, not the verification task.
  const latest = new Map<string, RuntimeEvent>()
  for (const e of data.events) {
    if (e.event_type !== 'verification_recorded' || e.body_json.method !== method) continue
    const task = data.tasks.find(t => t.task_id === e.task_id)
    const lease = task && latestLease(data, task)
    if (lease && e.attempt_id && e.attempt_id !== lease.attempt_id) continue
    latest.set(e.task_id, e)
  }
  return [...latest.values()]
}
export function goalDone(data: GoalData): boolean {
  if (['stopped', 'failed', 'cancelled'].includes(text(data.mission.state))) return false
  if (['deterministic', 'review'].some(m => verdicts(data, m).some(e => e.body_json.verdict === 'fail'))) return false
  return ['done', 'accepted'].includes(text(data.mission.state)) || (data.tasks.length > 0 && data.tasks.every(t => t.state === 'accepted'))
}
export function goalStages(data: GoalData): GoalStage[] {
  const done = goalDone(data)
  const stages: GoalStage[] = [{ label: 'Goal', state: 'done', tasks: [], completed: 1 }]
  for (const [role, label] of [['plan', 'Plan'], ['implement', 'Implement'], ['verify', 'Verify'], ['review', 'Review']]) {
    const tasks = data.tasks.filter(t => t.role === role)
    const results = role === 'verify' || role === 'review' ? verdicts(data, role === 'verify' ? 'deterministic' : 'review') : []
    const completed = tasks.filter(t => t.state === 'accepted' || (role === 'implement' && ['submitted', 'verifying'].includes(t.state))).length
    let state: StageState = 'future'
    if (tasks.some(t => ['failed', 'blocked', 'rejected'].includes(t.state)) || results.some(e => e.body_json.verdict === 'fail')) state = 'failed'
    else if (tasks.some(t => ['awaiting_owner', 'waiting_owner', 'paused'].includes(t.state) || t.status === 'waiting_owner')) state = 'waiting'
    else if (tasks.length && (completed === tasks.length || (results.length >= tasks.length && results.every(e => e.body_json.verdict === 'pass')))) state = 'done'
    else if (tasks.some(t => ['ready', 'leased', 'running', 'submitted', 'verifying', 'rework'].includes(t.state))) state = 'running'
    else if (role === 'plan' && !tasks.length) state = 'skipped'
    else if (!done && ['done', 'skipped'].includes(stages.at(-1)?.state || '') && (!tasks.length || tasks.some(t => t.state === 'proposed'))) state = 'running'
    stages.push({ label: label!, state, tasks, completed: results.length && state === 'done' ? tasks.length : completed })
  }
  stages.push({ label: 'Done', state: done ? 'done' : 'future', tasks: [], completed: done ? 1 : 0 })
  return stages
}
export function goalStatus(data: GoalData, waiting: boolean): string {
  if (['stopped', 'cancelled'].includes(text(data.mission.state))) return 'Stopped'
  if (waiting || data.mission.state === 'awaiting_owner') return 'Waiting for you'
  const stages = goalStages(data)
  if (stages.some(s => s.state === 'failed') || data.mission.state === 'failed') return 'Blocked'
  if (goalDone(data)) return 'Done'
  if (data.mission.state === 'paused' || stages.some(s => s.state === 'waiting')) return 'Blocked'
  const active = stages.find(s => s.state === 'running')?.label || stages.find(s => s.state === 'future')?.label
  return ({ Plan: 'Planning', Implement: 'Implementing', Verify: 'Checking', Review: 'Reviewing', Done: 'Checking' })[active || 'Plan'] || 'Planning'
}
export function goalQuestions(data: GoalData, questions: Question[]): Question[] {
  const plan = new Set(data.tasks.filter(t => t.role === 'plan').map(t => t.task_id))
  return questions.filter(q => q.task_id && plan.has(q.task_id))
}
export function goalWaitingLinks(id: string, data: GoalData, questions: Question[], runtime?: RuntimeProgress): { title: string; href: string }[] {
  const links = goalQuestions(data, questions).filter(q => q.status === 'open').map(q => ({ title: q.title, href: questionHref(q.id) }))
  const ids = new Set(data.tasks.map(t => t.task_id))
  for (const value of runtime?.needs_you.items ?? []) {
    const item = record(value), action = record(item.action)
    if (item.mission_id !== id && !ids.has(text(item.task_id)) && !(Array.isArray(record(item.blocks).missions) && (record(item.blocks).missions as unknown[]).includes(id))) continue
    const qid = text(action.question_id)
    if (qid && links.some(l => l.href === questionHref(qid))) continue
    links.push({ title: text(item.title) || 'Review the runtime request', href: qid ? questionHref(qid) : '#needs-you' })
  }
  return links
}
export function missionIds(runtime?: RuntimeProgress, submitted = ''): string[] {
  return [...new Set([submitted, ...(runtime?.work.missions.map(m => m.mission_id) ?? []), ...(runtime?.recent.tasks.map(t => text(t.mission_id)) ?? [])].filter(Boolean))]
    .sort((a, b) => a === submitted ? -1 : b === submitted ? 1 : b.localeCompare(a, undefined, { numeric: true }))
}
