import type { PatrolRun, PatrolRunResult, PatrolSkill } from '@/api/patrol'
import type { Reachability } from '@bifrost/ui'

export function latestPatrolRun(runs: readonly PatrolRun[]): PatrolRun | undefined {
  if (runs.length === 0) return undefined
  return [...runs].sort((a, b) => Date.parse(b.started_at) - Date.parse(a.started_at))[0]
}

/** Newest run per skill_id (by started_at). */
export function latestPatrolRunBySkill(runs: readonly PatrolRun[]): Map<string, PatrolRun> {
  const sorted = [...runs].sort((a, b) => Date.parse(b.started_at) - Date.parse(a.started_at))
  const map = new Map<string, PatrolRun>()
  for (const run of sorted) {
    if (!map.has(run.skill_id)) map.set(run.skill_id, run)
  }
  return map
}

export function nextPatrolRunAt(skills: readonly PatrolSkill[]): string | undefined {
  const times = skills
    .filter(s => s.enabled)
    .map(s => s.next_run_at)
    .filter((iso): iso is string => iso != null && iso !== '')
    .sort((a, b) => Date.parse(a) - Date.parse(b))
  return times[0]
}

/**
 * Skills OK fraction — enabled skills only.
 * Never-run (no last_result) counts as OK: weekly/idle schedules are not a fault.
 */
export function patrolSkillsOkCount(skills: readonly PatrolSkill[]): { ok: number; total: number } {
  const active = skills.filter(s => s.enabled)
  const total = active.length
  const ok = active.filter(s => {
    if (s.last_result == null) return true
    return s.last_result === 'success' || s.last_result === 'skipped'
  }).length
  return { ok, total }
}

/** True when the newest run overall (or any enabled skill last_result) is failure. */
export function patrolHasFailure(
  runs: readonly PatrolRun[],
  skills: readonly PatrolSkill[] = [],
): boolean {
  if (skills.some(s => s.enabled && s.last_result === 'failure')) return true
  return latestPatrolRun(runs)?.result === 'failure'
}

/** IconRail attention: current skill heads only — not historical skips in the run window. */
export function patrolRailSignal(runs: readonly PatrolRun[]): 'error' | 'warn' | null {
  const heads = [...latestPatrolRunBySkill(runs).values()]
  if (heads.some(run => run.result === 'failure')) return 'error'
  if (heads.some(run => run.result === 'skipped' || run.result === 'escalated' || run.result === 'running')) {
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

/** Board row lamp: disabled / never-run are not faults (align with Skills OK). */
export function patrolSkillLamp(skill: PatrolSkill): Reachability {
  if (!skill.enabled) return 'ok'
  if (skill.last_result == null) return 'ok'
  return patrolRunLamp(skill.last_result)
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
  const active = skills.filter(s => s.enabled)

  if (patrolHasFailure(runs, skills)) {
    return { lamp: 'fail', label: 'FAIL', summary: `${when} · ${ok}/${total} skills OK` }
  }

  const soft = (r: PatrolRunResult | undefined) =>
    r === 'escalated' || r === 'skipped' || r === 'running'

  // Current skill heads + newest run only — do not WARN on historical "already running" skips.
  if (active.some(s => soft(s.last_result)) || soft(latest?.result)) {
    return { lamp: 'degraded', label: 'WARN', summary: `${when} · ${ok}/${total} skills OK` }
  }
  if (latest == null && active.every(s => s.last_result == null)) {
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
