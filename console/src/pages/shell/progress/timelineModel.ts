import { text, type RuntimeProgress } from '@/api/agentRuntime'
import type { RuntimeEventRow } from '@/api/agentRuntimeEvents'
import type { ReleaseRecord } from '@/api/releases'

/**
 * What History is made of (2.5 G6, CUT-round2-2.5 §2; Owner 2026-10-19 「2.5 八条照建议」): the runtime's
 * events across streams, in place of the board's hand-written "done today" lines and its feed — steps
 * done, tasks accepted or rejected, attempts failed, decisions, plan changes decided, imports, stops and
 * lifts, and policy alerts (a report a host had refused, an executor that failed a task three times in a
 * row) — with the platform's release records merged in.
 */
export const TIMELINE_TYPES = ['plan_node_state_changed', 'task_state_changed', 'attempt_computed', 'decision_recorded',
  'plan_change_decided', 'legacy_imported', 'control_set', 'control_cleared', 'policy_alert'] as const
/** Events per read: the types above include every task move, of which only accepted and rejected are shown. */
export const TIMELINE_PAGE = 200

export type TimelineKind = 'step' | 'accepted' | 'rejected' | 'failed' | 'decision' | 'plan' | 'import' | 'stop' | 'lifted' | 'alert' | 'release'
export type TimelineRow = { key: string; at: string; kind: TimelineKind; label: string; text: string; detail: string; mission: string; order: number }

const CONTROL: Record<string, string> = { stop: 'Stop', pause: 'Pause', drain: 'Drain' }
/** The kinds of report agentd uploads that the platform may refuse (store reportKinds). */
const REPORT: Record<string, string> = { report: 'a report', verify: 'a verification report', review: 'a review report' }

/** Task titles the runtime's progress names: an event names a task by its id only. */
export function taskTitles(runtime: RuntimeProgress | undefined): Map<string, string> {
  const titles = new Map<string, string>()
  for (const t of runtime?.recent.tasks ?? []) if (text(t.task_id) && text(t.title)) titles.set(text(t.task_id), text(t.title))
  for (const m of runtime?.work.missions ?? []) for (const t of m.tasks) if (t.task_id && t.title) titles.set(t.task_id, t.title)
  return titles
}

function alertText(e: RuntimeEventRow, task: string): string {
  const source = text(e.body.source), phrase = text(e.body.phrase)
  switch (text(e.body.rule)) {
    case 'repeated_failure': return `An executor failed ${task || 'a task'} three times in a row; it is not offered that task again`
    case 'local_path':
    case 'workspace_ref': return `${e.event_stream_id.replace(/^host:/, 'Host ')} had ${REPORT[source] ?? 'a report'} refused: it named ${phrase}`
    case 'approval_claim': return `A ${source === 'message' ? 'message' : 'task spec'} claims the Owner's approval: "${phrase}"`
    case 'seat_alert': return `The chief of staff raised an alert in its ${source === 'seat_plan' ? 'plan' : 'answer'}`
    default: return [text(e.body.rule), phrase].filter(Boolean).join(': ') || 'Policy alert'
  }
}

/** One event as a timeline row, or null for an event of these types the timeline leaves out (a task moving to running). */
export function eventRow(e: RuntimeEventRow, titles: Map<string, string>): TimelineRow | null {
  const task = e.task_id ? titles.get(e.task_id) || e.task_id : ''
  const row = (kind: TimelineKind, label: string, words: string, detail = ''): TimelineRow =>
    ({ key: `e${e.event_id}`, at: e.at, kind, label, text: words, detail, mission: e.mission_id, order: e.event_id })
  const b = e.body
  switch (e.event_type) {
    case 'plan_node_state_changed':
      return text(b.to) === 'done' ? row('step', 'Step done', e.plan_node_id || 'A plan step', text(b.cause)) : null
    case 'task_state_changed':
      if (text(b.to) === 'accepted') return row('accepted', 'Accepted', task, text(b.reason))
      return text(b.to) === 'rejected' ? row('rejected', 'Rejected', task, text(b.reason)) : null
    case 'attempt_computed': {
      if (text(b.conclusion) !== 'failed') return null
      const how = text(b.end_how), code = typeof b.exit_code === 'number' ? ` ${b.exit_code}` : ''
      return row('failed', 'Attempt failed', task, how === 'exit' ? `exit${code}` : how.replace('_', ' '))
    }
    case 'decision_recorded':
      return row('decision', 'Decision', text(b.decision), [text(b.why), text(b.dropped) && `Not chosen: ${text(b.dropped)}`].filter(Boolean).join(' · '))
    case 'plan_change_decided':
      return row('plan', 'Plan change', `${text(b.decision) || 'decided'}${text(b.by) ? ` by ${text(b.by)}` : ''}`, text(b.error))
    case 'legacy_imported':
      return row('import', 'Import', text(b.source) || 'Records imported', typeof b.count === 'number' ? `${b.count} records` : '')
    case 'control_set':
      return row('stop', CONTROL[text(b.kind)] ?? 'Control', `${text(b.scope)}${text(b.scope_ref) ? ` ${text(b.scope_ref)}` : ''} by ${e.actor_id}`, text(b.reason))
    case 'control_cleared':
      return row('lifted', 'Lifted', `${text(b.control_id)} by ${e.actor_id}`)
    case 'policy_alert':
      return row('alert', 'Alert', alertText(e, task), e.attempt_id ? `attempt ${e.attempt_id}` : '')
    default:
      return null
  }
}

