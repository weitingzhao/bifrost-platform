import { formatDuration, type ProgressStep, type ProgressWork } from '@/api/agentProgress'
import type { ProgressEvent, ProgressQueueItem, ProgressRoadmap } from '@/api/agentProgressBoard'

const RUNNING_NOW = new Set(['running', 'slow', 'quiet', 'dead', 'failed'])

export function showsInRunningNow(step: ProgressStep): boolean {
  const state = step.state ?? ''
  if (state === 'waiting') return step.needs_owner !== true
  return RUNNING_NOW.has(state)
}

/** A step that is underway, as opposed to done or still to come. */
export function showLive(step: ProgressStep): boolean {
  const state = step.state ?? ''
  return state !== 'done' && state !== 'gap'
}

export function workIsFinished(work: ProgressWork): boolean {
  const steps = work.steps ?? []
  if (steps.length === 0) return work.finished === true
  return steps.every(step => step.state === 'done')
}

export function workFinishedAt(work: ProgressWork): number {
  let max = 0
  for (const step of work.steps ?? []) {
    const ended = step.ended ?? 0
    if (ended > max) max = ended
  }
  if (max > 0) return max
  return work.end ?? 0
}

function workBucket(work: ProgressWork): number {
  const steps = work.steps ?? []
  if (steps.some(step => step.needs_owner === true && step.state !== 'done')) return 0
  if (steps.some(step => step.state === 'running' || step.state === 'slow')) return 1
  return 2
}

/** Owner work first, then work with a running step, then the rest. Finished work is separate. */
export function splitWorks(works: readonly ProgressWork[]): { active: ProgressWork[]; finished: ProgressWork[] } {
  const active: ProgressWork[] = []
  const finished: ProgressWork[] = []
  works.forEach(work => {
    if (workIsFinished(work)) finished.push(work)
    else active.push(work)
  })
  const ranked = active
    .map((work, index) => ({ work, index, bucket: workBucket(work) }))
    .sort((a, b) => a.bucket - b.bucket || a.index - b.index)
    .map(row => row.work)
  return { active: ranked, finished }
}

export function workCardId(index: number): string {
  return `agent-work-${index}`
}

export function workShortName(work: ProgressWork): string {
  const short = work.short?.trim() ?? ''
  if (short !== '') return short
  return work.title?.trim() ?? ''
}

export type RunningRow = {
  workIndex: number
  stepIndex: number
  work: ProgressWork
  step: ProgressStep
}

export function runningRows(works: readonly ProgressWork[]): RunningRow[] {
  const rows: RunningRow[] = []
  works.forEach((work, workIndex) => {
    ;(work.steps ?? []).forEach((step, stepIndex) => {
      if (showsInRunningNow(step)) rows.push({ workIndex, stepIndex, work, step })
    })
  })
  return rows
}

export function flowSteps(
  work: ProgressWork,
  foldDone: boolean,
): { done: ProgressStep[]; flow: ProgressStep[] } {
  const steps = work.steps ?? []
  if (!foldDone) return { done: [], flow: steps }
  return {
    done: steps.filter(step => step.state === 'done'),
    flow: steps.filter(step => step.state !== 'done'),
  }
}

export function earlierStepsLabel(count: number): string {
  return count === 1 ? '1 earlier step done' : `${count} earlier steps done`
}

export type StepGroup = { parallel: boolean; steps: ProgressStep[] }

/** Consecutive steps flagged parallel share one stacked box. */
export function groupParallel(steps: readonly ProgressStep[]): StepGroup[] {
  const groups: StepGroup[] = []
  for (const step of steps) {
    const parallel = step.parallel === true
    const last = groups[groups.length - 1]
    if (last != null && parallel && last.parallel) last.steps.push(step)
    else groups.push({ parallel, steps: [step] })
  }
  return groups
}

export function tookLine(step: ProgressStep): string | null {
  if (step.state !== 'done') return null
  const started = step.started ?? 0
  const ended = step.ended ?? 0
  if (!(started > 0) || !(ended >= started)) return null
  return `took ${formatDuration(ended - started)}`
}

export function eventsNewestFirst(events: readonly ProgressEvent[]): ProgressEvent[] {
  return events
    .map((event, index) => ({ event, index }))
    .sort((a, b) => {
      const at = (b.event.at ?? -1) - (a.event.at ?? -1)
      if (at !== 0) return at
      return b.index - a.index
    })
    .map(row => row.event)
}

function shorten(text: string, max = 140): string {
  const flat = text.replace(/\s+/g, ' ').trim()
  if (flat.length <= max) return flat
  return `${flat.slice(0, max - 1)}…`
}

