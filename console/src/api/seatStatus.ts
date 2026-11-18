import { viewerRead } from '@/api/approvals'
import type { ApprovalItem } from '@/api/approvals'
import type { AgentThread } from '@/api/agentThreads'
import type { LineageThread } from '@/api/lineage'

/**
 * GET /api/v1/agentrt/seats — each line's own status: the latest "status"
 * message the seat sent to the status board (report_status). The Owner reads
 * where every line stands here instead of asking (2026-11-17). A status that
 * does not parse has a problem and empty fields.
 */
export type SeatStatus = {
  seat: string
  message_id: string
  at: string
  summary?: string
  problem?: string
  goal: string
  doing: string
  percent: number
  percent_basis: string
  waiting_on?: string
  next: string
  /** The charter this session says it works under: its own statement, not verified. */
  charter?: string
}

export async function fetchSeatStatuses(): Promise<SeatStatus[]> {
  const r = await viewerRead('Seat status', '/api/v1/agentrt/seats')
  const body = (await r.json()) as { seats?: SeatStatus[] }
  return body.seats ?? []
}

/** How far a commit got: written on a branch, on main, then each environment it reached. */
export type CommitStage = 'written' | 'main' | 'stg' | 'prod'

/** One line as Ops shows it: what the seat says, and what the platform counts beside it. */
export type LineRow = {
  session: string
  title: string
  /** The seat's latest status; undefined for a line that is active but has not reported one yet. */
  status?: SeatStatus
  /** Minutes since the seat wrote its status (undefined without one). */
  statusAgeMinutes?: number
  /** The session's last heartbeat: in a turn, idle, silent…; undefined when no beat is known. */
  thread?: Pick<AgentThread, 'status' | 'quiet_seconds' | 'at'>
  /** Its cards waiting for the Owner (drills apart). */
  ownerCards: number
  /** Its commits of the lineage window by the furthest stage they reached. */
  commits: Record<CommitStage, number>
  /** The line's last sign of life: heartbeat, commit or status (ISO time; "" when none). */
  lastActivity: string
}

/** A line with no status counts as active when it beat or committed within this. */
export const LINE_ACTIVE_HOURS = 24

/** A status older than this is shown as stale. */
export const SEAT_STATUS_STALE_MINUTES = 120

export function sessionOf(seat: string): string {
  return seat.startsWith('thread:') ? seat.slice('thread:'.length) : seat
}

export function stageOf(c: { landed: boolean; reached?: { env: string }[] }): CommitStage {
  const envs = new Set((c.reached ?? []).map(r => r.env.toLowerCase()))
  if (envs.has('prod')) return 'prod'
  if (envs.has('stg')) return 'stg'
  return c.landed ? 'main' : 'written'
}

/**
 * The rows: one per seat that wrote a status, and one per line active in the
 * last day (a heartbeat or a commit) that has not reported yet, so a page
 * with few statuses does not read as few lines working. Lines that report
 * come first, those that wait on something or have cards for the Owner
 * first among them, then newest status; then the ones not reporting, most
 * recently active first.
 */
export function linesOf(
  statuses: readonly SeatStatus[],
  threads: readonly AgentThread[],
  approvals: readonly ApprovalItem[],
  lineage: readonly LineageThread[],
  now: Date,
): LineRow[] {
  const row = (session: string, status?: SeatStatus): LineRow => {
    const beat = threads
      .filter(t => t.thread === session || t.session === session)
      .sort((a, b) => b.at.localeCompare(a.at))[0]
    const line = lineage.find(l => l.session === session)
    const commits: Record<CommitStage, number> = { written: 0, main: 0, stg: 0, prod: 0 }
    for (const repo of line?.repos ?? []) for (const c of repo.commits) commits[stageOf(c)]++
    const last = [status?.at ?? '', beat?.at ?? '', line?.last_at ?? ''].sort().at(-1) ?? ''
    return {
      session,
      title: line?.title || beat?.title || session,
      status,
      statusAgeMinutes: status ? Math.max(0, Math.round((now.getTime() - Date.parse(status.at)) / 60_000)) : undefined,
      thread: beat ? { status: beat.status, quiet_seconds: beat.quiet_seconds, at: beat.at } : undefined,
      ownerCards: approvals.filter(a => !a.drill && (a.requester === session || a.requester === `thread:${session}`)).length,
      commits,
      lastActivity: last,
    }
  }
  const reported = statuses.map(st => row(sessionOf(st.seat), st))
  const seen = new Set(reported.map(r => r.session))
  const since = now.getTime() - LINE_ACTIVE_HOURS * 3_600_000
  const candidates = new Set<string>()
  for (const t of threads) {
    const s = t.session || t.thread
    if (s && Date.parse(t.at) >= since) candidates.add(s)
  }
  for (const l of lineage) if (l.session && Date.parse(l.last_at) >= since) candidates.add(l.session)
  const silent = [...candidates].filter(s => !seen.has(s)).map(s => row(s))
  const waiting = (r: LineRow) => (r.ownerCards > 0 || r.status?.waiting_on ? 1 : 0)
  reported.sort((a, b) => waiting(b) - waiting(a) || (b.status?.at ?? '').localeCompare(a.status?.at ?? ''))
  silent.sort((a, b) => b.lastActivity.localeCompare(a.lastActivity))
  return [...reported, ...silent]
}
