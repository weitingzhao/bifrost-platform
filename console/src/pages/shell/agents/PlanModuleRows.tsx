import { useRef, useState, type KeyboardEvent, type MouseEvent } from 'react'
import type { PlanSnapshot } from '@/api/planSnapshot'
import { useNow } from '../progress/progressClock'
import { StepBadge } from './StepText'
import { liveLine, liveNow, liveSpoken, NOW_SHOWN, type LiveNow, type LiveStep } from './liveStepModel'
import type { RuntimeTasks } from './moduleDetailModel'
import { rememberModuleRow } from './moduleRowReturn'
import { cellLabels, cellStyle, moduleRowLinkId, NO_RULE_PILL, noRuleMarked, noRuleText, noViewReason, nowLines, spokenState, stepCounts, toGoText, unverifiedNote, type CellState, type ModuleRow,
  type RuleLine, type StepCell } from './planModuleModel'
import { sinceWords } from './readAge'
import type { OwnerAsks } from './ownerAsksModel'
import { stepRows } from './stepStateModel'
import { workTabHref } from './workViewModel'

const legend: CellState[] = ['done', 'active', 'waiting', 'held', 'idle', 'yours', 'planned', 'stopped', 'active-unknown', 'unknown']
const detailId = (cell: StepCell) => `module-step-${cell.id}`

function Rule({ lines }: { lines: RuleLine[] }) {
  return <ul className="m-0 list-disc space-y-1 pl-5">{lines.map((line, i) => <li key={i}>{line.text}{line.children && <Rule lines={line.children} />}</li>)}</ul>
}

function StepDetail({ cell, close }: { cell: StepCell; close: () => void }) {
  const note = unverifiedNote(cell)
  return <div id={detailId(cell)} role="region" aria-label={`Step ${cell.label}`} data-step-detail onKeyDown={e => { if (e.key === 'Escape') { e.stopPropagation(); close() } }}
    className="mx-3.5 mb-3 flex min-w-0 cursor-auto flex-col gap-2 rounded-xl bg-[var(--card)] p-3 text-sm [overflow-wrap:anywhere]">
    <div className="flex min-w-0 flex-wrap items-center gap-2">
      <StepBadge label={cell.label} />
      <strong data-step-state={cell.state}>{cellLabels[cell.state]}</strong>
      {noRuleMarked(cell) && <span data-no-rule className={NO_RULE_PILL}>No done rule</span>}
      {cell.owner && <span className="rounded-full bg-[var(--control-fill)] px-1.5 text-xs font-semibold" title="The Owner's step (owner_step)">Owner</span>}
      {cell.effect && <span className="rounded-full bg-[var(--control-fill)] px-1.5 text-xs font-semibold" title="An effect step (effect_step)">★</span>}
      <button type="button" onClick={close} className="ml-auto inline-flex min-h-11 items-center rounded-[var(--control-radius)] border border-[var(--card-border)] px-3 text-sm">Close</button>
    </div>
    <p className="m-0 break-words">{cell.title || 'Untitled step'}</p>
    {cell.step === null && <p className="m-0 text-muted-foreground">Unknown · the runtime holds no state for this step.</p>}
    {nowLines(cell).map(line => <p key={line} data-step-now={cell.now?.kind} className="m-0 text-muted-foreground">{line}</p>)}
    {note && <p data-step-note="unverified" className="m-0 text-xs text-muted-foreground">{note}</p>}
    <div className="flex min-w-0 flex-col gap-1">
      <span className="text-xs text-muted-foreground">Done when</span>
      {cell.rule.length > 0 ? <Rule lines={cell.rule} /> : <p className="m-0" data-no-rule-text>{noRuleText(cell)}</p>}
    </div>
    {cell.external.length > 0 && <div className="flex min-w-0 flex-col gap-1">
      <span className="text-xs text-muted-foreground">External work on this step</span>
      <ul className="m-0 list-none space-y-1 p-0">{cell.external.map((line, i) => <li key={i}>{line}</li>)}</ul>
    </div>}
    <ul aria-label={`Records of step ${cell.label}`} className="m-0 flex min-w-0 list-none flex-wrap gap-x-4 gap-y-1 p-0">
      {cell.links.map(link => <li key={`${link.kind}-${link.href}`} className="flex min-w-0 flex-col">
        <a href={link.href} data-step-link={link.kind} className="inline-flex min-h-11 items-center underline-offset-2 hover:underline">{link.text}</a>
        {link.note && <span className="text-xs text-muted-foreground">{link.note}</span>}
      </li>)}
    </ul>
  </div>
}

/**
 * Design v3.6 WorkPage row look: the mainline's current item outlined, the other mainline items on the selection tone,
 * branch and support items recessed. Ops tokens only (styles/opsAppearance.css).
 */
