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
 * Work → Plan's round row (R5:5.5-1; Design v3.6 WorkPage's tab row): the newest round GET /progress lists first, the
 * round the address names when it is another, and every earlier round in one menu. A round is read only once it is
 * chosen: the menu holds links, not reads. "Earlier" is by number only; it does not say a round has ended.
 */
export function PlanRoundNav({ plan }: { plan: PlanAddress }) {
  const now = useNow()
  const { rounds, root } = plan
  const newest = rounds.state === 'ok' ? rounds.newest : null
  const viewingNewest = root !== null && root === newest
  const known = rounds.state === 'ok' && root !== null && rounds.rounds.some(r => r.id === root)
  const earlier = rounds.state === 'ok' ? rounds.rounds.filter(r => r.id !== newest).reverse() : []
  const stale = rounds.state === 'ok' && (rounds.failed !== '' || now - rounds.at > 2 * AGENT_RUNTIME_POLL_MS)
  return <nav aria-label="Plan rounds" className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
    {(newest !== null || root !== null) && <div className="flex max-w-full flex-wrap gap-1 rounded-[14px] p-1 shadow-[var(--ops-inset)] [background:var(--ops-nav)]">
      {newest !== null && <a href={workRoundHref()} aria-current={viewingNewest ? 'page' : undefined} className={viewingNewest ? on : off}
        title="The newest round the runtime lists (GET /progress)">{roundLabel(newest)}</a>}
      {root !== null && !viewingNewest && <span aria-current="page" className={on}>{roundLabel(root)}</span>}
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
    </div>}
    <span className="text-xs text-muted-foreground [overflow-wrap:anywhere]">
      {/* Without a round in the address, reading, Unknown and no round are said once, in the plan's place below. */}
      {rounds.state === 'reading' ? (plan.from === 'address' ? 'Other rounds not read yet' : '')
        : rounds.state === 'unknown' ? (plan.from === 'address' ? `Other rounds Unknown · ${rounds.reason}` : '')
        : newest === null ? ''
        : viewingNewest ? 'newest in the runtime'
        : known ? `viewing an earlier round · ${roundLabel(newest)} is the newest in the runtime`
        : `${root} · not among the rounds the runtime lists`}
      {rounds.state === 'ok' && rounds.ignored.length > 0 && ` · ignored, ids not R<n>: ${rounds.ignored.join(', ')}`}
      {stale && rounds.state === 'ok' && ` · rounds as of ${time(rounds.at)} UTC${rounds.failed ? ` · the latest read failed: ${rounds.failed}` : ''}`}
    </span>
  </nav>
}
