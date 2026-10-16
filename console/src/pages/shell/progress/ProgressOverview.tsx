import type { ProgressDocument } from '@/api/agentProgress'
import type { RuntimeProgress } from '@/api/agentRuntime'
import { StatusLamp } from '@bifrost/ui'
import { OpsSection } from '@/components/layout/OpsSection'
import type { NeedsYouCount } from '../needs-you/needsYouModel'
import { ProgressUsageBlock } from './ProgressUsage'
import { whoseMove, workRunning } from './mergedProgressModel'

function Meter({ label, done, total }: { label: string; done: number; total: number }) {
  return <div><span>{done}/{total} {label}</span><progress aria-label={label} value={done} max={total || 1} className="block h-2 w-full" /></div>
}
export function ProgressOverview({ count, runtime, board, failed, silentThreads = 0 }: { count: NeedsYouCount; runtime?: RuntimeProgress; board?: ProgressDocument | null; failed: boolean; silentThreads?: number }) {
  const move = whoseMove(count, runtime, board, failed, silentThreads)
  const items = board?.roadmap?.round?.items
  const rows = board?.roadmap?.arch?.rows
  const card = 'flex min-w-0 flex-col gap-2 rounded border border-[var(--card-border)] p-3 text-sm'
  return <OpsSection title="Overview" bodyPadding="compact">
    <div className="grid gap-3 md:grid-cols-5">
      <div className={card}><strong>Whose move</strong><StatusLamp value={move === 'Unknown' ? 'unknown' : move === 'All done' ? 'ok' : 'degraded'} /><span>{move}</span></div>
      <div className={card}><strong>Running</strong><span className="text-2xl tabular-nums">{runtime ? runtime.live.length : '?'}</span><span>Agent attempts</span><span className="text-2xl tabular-nums">{board ? (board.works ?? []).filter(workRunning).length : '?'}</span><span>Coordinator works</span></div>
      <div className={card}><strong>Round progress</strong>{items ? <><Meter label="items" done={items.filter(i => i.state === 'done').length} total={items.length} /><Meter label="steps" done={items.reduce((n, i) => n + (i.done ?? i.steps.filter(s => s.state === 'done').length), 0)} total={items.reduce((n, i) => n + (i.total ?? i.steps.length), 0)} /></> : <span>{board ? 'No round published.' : 'Round unknown'}</span>}</div>
      <div className={card}><strong>Architecture</strong>{rows ? ['done', 'part', 'none'].map(state => <Meter key={state} label={state} done={rows.filter(r => r.state === state).length} total={rows.length} />) : <span>{board ? 'No architecture published.' : 'Architecture unknown'}</span>}</div>
      <div className={card}><strong>Quota</strong>{board?.usage ? <>
        {(board.usage.pools ?? []).slice(0, 3).map((pool, i) => <div key={i}><span>{pool.label || pool.vendor} · {pool.used ?? '?'}%</span><progress aria-label={`${pool.label || pool.vendor} quota`} value={pool.used ?? 0} max={100} className="block h-2 w-full" /></div>)}
        {!board.usage.pools?.length && <span>{board.usage.claude?.windows?.map(w => `${w.label} ${w.used ?? '?'}%`).join(' · ') || 'Usage available'}</span>}
        <details><summary className="cursor-pointer">Quota details</summary><ProgressUsageBlock usage={board.usage} /></details>
      </> : <span>{board ? 'No usage reported.' : 'Quota unknown'}</span>}</div>
    </div>
  </OpsSection>
}