function rowLook(row: ModuleRow) {
  if (row.current) return 'border-[1.5px] border-[color-mix(in_srgb,var(--primary)_55%,transparent)] [background:var(--ops-selection)]'
  return row.lane.startsWith('Mainline') ? 'border border-transparent [background:var(--ops-selection)]' : 'border border-transparent shadow-[var(--ops-inset)]'
}

function Module({ row, initial, noView, live }: { row: ModuleRow; initial: string; noView: string; live: Map<string, LiveStep> }) {
  const [open, setOpen] = useState(() => row.cells.some(c => c.id === initial) ? initial : '')
  const [focus, setFocus] = useState(() => Math.max(0, row.cells.findIndex(c => c.id === initial)))
  const buttons = useRef<(HTMLButtonElement | null)[]>([])
  const opened = row.cells.find(c => c.id === open)
  // One tab stop per bar (a toolbar): the cell last focused, kept inside the bar when a refetch drops a step.
  const tabStop = Math.min(focus, row.cells.length - 1)
  const move = (to: number) => { setFocus(to); buttons.current[to]?.focus() }
  const onKey = (e: KeyboardEvent, i: number) => {
    if (e.key === 'Escape' && open) { e.preventDefault(); setOpen(''); return }
    const last = row.cells.length - 1
    const to = e.key === 'ArrowRight' ? Math.min(last, i + 1) : e.key === 'ArrowLeft' ? Math.max(0, i - 1) : e.key === 'Home' ? 0 : e.key === 'End' ? last : -1
    if (to < 0) return
    e.preventDefault()
    move(to)
  }
  const close = () => { const i = row.cells.findIndex(c => c.id === open); setOpen(''); if (i >= 0) move(i) }
  // The whole row opens the module (Design v3.6: the row is one control). Its own controls — a step cell, a wait, the
  // opened step — keep doing their own thing, and a drag that selected text opens nothing.
  const openRow = (e: MouseEvent<HTMLLIElement>) => {
    if (e.defaultPrevented || e.button !== 0 || (e.target as Element).closest('a, button, [data-step-detail]')) return
    if (window.getSelection?.()?.toString()) return
    rememberModuleRow(row.id)
    window.location.hash = row.href
  }
  const counts = stepCounts(row)
  const title = row.title || 'Untitled module'
  // The link's focus ring is drawn around the whole row (has-[…]: below); the workspace's own ring on the link is dropped.
  return <li data-module={row.label} aria-current={row.current ? 'step' : undefined} onClick={openRow}
    className={`flex min-w-0 cursor-pointer flex-col rounded-2xl transition-shadow [overflow-wrap:anywhere] hover:shadow-[var(--ops-raised)] has-[[data-row-link]:focus-visible]:outline-2 has-[[data-row-link]:focus-visible]:outline-offset-2 has-[[data-row-link]:focus-visible]:outline-[var(--ring)] ${rowLook(row)}`}>
    <div className="flex min-w-0 flex-wrap items-center gap-x-3.5 gap-y-2 px-3.5 py-3">
      <div className="flex min-w-0 flex-[1_1_17.5rem] flex-col gap-1">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <span className="font-mono text-[13px] font-semibold">{row.label}</span>
          <span className="text-[11px] font-semibold uppercase tracking-[.06em] text-muted-foreground">{row.lane}</span>
          {row.current && <span title="The plan view's mainline position (mainline.current), not a verified milestone"
            className="inline-flex h-5 items-center rounded-full px-[7px] text-[11px] font-bold text-[var(--primary-foreground)] [background:var(--ops-accent)]">Current</span>}
        </div>
        <a id={moduleRowLinkId(row.id)} href={row.href} data-row-link onClick={() => rememberModuleRow(row.id)} aria-label={`Open module ${row.label} · ${title}`}
          className="break-words text-[15px] font-semibold leading-[1.4] text-foreground no-underline [text-wrap:pretty] hover:text-foreground focus-visible:outline-none!">{title}</a>
      </div>
      <div className="flex min-w-0 flex-[1_1_13.75rem] flex-col gap-1">
        {row.cells.length > 0 && <div role="toolbar" aria-label={`Steps of ${row.label}, ${toGoText(row)}`} className="flex min-w-0 gap-[3px]">
          {row.cells.map((cell, i) => {
            const running = live.get(cell.id)
            const noRule = noRuleMarked(cell)
            return <button key={cell.id} type="button" ref={el => { buttons.current[i] = el }}
              tabIndex={i === tabStop ? 0 : -1} aria-label={`Step ${cell.label}: ${spokenState(cell)}${running ? `, ${liveSpoken(running)}` : ''}${noRule ? ', no done rule' : ''}`} aria-expanded={open === cell.id}
              aria-controls={open === cell.id ? detailId(cell) : undefined}
              title={[cell.label, cellLabels[cell.state], noRule && 'No done rule', cell.title, running && `Live now: ${running.what}`, cell.since && cell.now && `active since ${cell.since}`].filter(Boolean).join(' · ')}
              data-step-cell={cell.label} data-state={cell.state} data-live={running ? running.quiet ? 'quiet' : 'true' : undefined} data-no-rule={noRule || undefined}
              onClick={() => { setFocus(i); setOpen(open === cell.id ? '' : cell.id) }} onKeyDown={e => onKey(e, i)} onFocus={() => setFocus(i)}
              className={`relative flex min-h-6 min-w-0 flex-1 cursor-pointer items-center border-0 border-b-2 bg-transparent p-0 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--ring)] pointer-coarse:min-h-11 ${open === cell.id ? 'border-[var(--foreground)]' : 'border-transparent'}`}>
              <span aria-hidden="true" className="block h-[7px] w-full rounded-[4px]" style={cellStyle[cell.state]} />
              {/* Work running now: a slow breathing glow around the bar, still and faint for a landing gone quiet (opsWorkspace.css;
                  still under reduced motion). */}
              {/* A done rule {} on an active or stopped step: a notch over the bar's end (the legend names it). */}
              {noRule && <span aria-hidden="true" data-no-rule-mark className="pointer-events-none absolute right-0 top-0.5 size-[5px] rounded-full bg-[var(--ops-attention-text)]" />}
              {running && <span aria-hidden="true" data-live-ring className={`work-live-ring${running.quiet ? ' work-live-ring--quiet' : ''} pointer-events-none absolute inset-x-0 top-1/2 h-[7px] -translate-y-1/2 rounded-[4px]`} />}
            </button>
          })}
        </div>}
        <div className="grid min-w-0 grid-cols-[auto_minmax(0,1fr)] gap-x-2 gap-y-px text-xs leading-[1.45]">
          <span className="text-muted-foreground">Runtime</span>
          <span className="min-w-0"><strong data-to-go className="font-semibold">{toGoText(row)}</strong> · <span className="font-mono">{counts}</span></span>
        </div>
      </div>
      <div className="flex min-w-0 flex-[1_1_11.25rem] flex-col gap-0.5 text-[13px] leading-[1.45]">
        <span className="text-xs text-muted-foreground">Waits on</span>
        {row.waits.length > 0 ? <ul aria-label={`${row.label} waits on`} className="m-0 flex list-none flex-wrap gap-x-3 p-0">{row.waits.map(w => <li key={w.label}>
          <a href={w.href} title={w.reason} className="inline-flex min-h-6 items-center underline-offset-2 hover:underline pointer-coarse:min-h-11">{w.label}</a>{w.status === 'Blocked' && <span> · Blocked</span>}
        </li>)}</ul>
          // waiting_on holds only the adopted view's blocking edges: none means no dependency waits, not that nothing
          // blocks; and only for an item the view document lists (row.inView), else its dependencies are not known.
          : <span>{row.inView ? 'No dependency waiting' : `Dependencies Unknown · ${noView}`}</span>}
        {row.waits.some(w => w.status === 'Unknown') && <span className="text-xs text-muted-foreground">Milestone verification Unknown</span>}
      </div>
      <span aria-hidden="true" className="flex-none text-lg leading-none text-muted-foreground">›</span>
    </div>
    {opened && <StepDetail cell={opened} close={close} />}
  </li>
}

