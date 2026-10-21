import type { RuntimeProgress } from '@/api/agentRuntime'
import { archCounts } from '@/api/agentRuntimePlan'
import { StatusLamp } from '@bifrost/ui'
import { OpsSection } from '@/components/layout/OpsSection'
import { ARCH_NOT_IMPORTED, PLAN_NOT_IMPORTED, type ArchRead, type PlanRead } from '../agents/useRuntimePlan'
import type { NeedsYouCount } from '../needs-you/needsYouModel'
import { PoolReadings } from '../owner/PoolReadings'
import { whoseMove } from './mergedProgressModel'

function Meter({ label, done, total }: { label: string; done: number; total: number }) {
  return <div><span>{done}/{total} {label}</span><progress aria-label={label} value={done} max={total || 1} className="block h-2 w-full" /></div>
}
/** plan and arch: the round and the architecture table, from the agent runtime only (2.5 slice 2 and ⑤). */
export function ProgressOverview({ count, runtime, plan, arch, failed, silentThreads = 0 }: { count: NeedsYouCount; runtime?: RuntimeProgress; plan: PlanRead; arch: ArchRead; failed: boolean; silentThreads?: number }) {
  const move = whoseMove(count, runtime, failed, silentThreads)
  const round = plan.from === 'runtime' ? plan.round : undefined
  const items = round?.unknown === undefined ? round?.items : undefined
  const counts = arch.from === 'runtime' ? archCounts(arch.table) : null
  const rows = arch.from === 'runtime' ? arch.table.rows.length : 0
  const card = 'flex min-w-0 flex-col gap-2 rounded border border-[var(--card-border)] p-3 text-sm'
  return <OpsSection title="Overview" bodyPadding="compact">
    <div className="grid gap-3 md:grid-cols-5">
      <div className={card}><strong>Whose move</strong><StatusLamp value={move === 'Unknown' ? 'unknown' : move === 'All done' ? 'ok' : 'degraded'} /><span>{move}</span></div>
      <div className={card}><strong>Running</strong><span className="text-2xl tabular-nums">{runtime ? runtime.live.length : '?'}</span><span>Agent attempts</span></div>
      <div className={card}><strong>Round progress</strong>{items ? <><Meter label="items" done={items.filter(i => i.state === 'done').length} total={items.length} /><Meter label="steps" done={items.reduce((n, i) => n + (i.done ?? i.steps.filter(s => s.state === 'done').length), 0)} total={items.reduce((n, i) => n + (i.total ?? i.steps.length), 0)} /></> : <span>{plan.from === 'error' ? `Plan unavailable: ${plan.error}` : plan.from === 'loading' ? 'Reading the plan from the agent runtime…' : round?.unknown !== undefined ? `Plan unavailable: ${round.unknown}` : plan.from === 'runtime' ? 'No items in the runtime\'s plan.' : plan.from === 'absent' ? PLAN_NOT_IMPORTED : 'Round unknown'}</span>}</div>
      <div className={card}><strong>Architecture</strong>{counts ? <><Meter label="passed" done={counts.passed} total={rows} /><Meter label="not yet" done={counts.notYet} total={rows} /><Meter label="no gate yet" done={counts.noGate} total={rows} /><Meter label="unknown" done={counts.unknown} total={rows} /></>
        : <span>{arch.from === 'error' ? `Architecture unavailable: ${arch.error}` : arch.from === 'loading' ? 'Reading the architecture table…' : ARCH_NOT_IMPORTED}</span>}</div>
      <div className={card}><strong>Quota</strong><PoolReadings now={Date.now()} /></div>
    </div>
  </OpsSection>
}
