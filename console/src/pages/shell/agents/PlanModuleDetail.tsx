import { useEffect, useMemo, useRef, useState, type ReactNode, type RefObject } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchRuntimeProgress } from '@/api/agentRuntime'
import { fetchApprovalList } from '@/api/approvals'
import type { PlanNode, PlanSnapshot, PlanSnapshotRead } from '@/api/planSnapshot'
import { fetchQuestionList } from '@/api/questions'
import { NEEDS_YOU_HREF } from '@/lib/shell/consoleRoutes'
import { useNow } from '../progress/progressClock'
import { DeliveryStrip } from './DeliveryStrip'
import { WorkRow } from './PlanLiveWork'
import { StepBadge } from './StepText'
import { acceptanceOf, actionsLeft, approvalActivity, dependentsOf, estimateOf, isStale, liveOn, mainlineOf, mergeActivity, moduleDetailId, moduleScopes, moduleStepRowId,
  needsNow, needsYouCards, nextInPlanOrder, nowSteps, planActivity, questionActivity, reviewWords, runtimeTasks, SHELL_POLL_MS, shellRead, stepTasks, taskActivity, taskLine, workTasks,
  type ModuleScope, type RuntimeTasks, type ShellRead, type ShellReads, type TaskLine } from './moduleDetailModel'
import { activeCounts, cellLabels, cellStyle, judgeRow, NO_RULE_PILL, noRuleMarked, noRuleText, noViewReason, nowLines, spokenState, toGoText, unverifiedNote, type CellState, type ModuleRow,
  type RuleLine, type StepCell } from './planModuleModel'
import { roundLabel, workRoundHref } from './planRoundModel'
import { responsibility, validity } from './planSnapshotModel'
import { explainBanner } from './explainModel'
import { viewOldReason } from './readAge'
import { StepSignoffHere } from './StepSignoffHere'
import { HowRead, StepWhy, WhyBanner } from './StepWhy'
import { stepJudge, withWhy } from './stepStateModel'
import { planNodeHref } from './usePlanSnapshot'

/** Recent activity shows this many rows until the Owner asks for all. */
const ACTIVITY_ROWS = 12

/**
 * The four lists the shell already polls on every Console page (ConsolePage → useOwnerYours → useNeedsYou: questions
 * and requests every 30 s, the runtime's progress every 5 s), read here from that cache with the same keys and never
 * fetched: enabled is false, so the detail adds no request (the 10-31 PROD platform-api OOM came from Work's reads).
 */
function useShellReads(): ShellReads {
  const questions = useQuery({ queryKey: ['questions', 'all'], queryFn: () => fetchQuestionList('all'), enabled: false, retry: false })
  const open = useQuery({ queryKey: ['approvals', 'open'], queryFn: () => fetchApprovalList('open'), enabled: false, retry: false })
  const all = useQuery({ queryKey: ['approvals', 'all'], queryFn: () => fetchApprovalList('all'), enabled: false, retry: false })
  const progress = useQuery({ queryKey: ['agent-runtime-progress'], queryFn: ({ signal }) => fetchRuntimeProgress(signal), enabled: false, retry: false })
  return { questions: shellRead(questions), open: shellRead(open), all: shellRead(all), progress: shellRead(progress) }
}
const SOURCES: [keyof ShellReads, string][] = [['questions', 'Questions'], ['open', 'Requests'], ['all', 'Request history'], ['progress', 'Runtime progress']]
const readWords = (read: ShellRead<unknown>, stale: boolean) => read.state === 'ok' ? `${new Date(read.at).toISOString()}${stale ? ' · Stale' : ''}`
  : read.state === 'unknown' ? `Unknown · ${read.reason}` : read.state === 'reading' ? 'reading…' : 'not read yet'

// Design v3.6 ItemDetail look: Ops tokens only (styles/opsAppearance.css).
const panel = 'flex min-w-0 flex-col rounded-[20px] border border-[var(--ops-edge)] p-3.5 shadow-[var(--ops-shadow)] [background:var(--ops-panel)] [overflow-wrap:anywhere]'
const caps = 'm-0 text-xs font-semibold uppercase tracking-[.08em] text-muted-foreground'
const note = 'm-0 text-xs leading-[1.45] text-muted-foreground'
const dashedPill = 'inline-flex h-5 shrink-0 items-center whitespace-nowrap rounded-full border border-dashed border-[var(--color-lamp-gray)] px-[7px] text-[11px] font-semibold text-muted-foreground'

