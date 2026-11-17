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
  status: SeatStatus
  /** Minutes since the seat wrote its status. */
  statusAgeMinutes: number
  /** The session's last heartbeat: in a turn, idle, silent…; undefined when no beat is known. */
  thread?: Pick<AgentThread, 'status' | 'quiet_seconds' | 'at'>
  /** Its cards waiting for the Owner (drills apart). */
  ownerCards: number
  /** Its commits of the lineage window by the furthest stage they reached. */
  commits: Record<CommitStage, number>
}

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
 * The rows: one per seat that wrote a status, newest status first; a line
 * that waits on something or has cards for the Owner comes before one that
 * does not.
 */
export function linesOf(
  statuses: readonly SeatStatus[],
  threads: readonly AgentThread[],
  approvals: readonly ApprovalItem[],
  lineage: readonly LineageThread[],
  now: Date,
): LineRow[] {
  const rows = statuses.map((status): LineRow => {
    const session = sessionOf(status.seat)
    const beat = threads
      .filter(t => t.thread === session || t.session === session)
      .sort((a, b) => b.at.localeCompare(a.at))[0]
    const line = lineage.find(l => l.session === session)
    const commits: Record<CommitStage, number> = { written: 0, main: 0, stg: 0, prod: 0 }
    for (const repo of line?.repos ?? []) for (const c of repo.commits) commits[stageOf(c)]++
    return {
      session,
      title: line?.title || beat?.title || session,
      status,
      statusAgeMinutes: Math.max(0, Math.round((now.getTime() - Date.parse(status.at)) / 60_000)),
      thread: beat ? { status: beat.status, quiet_seconds: beat.quiet_seconds, at: beat.at } : undefined,
      ownerCards: approvals.filter(a => !a.drill && (a.requester === session || a.requester === status.seat)).length,
      commits,
    }
  })
  const waiting = (r: LineRow) => (r.ownerCards > 0 || r.status.waiting_on ? 1 : 0)
  return rows.sort((a, b) => waiting(b) - waiting(a) || b.status.at.localeCompare(a.status.at))
}