/**
 * Now: the steps with work running now (R5:5.5-8, M-267), up to NOW_SHOWN of them (the moving ones first, then the rows'
 * order), each opening its module as the row's link does; nothing running is said only of a plan view read now, else
 * what runs now is Unknown. What the Live tab lists that marks no step here is counted with a link to it, so that
 * "Nothing running now" never stands beside work Live still shows.
 */
function NowLine({ live, now, root }: { live: LiveNow; now: number; root: string }) {
  const label = <span className="text-xs font-semibold uppercase tracking-[.08em] text-muted-foreground">Now</span>
  if (live.state === 'unknown') return <p data-live-now="unknown" className="m-0 px-1 text-sm [overflow-wrap:anywhere]">
    {label} · Unknown: {live.reason}, {sinceWords(live.asOf, now)} old. What runs now is not known from it.</p>
  const unmarked = live.unmarked > 0 && <a href={workTabHref('live', root, true)} data-live-unmarked
    className="inline-flex min-h-6 items-center text-xs text-muted-foreground underline-offset-2 hover:underline pointer-coarse:min-h-11">
    {live.unmarked} registered on Live not marked here: heartbeat unconfirmed, not started, or on no step shown</a>
  if (live.steps.length === 0) return <div data-live-now="none" className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 px-1 text-sm">
    <span>{label} · Nothing running now <span className="text-xs text-muted-foreground">· no running attempt, moving landing or running external work on a step</span></span>
    {unmarked}</div>
  const more = live.steps.length - NOW_SHOWN
  return <div data-live-now="live" className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 px-1 text-sm">
    {label}
    <ul aria-label="Running now" className="m-0 flex min-w-0 list-none flex-wrap gap-x-3 gap-y-1 p-0">
      {live.steps.slice(0, NOW_SHOWN).map(step => <li key={step.id} className="min-w-0">
        <a href={step.href} onClick={() => rememberModuleRow(step.row)} data-live-step={step.label}
          className="inline-flex min-h-6 items-center gap-1.5 underline-offset-2 [overflow-wrap:anywhere] hover:underline pointer-coarse:min-h-11">
          <span aria-hidden="true" className={`work-live-dot${step.quiet ? ' work-live-dot--quiet' : ''} shrink-0`} />{liveLine(step, now)}</a>
      </li>)}
    </ul>
    {more > 0 && <span className="text-xs text-muted-foreground">+{more} more live step{more === 1 ? '' : 's'} in the rows below</span>}
    {unmarked}
  </div>
}

