import { offlineWords } from '../owner/crewModel'
import { NowClock, useNow } from '../progress/progressClock'
import { HOSTS_GROUP, WORK_GROUP, cleanupBreakdown, cleanupWords, groupCount, type CleanupGroup, type NeedsCleanup } from './needsCleanupModel'
import { WorkRow } from './PlanLiveWork'
import { roundLabel } from './planRoundModel'
import { needsCleanupHref, useNeedsCleanup } from './useNeedsCleanup'
import { usePlanAddress, usePlanSnapshot, type PlanAddress } from './usePlanSnapshot'

/**
 * The summary's one way to the list (R5:5.5-4): each group's count with its reach, the list's own words (the same
 * needsCleanup() from the same reads), or what of it is Unknown; no "all clean". The list is a step away, on Work →
 * Activity; how each group is counted is in the summary's "How this is read".
 */
export function NeedsCleanupEntry({ plan, cleanup }: { plan: PlanAddress; cleanup: NeedsCleanup }) {
  return <a href={needsCleanupHref(plan)} data-summary-cleanup className="inline-flex items-center text-inherit underline-offset-2 hover:underline pointer-coarse:min-h-11" title={cleanupBreakdown(cleanup).join('\n')}>
    Needs cleanup: {cleanupWords(cleanup)} ›</a>
}

const panel = 'flex min-w-0 scroll-mt-4 flex-col gap-2 rounded-[20px] border border-[var(--ops-edge)] p-3.5 shadow-[var(--ops-shadow)] [background:var(--ops-panel)] [overflow-wrap:anywhere]'
const caps = 'm-0 text-xs font-semibold uppercase tracking-[.08em] text-muted-foreground'
const note = 'm-0 text-xs text-muted-foreground'

function GroupState({ group, none }: { group: CleanupGroup<unknown>; none: string }) {
  if (group.state === 'reading') return <p role="status" className="m-0 text-sm text-muted-foreground">Reading…</p>
  if (group.state === 'unknown') return <p role="alert" className="m-0 text-sm">Unknown · {group.reason}.{group.items.length > 0 && (group.lastRead ? ' As last read:' : ' Named so far:')}</p>
  return group.items.length === 0 ? <p className="m-0 text-sm">{none}</p> : null
}

function NeedsCleanupList() {
  const now = useNow()
  const plan = usePlanAddress()
  const query = usePlanSnapshot(plan.root)
  const cleanup = useNeedsCleanup(plan, query, now)
  const { hosts, work } = cleanup
  const view = query.data
  const round = plan.root ? roundLabel(plan.root) : 'the round'
  return <section id="needs-cleanup" aria-label="Needs cleanup" data-needs-cleanup className={panel}>
    <div className="flex flex-wrap items-center justify-between gap-2">
      <h3 className={caps} title={cleanupBreakdown(cleanup).join('\n')}>Needs cleanup · {cleanupWords(cleanup)}</h3>
      <span className="font-mono text-xs text-muted-foreground">GET /progress · plan-view {plan.root ?? '—'}</span>
    </div>
    <p className={note}>What was left stale, moved here from the top of Work. Listed, not changed: nothing on this page deletes, stops or drains anything. Each group is counted by the runtime's own rule.</p>

    <h4 className={`${caps} pt-1`} data-cleanup-group="hosts">{HOSTS_GROUP} · {groupCount(hosts)} · every host of the runtime</h4>
    <p className={note}>Silent past 30 s since their last contact, active, not the external host, no stop or drain on them (GET /progress, reported_offline). The Crew card below shows every host.</p>
    <GroupState group={hosts} none="None reported offline." />
    {hosts.state !== 'reading' && hosts.items.length > 0 && <ul aria-label="Offline hosts" className="m-0 flex list-none flex-col gap-1.5 p-0">
      {hosts.items.map(h => <li key={h.host_id} data-cleanup-host={h.host_id} className="rounded-[14px] px-3 py-2.5 text-sm font-semibold [background:var(--ops-selection)]">{offlineWords(h, now)}</li>)}</ul>}

    <h4 className={`${caps} pt-1`} data-cleanup-group="work">{WORK_GROUP} · {groupCount(work)} · {round} only</h4>
    <p className={note}>Every record of {round}&apos;s plan view (external_work) still Running or Registered and Unconfirmed, as Live says it: the server&apos;s word after 60 s without a heartbeat, or the heartbeat age it gave plus the time since. That includes records on a task a runtime attempt now runs, which Live shows under the attempt. Each other round&apos;s registered work is on its own Live tab, not read here. Runtime attempts are not listed: the controller takes them back itself, and Now and Crew say which are not confirmed running.</p>
    <GroupState group={work} none={`None in ${round}'s plan view.`} />
    {view && work.state !== 'reading' && work.items.length > 0 && <ul aria-label="Registered external work with no heartbeat" className="m-0 flex list-none flex-col gap-2 p-0">
      {work.items.map(w => <WorkRow key={w.external_work_id} work={w} view={view} stale={work.state === 'unknown'} />)}</ul>}
  </section>
}

/** Work → Activity's first block: what needs cleanup, read on its own clock. */
export function NeedsCleanupSection() {
  return <NowClock><NeedsCleanupList /></NowClock>
}
