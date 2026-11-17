import { useQuery } from '@tanstack/react-query'
import { fetchAgentThreads, formatSeconds } from '@/api/agentThreads'
import { fetchApprovalList } from '@/api/approvals'
import { fetchLineage } from '@/api/lineage'
import { fetchSeatStatuses, linesOf, SEAT_STATUS_STALE_MINUTES, type LineRow } from '@/api/seatStatus'
import { OpsSection } from '@/components/layout/OpsSection'

const REFRESH_MS = 30_000
/** The lineage window the commit stages are counted over. */
const LINEAGE_DAYS = 2

/** The dot: running, waiting on someone, idle or not heard from. */
function dot(r: LineRow): { cls: string; label: string } {
  if (r.ownerCards > 0 || r.status.waiting_on) return { cls: 'bg-amber-500', label: 'waiting' }
  if (r.thread?.status === 'in_turn') return { cls: 'bg-emerald-500', label: 'running' }
  if (r.thread == null) return { cls: 'bg-muted-foreground', label: 'no heartbeat' }
  return { cls: 'bg-sky-500', label: r.thread.status }
}

function age(minutes: number): string {
  return minutes < 60 ? `${minutes} min ago` : `${Math.floor(minutes / 60)} h ${minutes % 60} min ago`
}

function LineItem({ r }: { r: LineRow }) {
  const d = dot(r)
  const stale = r.statusAgeMinutes > SEAT_STATUS_STALE_MINUTES
  const c = r.commits
  return (
    <li className="flex w-full min-w-0 list-none flex-col gap-1 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-3 py-2.5" data-line={r.session}>
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <span className={`inline-block h-2 w-2 shrink-0 rounded-full ${d.cls}`} aria-label={d.label} title={d.label} />
        <span className="min-w-0 truncate text-sm font-medium">{r.title}</span>
        <span className="text-xs text-muted-foreground">{r.status.percent}% · {r.status.percent_basis}</span>
        {r.ownerCards > 0 ? <span className="text-xs font-medium text-amber-600">{r.ownerCards} for you</span> : null}
        <span className={`ml-auto text-xs ${stale ? 'font-medium text-red-600' : 'text-muted-foreground'}`}>
          status {age(r.statusAgeMinutes)}{stale ? ' · stale' : ''}
        </span>
      </div>
      {r.status.problem ? (
        <p className="m-0 text-sm font-medium text-red-600">Format wrong ({r.status.seat}): {r.status.problem}{r.status.summary ? ` · it said: ${r.status.summary}` : ''}</p>
      ) : null}
      <p className="m-0 text-sm"><span className="text-muted-foreground">Doing </span>{r.status.doing}</p>
      {r.status.waiting_on ? <p className="m-0 text-sm"><span className="text-muted-foreground">Waiting on </span>{r.status.waiting_on}</p> : null}
      <p className="m-0 text-sm"><span className="text-muted-foreground">Next </span>{r.status.next}</p>
      <details className="text-xs text-muted-foreground">
        <summary className="cursor-pointer">Goal, commits, session</summary>
        <p className="m-0 mt-1">Goal: {r.status.goal}</p>
        <p className="m-0">
          Commits (last {LINEAGE_DAYS} days, furthest stage): written {c.written} · main {c.main} · STG {c.stg} · PROD {c.prod}
        </p>
        <p className="m-0">
          Session: {r.thread ? `${r.thread.status}, last beat ${formatSeconds(r.thread.quiet_seconds)} ago` : 'no heartbeat known'}
        </p>
      </details>
    </li>
  )
}

/**
 * Work → Activity: where every line stands. The seat sends doing, waiting
 * on, next, goal and percent as a status message (report_status); the platform adds what it
 * counts: cards waiting for the Owner, how far the line's commits got, and
 * the session's heartbeat (2026-11-17, the Owner: he asks for progress
 * because Ops does not show it).
 */
export function LinesNow() {
  const statuses = useQuery({ queryKey: ['seat-statuses'], queryFn: fetchSeatStatuses, refetchInterval: REFRESH_MS, retry: false })
  const threads = useQuery({ queryKey: ['agent-threads'], queryFn: fetchAgentThreads, refetchInterval: REFRESH_MS, retry: false })
  const approvals = useQuery({ queryKey: ['approvals', 'pending'], queryFn: () => fetchApprovalList('pending'), refetchInterval: REFRESH_MS, retry: false })
  const lineage = useQuery({ queryKey: ['lineage-lines', LINEAGE_DAYS], queryFn: () => fetchLineage(LINEAGE_DAYS), staleTime: 60_000, retry: false })

  const rows = linesOf(statuses.data ?? [], threads.data?.threads ?? [], approvals.data ?? [], lineage.data?.threads ?? [], new Date())
  const forOwner = rows.reduce((n, r) => n + r.ownerCards, 0)
  const unread = [
    statuses.isError ? 'statuses' : '', threads.isError ? 'heartbeats' : '', approvals.isError ? 'cards' : '', lineage.isError ? 'commits' : '',
  ].filter(Boolean)
  const readAt = statuses.dataUpdatedAt ? new Date(statuses.dataUpdatedAt).toLocaleTimeString() : '…'

  let body
  if (statuses.isError) {
    body = <p className="m-0 text-sm text-muted-foreground">{`Could not read line statuses: ${statuses.error instanceof Error ? statuses.error.message : 'request failed'}`}</p>
  } else if (statuses.data == null) {
    body = <p className="m-0 text-sm text-muted-foreground">Loading…</p>
  } else if (rows.length === 0) {
    body = <p className="m-0 text-sm text-muted-foreground">No line has reported its status yet (report_status).</p>
  } else {
    body = <ul className="m-0 flex w-full min-w-0 flex-col gap-2 p-0" aria-label="Lines">{rows.map(r => <LineItem key={r.session} r={r} />)}</ul>
  }
  return (
    <section aria-label="Lines" className="flex w-full min-w-0 flex-col">
      <OpsSection title={`Lines · ${rows.length} · ${forOwner} for you`} bodyPadding="compact">
        <div className="flex w-full min-w-0 flex-col gap-2">
          <p className="m-0 text-xs text-muted-foreground">
            Read {readAt}. A status older than {SEAT_STATUS_STALE_MINUTES / 60} h is marked stale. Percent is the line's own count; its basis is beside it.
            {unread.length > 0 ? ` Not read: ${unread.join(', ')}.` : ''}
          </p>
          {body}
        </div>
      </OpsSection>
    </section>
  )
}
