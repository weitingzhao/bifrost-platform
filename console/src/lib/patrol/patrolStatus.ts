import type { PatrolRun, PatrolRunResult, PatrolSkill } from '@/api/patrol'
import type { Reachability } from '@bifrost/ui'

export function latestPatrolRun(runs: readonly PatrolRun[]): PatrolRun | undefined {
  if (runs.length === 0) return undefined
  return [...runs].sort((a, b) => Date.parse(b.started_at) - Date.parse(a.started_at))[0]
}

export function nextPatrolRunAt(skills: readonly PatrolSkill[]): string | undefined {
  const times = skills
    .map(s => s.next_run_at)
    .filter((iso): iso is string => iso != null && iso !== '')
    .sort((a, b) => Date.parse(a) - Date.parse(b))
  return times[0]
}

export function patrolSkillsOkCount(skills: readonly PatrolSkill[]): { ok: number; total: number } {
  const total = skills.length
  const ok = skills.filter(s => s.last_result === 'success' || s.last_result === 'skipped').length
  return { ok, total }
}

export function patrolHasFailure(runs: readonly PatrolRun[]): boolean {
  return runs.some(run => run.result === 'failure')
}

/** IconRail attention: failure → error; skipped/escalated only → warn. */
export function patrolRailSignal(runs: readonly PatrolRun[]): 'error' | 'warn' | null {
  if (runs.some(run => run.result === 'failure')) return 'error'
  if (runs.some(run => run.result === 'skipped' || run.result === 'escalated' || run.result === 'running')) {
    return 'warn'
  }
  return null
}

export function patrolRunLamp(result: PatrolRunResult | undefined): Reachability {
  if (result === 'success') return 'ok'
  if (result === 'skipped' || result === 'running') return 'degraded'
  if (result === 'failure' || result === 'escalated') return 'fail'
  return 'unknown'
}

export type PatrolPosture = {
  lamp: Reachability
  label: 'All OK' | 'WARN' | 'FAIL' | 'Idle'
  summary: string
}

export function patrolPosture(
  skills: readonly PatrolSkill[],
  runs: readonly PatrolRun[],
  now = Date.now(),
): PatrolPosture {
  const latest = latestPatrolRun(runs)
  const { ok, total } = patrolSkillsOkCount(skills)
  const ago = latest?.finished_at ?? latest?.started_at
  const when = ago != null ? formatPatrolRelativeTime(ago, now) : 'no runs'

  if (patrolHasFailure(runs) || skills.some(s => s.last_result === 'failure')) {
    return { lamp: 'fail', label: 'FAIL', summary: `${when} · ${ok}/${total} skills OK` }
  }
  if (
    runs.some(r => r.result === 'escalated' || r.result === 'skipped' || r.result === 'running') ||
    skills.some(
      s => s.last_result === 'escalated' || s.last_result === 'skipped' || s.last_result === 'running',
    )
  ) {
    return { lamp: 'degraded', label: 'WARN', summary: `${when} · ${ok}/${total} skills OK` }
  }
  if (latest == null) {
    return { lamp: 'unknown', label: 'Idle', summary: 'No patrol runs yet' }
  }
  return { lamp: 'ok', label: 'All OK', summary: `${when} · ${ok}/${total} skills OK` }
}

/** Compact relative clock: `5h ago` / `in 3h`. */
export function formatPatrolRelativeTime(iso: string, now = Date.now()): string {
  const delta = now - Date.parse(iso)
  if (Number.isNaN(delta)) return '—'
  const abs = Math.abs(delta)
  const mins = Math.round(abs / 60_000)
  if (mins < 1) return delta >= 0 ? 'just now' : 'soon'
  if (mins < 60) return delta >= 0 ? `${mins}m ago` : `in ${mins}m`
  const hours = Math.round(mins / 60)
  if (hours < 48) return delta >= 0 ? `${hours}h ago` : `in ${hours}h`
  const days = Math.round(hours / 24)
  return delta >= 0 ? `${days}d ago` : `in ${days}d`
}