function eventPlain(event: ProgressEvent): string {
  const text = (event.text ?? '').trim()
  switch (event.kind) {
    case 'edit':
      return text === '' ? 'edited a file' : text
    case 'read':
      return text === '' ? 'read a file' : text
    case 'search':
      return text === '' ? 'searched' : text
    case 'run':
      return text === '' ? 'ran a command' : text
    default:
      return text
  }
}

export function latestEventWords(step: ProgressStep): string {
  const event = eventsNewestFirst(step.events ?? [])[0]
  if (event == null) return (step.note ?? '').trim() || (step.doing ?? '').trim()
  return shorten(eventPlain(event))
}

export function formatTenths(ms: number): string {
  const tenths = Math.round(ms / 100)
  const seconds = tenths / 10
  const text = Number.isInteger(seconds) ? String(seconds) : seconds.toFixed(1)
  return `${text} s`
}

export function runResultLine(event: ProgressEvent): string | null {
  const exit = event.exit
  const ms = event.ms
  if (exit == null && ms == null && event.done !== true) return null
  if (typeof exit === 'number' && exit !== 0) return `exit ${exit}`
  if (typeof ms === 'number' && Number.isFinite(ms) && ms >= 0) return `ok · ${formatTenths(ms)}`
  if (exit === 0 || event.done === true) return 'ok'
  return null
}

export function editDelta(event: ProgressEvent): string | null {
  if (event.kind !== 'edit') return null
  if (event.added == null && event.removed == null) return null
  return `+${event.added ?? 0} −${event.removed ?? 0}`
}

export function lastOutLine(out: string | undefined): string | null {
  if (out == null) return null
  const lines = out.split(/\r?\n/).filter(line => line.trim() !== '')
  const last = lines[lines.length - 1]
  if (last == null || last.trim() === '') return null
  return last
}

export function worksHaveRunningStep(works: readonly ProgressWork[] | undefined): boolean {
  return (works ?? []).some(work =>
    (work.steps ?? []).some(step => step.state === 'running' || step.state === 'slow'),
  )
}

/** The line under the verdict when nothing is running. Null while a step is running. */
export function queuedNextLine(
  works: readonly ProgressWork[] | undefined,
  queue: readonly ProgressQueueItem[] | undefined,
): string | null {
  if (worksHaveRunningStep(works)) return null
  const first = (queue ?? []).find(row => (row.title ?? '').trim() !== '')
  if (first == null) return 'Nothing is queued.'
  const title = first.title?.trim() ?? ''
  const why = first.why_waiting?.trim() ?? ''
  return why === '' ? `Next: ${title}` : `Next: ${title} — ${why}`
}

export function planCounts(roadmap: ProgressRoadmap): { done: number; doing: number; todo: number } {
  const counts = { done: 0, doing: 0, todo: 0 }
  for (const group of roadmap.groups ?? []) {
    const state = group.state
    if (state !== 'done' && state !== 'doing' && state !== 'todo') continue
    counts[state] += group.items?.length ?? 0
  }
  return counts
}

export function planMeterLine(roadmap: ProgressRoadmap): string {
  const counts = planCounts(roadmap)
  return `${counts.done} shipped · ${counts.doing} in progress · ${counts.todo} not started`
}

export function planStateLabel(state: string | undefined): string {
  switch (state) {
    case 'done':
      return 'Done'
    case 'doing':
      return 'In progress'
    case 'todo':
      return 'Not started'
    case 'dropped':
      return 'Dropped'
    case 'later':
      return 'Later'
    default:
      return state?.trim() ?? ''
  }
}

/** `used` is a fraction from 0 to 1, or a percent when it is greater than 1. */
export function usageFraction(used: number | undefined): number | null {
  if (typeof used !== 'number' || !Number.isFinite(used) || used < 0) return null
  if (used <= 1) return used
  return used / 100
}

export function windowIsHot(used: number | undefined): boolean {
  const fraction = usageFraction(used)
  return fraction != null && fraction >= 0.75
}

export function percentText(used: number | undefined): string {
  const fraction = usageFraction(used)
  if (fraction == null) return ''
  return `${Math.round(fraction * 100)}%`
}

/** Reset copy such as `resets in 1d 7h`. Days start at 24 hours. */
export function resetsIn(unixSeconds: number, nowMs: number): string {
  if (!Number.isFinite(unixSeconds)) return 'resets in 0s'
  const sec = Math.max(0, Math.floor(unixSeconds - nowMs / 1000))
  if (sec < 60) return `resets in ${sec}s`
  const min = Math.floor(sec / 60)
  if (min < 60) return `resets in ${min}m`
  const hours = Math.floor(min / 60)
  const days = Math.floor(hours / 24)
  if (days === 0) {
    const remMin = min % 60
    return remMin === 0 ? `resets in ${hours}h` : `resets in ${hours}h ${remMin}m`
  }
  const remH = hours % 24
  return remH === 0 ? `resets in ${days}d` : `resets in ${days}d ${remH}h`
}
