import type { RuntimeAttempt, RuntimeHost, RuntimeMission } from '@/api/agentRuntime'
import { AttemptActionLine } from './AttemptActionLine'
import { crewRows, duration } from './crewModel'
import { Card, Dot } from './OwnerParts'
import { ago, type Tone } from './ownerModel'

const ROLE: Record<string, string> = { implement: 'Implement', verify: 'Verify', review: 'Review', plan: 'Plan', brief: 'Chief of staff' }
/** A heartbeat older than this, on an attempt still live, is shown as late. */
const LATE_HEARTBEAT_S = 120

function hostTone(h: RuntimeHost | undefined): Tone {
  if (h?.online === true) return 'ok'
  return h?.online === false ? 'fail' : 'unknown'
}

/**
 * Crew (2.5, thin; the Owner 2026-10-18): which agent works on which host, on what, now. Only this
 * platform's runtime is shown: threads on the Mac (the coordinator and the reviews it runs) work
 * outside it.
 */
export function OwnerCrew({ hosts, live, missions, now }: { hosts: RuntimeHost[]; live: RuntimeAttempt[]; missions: RuntimeMission[]; now: number }) {
  const tasks = new Map<string, { title: string; role: string; mission: string }>()
  for (const m of missions) for (const t of m.tasks) tasks.set(t.task_id, { title: t.title, role: t.role, mission: m.mission_id })
  const rows = crewRows(hosts, live)
  const known = rows.filter(r => r.host)
  const online = known.filter(r => r.host?.online === true).length
  return <Card label="Crew" title={known.length ? `Crew · ${online} of ${known.length} hosts online` : 'Crew'} more={{ href: '#progress', text: 'Goals' }}>
    {rows.length === 0 && <p className="m-0 text-sm text-muted-foreground">No agent host has polled this platform.</p>}
    <ul className="m-0 flex list-none flex-col gap-3 p-0">{rows.map(row => {
      const h = row.host
      return <li key={row.id} className="flex min-w-0 flex-col gap-2 rounded-[var(--control-radius)] bg-[var(--control-fill)] p-2.5">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <span className="flex min-w-0 items-center gap-2"><Dot tone={hostTone(h)} /><strong className="truncate font-mono text-sm" title={row.id}>{row.id}</strong>
            <span className="text-xs text-muted-foreground">{!h ? 'not a registered host' : h.online ? 'online' : `last poll ${ago(h.last_poll_at, now)}`}</span></span>
          <span className="text-xs font-semibold">{row.attempts.length ? `${row.attempts.length} running` : 'Idle'}</span>
        </div>
        {row.attempts.map(a => {
          const t = tasks.get(a.task_id)
          const late = a.heartbeat_age_s != null && a.heartbeat_age_s > LATE_HEARTBEAT_S
          return <div key={a.attempt_id} className="flex min-w-0 flex-col gap-1 border-t border-[var(--table-rule)] pt-2">
            <span className="min-w-0 break-words text-sm"><strong>{ROLE[t?.role ?? ''] ?? t?.role ?? 'Task'}</strong> · {t?.title || a.task_id}</span>
            <span className="min-w-0 break-words text-xs text-muted-foreground">
              {a.executor.replace(/^exec:/, '')}{a.model ? ` (${a.model})` : ''} · {t?.mission ?? a.mission_id} · {a.state} {duration(a.elapsed_s)}
              {late && <span className="font-semibold text-[var(--color-warning)]"> · no heartbeat for {duration(a.heartbeat_age_s)}</span>}</span>
            <AttemptActionLine attempt={a} now={now} who={false} />
          </div>
        })}
      </li>
    })}</ul>
    <p className="m-0 text-xs text-muted-foreground">Threads on the Mac (the coordinator and the reviews it runs) work outside the runtime and are not shown.</p>
  </Card>
}
