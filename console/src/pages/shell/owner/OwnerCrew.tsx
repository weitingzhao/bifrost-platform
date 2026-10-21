import { goalHref } from '@/lib/shell/consoleRoutes'
import { useQueries } from '@tanstack/react-query'
import { fetchRuntimeTaskDetail, type RuntimeAttempt, type RuntimeHost, type RuntimeMission, type RuntimeTaskDetail } from '@/api/agentRuntime'
import { AttemptActionLine } from './AttemptActionLine'
import { crewRows, duration, firstLine, unpersistedWords } from './crewModel'
import { Card, Dot } from './OwnerParts'
import { ago, type Tone } from './ownerModel'

const ROLE: Record<string, string> = { implement: 'Implement', verify: 'Verify', review: 'Review', plan: 'Plan', brief: 'Chief of staff' }
/** A heartbeat older than this, on an attempt still live, is shown as late. */
const LATE_HEARTBEAT_S = 120
const TASK_STALE_MS = 10 * 60_000

function hostTone(h: RuntimeHost | undefined): Tone {
  if (h?.online === true) return 'ok'
  return h?.online === false ? 'fail' : 'unknown'
}

/** How the runtime was read: the card says nothing about hosts it has not read (review 289 B1). */
export type CrewRead = 'ok' | 'loading' | 'error'

/**
 * Crew (2.5, thin; the Owner 2026-10-18): which agent works on which host, on what, now. Each live
 * attempt is named by its task as the task itself answers (a mission past the progress page's first
 * hundred, a review's or verification's subject: review 289 B2, B3), with its goal. Only this
 * platform's runtime is shown: threads on the Mac (the coordinator and the reviews it runs) work
 * outside it.
 */
