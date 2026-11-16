import type { ReactNode, RefObject } from 'react'
import type { PlanNode, PlanSnapshot } from '@/api/planSnapshot'
import { isStale, mainlineOf, nextInPlanOrder, nowSteps, SHELL_POLL_MS, type ModuleScope, type ShellRead, type ShellReads } from '@/pages/shell/agents/moduleDetailModel'
import { activeCounts, cellLabels, noViewReason, spokenState, toGoText, type ModuleRow } from '@/pages/shell/agents/planModuleModel'
import { responsibility } from '@/pages/shell/agents/planSnapshotModel'
import { planNodeHref } from '@/pages/shell/agents/usePlanSnapshot'
import { StepBadge } from './StepText'

export const SOURCES: [keyof ShellReads, string][] = [['questions', 'Questions'], ['open', 'Requests'], ['all', 'Request history'], ['progress', 'Runtime progress']]
const readWords = (read: ShellRead<unknown>, stale: boolean) => read.state === 'ok' ? `${new Date(read.at).toISOString()}${stale ? ' · Stale' : ''}`
  : read.state === 'unknown' ? `Unknown · ${read.reason}` : read.state === 'reading' ? 'reading…' : 'not read yet'

const panel = 'flex min-w-0 flex-col rounded-[20px] border border-[var(--ops-edge)] p-3.5 shadow-[var(--ops-shadow)] [background:var(--ops-panel)] [overflow-wrap:anywhere]'
const caps = 'm-0 text-xs font-semibold uppercase tracking-[.08em] text-muted-foreground'
const dashedPill = 'inline-flex h-5 shrink-0 items-center whitespace-nowrap rounded-full border border-dashed border-[var(--color-lamp-gray)] px-[7px] text-[11px] font-semibold text-muted-foreground'
export function Big({ children }: { children: ReactNode }) {
  return <strong className="text-2xl font-bold leading-tight">{children}</strong>
}
function Who({ owner }: { owner: boolean }) {
  return <span className="shrink-0 rounded-full bg-[var(--card-fill)] px-1.5 text-xs font-semibold" title={owner ? "The Owner's step (owner_step)" : 'Not an Owner step: the crew does it'}>{owner ? 'Owner' : 'Agent'}</span>
}
const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`
function Field({ label, children }: { label: string; children: ReactNode }) {
  return <section aria-label={label} className="flex min-w-0 flex-col gap-1 text-[13px] leading-[1.45]">
    <h4 className="m-0 text-[11px] font-semibold uppercase tracking-[.06em] text-muted-foreground">{label}</h4>
    {children}
  </section>
}

/** The head card (v3.6 item header): which module and where on the mainline, its title, steps to go, now, next and ETA. */
export function Head({ view, item, row, scope, reads, stale, now, heading }: { view: PlanSnapshot; item: PlanNode; row: ModuleRow; scope: ModuleScope
  reads: ShellReads; stale: boolean; now: number; heading: RefObject<HTMLHeadingElement | null> }) {
  const current = nowSteps(scope)
  const next = nextInPlanOrder(scope)
  const of = mainlineOf(view, item)
  return <section aria-label={`Summary of ${item.label}`} className={`${panel} gap-2.5 px-4`}>
    <div className="flex min-w-0 flex-wrap items-center gap-2">
      <span data-kicker className={caps}>{item.label} · {row.lane}{of !== null && ` of ${of}`}</span>
      {row.current && <span title="The plan view's mainline position (mainline.current), not a verified milestone"
        className="inline-flex h-5 items-center rounded-full px-[7px] text-[11px] font-bold text-[var(--primary-foreground)] [background:var(--ops-accent)]">Current</span>}
    </div>
    <h3 ref={heading} tabIndex={-1} className="m-0 break-words text-xl font-[650] leading-[1.35] tracking-[-.01em] [text-wrap:pretty] focus:outline-none">{row.title || 'Untitled module'}</h3>
    <section aria-label="Steps to go" className="flex min-w-0 flex-wrap items-baseline gap-x-2 gap-y-0.5 text-[15px]">
      {row.total === 0 || row.toGo === 0 ? <Big>{toGoText(row)}</Big>
        : <p className="m-0" data-to-go><Big>{row.toGo}</Big> {row.toGo === 1 ? 'step' : 'steps'} to go</p>}
      <span>of {row.total} · {row.done} done{activeCounts(row).map(p => ` · ${p}`).join('')}{row.stopped > 0 && ` · ${row.stopped} stopped`}</span>
      {row.unknown > 0 && <span>{plural(row.unknown, 'step')} without a state: Unknown</span>}
      {row.dropped > 0 && <span className="text-xs text-muted-foreground">{row.dropped} dropped, not counted</span>}
      <span className="basis-full text-xs text-muted-foreground">Execution progress from the controller's step states; an active step by what the plan view, read now, shows running or queued on it. Not acceptance, not deployment.</span>
    </section>
    <div className="grid min-w-0 grid-cols-1 gap-x-4 gap-y-2.5 border-t border-[var(--table-rule)] pt-2.5 sm:grid-cols-2 xl:grid-cols-4">
      <Field label="Now">
        {current.length === 0
          ? <p className="m-0">{row.unknown > 0 ? `No step in progress by the runtime's states; ${plural(row.unknown, 'step')} without a state (Unknown).` : 'No step in progress.'}</p>
          : <ul className="m-0 flex list-none flex-col gap-1 p-0">{current.map(cell => <li key={cell.id} className="flex min-w-0 flex-wrap items-center gap-2">
            <a href={planNodeHref(view.root, cell.id)} aria-label={`Step ${cell.label} ${spokenState(cell)}`} className="inline-flex pointer-coarse:min-h-11 pointer-coarse:items-center"><StepBadge label={cell.label} /></a>
            <span className="min-w-0 break-words">{cell.title || 'Untitled step'}</span>
            <span data-now-state={cell.state} className="text-xs text-muted-foreground">{cellLabels[cell.state]}</span></li>)}</ul>}
      </Field>
      <Field label="Next in plan order">
        {next ? <>
          <span className="flex min-w-0 flex-wrap items-center gap-2"><StepBadge label={next.label} /><Who owner={next.owner} /><span className="min-w-0 break-words">{next.title || 'Untitled step'}</span></span>
          <span className="text-xs text-muted-foreground">Plan order only, not a commitment.</span>
        </> : <p className="m-0">{row.unknown > 0 ? `Unknown · ${plural(row.unknown, 'step')} without a state` : 'No step left to start'}</p>}
      </Field>
      <Field label="Next responsible">
        <span data-next-responsible>{item.view ? responsibility(item) : `Unknown · ${noViewReason(view)}`}</span>
      </Field>
      <Field label="ETA">
        <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5">
          <span className={dashedPill}>{view.eta.status}</span>
          <span>{view.eta.reason}</span>
          <span className="text-xs text-muted-foreground">as of <time dateTime={view.as_of}>{view.as_of}</time></span>
        </span>
      </Field>
    </div>
    <dl aria-label="Read as of" className="m-0 flex min-w-0 flex-wrap gap-x-4 gap-y-0.5 font-mono text-[11px] text-muted-foreground">
      <div className="flex min-w-0 flex-wrap gap-1"><dt>Plan snapshot</dt><dd className="m-0"><time dateTime={view.as_of}>{view.as_of}</time> · {stale ? 'Stale' : 'Fresh'}</dd></div>
      {SOURCES.map(([key, name]) => <div key={key} className="flex min-w-0 flex-wrap gap-1"><dt>{name}</dt>
        <dd className="m-0" data-source={key}>{readWords(reads[key], isStale(reads[key], SHELL_POLL_MS[key], now))}</dd></div>)}
    </dl>
  </section>
}
