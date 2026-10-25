import { useRef } from 'react'
import type { RuntimeProgress } from '@/api/agentRuntime'
import { archCounts } from '@/api/agentRuntimePlan'
import { StatusLamp } from '@bifrost/ui'
import { OpsSection } from '@/components/layout/OpsSection'
import { ARCH_NOT_IMPORTED, PLAN_NOT_IMPORTED, type ArchRead, type PlanRead } from '../agents/useRuntimePlan'
import type { NeedsYouCount } from '../needs-you/needsYouModel'
import { LastRead } from '../owner/LastRead'
import { reasonText } from '../owner/ownerModel'
import { activityCounts, attemptActivity } from '../owner/attemptActivity'
import { whoseMove } from './mergedProgressModel'

function Meter({ label, done, total }: { label: string; done: number; total: number }) {
  return <div><span>{done}/{total} {label}</span><progress aria-label={label} value={done} max={total || 1} className="block h-2 w-full" /></div>
}
/** plan and arch: the round and the architecture table, from the agent runtime only (2.5 slice 2 and ⑤). */
export function ProgressOverview({ count, runtime, plan, arch, failed, unread, silentThreads = 0, openedAt }: { count: NeedsYouCount; runtime?: RuntimeProgress; plan: PlanRead; arch: ArchRead; failed: boolean; unread?: string; silentThreads?: number; openedAt?: number }) {
  // The page's mount time when it passes one, so every line on the page counts from the same opening (review 493 C).
  const mounted = useRef(Date.now())
  const opened = openedAt ?? mounted.current
  const move = whoseMove(count, runtime, failed, silentThreads)
  const round = plan.from === 'runtime' ? plan.round : undefined
  const items = round?.unknown === undefined ? round?.items : undefined
  const counts = arch.from === 'runtime' ? archCounts(arch.table) : null
  const rows = arch.from === 'runtime' ? arch.table.rows.length : 0
  const card = 'flex min-w-0 flex-col gap-2 rounded border border-[var(--card-border)] p-3 text-sm'
  // An attempt on a lost host, or maybe stuck, is not counted as running (T24).
  const stalled = runtime ? runtime.live.filter(a => attemptActivity(a) !== 'running') : []
  const manual = items?.reduce((n, i) => n + (i.manual ?? 0), 0) ?? 0
  return <OpsSection title="Overview" bodyPadding="compact">
    <div className="grid gap-3 md:grid-cols-5">
      <div className={card}><strong>Whose move</strong><StatusLamp value={move === 'Unknown' ? 'unknown' : move === 'All done' ? 'ok' : 'degraded'} /><span>{move}</span>{failed && move === 'Unknown' && unread && <span>{unread}</span>}</div>
      <div className={card}><strong>Running</strong><span className="text-2xl tabular-nums">{runtime ? runtime.live.length - stalled.length : '?'}</span><span>Agent attempts</span>
        {stalled.length > 0 && <span className="font-semibold text-[var(--color-warning)]" data-stalled>Not confirmed running: {activityCounts(stalled)}</span>}</div>
      <div className={card}><strong>Round progress</strong>{items ? <><Meter label="items" done={items.filter(i => i.state === 'done').length} total={items.length} /><Meter label="steps" done={items.reduce((n, i) => n + (i.done ?? i.steps.filter(s => s.state === 'done').length), 0)} total={items.reduce((n, i) => n + (i.total ?? i.steps.length), 0)} />{manual > 0 && <span className="text-xs text-muted-foreground">{manual} of the done steps manual</span>}</> : <span>{plan.from === 'error' ? <>Plan unavailable: {reasonText(plan.error)}; <LastRead readAt={plan.readAt} openedAt={opened} />.</> : plan.from === 'loading' ? 'Reading the plan from the agent runtime…' : round?.unknown !== undefined ? `Plan unavailable: ${round.unknown}` : plan.from === 'runtime' ? 'No items in the runtime\'s plan.' : plan.from === 'absent' ? PLAN_NOT_IMPORTED : 'Round unknown'}</span>}</div>
      <div className={card}><strong>Architecture</strong><a href="#crew?section=capability-evidence">Gate evidence · Crew</a>{counts ? <><Meter label="passed" done={counts.passed} total={rows} /><Meter label="not yet" done={counts.notYet} total={rows} /><Meter label="no gate yet" done={counts.noGate} total={rows} /><Meter label="unknown" done={counts.unknown} total={rows} /></>
        : <span>{arch.from === 'error' ? <>Architecture unavailable: {reasonText(arch.error)}; <LastRead readAt={arch.readAt} openedAt={opened} />.</> : arch.from === 'loading' ? 'Reading the architecture table…' : ARCH_NOT_IMPORTED}</span>}</div>
      <div className={card}><strong>Quota</strong><a href="#crew?section=capacity">Pool readings and reset times · Crew</a></div>
    </div>
  </OpsSection>
}
