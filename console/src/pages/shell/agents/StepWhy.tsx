import { useState } from 'react'
import { clockWords, EXPLAIN_OLD_MS, flatRows, SHOWN, type ExplainBanner, type PushKind, type StepWhyModel, type WhyEntry, type WhyRow } from './explainModel'
import { cellLabels, howRead, ruleSentence, type StepCell } from './planModuleModel'
import { sinceWords } from './readAge'

/*
 * Why a step in progress is not done (R5:5.5-20, Design v3.15 ItemDetail Stages): a bold conclusion and one reason, then
 * "In the way · N" as rows. A step with work running keeps its live state on top and says what is left at its bottom,
 * folded. On the round's rows (compact) only the conclusion, with the module page's step card for the rows. Every row
 * wraps (no fixed width beside its kind label), so nothing scrolls sideways at a phone's width.
 */

/** Design v3.15 push pills: Moving info, Quiet yellow, Unconfirmed dashed grey, Changed grey (a currentColor capsule); Ops tokens only. */
const PUSH_LOOK: Record<PushKind, string> = {
  moving: 'text-[var(--color-info)] [background:color-mix(in_srgb,var(--color-info)_14%,transparent)]',
  quiet: 'text-[var(--color-lamp-yellow)] [background:color-mix(in_srgb,var(--color-lamp-yellow)_14%,transparent)]',
  unconfirmed: 'border border-dashed border-[var(--color-lamp-gray)] text-muted-foreground',
  changed: 'border border-current text-muted-foreground',
  queued: 'text-foreground [background:var(--control-fill)]',
  none: '',
}
const caps = 'text-[10px] font-bold uppercase tracking-[.08em] text-muted-foreground'
const mono = 'font-mono text-[11px] leading-[1.4] text-muted-foreground [overflow-wrap:anywhere]'
const link = 'underline-offset-2 hover:underline pointer-coarse:inline-flex pointer-coarse:min-h-11 pointer-coarse:items-center'

/** One row: its kind, what it is with its ids under it, then its record word, who is pushing it and since when. */
function Row({ row, now }: { row: WhyRow; now: number }) {
  return <li data-why-row={row.kind} data-push={row.push.kind}
    className={`grid min-w-0 grid-cols-[3.625rem_minmax(0,1fr)] items-baseline gap-x-2.5 gap-y-0.5 border-t border-[var(--table-rule)] py-2 ${row.child ? 'ml-3 border-l-2 border-l-[var(--table-rule)] pl-2' : 'px-0.5'}`}>
    <span className={caps}>{row.kind}</span>
    <span className="flex min-w-0 flex-wrap items-baseline gap-x-2.5 gap-y-1">
      <span className="flex min-w-0 flex-[1_1_12.5rem] flex-col gap-0.5">
        {row.href ? <a href={row.href} data-why-what className={`text-[13px] font-semibold leading-[1.4] text-[var(--primary)] [overflow-wrap:anywhere] ${link}`}>{row.what}</a>
          : <span data-why-what className="text-[13px] font-semibold leading-[1.4] [overflow-wrap:anywhere]">{row.what}</span>}
        <span data-why-sub className={mono}>{row.sub}</span>
      </span>
      <span className="flex min-w-0 flex-[0_1_auto] flex-wrap items-center gap-x-2 gap-y-1">
        {row.record && <span data-why-record className="text-xs font-semibold">{row.record}</span>}
        {row.request && <a href={row.request.href} data-why-request className={`text-xs font-semibold ${link}`}>{row.request.text}</a>}
        {row.push.word && <span data-why-push={row.push.kind} className={`inline-flex min-h-5 items-center whitespace-nowrap rounded-full px-[7px] text-[11px] font-semibold ${PUSH_LOOK[row.push.kind]}`}>{row.push.word}</span>}
        {row.since && <time dateTime={row.since} data-why-since className="whitespace-nowrap font-mono text-[11px] text-muted-foreground">since {clockWords(row.since, now)}</time>}
      </span>
    </span>
  </li>
}

/**
 * What the list shows of its entries: every one, or (folded) its first rows counted across every group and depth. A
 * group keeps its heading with all its paths counted; a path keeps its title and says how many of its rows are folded;
 * a path with none shown is named under its group, so a folded row can be told to its path.
 */
