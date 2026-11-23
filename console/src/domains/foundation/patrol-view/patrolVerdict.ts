import type { StatusWord } from '@/lib/shell/stateWords'
import { formatDurationParts, iterCronFiresUtc } from './cronSchedule'
import type { PatrolRunResult, PatrolSkill } from './readings'

/**
 * Records → Patrol's head line, from the skills read (箭10 2026-11-23, the inventory's "false OK": the page drew an
 * ok lamp that nothing had checked). The word is the worst over the enabled skills, in the engine's seven words, of
 * each skill's last result and how late its next run is; a skill that never ran or was skipped cannot be judged, so
 * it is unknown, never ok.
 */
const RANK: StatusWord[] = ['fail', 'warn', 'unknown', 'pending', 'ok']
const worse = (a: StatusWord, b: StatusWord) => (RANK.indexOf(a) <= RANK.indexOf(b) ? a : b)

/** A run's result in the seven words: the one rule the lamp and the Last result tags share. */
export function wordOfResult(r: PatrolRunResult | undefined): StatusWord {
  if (r === 'success') return 'ok'
  if (r === 'failure') return 'fail'
  if (r === 'escalated') return 'warn'
  if (r === 'running') return 'pending'
  return 'unknown'
}

const DAY = 86_400_000
const intervals = new Map<string, number | null>()
/** The gap between two fires of a skill's cron (its declared freshness); null when the cron cannot be read. */
export function scheduleIntervalMs(expr: string): number | null {
  if (!intervals.has(expr)) {
    const from = new Date(Date.UTC(2026, 0, 5)) // a Monday: weekly crons fire twice in the window
    const fires = iterCronFiresUtc(expr, from, new Date(from.getTime() + 15 * DAY)).slice(0, 2)
    intervals.set(expr, fires.length === 2 ? fires[1].getTime() - fires[0].getTime() : null)
  }
  return intervals.get(expr) ?? null
}

/**
 * How late a skill is (箭10 2026-11-23): before its next run, nothing to say; past it by less than one schedule
 * interval, warn; by more, fail (older than its declared freshness); no schedule declared, unknown.
 */
export function overdueOf(skill: PatrolSkill, now: number): { word: StatusWord; note: string } | null {
  if (skill.last_result === 'running') return null
  if (!skill.schedule?.trim()) return { word: 'unknown', note: `${skill.name}: no schedule declared` }
  const reported = skill.next_run_at ? Date.parse(skill.next_run_at) : NaN
  if (!Number.isFinite(reported) || now <= reported) return null
  const every = scheduleIntervalMs(skill.schedule)
  if (every == null) return { word: 'unknown', note: `${skill.name}: schedule not understood (${skill.schedule})` }
  // A reported next run older than the last run is not updated (seen on PROD 2026-11-23: last 14:15, next 14:00);
  // the run is then due one interval after the last one.
  const next = dueAt(skill, every, reported)
  if (now <= next) return null
  const late = now - next
  return {
    word: late < every ? 'warn' : 'fail',
    note: `${skill.name} overdue ${formatDurationParts(late)} (schedule every ${formatDurationParts(every)})`,
  }
}

function dueAt(skill: PatrolSkill, every: number, reported: number): number {
  const last = skill.last_run_at ? Date.parse(skill.last_run_at) : NaN
  return Number.isFinite(last) && last >= reported ? last + every : reported
}

/**
 * When the next run is really due: the reported time, or one interval after the last run when the reported time is
 * older than the last run (not updated). `stale` says the reported time was replaced, for the Next run column.
 */
export function effectiveNextRun(skill: PatrolSkill): { at: string; stale: boolean } | null {
  const reported = skill.next_run_at ? Date.parse(skill.next_run_at) : NaN
  if (!Number.isFinite(reported)) return null
  const every = skill.schedule?.trim() ? scheduleIntervalMs(skill.schedule) : null
  if (every == null) return { at: skill.next_run_at!, stale: false }
  const due = dueAt(skill, every, reported)
  return due === reported ? { at: skill.next_run_at!, stale: false } : { at: new Date(due).toISOString(), stale: true }
}

export type PatrolVerdict = { word: StatusWord; sentence: string }

export function patrolVerdict(
  skills: readonly PatrolSkill[] | undefined,
  loading: boolean,
  error: string | null,
  now: number = Date.now(),
): PatrolVerdict {
  if (error != null) return { word: 'unknown', sentence: `Skills could not be read: ${error}` }
  if (loading || skills == null) return { word: 'pending', sentence: 'Reading the skills…' }
  const enabled = skills.filter(s => s.enabled)
  if (enabled.length === 0) return { word: 'boundary', sentence: `No skill is enabled (${skills.length} loaded)` }
  const counts = new Map<string, number>()
  const late: string[] = []
  let word: StatusWord = 'ok'
  for (const s of enabled) {
    word = worse(word, wordOfResult(s.last_result))
    const over = overdueOf(s, now)
    if (over != null) {
      word = worse(word, over.word)
      late.push(over.note)
    }
    const key = s.last_result ?? 'never run'
    counts.set(key, (counts.get(key) ?? 0) + 1)
  }
  const parts = [...counts.entries()].map(([k, n]) => `${n} ${k}`)
  return { word, sentence: [`${enabled.length} enabled, last runs: ${parts.join(' · ')}`, ...late].join(' · ') }
}
