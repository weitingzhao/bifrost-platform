import { ChevronDown } from 'lucide-react'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { AGENT_RUNTIME_POLL_MS } from '@/api/agentRuntime'
import { useNow } from '../progress/progressClock'
import { roundLabel, workRoundHref } from './planRoundModel'
import type { PlanAddress } from './usePlanSnapshot'

const segment = 'inline-flex min-h-10 items-center gap-1.5 whitespace-nowrap rounded-[10px] border-0 px-3.5 text-sm no-underline pointer-coarse:min-h-11 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--ring)]'
const on = `${segment} bg-[var(--card)] font-semibold text-[var(--primary)] shadow-[var(--ops-raised)] hover:text-[var(--primary)]`
const off = `${segment} cursor-pointer bg-transparent font-medium text-foreground hover:text-foreground`
const time = (at: number) => new Date(at).toISOString().slice(11, 19)

/**
 * Work's round segments (R5:5.5-1; Design v3.6 WorkPage's tab row, R5:5.5-2): the newest round GET /progress lists
 * first, the round the address names when it is another, and every earlier round in one menu. A round is read only once
 * it is chosen: the menu holds links, not reads. "Earlier" is by number only; it does not say a round has ended. They
 * are the first tab of Work's tab row: drawn selected only while the round's own tab is open (active), else each is a
 * link back to its round.
 */
export function PlanRoundNav({ plan, active = true }: { plan: PlanAddress; active?: boolean }) {
  const { rounds, root } = plan
  const newest = rounds.state === 'ok' ? rounds.newest : null
  const viewingNewest = root !== null && root === newest
  const earlier = rounds.state === 'ok' ? rounds.rounds.filter(r => r.id !== newest).reverse() : []
  if (newest === null && root === null && earlier.length === 0) return null
  return <nav aria-label="Plan rounds" className="flex max-w-full flex-wrap gap-1">
    {newest !== null && <a href={workRoundHref()} aria-current={viewingNewest && active ? 'page' : undefined} className={viewingNewest && active ? on : off}
      title="The newest round the runtime lists (GET /progress)">{roundLabel(newest)}</a>}
    {root !== null && !viewingNewest && (active ? <span aria-current="page" className={on}>{roundLabel(root)}</span>
      : <a href={workRoundHref(root)} className={off}>{roundLabel(root)}</a>)}
    {earlier.length > 0 && <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button type="button" className={off}>Earlier rounds<ChevronDown size={16} aria-hidden /></button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="min-w-[15rem] rounded-[14px] border-[var(--ops-edge)] bg-[var(--card)] p-1.5 shadow-[var(--ops-shadow)]">
        {earlier.map(r => <DropdownMenuItem key={r.id} asChild>
          <a href={workRoundHref(r.id)} aria-current={r.id === root ? 'page' : undefined}
            className="flex min-h-11 cursor-pointer flex-col items-start justify-center gap-0 rounded-[10px] px-3 py-1.5 text-sm text-foreground no-underline aria-[current=page]:[background:var(--ops-selection)] aria-[current=page]:shadow-[var(--ops-inset)]">
            <span className="font-semibold">{roundLabel(r.id)}</span>{r.title && <span className="text-xs text-muted-foreground">{r.title}</span>}
          </a>
        </DropdownMenuItem>)}
      </DropdownMenuContent>
    </DropdownMenu>}
  </nav>
}

/** Beside the tab row: which round this is among the rounds the runtime lists, and when they were read if that is old. */
export function PlanRoundNote({ plan }: { plan: PlanAddress }) {
  const now = useNow()
  const { rounds, root } = plan
  const newest = rounds.state === 'ok' ? rounds.newest : null
  const viewingNewest = root !== null && root === newest
  const known = rounds.state === 'ok' && root !== null && rounds.rounds.some(r => r.id === root)
  const stale = rounds.state === 'ok' && (rounds.failed !== '' || now - rounds.at > 2 * AGENT_RUNTIME_POLL_MS)
  return <span data-round-note className="text-xs text-muted-foreground [overflow-wrap:anywhere]">
    {/* Without a round in the address, reading, Unknown and no round are said once, in the summary's place (R5:5.5-1 review). */}
    {rounds.state === 'reading' ? (plan.from === 'address' ? 'Other rounds not read yet' : '')
      : rounds.state === 'unknown' ? (plan.from === 'address' ? `Other rounds Unknown · ${rounds.reason}` : '')
      : newest === null ? ''
      : viewingNewest ? 'newest in the runtime'
      : known ? `viewing an earlier round · ${roundLabel(newest)} is the newest in the runtime`
      : `${root} · not among the rounds the runtime lists`}
    {rounds.state === 'ok' && rounds.ignored.length > 0 && ` · ignored, ids not R<n>: ${rounds.ignored.join(', ')}`}
    {stale && rounds.state === 'ok' && ` · rounds as of ${time(rounds.at)} UTC${rounds.failed ? ` · the latest read failed: ${rounds.failed}` : ''}`}
  </span>
}