type Shown = { kind: 'row'; row: WhyRow }
  | { kind: 'any'; of: number; paths: { n: number; entries: Shown[]; folded: number }[]; hidden: { n: number; rows: number }[] }
const rowsOf = (entries: WhyEntry[]) => flatRows(entries).length
function fold(entries: WhyEntry[], budget: { left: number }): Shown[] {
  const out: Shown[] = []
  for (const e of entries) {
    if (budget.left <= 0) break
    if (e.kind === 'row') { out.push(e); budget.left--; continue }
    const paths: { n: number; entries: Shown[]; folded: number }[] = [], hidden: { n: number; rows: number }[] = []
    e.paths.forEach((path, i) => {
      if (budget.left <= 0) { hidden.push({ n: i + 1, rows: rowsOf(path) }); return }
      const before = budget.left
      const shown = fold(path, budget)
      paths.push({ n: i + 1, entries: shown, folded: rowsOf(path) - (before - budget.left) })
    })
    out.push({ kind: 'any', of: e.paths.length, paths, hidden })
  }
  return out
}
const more = (n: number) => `${n} more ${n === 1 ? 'row' : 'rows'}`

/** The rows an entry shows: a row, or "Any one of · N paths" with each path indented under it, its own entries at any depth. */
function Entry({ entry, now }: { entry: Shown; now: number }) {
  if (entry.kind === 'row') return <Row row={entry.row} now={now} />
  return <li data-why-any className="flex min-w-0 flex-col gap-1.5 border-t border-[var(--table-rule)] py-2">
    <span className={caps}>Any one of · {entry.of} paths</span>
    <ol className="m-0 flex min-w-0 list-none flex-col gap-1.5 p-0">{entry.paths.map(path =>
      <li key={path.n} data-why-path={path.n} className="flex min-w-0 flex-col border-l-2 border-[var(--table-rule)] pl-2.5">
        <span className="py-0.5 text-xs font-semibold">Path {path.n}</span>
        {path.entries.length === 0 && path.folded === 0 ? <span className="text-xs text-muted-foreground">Nothing named on this path</span>
          : <ul className="m-0 flex min-w-0 list-none flex-col p-0">{path.entries.map((e, j) => <Entry key={j} entry={e} now={now} />)}</ul>}
        {path.folded > 0 && <span data-why-path-folded className="pt-0.5 text-xs text-muted-foreground">{more(path.folded)} on this path, under Show all</span>}
      </li>)}
      {entry.hidden.map(h => <li key={h.n} data-why-path-hidden={h.n} className="border-l-2 border-[var(--table-rule)] pl-2.5 text-xs text-muted-foreground">
        <span className="font-semibold">Path {h.n}</span> · {h.rows === 0 ? 'nothing named' : `${h.rows} ${h.rows === 1 ? 'row' : 'rows'}, under Show all`}</li>)}
    </ol>
  </li>
}

/**
 * "In the way · N": its read time at the right (yellow, with its age and version, while it is not confirmed now), the
 * rule in words (the rule of the version read: the plan view holds only its own, so another version's is said not
 * available), the rows (over SHOWN + 1 of them, counted inside every "any one of" and at every depth, the first SHOWN
 * and "Show all N" in place, the groups and paths they sit in kept), and how many records the server did not list, the
 * same total as the heading's.
 */
