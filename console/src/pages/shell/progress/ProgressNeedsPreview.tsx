import { OpsSection } from '@/components/layout/OpsSection'
import type { useNeedsYou } from '../needs-you/useNeedsYou'
import { needsYouCountText } from '../needs-you/needsYouModel'
import type { WaitingItem } from '../needs-you/mergedNeedsYou'
function Items({ items }: { items: WaitingItem[] }) {
  return <ul className="flex flex-col gap-1">{items.map(item => <li key={item.id} className="min-w-0 text-sm"><a href={item.href} title={`${item.title} · ${item.instruction}`} className="block truncate underline">{item.title} · {item.instruction}</a></li>)}</ul>
}
export function ProgressNeedsPreview({ needs }: { needs: ReturnType<typeof useNeedsYou> }) {
  const { count } = needs
  return <OpsSection title={`Needs you · ${needsYouCountText(count)}`} bodyPadding="compact">
    <p data-needs-you-count={needsYouCountText(count)}>{count.state === 'loading' ? 'Checking what needs you…' : count.state === 'unknown' ? `Count is incomplete: ${count.reason}` : count.count === 0 ? 'Nothing needs you right now' : `${count.count} waiting for you`}</p>
    <div aria-label="Decisions for you"><h4 className="text-sm font-medium">Decisions for you · {needs.decisionItems.length}</h4><Items items={needs.decisionItems} /></div>
    <details className="my-2"><summary className="cursor-pointer text-sm">Silent threads · {needs.silentItems.length}</summary><p className="text-xs text-muted-foreground">Claude sessions that went quiet.</p><Items items={needs.silentItems} /></details>
    <a href="#needs-you" className="text-sm underline">All on Needs you</a>
  </OpsSection>
}