export function OwnerCrew({ hosts, live, missions, now, read }: {
  hosts: RuntimeHost[]; live: RuntimeAttempt[]; missions: RuntimeMission[]; now: number; read: CrewRead
}) {
  const goals = new Map(missions.map(m => [m.mission_id, firstLine(m.title || m.goal)]))
  const listed = new Map<string, { title: string; role: string }>()
  for (const m of missions) for (const t of m.tasks) listed.set(t.task_id, { title: t.title, role: t.role })
  const ids = [...new Set(live.map(a => a.task_id).filter(Boolean))]
  const details = useQueries({ queries: ids.map(id => ({ queryKey: ['agent-runtime-task', id], queryFn: ({ signal }: { signal: AbortSignal }) =>
    fetchRuntimeTaskDetail(id, signal), staleTime: TASK_STALE_MS, retry: false })) })
  const detail = new Map<string, RuntimeTaskDetail>()
  details.forEach((q, i) => { if (q.data) detail.set(ids[i], q.data) })
  const subjects = [...new Set([...detail.values()].map(d => d.subject_task_id).filter(id => id && !listed.has(id)))]
  const subjectQs = useQueries({ queries: subjects.map(id => ({ queryKey: ['agent-runtime-task', id], queryFn: ({ signal }: { signal: AbortSignal }) =>
    fetchRuntimeTaskDetail(id, signal), staleTime: TASK_STALE_MS, retry: false })) })
  const subjectTitle = (id: string) => listed.get(id)?.title || subjectQs[subjects.indexOf(id)]?.data?.title || id
  const name = (a: RuntimeAttempt) => {
    const d = detail.get(a.task_id), l = listed.get(a.task_id)
    const role = d?.role || l?.role || ''
    const title = d?.subject_task_id ? subjectTitle(d.subject_task_id) : d?.title || l?.title || a.task_id
    return { role: ROLE[role] ?? (role || 'Task'), title, failed: details[ids.indexOf(a.task_id)]?.isError === true && !l }
  }

  // A lost attempt whose result is not persisted stays under its host, never as done (TD-352).
  const lost = missions.flatMap(m => m.tasks.flatMap(t => t.lost_attempt ? [t.lost_attempt] : []))
  const rows = crewRows(hosts, live, lost)
  const known = rows.filter(r => r.host)
  const online = known.filter(r => r.host?.online === true).length
  const title = read === 'ok' && known.length ? `Crew · ${online} of ${known.length} hosts online` : 'Crew'
  return <Card label="Crew" title={title} more={{ href: goalHref(), text: 'Goals' }}>
    {read === 'loading' && <p className="m-0 text-sm text-muted-foreground">Reading the agent runtime…</p>}
    {read === 'error' && <p role="alert" className="m-0 text-sm">The agent runtime could not be read: hosts and what they run are not known.</p>}
    {read === 'ok' && rows.length === 0 && <p className="m-0 text-sm text-muted-foreground">No agent host has polled this platform.</p>}
    {read === 'ok' && <ul className="m-0 flex list-none flex-col gap-3 p-0">{rows.map(row => {
      const h = row.host
      return <li key={row.id} className="flex min-w-0 flex-col gap-2 rounded-[var(--control-radius)] bg-[var(--control-fill)] p-2.5">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <span className="flex min-w-0 items-center gap-2"><Dot tone={hostTone(h)} /><strong className="truncate font-mono text-sm" title={row.id}>{row.id}</strong>
            <span className="text-xs text-muted-foreground">{!h ? 'not a registered host' : h.online ? 'online' : `last poll ${ago(h.last_poll_at, now)}`}</span></span>
          <span className="text-xs font-semibold">{row.attempts.length ? `${row.attempts.length} running` : 'Idle'}{row.lost.length > 0 && ` · ${row.lost.length} lost`}</span>
        </div>
        {row.attempts.map(a => {
          const n = name(a)
          const late = a.heartbeat_age_s != null && a.heartbeat_age_s > LATE_HEARTBEAT_S
          const goal = goals.get(a.mission_id)
          const unpersisted = unpersistedWords(a, now)
          return <div key={a.attempt_id} className="flex min-w-0 flex-col gap-1 border-t border-[var(--table-rule)] pt-2">
            <span className="min-w-0 [overflow-wrap:anywhere] text-sm"><strong>{n.role}</strong> · {n.title}</span>
            <span className="min-w-0 [overflow-wrap:anywhere] text-xs text-muted-foreground">
              {a.executor.replace(/^exec:/, '')}{a.model ? ` (${a.model})` : ''} · {a.mission_id}{goal ? ` ${goal}` : ''} · {a.state} {duration(a.elapsed_s)}
              {late && <span className="font-semibold text-[var(--color-warning)]"> · no heartbeat for {duration(a.heartbeat_age_s)}</span>}
              {n.failed && <span> · its task could not be read</span>}</span>
            {unpersisted && <span className="min-w-0 [overflow-wrap:anywhere] text-xs font-semibold text-[var(--color-warning)]">{unpersisted}</span>}
            <AttemptActionLine attempt={a} now={now} who={false} />
          </div>
        })}
        {row.lost.map(a => {
          const n = name(a)
          return <div key={a.attempt_id} className="flex min-w-0 flex-col gap-1 border-t border-[var(--table-rule)] pt-2">
            <span className="min-w-0 [overflow-wrap:anywhere] text-sm"><strong>Lost</strong> · {n.role} · {n.title}</span>
            <span className="min-w-0 [overflow-wrap:anywhere] text-xs text-muted-foreground">
              Attempt {a.attempt_no ?? '?'} · {a.executor.replace(/^exec:/, '')} · {a.mission_id} · lost {ago(a.ended_at, now)} · not done</span>
            <span className="min-w-0 [overflow-wrap:anywhere] text-xs font-semibold text-[var(--color-warning)]">{unpersistedWords(a, now)}</span>
          </div>
        })}
      </li>
    })}</ul>}
    <p className="m-0 text-xs text-muted-foreground">Threads on the Mac (the coordinator and the reviews it runs) work outside the runtime and are not shown.</p>
  </Card>
}