function WhyList({ why, doneWhen, now }: { why: StepWhyModel; doneWhen: string; now: number }) {
  const [all, setAll] = useState(false)
  const list = why.list!
  // Folded by rows, not entries: a group of any number of rows is no one row (R3: over SHOWN + 1 rows, the first SHOWN).
  const long = list.rows > SHOWN + 1
  const shown = fold(list.entries, { left: long && !all ? SHOWN : Infinity })
  const old = why.readAt !== null && now - Date.parse(why.readAt) > EXPLAIN_OLD_MS
  const otherVersion = why.version !== null && why.version !== why.stepVersion
  const rule = otherVersion ? `rule of version ${why.version} not available (this plan view holds version ${why.stepVersion}'s)` : doneWhen
  return <section aria-label="In the way" data-why-list className="flex min-w-0 flex-col">
    <div className="flex min-w-0 flex-wrap items-baseline gap-x-2.5 gap-y-1 pb-1">
      <h5 className={`m-0 ${caps}`} data-why-count>In the way · {list.rows}{list.omitted > 0 && ` + ${list.omitted} not listed`}</h5>
      {why.readAt && <span data-why-read={why.confirmed ? 'now' : 'unconfirmed'} className={`ml-auto font-mono text-[11px] ${why.confirmed ? 'text-muted-foreground' : 'font-semibold text-[var(--color-lamp-yellow)]'}`}>
        read {clockWords(why.readAt, now)}{old && ` · ${sinceWords(Date.parse(why.readAt), now)} old`}{otherVersion && ` · version ${why.version}`}</span>}
    </div>
    {rule && <p data-why-done-when className="m-0 pb-1 text-xs leading-[1.45] text-muted-foreground [text-wrap:pretty]">Done when: {rule}</p>}
    <ul className="m-0 flex min-w-0 list-none flex-col p-0">{shown.map((e, i) => <Entry key={i} entry={e} now={now} />)}</ul>
    {long && <button type="button" data-why-all aria-expanded={all} onClick={() => setAll(v => !v)}
      className="min-h-9 w-full border-0 border-t border-[var(--table-rule)] bg-transparent px-0.5 text-left text-[13px] font-semibold text-[var(--primary)] pointer-coarse:min-h-11">
      {all ? 'Show fewer' : `Show all ${list.rows}`}{!all && <span className="sr-only">, {more(list.rows - SHOWN)}</span>}</button>}
    {list.omitted > 0 && <p data-why-omitted className="m-0 border-t border-dashed border-[var(--border)] pt-1.5 text-xs leading-[1.45] text-muted-foreground">
      {list.omitted} more not listed by the server.</p>}
  </section>
}

/** The conclusion: the bold word and its reason (muted when it is about the read; on the attention tone while not confirmed now). */
function Conclusion({ why, lead = '' }: { why: StepWhyModel; lead?: string }) {
  return <p data-why-verdict={why.word} data-why-confirmed={why.confirmed} className={`m-0 text-sm leading-[1.5] [text-wrap:pretty] ${why.confirmed
    ? why.muted ? 'text-muted-foreground' : ''
    : 'rounded-[10px] px-2.5 py-1.5 text-[var(--color-lamp-yellow)] [background:color-mix(in_srgb,var(--color-lamp-yellow)_12%,transparent)]'}`}>
    {lead}<b className="font-[650]">{why.word}</b> {why.reason}</p>
}

/**
 * Why an active step is not done (R1–R5, R7). In a step card: the conclusion, then what is in the way; while work runs,
 * one folded "To finish · N in the way" line at the bottom instead. compact (the round's rows): the conclusion alone, and
 * a link to the step card that lists the rows. Nothing for a step not active, or when nothing is said of it.
 */
