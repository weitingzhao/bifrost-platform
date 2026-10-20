import type { ProgressDocument } from '@/api/agentProgress'
import type { RuntimeProgress } from '@/api/agentRuntime'
import { StatusLamp } from '@bifrost/ui'
import { OpsSection } from '@/components/layout/OpsSection'
import { PLAN_FROM_BOARD, type PlanRead } from '../agents/useRuntimePlan'
import type { NeedsYouCount } from '../needs-you/needsYouModel'
import { PoolReadings } from '../owner/PoolReadings'
import { whoseMove } from './mergedProgressModel'

function Meter({ label, done, total }: { label: string; done: number; total: number }) {
  return <div><span>{done}/{total} {label}</span><progress aria-label={label} value={done} max={total || 1} className="block h-2 w-full" /></div>
}
/** plan: where the round comes from (2.5 slice 2): the runtime once it holds the round, the board's until then. */
export function ProgressOverview({ count, runtime, board, plan, failed, silentThreads = 0 }: { count: NeedsYouCount; runtime?: RuntimeProgress; board?: ProgressDocument | null; plan: PlanRead; failed: boolean; silentThreads?: number }) {
  const move = whoseMove(count, runtime, failed, silentThreads)
  const round = plan.from === 'runtime' ? plan.round : plan.from === 'board' ? board?.roadmap?.round : undefined
  const items = round?.unknown === undefined ? round?.items : undefined
  const rows = board?.roadmap?.arch?.rows
  const card = 'flex min-w-0 flex-col gap-2 rounded border border-[var(--card-border)] p-3 text-sm'
  return <OpsSection title="Overview" bodyPadding="compact">
    <div className="grid gap-3 md:grid-cols-5">
      <div className={card}><strong>Whose move</strong><StatusLamp value={move === 'Unknown' ? 'unknown' : move === 'All done' ? 'ok' : 'degraded'} /><span>{move}</span></div>
      <div className={card}><strong>Running</strong><span className="text-2xl tabular-nums">{runtime ? runtime.live.length : '?'}</span><span>Agent attempts</span></div>
      <div className={card}><strong>Round progress</strong>{plan.from === 'board' && <span className="text-xs text-muted-foreground">{PLAN_FROM_BOARD}</span>}{items ? <><Meter label="items" done={items.filter(i => i.state === 'done').length} total={items.length} /><Meter label="steps" done={items.reduce((n, i) => n + (i.done ?? i.steps.filter(s => s.state === 'done').length), 0)} total={items.reduce((n, i) => n + (i.total ?? i.steps.length), 0)} /></> : <span>{plan.from === 'error' ? `Plan unavailable: ${plan.error}` : plan.from === 'loading' ? 'Reading the plan from the agent runtime…' : round?.unknown !== undefined ? `Plan unavailable: ${round.unknown}` : plan.from === 'runtime' ? 'No items in the runtime\'s plan.' : board ? 'No round published.' : 'Round unknown'}</span>}</div>
      <div className={card}><strong>Architecture</strong>{rows ? ['done', 'part', 'none'].map(state => <Meter key={state} label={state} done={rows.filter(r => r.state === state).length} total={rows.length} />) : <span>{board ? 'No architecture published.' : 'Architecture unknown'}</span>}</div>
      <div className={card}><strong>Quota</strong><PoolReadings now={Date.now()} /></div>
    </div>
  </OpsSection>
}