/** Work → Plan: every module (plan item) as a row of its runtime steps and how many are left (Design v3.6 WorkPage, DATA-MAP P25). */
export function PlanModuleRows({ view, selected, failed = null, runtime, asks }: { view: PlanSnapshot; selected: string
  /** Why the plan view's latest refresh failed (or Offline); null while it did not. */
  failed?: string | null
  /** The runtime's tasks from the shell's cached GET /progress, for a running task's role; none read when absent. */
  runtime?: RuntimeTasks
  /** The open questions and requests from the shell's cache (R5:5.5-10 slice 2); none read when absent. */
  asks?: OwnerAsks }) {
  const now = useNow()
  // Each active step by what it has now (R5:5.5-10), at the same clock and read as what runs now (R5:5.5-8).
  const rows = stepRows(view, { failed, now, runtime, asks })
  const live = liveNow(view, rows, { failed, now, runtime })
  const liveSteps = live.state === 'ok' ? live.byStep : new Map<string, LiveStep>()
  const step = view.nodes.find(n => n.node_kind === 'step' && (n.plan_node_id === selected || n.label === selected))?.plan_node_id ?? ''
  const total = rows.reduce((sum, r) => sum + r.total, 0), done = rows.reduce((sum, r) => sum + r.done, 0)
  const noView = noViewReason(view)
  return <section aria-label="Modules" className="flex min-w-0 flex-col gap-2 rounded-[20px] border border-[var(--ops-edge)] p-3.5 shadow-[var(--ops-shadow)] [background:var(--ops-panel)]">
    <header className="flex min-w-0 flex-wrap items-baseline justify-between gap-x-3 gap-y-1 px-1">
      <h3 className="m-0 text-base font-semibold">Modules · {view.root}</h3>
      <p className="m-0 text-sm text-muted-foreground">{rows.length} module{rows.length === 1 ? '' : 's'} · {done} / {total} steps done · {toGoText({ total, toGo: total - done })}</p>
    </header>
    <NowLine live={live} now={now} root={view.root} />
    <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 px-1 text-xs text-muted-foreground">
      {legend.map(s => <span key={s} data-legend={s} className="inline-flex items-center gap-1"><span aria-hidden="true" className="inline-block h-[7px] w-5 rounded-[4px]" style={cellStyle[s]} />{cellLabels[s]}{s === 'idle' ? ' (idle)' : s === 'unknown' ? ' (no state)' : ''}</span>)}
      <span className="inline-flex items-center gap-1"><span aria-hidden="true" className="inline-block size-[5px] rounded-full bg-[var(--ops-attention-text)]" />No done rule (active or stopped)</span>
      <span>Execution progress: one cell per runtime step in plan order, from the controller's step states; an active step by what the plan view, read now, shows on it: in progress while work on it runs or is queued, waiting on you while something on it is only yours to move (a task awaiting your decision, a landing awaiting your grant, an acceptance sign-off Ready, an open question on its tasks or named by its done rule, a request its done rule names awaiting your decision), held while a task on it waits on a decision, else active with nothing running, or yours for an Owner step; Unknown now while the read is old, a run on it cannot be confirmed, or the verification reading of submitted external work on it is stale. Not acceptance, not deployment. Dropped steps are left out. A cell that glows has work running now; a faint still ring, a landing with no record for over 30 min.</span>
    </div>
    {rows.length === 0 ? <p className="m-0 px-1">No plan items in {view.root}.</p>
      : <ol aria-label="Modules in mainline order" className="m-0 flex min-w-0 list-none flex-col gap-2 p-0">
        {rows.map(row => <Module key={row.id} row={row} initial={step} noView={noView} live={liveSteps} />)}
      </ol>}
  </section>
}
