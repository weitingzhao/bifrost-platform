import type { LineageThread } from '@/api/lineage'
import { LINE_ACTIVE_HOURS, stageOf } from '@/api/seatStatus'

/**
 * What one line did in the lineage window, counted the way every view counts it
 * (Console Commit lineage, Status → Lines, the City): from GET /api/v1/lineage only.
 * Counts are cumulative by furthest stage: a commit in PROD also counts as reached STG
 * and as merged, the same `stageOf` the Lines page uses.
 */
export type LineActivity = {
  session: string
  title: string
  merged: number
  stg: number
  prod: number
  /** The line's latest commit in the window (ISO). */
  lastAt: string
  /** Whole hours since that commit; null when the time does not parse. */
  quietHours: number | null
  /** Committed within LINE_ACTIVE_HOURS: the same cut Status → Lines uses for "active". */
  movingToday: boolean
}

export function lineActivity(t: LineageThread, now: Date): LineActivity {
  let merged = 0
  let stg = 0
  let prod = 0
  for (const repo of t.repos) {
    for (const c of repo.commits) {
      const stage = stageOf(c)
      if (stage === 'prod') prod++
      if (stage === 'prod' || stage === 'stg') stg++
      if (stage !== 'written') merged++
    }
  }
  const at = Date.parse(t.last_at)
  const quietHours = Number.isFinite(at) ? Math.max(0, Math.floor((now.getTime() - at) / 3_600_000)) : null
  return {
    session: t.session,
    title: t.title || t.session,
    merged,
    stg,
    prod,
    lastAt: t.last_at,
    quietHours,
    movingToday: quietHours != null && quietHours < LINE_ACTIVE_HOURS,
  }
}

/** Named lines only (the unnamed bucket is not a line), most recently moved first. */
export function linesActivity(threads: readonly LineageThread[], now: Date): LineActivity[] {
  return threads
    .filter(t => t.session !== '')
    .map(t => lineActivity(t, now))
    .sort((a, b) => b.lastAt.localeCompare(a.lastAt))
}

/** "2 h ago", "3 d ago": how long a line has been quiet, readable without a timestamp. */
export function quietLabel(hours: number | null): string {
  if (hours == null) return 'unknown'
  if (hours < 1) return 'within the hour'
  if (hours < 48) return `${hours} h ago`
  return `${Math.floor(hours / 24)} d ago`
}
