import type { RuntimeMission } from '@/api/agentRuntime'
import { firstLine } from './crewModel'
import { Card } from './OwnerParts'
import { etaBasis, etaLine } from './ownerModel'

/** How many open goals the card lists; the rest are on Progress. */
const SHOWN = 6

/**
 * Open goals with when each should be done (2.5 G8): the runtime's estimate as a range of times with
 * what it stands on, or unknown with why — from GET /agentrt/progress, newest goal first.
 */
export function OwnerGoals({ missions, now, read }: { missions: RuntimeMission[]; now: number; read: 'ok' | 'loading' | 'error' }) {
  const open = [...missions].sort((a, b) => b.mission_id.localeCompare(a.mission_id, undefined, { numeric: true }))
  return <Card label="Open goals" title={read === 'ok' ? `Open goals · ${open.length}` : 'Open goals'} more={{ href: '#progress', text: 'Goals' }}>
    {read === 'loading' && <p className="m-0 text-sm text-muted-foreground">Reading the agent runtime…</p>}
    {read === 'error' && <p role="alert" className="m-0 text-sm">The agent runtime could not be read: open goals and when they finish are not known.</p>}
    {read === 'ok' && open.length === 0 && <p className="m-0 text-sm text-muted-foreground">No open goals.</p>}
    {read === 'ok' && <ul className="m-0 flex list-none flex-col gap-2 p-0">{open.slice(0, SHOWN).map(m => {
      const basis = etaBasis(m.eta)
      return <li key={m.mission_id} className="flex min-w-0 flex-col gap-1 rounded-[var(--control-radius)] bg-[var(--control-fill)] p-2.5">
        <a href={`#progress?goal=${encodeURIComponent(m.mission_id)}`} className="min-w-0 text-sm text-foreground [overflow-wrap:anywhere]">
          <strong>{m.mission_id}</strong> {firstLine(m.title || m.goal)}</a>
        <span className="min-w-0 text-xs text-muted-foreground [overflow-wrap:anywhere]">{m.status || m.state}{m.reason ? ` · ${m.reason}` : ''}</span>
        {basis.length === 0
          ? <span className={`min-w-0 text-sm [overflow-wrap:anywhere] ${m.eta.state === 'unknown' ? 'text-muted-foreground' : ''}`}>{etaLine(m.eta, now)}</span>
          : <details className="min-w-0 text-sm"><summary className="cursor-pointer [overflow-wrap:anywhere]">{etaLine(m.eta, now)}</summary>
            <ul className="m-0 mt-1 flex list-none flex-col gap-0.5 p-0 text-xs text-muted-foreground">
              {basis.map(line => <li key={line} className="[overflow-wrap:anywhere]">{line}</li>)}
              {m.eta.state === 'estimated' && m.eta.reason && <li className="[overflow-wrap:anywhere]">How: {m.eta.reason}</li>}
            </ul></details>}
      </li>
    })}</ul>}
    {read === 'ok' && open.length > SHOWN && <a href="#progress" className="text-sm">{open.length - SHOWN} more on Progress</a>}
  </Card>
}
