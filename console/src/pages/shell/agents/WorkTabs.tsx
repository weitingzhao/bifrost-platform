import type { PlanSnapshot } from '@/api/planSnapshot'
import { PlanRoundNav, PlanRoundNote } from './PlanRoundNav'
import type { PlanAddress } from './usePlanSnapshot'
import { workTabHref, type WorkTab } from './workViewModel'
import { signoffWords, type SignoffCount } from './workSummaryModel'

const segment = 'inline-flex min-h-10 items-center gap-1.5 whitespace-nowrap rounded-[10px] border-0 px-3.5 text-sm no-underline pointer-coarse:min-h-11 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--ring)]'
const on = `${segment} bg-[var(--card)] font-semibold text-[var(--primary)] shadow-[var(--ops-raised)] hover:text-[var(--primary)]`
const off = `${segment} cursor-pointer bg-transparent font-medium text-foreground hover:text-foreground`

/**
 * Work's tab row (Design v3.6 WorkPage): the round (with its earlier rounds), Sign-offs, Live and Changes. Each is a
 * link, so the address keeps the tab (`#work?tab=live`) and the round it shows (`root=`); one tab is drawn at a time.
 * Live counts the work registered in this round's plan view, never "nobody works": unregistered work is not seen.
 */
export function WorkTabs({ plan, tab, count, view, stale }: { plan: PlanAddress; tab: WorkTab; count: SignoffCount; view: PlanSnapshot | undefined; stale: boolean }) {
  const named = plan.from === 'address'
  const tabs: [WorkTab, string][] = [
    ['signoffs', `Sign-offs · ${signoffWords(count)}`],
    ['live', view ? `Live · ${stale ? 'last read ' : ''}${view.working.length} registered` : 'Live'],
    ['changes', 'Changes'],
  ]
  return <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
    <nav aria-label="Work views" className="flex max-w-full flex-wrap gap-1 rounded-[14px] p-1 shadow-[var(--ops-inset)] [background:var(--ops-nav)]">
      <PlanRoundNav plan={plan} active={tab === 'round'} />
      {plan.root === null && <a href={workTabHref('round', null, false)} aria-current={tab === 'round' ? 'page' : undefined} className={tab === 'round' ? on : off}>Round</a>}
      {tabs.map(([id, label]) => <a key={id} href={workTabHref(id, plan.root, named)} data-work-tab={id} aria-current={tab === id ? 'page' : undefined} className={tab === id ? on : off}>{label}</a>)}
    </nav>
    <PlanRoundNote plan={plan} />
    <a href="#work?section=delivery-boundaries" className={off}>Delivery boundaries</a>
  </div>
}
