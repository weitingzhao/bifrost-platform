import { useState } from 'react'
import { useNarrow } from './useNarrow'
import { TIMELINE_DEFAULT, hhmm, timelineGeometry, touches, type ChangeMap, type PageRun } from './ciPageModel'

/*
 * Work > CI > By time: one bar a run that tested itself, newest on top; width is seconds; the segments are waiting to
 * start, everything before the tests, and the tests coloured by the side that took longest. A red run carries the name
 * of the task that failed; a selective run is marked. Clicking a bar selects the run.
 */

const PHASE = {
  lag: 'color-mix(in srgb, var(--muted-foreground) 45%, transparent)',
  setup: 'var(--muted-foreground)',
  api: 'var(--color-env-stg)',
  con: 'var(--color-env-prod)',
  tests: 'var(--primary)',
} as const

type Segment = { phase: keyof typeof PHASE; seconds: number }
function segments(run: PageRun): Segment[] {
  const took = run.took ?? 0
  // A run in flight has no parts yet; a finished one whose parts were not read is one bar in its tests' colour.
  if (run.ok == null) return [{ phase: 'setup', seconds: took }]
  if (run.lag == null || run.setup == null) return [{ phase: run.testsBy, seconds: took }]
  return [{ phase: 'lag', seconds: run.lag }, { phase: 'setup', seconds: run.setup }, { phase: run.testsBy, seconds: Math.max(0, took - run.lag - run.setup) }]
}

export function CiTimeline({ runs, changes, domain, selectedRun, onSelectRun, narrow: forced }: {
  runs: PageRun[]; changes: ChangeMap; domain: string; selectedRun: string; onSelectRun: (name: string) => void; narrow?: boolean
}) {
  const measuredNarrow = useNarrow()
  const narrow = forced ?? measuredNarrow
  const [all, setAll] = useState(false)
  const drawable = runs.filter(r => r.took != null)
  const shown = all ? drawable : drawable.slice(0, TIMELINE_DEFAULT)
  if (drawable.length === 0) return <p className="m-0 text-sm text-[var(--muted-foreground)]">No run that tested itself is retained for this repository.</p>
  const maxS = Math.max(...shown.map(r => r.took ?? 0))
  const g = timelineGeometry(narrow, maxS)
  const height = shown.length * (g.rowH + g.gap) + 26
  const ticks: number[] = []
  for (let s = 0; s <= maxS; s += g.step) ticks.push(s)
  return (
    <div className="min-w-0">
      <svg viewBox={`0 0 ${g.width} ${height}`} className="block h-auto w-full" role="group" aria-label="One bar a run, newest first; width is seconds, colour is phase" data-testid="ci-timeline" data-narrow={narrow ? 'yes' : 'no'}>
        {ticks.map(s => {
          const x = g.left + s * g.scale
          return <g key={s}><line x1={x} y1={14} x2={x} y2={height} stroke="var(--table-rule)" /><text x={x + 2} y={10} fontSize={10} fill="var(--muted-foreground)">{`${s} s`}</text></g>
        })}
        {shown.map((run, i) => {
          const y = 18 + i * (g.rowH + g.gap)
          const dim = !touches(run, domain, changes)
          const picked = selectedRun === run.name
          let x = g.left
          const label = run.ok === false ? (run.bad.join(', ') || 'failed') : run.running ? `running${run.nowTasks.length ? ` · ${run.nowTasks.join(', ')}` : ''}` : run.scope === 'incremental' ? 'selective' : ''
          return (
            <g key={run.name} data-testid={`ci-bar-${run.name}`} data-dim={dim ? 'yes' : 'no'} opacity={dim ? 0.18 : 1}>
              {(i % 8 === 0 || picked) && <text x={0} y={y + g.rowH - 1} fontSize={10} fill="var(--muted-foreground)">{hhmm(run.at)}</text>}
              {segments(run).map(seg => {
                const w = Math.max(0.5, seg.seconds * g.scale)
                const rect = <rect key={seg.phase} x={x} y={y} width={w} height={g.rowH} rx={2} fill={PHASE[seg.phase]} />
                x += w
                return rect
              })}
              {run.ok === false && <circle cx={x + 4 + g.rowH / 2} cy={y + g.rowH / 2} r={g.rowH / 2} fill="var(--destructive)" />}
              {label && <text x={x + (run.ok === false ? g.rowH + 8 : 5)} y={y + g.rowH - 1} fontSize={10} fill={run.ok === false ? 'var(--destructive)' : 'var(--muted-foreground)'}>{label}</text>}
              <rect role="button" tabIndex={0} aria-pressed={picked} aria-label={`Run of ${run.sha}, ${hhmm(run.at)}, ${run.took} s${run.ok === false ? `, failed in ${run.bad.join(', ') || 'an unnamed task'}` : run.running ? ', running' : ''}`}
                x={g.left - 2} y={y - 1.5} width={g.width - g.left} height={g.rowH + 3} style={{ cursor: 'pointer' }}
                fill={picked ? 'color-mix(in srgb, var(--primary) 12%, transparent)' : 'transparent'} stroke={picked ? 'var(--primary)' : 'none'} strokeWidth={1.5}
                onClick={() => onSelectRun(picked ? '' : run.name)}
                onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onSelectRun(picked ? '' : run.name) } }} />
            </g>
          )
        })}
      </svg>
      <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-[var(--muted-foreground)]">
        {([['lag', 'waiting to start'], ['setup', 'clone and scope'], ['api', 'tests, backend longest'], ['con', 'tests, console longest']] as const).map(([phase, text]) =>
          <span key={phase}><i className="mr-1 inline-block h-2.5 w-2.5 rounded-sm align-[-1px]" style={{ background: PHASE[phase] }} />{text}</span>)}
        <span><i className="mr-1 inline-block h-2.5 w-2.5 rounded-full align-[-1px]" style={{ background: 'var(--destructive)' }} />failed, with the task</span>
        <span>newest on top · times Chicago</span>
        {drawable.length > TIMELINE_DEFAULT && <button type="button" className="min-h-11 underline underline-offset-4" onClick={() => setAll(v => !v)}>{all ? `show the newest ${TIMELINE_DEFAULT}` : `show all ${drawable.length}`}</button>}
      </div>
    </div>
  )
}