function Big({ children }: { children: ReactNode }) {
  return <strong className="text-2xl font-bold leading-tight">{children}</strong>
}
/** A card's heading row: the caps title, and what it was read from or counted at the right. */
function CardHead({ title, children }: { title: string; children?: ReactNode }) {
  return <div className="flex min-w-0 flex-wrap items-center justify-between gap-x-2 gap-y-1"><h4 className={caps}>{title}</h4>{children}</div>
}
function Who({ owner }: { owner: boolean }) {
  return <span className="shrink-0 rounded-full bg-[var(--card-fill)] px-1.5 text-xs font-semibold" title={owner ? "The Owner's step (owner_step)" : 'Not an Owner step: the crew does it'}>{owner ? 'Owner' : 'Agent'}</span>
}
function Task({ task }: { task: TaskLine }) {
  return <span className="min-w-0">
    {task.role} · <a href={task.href} className="underline-offset-2 hover:underline pointer-coarse:inline-flex pointer-coarse:min-h-11 pointer-coarse:items-center">{task.title}</a>
    {' · '}{task.state}{task.round && ` · ${task.round}`}{task.attempt && ` · ${task.attempt}`}{task.detail && ` · ${task.detail}`}
    {task.waiting && <span className="block text-xs text-muted-foreground">{task.waitingHref ? <a href={task.waitingHref} className="underline-offset-2 hover:underline">{task.waiting}</a> : task.waiting}</span>}
  </span>
}
function Rule({ lines }: { lines: RuleLine[] }) {
  return <ul className="m-0 list-disc space-y-0.5 pl-5">{lines.map((line, i) => <li key={i}>{line.text}{line.children && <Rule lines={line.children} />}</li>)}</ul>
}
const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`

/**
 * A step's state word at the right of its line: the step in progress in the accent, waiting on you filled on the
 * attention tone with a heavy ring, held and the Owner's on the attention tone, an active one with nothing running outlined, an active one Unknown now dashed in the primary tone,
 * stopped in red, unknown (no state) dashed grey.
 */
const STATE_LOOK: Record<CellState, string> = {
  active: 'h-[22px] rounded-full px-2 font-bold text-[var(--primary-foreground)] [background:var(--ops-accent)]',
  waiting: 'h-[22px] rounded-full border-2 border-[var(--ops-attention-text)] px-2 font-bold text-[var(--ops-attention-text)] [background:var(--ops-attention-fill)]',
  held: 'h-[22px] rounded-full border border-[var(--ops-attention-text)] px-2 text-[var(--ops-attention-text)]',
  idle: 'h-[22px] rounded-full border border-[var(--primary)] px-2 text-foreground',
  'active-unknown': 'h-5 rounded-full border border-dashed border-[var(--primary)] px-[7px] text-foreground',
  yours: 'h-[22px] rounded-full px-2 text-[var(--ops-attention-text)] [background:var(--ops-attention-fill)]',
  done: 'text-muted-foreground', planned: 'text-muted-foreground', stopped: 'text-[var(--color-lamp-red)]',
  unknown: 'h-5 rounded-full border border-dashed border-[var(--color-lamp-gray)] px-[7px] text-muted-foreground',
}
function StateWord({ state }: { state: CellState }) {
  return <span data-step-state={state} className={`ml-auto inline-flex shrink-0 items-center whitespace-nowrap text-xs font-semibold ${STATE_LOOK[state]}`}>{cellLabels[state]}</span>
}
/**
 * The timeline's mark for a step: ✓ done, the accent with a halo in progress, the attention tone waiting on you (filled,
 * heavy ring), held (ring) and yours (filled), an empty ring active with nothing running, dashed primary active Unknown now, recessed not started, hatched
 * red stopped, dashed grey unknown.
 */
function Dot({ state }: { state: CellState }) {
  const base = 'mt-[3px] flex size-[18px] flex-none items-center justify-center rounded-full text-[11px] leading-none'
  if (state === 'done') return <span className={`${base} bg-[var(--primary)] text-[var(--primary-foreground)]`}>✓</span>
  if (state === 'active') return <span className={`${base} shadow-[0_0_0_4px_color-mix(in_srgb,var(--primary)_22%,transparent)] [background:var(--ops-accent)]`} />
  if (state === 'waiting') return <span className={`${base} border-[3px] border-[var(--ops-attention-text)] bg-[var(--ops-attention-fill)]`} />
  if (state === 'held') return <span className={`${base} border-2 border-[var(--ops-attention-text)]`} />
  if (state === 'idle') return <span className={`${base} border-2 border-[var(--primary)]`} />
  if (state === 'active-unknown') return <span className={`${base} border-2 border-dashed border-[var(--primary)]`} />
  if (state === 'yours') return <span className={`${base} border-2 border-[var(--ops-attention-text)] bg-[var(--ops-attention-fill)]`} />
  if (state === 'planned') return <span className={`${base} bg-[var(--muted)] shadow-[var(--ops-inset)]`} />
  if (state === 'stopped') return <span className={`${base} border-2 border-[var(--color-lamp-red)]`} style={cellStyle.stopped} />
  return <span className={`${base} border-2 border-dashed border-[var(--color-lamp-gray)]`} />
}

/**
 * One step on the timeline: its state, title, number and who does it; its latest own task and the review of it. The
 * step in progress, and the step an address names, open into a card: the rest of its own work (for a step in
 * progress, this is what runs now), its done rule in words, external work and the records its rule names.
 */
function Stage({ cell, scope, view, runtime, selected, last, now, stale }: { cell: StepCell; scope: ModuleScope; view: PlanSnapshot; runtime: RuntimeTasks; selected: boolean; last: boolean
  now: number; stale: boolean }) {
  // While work on it runs, the live state stays on top and what is left to finish sits at the bottom, folded (R7).
  const running = cell.now?.kind === 'running'
  // Latest task is the newest of all the step's own work (the one Review is of), never an older one. A task the rows in
  // the way show is not named again here (R5:5.5-20: a task's name appears once on the card): the newest so shown leaves
  // out the Latest task line, the others stay earlier work. While those rows are folded (work running), none is left out.
  const named = new Set(running ? [] : cell.why?.tasks ?? [])
  const all = workTasks(scope, cell.id, runtime)
  const checks = stepTasks(scope, cell.id).length - all.length
  const latest = all[0] && !named.has(all[0].task_id) ? taskLine(all[0], runtime) : null
  const earlier = all.slice(1).filter(t => !named.has(t.task_id))
  // Every step the controller has active opens, whatever it has now (in progress, nothing running, yours or Unknown).
  const active = cell.step === 'active'
  const open = active || selected
  const note = active ? unverifiedNote(cell) : null
  const listed = active && !!cell.why?.list
  // The rule's question and the step's goal; tasks are told under Latest task and Review (a task link would list a
  // check as "accepted" beside the work it failed), a request by its number in the rule, the step's own address is here.
  const records = cell.links.filter(l => l.kind === 'question' || l.kind === 'goal')
  const own = <>
    {(latest || all.length === 0) && <dl className="m-0 min-w-0"><dt className="mr-1.5 inline text-muted-foreground">Latest task</dt>
      <dd className="m-0 inline">{latest ? <Task task={latest} /> : 'None'}{all.length > 1 && ` · ${all.length - 1} earlier`}{checks > 0 && ` · ${plural(checks, 'check')}`}
        {latest && <DeliveryStrip task={latest.id} />}</dd></dl>}
    {open && earlier.length > 0 && <div className="flex min-w-0 flex-col gap-0.5">
      <span className="text-xs text-muted-foreground">Earlier own work</span>
      <ul className="m-0 flex list-none flex-col gap-0.5 p-0">{earlier.map(t => <li key={t.task_id}><Task task={taskLine(t, runtime)} /><DeliveryStrip task={t.task_id} /></li>)}</ul>
    </div>}
    {active && all.length === 0 && <span className="text-muted-foreground">No work task on this step yet (checks are under Review)</span>}
  </>
  return <li id={moduleStepRowId(cell.id)} tabIndex={-1} data-step-row={cell.label} data-step-open={open || undefined} aria-current={selected ? 'step' : undefined}
    className="grid min-w-0 scroll-mt-4 grid-cols-[24px_minmax(0,1fr)] gap-2.5 rounded-xl focus:outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--ring)]">
    <span aria-hidden="true" className="flex flex-col items-center">
      <Dot state={cell.state} />
      {!last && <span className="mt-0.5 w-0.5 flex-1" style={{ background: cell.state === 'done' ? 'var(--primary)' : 'var(--border)' }} />}
    </span>
    <div className={`flex min-w-0 flex-col ${open
      ? 'mb-3.5 gap-1.5 rounded-[14px] border-[1.5px] border-[color-mix(in_srgb,var(--primary)_55%,transparent)] px-3 py-2.5 [background:var(--ops-selection)]'
      : `gap-0.5 ${last ? '' : 'pb-3.5'}`}`}>
      <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
        <span className="min-w-0 break-words text-[15px] font-semibold leading-[1.4]">{cell.title || 'Untitled step'}</span>
        <a href={planNodeHref(view.root, cell.id)} aria-label={`Open step ${cell.label}`}
          className="inline-flex min-h-6 items-center font-mono text-xs text-muted-foreground underline-offset-2 hover:underline pointer-coarse:min-h-11">{cell.label}</a>
        <Who owner={cell.owner} />
        {noRuleMarked(cell) && <span data-no-rule className={NO_RULE_PILL} title="Its done rule is {}: nothing closes it by evidence">No done rule</span>}
        <StateWord state={cell.state} />
      </div>
      {active && nowLines(cell).map(line => <p key={line} data-step-now={cell.now?.kind} className="m-0 text-[13px] leading-[1.45] text-muted-foreground">{line}</p>)}
      {active && !running && <StepWhy cell={cell} now={now} />}
      {note && <p data-step-note="unverified" className="m-0 text-xs leading-[1.45] text-muted-foreground">{note}</p>}
      <div className={`flex min-w-0 flex-col gap-0.5 leading-[1.45] ${open ? 'text-sm' : 'text-[13px]'}`}>
        {active ? <div role="group" aria-label={`Now on ${cell.label}`} className="flex min-w-0 flex-col gap-0.5">{own}</div> : own}
        <dl className="m-0 min-w-0"><dt className="mr-1.5 inline text-muted-foreground">Review</dt><dd className="m-0 inline">{reviewWords(scope, view, cell.id, runtime)}</dd></dl>
      </div>
      {/* The rows in the way say the rule in one line ("Done when: …"); without them it is said here. */}
      {open && !listed && <div className="flex min-w-0 flex-col gap-0.5 text-[13px] leading-[1.45]">
        <span className="text-xs text-muted-foreground">Done when</span>
        {cell.rule.length > 0 ? <Rule lines={cell.rule} /> : <p className="m-0" data-no-rule-text>{noRuleText(cell)}</p>}
      </div>}
      {open && <StepSignoffHere node={cell.id} label={cell.label} state={cell.step ?? ''} rule={view.nodes.find(n => n.plan_node_id === cell.id)?.done_rule_json} now={now} viewStale={stale} />}
      {open && cell.external.length > 0 && <div className="flex min-w-0 flex-col gap-0.5 text-[13px]">
        <span className="text-xs text-muted-foreground">External work on this step</span>
        <ul className="m-0 list-none space-y-0.5 p-0">{cell.external.map((line, i) => <li key={i}>{line}</li>)}</ul>
      </div>}
      {open && records.length > 0 && <ul aria-label={`Records of step ${cell.label}`} className="m-0 flex min-w-0 list-none flex-wrap gap-1.5 p-0">
        {records.map(link => <li key={`${link.kind}-${link.href}`}><a href={link.href} data-step-link={link.kind}
          className="inline-flex min-h-8 items-center rounded-[10px] border border-[var(--ops-edge)] px-2.5 text-xs text-foreground no-underline shadow-[var(--ops-raised)] [background:var(--ops-control)] hover:text-foreground pointer-coarse:min-h-11">{link.text}</a></li>)}
      </ul>}
      {active && running && <StepWhy cell={cell} now={now} />}
      {active && <HowRead cell={cell} asOf={view.as_of} now={now} />}
    </div>
  </li>
}

/** A labelled field of the head card. */
function Field({ label, children }: { label: string; children: ReactNode }) {
  return <section aria-label={label} className="flex min-w-0 flex-col gap-1 text-[13px] leading-[1.45]">
    <h4 className="m-0 text-[11px] font-semibold uppercase tracking-[.06em] text-muted-foreground">{label}</h4>
    {children}
  </section>
}

/** The head card (v3.6 item header): which module and where on the mainline, its title, steps to go, now, next and ETA. */
function Head({ view, item, row, scope, runtime, reads, stale, now, heading }: { view: PlanSnapshot; item: PlanNode; row: ModuleRow; scope: ModuleScope; runtime: RuntimeTasks
  reads: ShellReads; stale: boolean; now: number; heading: RefObject<HTMLHeadingElement | null> }) {
  const current = nowSteps(scope, runtime)
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
          : <ul className="m-0 flex list-none flex-col gap-1 p-0">{current.map(s => <li key={s.cell.id} className="flex min-w-0 flex-wrap items-center gap-2">
            <a href={planNodeHref(view.root, s.cell.id)} aria-label={`Step ${s.cell.label} ${spokenState(s.cell)}`} className="inline-flex pointer-coarse:min-h-11 pointer-coarse:items-center"><StepBadge label={s.cell.label} /></a>
            <span className="min-w-0 break-words">{s.cell.title || 'Untitled step'}</span>
            <span data-now-state={s.cell.state} className="text-xs text-muted-foreground">{cellLabels[s.cell.state]}</span></li>)}</ul>}
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

/** v3.6 Acceptance: the adopted view's criteria for the module and what the PV2 readings say of each. */
function Acceptance({ view, item }: { view: PlanSnapshot; item: PlanNode }) {
  const [open, setOpen] = useState<Record<string, boolean>>({})
  const acc = acceptanceOf(view, item)
  const counts = acc.state === 'ok' ? [`${acc.verified} verified`, acc.notVerified > 0 && `${acc.notVerified} not verified`, acc.unknown > 0 && `${acc.unknown} Unknown`].filter(Boolean).join(' · ') : ''
  return <section aria-label={`Acceptance of ${item.label}`} className={`${panel} gap-2.5`}>
    <CardHead title={acc.state === 'ok' ? `Acceptance · ${acc.criteria.length}` : 'Acceptance'}>
      {acc.state === 'ok' ? acc.criteria.length > 0 && <span data-acceptance-count className={dashedPill}>{counts}</span> : <span className={dashedPill}>Unknown</span>}
    </CardHead>
    {acc.state === 'unknown' ? <p className="m-0 text-sm">Acceptance Unknown · {acc.reason}</p>
      : acc.criteria.length === 0 ? <p className="m-0 text-sm">No acceptance criteria for {item.label} in the adopted plan view.</p>
      : <ul className="m-0 flex list-none flex-col gap-2 p-0">{acc.criteria.map(c => <li key={c.id} data-criterion={c.id} data-verdict={c.verdict}
        className="flex min-w-0 flex-col gap-1.5 rounded-[14px] px-3 py-2.5 [background:var(--ops-selection)]">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <span className="font-mono text-xs text-muted-foreground">{c.milestone}</span>
          <span className="inline-flex h-5 items-center whitespace-nowrap rounded-md border-[1.5px] border-foreground px-1.5 font-mono text-[10px] font-bold uppercase tracking-[.06em]">{c.environment}</span>
          {c.evidence.length > 0 && <button type="button" aria-expanded={!!open[c.id]} onClick={() => setOpen(o => ({ ...o, [c.id]: !o[c.id] }))}
            className="ml-auto inline-flex min-h-8 items-center whitespace-nowrap border-0 bg-transparent p-0 text-xs text-muted-foreground pointer-coarse:min-h-11">
            {c.evidence.length} evidence required {open[c.id] ? '−' : '+'}</button>}
        </div>
        <span className="text-sm font-semibold leading-[1.45]">{c.criterion}</span>
        {c.readings.length === 0 ? <span className="text-xs text-muted-foreground">Not bound to a trusted source · Unknown</span>
          : <ul className="m-0 flex list-none flex-col gap-0.5 p-0 text-xs">{c.readings.map(r => <li key={r.binding}>
            <strong>{validity(r.value)}</strong> · {r.atom} · {r.freshness}{r.reason && ` · ${r.reason}`}{r.last_success && ` · last success ${r.last_success}`}</li>)}</ul>}
        {open[c.id] && <ul aria-label={`Evidence required for ${c.milestone}`} className="m-0 flex list-none flex-col gap-0.5 border-t border-[var(--table-rule)] p-0 pt-1.5 text-[13px] leading-[1.45]">
          {c.evidence.map((e, i) => <li key={i} className="flex gap-2"><span aria-hidden="true" className="text-muted-foreground">○</span>{e}</li>)}</ul>}
      </li>)}</ul>}
    {acc.state === 'ok' && <p className={note}>Criteria and the evidence they require, from the adopted plan view (plan v{view.plan_version}). A criterion reads Verified only when every binding on it reads true (PV2); with no binding it is Unknown, not 0.</p>}
  </section>
}

/** v3.6 Waiting on: what the module waits on (the module rows' rule) and what in this round's view waits on it. */
function Dependencies({ view, item, row }: { view: PlanSnapshot; item: PlanNode; row: ModuleRow }) {
  const dependents = dependentsOf(view, item)
  return <section aria-label={`Dependencies of ${item.label}`} className={`${panel} gap-2.5`}>
    <CardHead title="Dependencies"><span className="font-mono text-[11px] text-muted-foreground">waiting_on · edges</span></CardHead>
    <section aria-label={`${item.label} waits on`} className="flex min-w-0 flex-col gap-1 text-sm">
      <h5 className="m-0 text-xs font-semibold text-muted-foreground">{item.label} waits on</h5>
      {row.waits.length > 0 ? <ul className="m-0 flex list-none flex-col gap-1 p-0">{row.waits.map(w => <li key={w.label} className="flex min-w-0 flex-col">
        <span><a href={w.href} className="inline-flex min-h-6 items-center underline-offset-2 hover:underline pointer-coarse:min-h-11">{w.label}</a>{w.status === 'Blocked' && <span> · Blocked</span>}</span>
        {w.reason && <span className="text-xs text-muted-foreground">{w.reason}</span>}
      </li>)}</ul>
        // waiting_on holds only the adopted view's blocking edges: none means no dependency waits, not that nothing
        // blocks; and only for an item the view document lists (row.inView), else its dependencies are not known.
        : <p className="m-0">{row.inView ? 'No dependency waiting' : `Dependencies Unknown · ${noViewReason(view)}`}</p>}
      {row.waits.some(w => w.status === 'Unknown') && <span className="text-xs text-muted-foreground">Milestone verification Unknown</span>}
    </section>
    <section aria-label={`Waits on ${item.label}`} className="flex min-w-0 flex-col gap-1.5 border-t border-[var(--table-rule)] pt-2.5 text-sm">
      <h5 className="m-0 text-xs font-semibold text-muted-foreground">Waits on {item.label}</h5>
      {dependents.state === 'unknown' ? <p className="m-0">Unknown · {dependents.reason}</p>
        : dependents.groups.length === 0 ? <p className="m-0">Nothing in {view.root}'s adopted plan view waits on {item.label}.</p>
        : <dl className="m-0 grid min-w-0 grid-cols-[auto_minmax(0,1fr)] gap-x-2.5 gap-y-1.5">{dependents.groups.map(g => <div key={g.heading} data-dependents={g.heading} className="contents">
          <dt className="flex flex-col pt-0.5"><span className="whitespace-nowrap text-[11px] font-semibold uppercase tracking-[.04em] text-muted-foreground">{g.heading}</span>
            {g.status && <span className="whitespace-nowrap text-[11px] text-muted-foreground">{g.status}</span>}</dt>
          <dd className="m-0 min-w-0"><ul className="m-0 flex list-none flex-col gap-1 p-0">{g.entries.map(d => <li key={d.key} className="flex min-w-0 flex-col">
            <span><a href={d.href} className="underline-offset-2 hover:underline pointer-coarse:inline-flex pointer-coarse:min-h-11 pointer-coarse:items-center">{d.label}</a>{d.title && ` ${d.title}`}
              {!g.related && !g.status && <span className="text-muted-foreground"> · {d.status}</span>}</span>
            <span className="text-xs text-muted-foreground">{g.related ? `${d.status} · ${d.reason}` : `for ${d.requiredFor} — ${d.reason}`}</span>
          </li>)}</ul></dd>
        </div>)}</dl>}
      <p className={note}>Edges declared in {view.root}'s adopted plan view; other rounds' plans are not read here. No wait recorded: the edge is not in the plan view's waiting_on.</p>
    </section>
  </section>
}

/** v3.6 Live on: registered work on the module's steps now (plan-view working). */
function Live({ view, item, scope, stale }: { view: PlanSnapshot; item: PlanNode; scope: ModuleScope; stale: boolean }) {
  const live = liveOn(view, scope)
  return <section aria-label={`Live on ${item.label}`} className={`${panel} gap-2`}>
    <CardHead title={`Live on ${item.label} · ${stale ? 'last read ' : ''}${live.length} registered`}><span className="font-mono text-[11px] text-muted-foreground">plan-view · working</span></CardHead>
    {live.length === 0 ? <p className="m-0 text-sm">{stale ? `Current activity is Unknown. Last read contained no registered work on ${item.label}.` : `No registered work on ${item.label} in this snapshot.`}</p>
      : <ul aria-label={`Work now on ${item.label}`} className="m-0 flex list-none flex-col gap-2 p-0">{live.map(w => <WorkRow key={'external_work_id' in w ? w.external_work_id : w.task_id} work={w} view={view} stale={stale} />)}</ul>}
    <p className={note}>Runtime attempts and registered external work on {item.label}'s steps · Unconfirmed after 60s without a heartbeat. Unregistered work is not seen ({view.root} coverage: {view.coverage.unregistered_work} unregistered · {view.coverage.reconciled ? 'Reconciled' : 'Not reconciled'}).</p>
  </section>
}

/**
 * Work → Plan: one module's detail (R5:5.1-7, laid out as Design v3.6 ItemDetail in R5:5.5-3), opened by the module's
 * link or by an address naming the module or one of its steps. A head card (where the module stands, now, next, ETA),
 * its steps as a timeline with the step in progress open, and beside them what needs the Owner, acceptance,
 * dependencies and live work; then what happened lately. Each from a field the Console already reads, Unknown where
 * none says (claims: MODULE_DETAIL_CLAIMS, WORK_V36_3_CLAIMS).
 */
export function PlanModuleDetail({ view, item, step, stale, failed = null, readAgain }: { view: PlanSnapshotRead; item: PlanNode; step: string; stale: boolean
  /** Why the plan view's latest refresh failed (or Offline); null while it did not. */
  failed?: string | null
  /** Reads the plan view, and its explanation, again now (the query's own refetch: no request of its own). */
  readAgain?: () => void }) {
  const now = useNow()
  const reads = useShellReads()
  // The questions and requests the step judge reads are these same shell reads (ownerAsksModel).
  const asks = { questions: reads.questions, requests: reads.open }
  const heading = useRef<HTMLHeadingElement>(null)
  const [allActivity, setAllActivity] = useState(false)
  const runtime = runtimeTasks(reads.progress)
  // The modules' scopes, once per plan view; this module's row is judged again on the page clock (R5:5.5-10): its
  // active steps by what they have now, as the read ages and heartbeats go unconfirmed.
  const scopes = useMemo(() => moduleScopes(view), [view])
  const base = scopes.get(item.plan_node_id)!
  const clock = { failed, now, runtime, asks }
  const scope: ModuleScope = { ...base, row: withWhy(view, judgeRow(view, base.row, stepJudge(view, clock)), clock) }
  const row = scope.row
  const cards = needsYouCards(reads.progress)?.cards ?? new Map()
  const needs = needsNow(view, scopes, scope, reads, now)
  const actions = actionsLeft(scope, needs)
  const plan = planActivity(view, scope, scopes, cards)
  const activity = mergeActivity(plan.rows, questionActivity(view, scope, scopes, reads.questions, cards), approvalActivity(view, scope, scopes, reads.all), taskActivity(scope, runtime))
  const shown = allActivity ? activity : activity.slice(0, ACTIVITY_ROWS)
  const stales = SOURCES.filter(([key]) => isStale(reads[key], SHELL_POLL_MS[key], now)).map(([, name]) => name)
  const estimate = estimateOf(view, item)
  // What the steps wait on, when its read cannot be used as now: once above them (each card says it too).
  const banner = explainBanner(view, row.cells.flatMap(c => view.nodes.filter(n => n.plan_node_id === c.id)), now, viewOldReason(view, view.root, failed, now))
  // Arriving at a module, or at one of its steps, puts the keyboard there: the step's row, else the module's heading.
  useEffect(() => {
    const target = step ? document.getElementById(moduleStepRowId(step)) : heading.current
    target?.focus({ preventScroll: true })
  }, [item.plan_node_id, step])
  // ‹ Work and the round go back to the round's rows, where the row was (moduleRowReturn); no `section=`, so nothing
  // else on Work scrolls to the plan's top over it (R5:5.5-1, Design v3.6 item header).
  const back = workRoundHref(view.root)
  return <section id={moduleDetailId(item.plan_node_id)} aria-label={`Module ${item.label}`} data-module-detail={item.label}
    className="flex min-w-0 scroll-mt-4 flex-col gap-2.5 [overflow-wrap:anywhere]">
    <nav aria-label="Module path" className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2">
      <a href={back} aria-label="Back to Work"
        className="inline-flex min-h-11 shrink-0 items-center gap-1.5 rounded-xl border border-[var(--ops-edge)] px-3.5 text-sm font-semibold text-foreground no-underline shadow-[var(--ops-raised)] [background:var(--ops-control)] hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--ring)]">
        <span aria-hidden="true" className="text-lg leading-none">‹</span> Work</a>
      <ol className="m-0 flex min-w-0 list-none flex-wrap items-baseline gap-x-1.5 p-0 text-[22px] font-[650] leading-tight tracking-[-.02em]">
        <li><a href={back} className="text-foreground no-underline underline-offset-4 hover:text-foreground hover:underline">{roundLabel(view.root)}</a></li>
        <li aria-hidden="true" className="text-muted-foreground">›</li>
        <li aria-current="page">{item.label}</li>
      </ol>
    </nav>

    <Head view={view} item={item} row={row} scope={scope} runtime={runtime} reads={reads} stale={stale} now={now} heading={heading} />

    {/* Steps, then what needs the Owner (on a phone, before the activity); beside the steps on a wide screen, and the activity under them. */}
    <div className="grid min-w-0 grid-cols-1 items-start gap-2.5 xl:grid-cols-[minmax(0,1.7fr)_minmax(0,1fr)] xl:grid-rows-[auto_1fr]">
      <section aria-label={`Steps of ${item.label}`} className={`${panel} gap-2.5 xl:col-start-1 xl:row-start-1`}>
        <CardHead title="Steps · plan order"><span className="font-mono text-[11px] text-muted-foreground">plan-view nodes · tasks · runtime progress{view.explain && ' · explain'}</span></CardHead>
        {banner && <WhyBanner banner={banner} readAgain={readAgain} />}
        {row.cells.length === 0 ? <p className="m-0 text-sm">No steps in this module.</p>
          : <ol className="m-0 flex min-w-0 list-none flex-col p-0">{row.cells.map((cell, i) =>
            <Stage key={cell.id} cell={cell} scope={scope} view={view} runtime={runtime} selected={cell.id === step} last={i === row.cells.length - 1} now={now} stale={stale} />)}</ol>}
        <div className="flex min-w-0 flex-col gap-1.5 border-t border-[var(--table-rule)] pt-2.5">
          {row.dropped > 0 && <p className={note}>{plural(row.dropped, 'dropped step')} not listed.</p>}
          <p className={note}>Latest task is the step's newest own work, not its checks: a task's role is the runtime's while it lists the task (unfinished, or among its latest 20 ended), else the title the controller gives a check, else Unknown. Review is the newest check of that task only (by the task its title names); its verdict is its attempt's conclusion (done passes) while the runtime lists it among its latest 20 ended tasks. A check the controller closed without using its result is not told apart.</p>
          <div data-estimate className="flex min-w-0 flex-wrap items-center gap-x-2.5 gap-y-1 text-[13px] text-muted-foreground">
            {estimate.state === 'ok' ? <>
              <span className={dashedPill}>Plan estimate</span>
              <span>{estimate.days} · {plural(estimate.rounds, 'review round')} · confidence {estimate.confidence}</span>
              <span className="min-w-0 basis-full">{estimate.reason} · the adopted plan view's own estimate (source: {estimate.source || 'Unknown'}), not measured</span>
            </> : <span>Plan estimate Unknown · {estimate.reason}</span>}
          </div>
        </div>
      </section>

      <div className="flex min-w-0 flex-col gap-2.5 xl:col-start-2 xl:row-span-2 xl:row-start-1">
        <div className={`${panel} gap-3`}>
          <section aria-label="Your actions found" className="flex min-w-0 flex-col gap-1 text-sm">
            <div className="flex min-w-0 flex-wrap items-center justify-between gap-2">
              <h4 className={caps}>Your actions found</h4>
              <p className="m-0" data-actions-found><Big>{actions.total}{actions.floor && <span aria-hidden="true">+</span>}</Big>
                {actions.floor && <span className="sr-only"> or more</span>}</p>
            </div>
            <span>{actions.items} counted under Needs you now{actions.notCounted > 0 && ` · ${actions.notCounted} listed, not counted`}</span>
            <span>{plural(actions.ownerSteps, 'Owner step')} not done, {actions.bareOwnerSteps} with nothing counted</span>
            {actions.stopped > 0 && <span>{plural(actions.stopped, 'Owner step')} stopped, not counted</span>}
            <span className="text-xs text-muted-foreground">Owner steps not done, plus what Needs you now counts; cards without evidence here are not counted (see Needs you).</span>
            {actions.floor && <span className="text-xs text-muted-foreground">At least: a list is not read or was cut (Needs you now says which).</span>}
            {stales.length > 0 && <span className="text-xs text-muted-foreground">Stale: {stales.join(', ')} not refreshed for over twice their poll.</span>}
          </section>

          <section aria-label="Needs you now" className="flex min-w-0 flex-col gap-1.5 border-t border-[var(--table-rule)] pt-2.5 text-sm">
            <h4 className={caps}>Needs you now</h4>
            {needs.items.length === 0 && <p className="m-0">Nothing linked to {item.label} found.</p>}
            {needs.items.length > 0 && <ul className="m-0 flex list-none flex-col gap-1.5 p-0">{needs.items.map(n => <li key={n.key} data-need={n.kind}
              className={`flex min-w-0 flex-col rounded-xl px-3 py-1 ${n.counted ? '[background:var(--ops-selection)]' : 'border border-dashed border-[var(--color-lamp-gray)]'}`}>
              <a href={n.href} className="inline-flex min-h-8 items-center font-semibold underline-offset-2 hover:underline pointer-coarse:min-h-11">{n.text}</a>
              {n.note && <span className="pb-1 text-xs text-muted-foreground">{n.note}{!n.counted && ' · not counted'}</span>}
            </li>)}</ul>}
            {needs.gaps.map(g => <span key={g} className="text-xs text-muted-foreground">{g}</span>)}
            <span className="text-xs text-muted-foreground">Listed only with direct evidence: its task is on a step of {item.label}, a step's done rule names it, or the plan change it decides touches {item.label} (seen only while its proposal is among the plan's latest {view.recent_changes.length} events). Not listed: cards and requests with no such evidence here, including those that only share a mission with {item.label}; so a card of {item.label} can be missing from this list and from Your actions found.</span>
            <a href={NEEDS_YOU_HREF} data-needs-you className="inline-flex min-h-11 items-center text-xs underline-offset-2 hover:underline">Cards with no evidence here for a module of {view.root} are on Needs you</a>
          </section>
        </div>
        <Acceptance view={view} item={item} />
        <Dependencies view={view} item={item} row={row} />
        <Live view={view} item={item} scope={scope} stale={stale} />
      </div>

      <section aria-label={`Recent activity on ${item.label}`} className={`${panel} gap-1.5 xl:col-start-1 xl:row-start-2`}>
        <CardHead title="Recent activity" />
        {activity.length === 0 ? <p className="m-0 text-sm">No activity on {item.label} in what was read.</p>
          : <ol className="m-0 flex min-w-0 list-none flex-col p-0">{shown.map(a => <li key={a.key} data-activity={a.source} className="flex min-w-0 flex-wrap gap-x-2 border-b border-[var(--table-rule)] py-1.5 text-sm last:border-b-0">
            <time dateTime={a.at} className="shrink-0 font-mono text-xs text-muted-foreground">{a.at}</time>
            <strong className="shrink-0">{a.kind}</strong>
            {a.href ? <a href={a.href} className="min-w-0 break-words underline-offset-2 hover:underline">{a.text}</a> : <span className="min-w-0 break-words">{a.text}</span>}
          </li>)}</ol>}
        {activity.length > ACTIVITY_ROWS && <button type="button" onClick={() => setAllActivity(v => !v)} aria-expanded={allActivity}
          className="min-h-11 self-start border-0 bg-transparent p-0 text-sm font-semibold text-[var(--primary)]">{allActivity ? 'Show fewer' : `Show all ${activity.length}`}</button>}
        <p className={note}>Plan events: only the latest {view.recent_changes.length} of {view.root} up to <time dateTime={view.recent_as_of}>{view.recent_as_of}</time> (older ones under Recent changes); a step's earlier moves are not tied to it there. A plan change appears here only when one of its events in that window names a node of {item.label} or decides a question of it; the others are under Recent changes.</p>
        <p className={note}>Ended tasks and checks: only those among the runtime's latest 20 ended tasks of every goal. Questions: the question store keeps a bounded history of answered ones. Requests: as the Console last read them.</p>
      </section>
    </div>
  </section>
}
