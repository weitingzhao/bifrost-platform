import { useRef, useState, type KeyboardEvent } from 'react'
import type { PlanSnapshot } from '@/api/planSnapshot'
import { Card } from '../owner/OwnerParts'
import { StepBadge } from './StepText'
import { cellLabels, cellStyle, cellWords, moduleRowLinkId, moduleRows, toGoText, type CellState, type ModuleRow, type RuleLine, type StepCell } from './planModuleModel'

const legend: CellState[] = ['done', 'active', 'planned', 'stopped', 'unknown']
const detailId = (cell: StepCell) => `module-step-${cell.id}`

function Rule({ lines }: { lines: RuleLine[] }) {
  return <ul className="m-0 list-disc space-y-1 pl-5">{lines.map((line, i) => <li key={i}>{line.text}{line.children && <Rule lines={line.children} />}</li>)}</ul>
}

function StepDetail({ cell, close }: { cell: StepCell; close: () => void }) {
  return <div id={detailId(cell)} role="region" aria-label={`Step ${cell.label}`} onKeyDown={e => { if (e.key === 'Escape') { e.stopPropagation(); close() } }}
    className="flex min-w-0 flex-col gap-2 rounded-[var(--control-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] p-3 text-sm [overflow-wrap:anywhere]">
    <div className="flex min-w-0 flex-wrap items-center gap-2">
      <StepBadge label={cell.label} />
      <strong data-step-state={cell.state}>{cellLabels[cell.state]}</strong>
      {cell.owner && <span className="rounded-full bg-[var(--control-fill)] px-1.5 text-xs font-semibold" title="The Owner's step (owner_step)">Owner</span>}
      {cell.effect && <span className="rounded-full bg-[var(--control-fill)] px-1.5 text-xs font-semibold" title="An effect step (effect_step)">★</span>}
      <button type="button" onClick={close} className="ml-auto inline-flex min-h-11 items-center rounded-[var(--control-radius)] border border-[var(--card-border)] px-3 text-sm">Close</button>
    </div>
    <p className="m-0 break-words">{cell.title || 'Untitled step'}</p>
    {cell.state === 'unknown' && <p className="m-0 text-muted-foreground">Unknown · the runtime holds no state for this step.</p>}
    <div className="flex min-w-0 flex-col gap-1">
      <span className="text-xs text-muted-foreground">Done when</span>
      {cell.rule.length > 0 ? <Rule lines={cell.rule} />
        : <p className="m-0">No done rule yet. The runtime does not move this step on evidence; it changes only by a recorded event.</p>}
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

function Module({ row, initial }: { row: ModuleRow; initial: string }) {
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
  const counts = [`${row.done} / ${row.total} done`, row.active && `${row.active} in progress`, row.stopped && `${row.stopped} stopped`,
    row.unknown && `${row.unknown} unknown`, row.dropped && `${row.dropped} dropped`].filter(Boolean).join(' · ')
  return <li data-module={row.label} aria-current={row.current ? 'step' : undefined}
    className={`flex min-w-0 flex-col gap-2 rounded-[var(--card-radius)] border p-3 [overflow-wrap:anywhere] ${row.current ? 'border-[var(--color-info)] bg-[var(--control-fill)]' : 'border-[var(--card-border)] bg-[var(--card-fill)]'}`}>
    <div className="flex min-w-0 flex-wrap items-start gap-x-4 gap-y-2">
      <div className="flex min-w-0 flex-[1_1_16rem] flex-col gap-1">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <a id={moduleRowLinkId(row.id)} href={row.href} className="inline-flex min-h-6 items-center font-mono pointer-coarse:min-h-11 text-sm font-semibold underline-offset-2 hover:underline" aria-label={`Open module ${row.label}`}>{row.label}</a>
          <span className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{row.lane}</span>
          {row.current && <span className="rounded-full border border-[var(--color-info)] px-2 text-xs font-semibold">Current</span>}
        </div>
        <span className="break-words font-semibold">{row.title}</span>
      </div>
      <div className="flex min-w-0 flex-[1_1_14rem] flex-col gap-1">
        <div className="flex min-w-0 flex-wrap items-baseline justify-between gap-x-2">
          <span className="text-xs text-muted-foreground">Runtime steps</span>
          <strong data-to-go className="text-sm">{toGoText(row)}</strong>
        </div>
        {row.cells.length > 0 && <div role="toolbar" aria-label={`Steps of ${row.label}, ${toGoText(row)}`} className="flex min-w-0 gap-[3px]">
          {row.cells.map((cell, i) => <button key={cell.id} type="button" ref={el => { buttons.current[i] = el }}
            tabIndex={i === tabStop ? 0 : -1} aria-label={`Step ${cell.label}: ${cellWords[cell.state]}`} aria-expanded={open === cell.id}
            aria-controls={open === cell.id ? detailId(cell) : undefined} title={`${cell.label} · ${cellLabels[cell.state]} · ${cell.title}`} data-step-cell={cell.label} data-state={cell.state}
            onClick={() => { setFocus(i); setOpen(open === cell.id ? '' : cell.id) }} onKeyDown={e => onKey(e, i)} onFocus={() => setFocus(i)}
            className={`flex min-h-6 min-w-0 flex-1 cursor-pointer items-center border-0 border-b-2 bg-transparent p-0 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--ring)] pointer-coarse:min-h-11 ${open === cell.id ? 'border-[var(--foreground)]' : 'border-transparent'}`}>
            <span aria-hidden="true" className="block h-2 w-full rounded-full" style={cellStyle[cell.state]} />
          </button>)}
        </div>}
        <span className="text-xs text-muted-foreground">{counts}</span>
      </div>
      <div className="flex min-w-0 flex-[1_1_11rem] flex-col gap-1 text-sm">
        <span className="text-xs text-muted-foreground">Waits on</span>
        {row.waits.length === 0 ? <span>Nothing blocking</span>
          : <ul aria-label={`${row.label} waits on`} className="m-0 flex list-none flex-wrap gap-x-3 p-0">{row.waits.map(w => <li key={w.label}>
            <a href={w.href} title={w.reason} className="inline-flex min-h-6 items-center underline-offset-2 hover:underline pointer-coarse:min-h-11">{w.label}</a>{w.status === 'Blocked' && <span> · Blocked</span>}
          </li>)}</ul>}
        {row.waits.some(w => w.status === 'Unknown') && <span className="text-xs text-muted-foreground">Milestone verification Unknown</span>}
      </div>
    </div>
    {opened && <StepDetail cell={opened} close={close} />}
  </li>
}

/** Work → Plan: every module (plan item) as a row of its runtime steps and how many are left (Design v3.6 WorkPage, DATA-MAP P25). */
export function PlanModuleRows({ view, selected }: { view: PlanSnapshot; selected: string }) {
  const rows = moduleRows(view)
  const step = view.nodes.find(n => n.node_kind === 'step' && (n.plan_node_id === selected || n.label === selected))?.plan_node_id ?? ''
  const total = rows.reduce((sum, r) => sum + r.total, 0), done = rows.reduce((sum, r) => sum + r.done, 0)
  return <Card label="Modules" title={`Modules · ${view.root}`}>
    <p className="m-0 text-sm">{rows.length} module{rows.length === 1 ? '' : 's'} · {done} / {total} steps done · {toGoText({ total, toGo: total - done })}</p>
    <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
      {legend.map(s => <span key={s} className="inline-flex items-center gap-1"><span aria-hidden="true" className="inline-block h-2 w-5 rounded-full" style={cellStyle[s]} />{cellLabels[s]}</span>)}
    </div>
    <p className="m-0 text-xs text-muted-foreground">Execution progress: one cell per runtime step in plan order, from the controller's step states. Not acceptance, not deployment. Dropped steps are left out.</p>
    {rows.length === 0 ? <p className="m-0">No plan items in {view.root}.</p>
      : <ol aria-label="Modules in mainline order" className="m-0 flex min-w-0 list-none flex-col gap-2 p-0">
        {rows.map(row => <Module key={row.id} row={row} initial={step} />)}
      </ol>}
  </Card>
}