export function releaseRow(r: ReleaseRecord): TimelineRow {
  const repos = Object.entries(r.repos ?? {}).map(([repo, build]) => `${repo}@${text(build?.sha).slice(0, 7)}`).join(', ')
  return { key: `r${r.run}`, at: r.completed_at, kind: 'release', label: 'Release', text: `${r.lane || r.pipeline} ${text(r.env).toUpperCase()}`,
    detail: repos, mission: '', order: 0 }
}

const NO_TITLES = new Map<string, string>()

/** Events kept from reading on: the newest this many shown events. What Show older loaded is kept apart. */
export const TIMELINE_KEEP = 300

/** The earliest time among rows, by a loop: a long list is never spread into arguments. */
export function earliestAt(rows: Iterable<{ at: string }>): number {
  let min = Infinity
  for (const r of rows) {
    const t = Date.parse(r.at)
    if (t < min) min = t
  }
  return min
}

/**
 * Events are kept once by id: a read on from a latest read's cursor may list some of them again. Only
 * events the timeline shows are kept; a task moving to running is read and let go.
 */
export function mergeEvents(into: Map<number, RuntimeEventRow>, page: readonly RuntimeEventRow[]): void {
  for (const e of page) if (eventRow(e, NO_TITLES) !== null) into.set(e.event_id, e)
}

/**
 * Keeps the `limit` events of the largest ids and lets the rest go: they are behind Show older again.
 * Returns the smallest id kept, or null when nothing went. Every id let go is below it.
 */
export function trimEvents(into: Map<number, RuntimeEventRow>, limit: number): number | null {
  if (into.size <= limit) return null
  const ids = [...into.keys()].sort((a, b) => b - a)
  for (const id of ids.slice(limit)) into.delete(id)
  return ids[limit - 1]
}

/**
 * How far back release records show while older events are unread: the earliest time the raw latest and
 * back pages reached, or of an event shown, whichever is earlier (review 413 B1). Event ids and times do
 * not run together, so what was let go never moves it later; a timeline that never read a raw page back
 * (its first latest page was empty) takes it from the events shown. -Infinity once every event is read.
 */
export function releasesFrom(rawFrom: number, shown: Iterable<{ at: string }>, complete: boolean): number {
  return complete ? -Infinity : Math.min(rawFrom, earliestAt(shown))
}

/**
 * Newest first. `releasesFrom` is how far back the events were read, from the raw pages (not from the
 * events kept to show): release records older than it wait with the events behind Show older, so one
 * order holds as the reader goes back; -Infinity once every event has been read.
 */
export function timelineRows(events: readonly RuntimeEventRow[], releases: readonly ReleaseRecord[], o: { titles: Map<string, string>; releasesFrom: number }): TimelineRow[] {
  const rows = [
    ...events.map(e => eventRow(e, o.titles)).filter((r): r is TimelineRow => r !== null),
    ...releases.filter(r => Date.parse(r.completed_at) >= o.releasesFrom).map(releaseRow),
  ]
  return rows.sort((a, b) => Date.parse(b.at) - Date.parse(a.at) || b.order - a.order)
}