export function StepWhy({ cell, now, compact = false, cardHref }: { cell: StepCell
  /** The page clock the step was judged at (its times are said against it). */
  now: number; compact?: boolean
  /** The module page at this step, where the step card lists the rows (compact only). */
  cardHref?: string }) {
  const [open, setOpen] = useState(false)
  const why = cell.why
  if (cell.step !== 'active' || !why) return null
  const running = cell.now?.kind === 'running'
  const doneWhen = ruleSentence(cell.rule)
  if (compact) return <div data-step-why="compact" className="flex min-w-0 flex-col gap-0.5 text-[13px] leading-[1.45] text-muted-foreground">
    <Conclusion why={why} lead={running ? 'To finish: ' : ''} />
    {why.list && cardHref && <a href={cardHref} data-why-card className={`self-start text-xs ${link}`}>In the way · {why.list.rows}{why.list.omitted > 0 && ` + ${why.list.omitted} not listed`} on the step card</a>}
  </div>
  if (running) {
    if (!why.list) return <div data-step-why="running" className="flex min-w-0 flex-col gap-1 border-t border-[var(--table-rule)] pt-1.5 text-[13px] text-muted-foreground">
      <Conclusion why={why} lead="To finish: " /></div>
    return <div data-step-why="running" className="flex min-w-0 flex-col gap-1">
      <button type="button" data-why-finish aria-expanded={open} onClick={() => setOpen(v => !v)}
        className="flex min-h-9 w-full min-w-0 items-center gap-2 border-0 border-t border-[var(--table-rule)] bg-transparent px-0.5 text-left text-[13px] text-muted-foreground pointer-coarse:min-h-11">
        <span className="min-w-0 flex-1 [overflow-wrap:anywhere]">To finish · {why.list.rows} in the way{why.list.omitted > 0 && ` + ${why.list.omitted} not listed`}{!why.confirmed && why.readAt && ` · as of ${clockWords(why.readAt, now)}`}</span>
        <span aria-hidden="true" className="w-4 text-center text-[15px]">{open ? '−' : '+'}</span>
      </button>
      {/* A read not confirmed now says so folded too: its conclusion stays outside the fold. */}
      {(open || !why.confirmed) && <Conclusion why={why} />}
      {open && <WhyList why={why} doneWhen={doneWhen} now={now} />}
    </div>
  }
  return <div data-step-why="full" className="flex min-w-0 flex-col gap-1.5">
    <Conclusion why={why} />
    {why.list && <WhyList why={why} doneWhen={doneWhen} now={now} />}
  </div>
}

/** "How “state” was read" (R6): folded; open, the four rows Running · Queued · Waits on you · Held, read at asOf. */
export function HowRead({ cell, asOf, now }: { cell: StepCell; asOf: string; now: number }) {
  const [open, setOpen] = useState(false)
  const rows = howRead(cell)
  if (!rows) return null
  return <div data-step-read={cell.now?.kind} className="flex min-w-0 flex-col">
    <button type="button" aria-expanded={open} onClick={() => setOpen(v => !v)}
      className="flex min-h-9 w-full min-w-0 items-center gap-2 border-0 border-t border-[var(--table-rule)] bg-transparent px-0.5 text-left text-xs text-muted-foreground pointer-coarse:min-h-11">
      <span className="min-w-0 flex-1 [overflow-wrap:anywhere]">How “{cellLabels[cell.state]}” was read · {clockWords(asOf, now)}</span>
      <span aria-hidden="true" className="w-4 text-center text-[15px]">{open ? '−' : '+'}</span>
    </button>
    {open && <dl className="m-0 grid min-w-0 grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 px-0.5 pb-0.5 pt-1 text-xs leading-[1.45]">
      {rows.map(r => <div key={r.label} data-read-row={r.label} className="contents">
        <dt className="whitespace-nowrap text-muted-foreground">{r.label}</dt><dd className="m-0 min-w-0 [overflow-wrap:anywhere]">{r.text}</dd></div>)}
    </dl>}
  </div>
}

/** The line above a module's steps while their read cannot be used as now (Design v3.15 Stages banner), with Read again on a failure. */
export function WhyBanner({ banner, readAgain }: { banner: ExplainBanner; readAgain?: () => void }) {
  const look = banner.kind === 'old' ? '[background:color-mix(in_srgb,var(--color-lamp-yellow)_12%,transparent)]'
    : banner.kind === 'changed' ? '[background:color-mix(in_srgb,var(--color-info)_12%,transparent)]' : 'border border-dashed border-[var(--color-lamp-gray)]'
  return <div role="status" data-why-banner={banner.kind} className={`flex min-w-0 flex-wrap items-start gap-x-2.5 gap-y-1.5 rounded-xl px-3 py-2 ${look}`}>
    <span className="min-w-0 flex-[1_1_15rem] text-[13px] leading-[1.5] [overflow-wrap:anywhere]">{banner.text}</span>
    {banner.kind === 'failed' && readAgain && <button type="button" onClick={readAgain}
      className="inline-flex min-h-9 flex-none items-center rounded-[10px] border border-[var(--ops-edge)] px-3 text-xs font-semibold shadow-[var(--ops-raised)] [background:var(--ops-control)] pointer-coarse:min-h-11">Read again</button>}
  </div>
}
