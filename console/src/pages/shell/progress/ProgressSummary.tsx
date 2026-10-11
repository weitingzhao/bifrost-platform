import { useRef } from 'react'
import type { ApprovalItem } from '@/api/approvals'
import {
  ageSeconds,
  computedAgeLine,
  formatDuration,
  generationIsSkewed,
  type AgentProgressResponse,
  type ProgressDocument,
  type ProgressWork,
} from '@/api/agentProgress'
import { isApprovalId } from '@/api/agentProgressExtra'
import type { ProgressPool } from '@/api/agentProgressExtra'
import { AgentText } from '@/pages/shell/progress/AgentText'
import { NeedsYouBlock } from '@/pages/shell/progress/NeedsYouBlock'
import { useProgressApprovals } from '@/pages/shell/progress/ProgressNeedsYou'
import { Tick, useNow } from '@/pages/shell/progress/progressClock'
import { useOpenSection } from '@/pages/shell/progress/progressFold'
import { planCounts, runningRows, workShortName, type RunningRow } from '@/pages/shell/progress/progressModel'
import {
  agentText,
  askAct,
  asksFromDocument,
  doingDetails,
  elapsedPercent,
  finishedStepCount,
  idleForText,
  idleQueue,
  laterRequestCount,
  nightsFrom,
  openDetails,
  runningRank,
  runningTone,
  runningWhen,
  segmentFlex,
  sliceIsUnknown,
  sliceLabel,
  slicePercent,
  slicesHeadline,
  sumOpen,
  troubleTone,
  uncoveredOpenCount,
  waitsOnOwner,
  type ApprovalRead,
  type AskSource,
  type DocumentRead,
} from '@/pages/shell/progress/progressSummaryModel'
import { barPercent, usedIsDisplayable } from '@/pages/shell/progress/quotaModel'

const COORDINATOR_STALE_SECONDS = 20 * 60

function approvalRead(enabled: boolean, isSuccess: boolean, isError: boolean, isPending: boolean): ApprovalRead {
  if (!enabled) return 'ready'
  if (isPending) return 'loading'
  if (isError || !isSuccess) return 'unreadable'
  return 'ready'
}


function PlanPicture({ doc }: { doc: ProgressDocument }) {
  const renders = useRef(0)
  renders.current += 1
  const openSection = useOpenSection()
  const roadmap = doc.roadmap
  const counts = roadmap != null ? planCounts(roadmap) : null
  const total = counts != null ? counts.done + counts.doing + counts.todo : 0
  const details = openDetails(roadmap)
  const uncovered = uncoveredOpenCount(roadmap)
  const mine = sumOpen(details, 'mine', uncovered)
  const ownerTimes = sumOpen(details, 'owner', uncovered)
  const pace = roadmap?.pace
  const nights = pace == null || pace.unreadable ? null : nightsFrom(mine, ownerTimes, pace.per_night)
  const slicesText = slicesHeadline(mine)
  const showFigures = slicesText != null || nights != null
  const items = doingDetails(roadmap).filter(item => !item.unreadable && item.est != null)
  return (
    <button
      type="button"
      data-summary-block="plan"
      data-summary-renders={renders.current}
      className="flex min-h-11 w-full min-w-0 flex-col gap-2 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-3 py-2 text-left"
      onClick={() => openSection('sec-plan')}
    >
      <span className="text-sm font-medium">Plan</span>
      {roadmap == null ? <span className="text-sm text-muted-foreground">No plan</span> : null}
      {roadmap != null && (roadmap.stages ?? []).length > 0 ? (
        <span data-stages="" className="flex min-w-0 flex-wrap gap-1">
          {(roadmap.stages ?? []).map((stage, index) => (
            <span
              key={`${stage.name ?? 'stage'}-${index}`}
              data-stage={stage.state ?? ''}
              className={
                stage.state === 'current'
                  ? 'rounded bg-emerald-500/15 px-1.5 py-0.5 text-xs font-medium text-foreground'
                  : 'rounded bg-foreground/10 px-1.5 py-0.5 text-xs text-muted-foreground'
              }
            >
              <AgentText value={stage.name} />
            </span>
          ))}
        </span>
      ) : null}
      {counts != null ? (
        <span data-plan-bar="" className="flex h-7 min-w-0 overflow-hidden rounded text-xs font-medium">
          <span data-segment="live" className="flex min-w-0 items-center justify-center overflow-hidden bg-foreground/80 px-1 text-[var(--card-fill)]" style={{ flex: segmentFlex(counts.done, total) }}>
            {`${counts.done} live`}
          </span>
          <span data-segment="doing" className="flex min-w-0 items-center justify-center overflow-hidden bg-emerald-500/80 px-1" style={{ flex: segmentFlex(counts.doing, total) }}>
            {`${counts.doing} in progress`}
          </span>
          <span data-segment="todo" className="flex min-w-0 items-center justify-center overflow-hidden bg-foreground/10 px-1 text-muted-foreground" style={{ flex: segmentFlex(counts.todo, total) }}>
            {`${counts.todo} not started`}
          </span>
        </span>
      ) : null}
      {items.length > 0 ? (
        <ul className="m-0 flex w-full min-w-0 list-none flex-col gap-1 p-0">
          {items.map((item, index) => {
            const unknown = sliceIsUnknown(item.est?.done, item.est?.total)
            const pct = slicePercent(item.est?.done, item.est?.total)
            const blocked = typeof item.est?.blocked === 'string' ? agentText(item.est.blocked) : ''
            const owner = waitsOnOwner(blocked)
            return (
              <li key={`${item.name ?? 'item'}-${index}`} data-plan-item="" className="grid min-w-0 grid-cols-[minmax(0,1fr)_4rem_2.5rem] items-center gap-2 text-xs">
                <span className="min-w-0 truncate">
                  <AgentText value={item.name} />
                  {owner ? (
                    <a href="#needs-you" className="ml-1 inline-flex min-h-11 min-w-11 items-center" aria-label="Waiting on you" onClick={event => event.stopPropagation()}>
                      <i data-owner-dot="" className="inline-block size-2 rounded-full bg-amber-500" />
                    </a>
                  ) : null}
                </span>
                <span className="block h-1.5 overflow-hidden rounded bg-foreground/10">
                  {unknown ? null : (
                    <span className="block h-full bg-emerald-500" style={{ width: `${pct ?? 0}%` }} data-slice-pct={pct ?? 0} />
                  )}
                </span>
                <span className="text-right text-muted-foreground" data-slice={unknown ? 'unknown' : 'bar'}>
                  {unknown ? 'Unknown' : sliceLabel(item.est?.done, item.est?.total)}
                </span>
              </li>
            )
          })}
        </ul>
      ) : null}
      {showFigures ? (
        <span data-plan-figures="" className="flex flex-wrap gap-3 text-xs text-muted-foreground">
          {slicesText != null ? <span>{slicesText}</span> : null}
          {nights === 'Unknown' ? <span data-nights="unknown">Unknown</span> : nights != null ? <span data-nights="">{`${nights} nights`}</span> : null}
        </span>
      ) : null}
    </button>
  )
}

function RunningRowView({ row }: { row: RunningRow }) {
  const now = useNow()
  const tone = runningTone(row.step, now)
  const pct = elapsedPercent(row.step, now)
  const who = row.step.who?.trim() ?? ''
  const when = runningWhen(row.step)
  const barClass = tone === 'bad' ? 'bg-destructive' : tone === 'amber' || tone === 'owner' ? 'bg-amber-500' : 'bg-emerald-500'
  return (
    <li data-running-row="" data-tone={tone} className="grid min-w-0 grid-cols-[minmax(0,1.4fr)_auto_minmax(3rem,1fr)_auto] items-center gap-2 py-1 text-xs">
      <span className="min-w-0 truncate">
        <span className="font-medium">
          <AgentText value={workShortName(row.work)} />
        </span>
        <span className="text-muted-foreground">
          {' '}
          <AgentText value={row.step.name} />
        </span>
      </span>
      <span className="truncate text-muted-foreground">
        <AgentText value={who} />
      </span>
      {pct === 'unknown' ? (
        <span data-elapsed="unknown" className="text-muted-foreground">Unknown</span>
      ) : (
        <span className="block h-2 overflow-hidden rounded bg-foreground/10">
          <span data-elapsed-pct={Math.round(pct)} className={`block h-full ${barClass}`} style={{ width: `${Math.round(pct)}%` }} />
        </span>
      )}
      <span data-running-when="" className={tone === 'amber' || tone === 'owner' ? 'text-warning' : tone === 'bad' ? 'text-destructive' : ''}>{when}</span>
    </li>
  )
}

function canDoNow(asks: readonly AskSource[], items: readonly ApprovalItem[] | undefined, read: ApprovalRead, nowMs: number): boolean {
  return asks.some(ask => !ask.unreadable && askAct(ask, items, read, nowMs) === 'now')
}

const PILL: Record<string, string> = {
  green: 'bg-emerald-500 text-[var(--card-fill)]',
  red: 'bg-destructive text-white',
  amber: 'bg-amber-500 text-[var(--card-fill)]',
  unknown: 'bg-foreground/20 text-foreground',
  running: 'bg-foreground/10 text-foreground',
}

function RunPill({
  works,
  asks,
  items,
  read,
  count,
}: {
  works: ProgressWork[] | undefined
  asks: readonly AskSource[]
  items: ApprovalItem[] | undefined
  read: ApprovalRead
  count: number
}) {
  const now = useNow()
  const tone = idleQueue(works, canDoNow(asks, items, read, now)).tone
  return (
    <span data-run-state={tone} className={`rounded-full px-2 text-xs ${PILL[tone] ?? PILL.running}`}>
      {count}
    </span>
  )
}

function IdleLine({
  works,
  asks,
  items,
  read,
}: {
  works: ProgressWork[] | undefined
  asks: readonly AskSource[]
  items: ApprovalItem[] | undefined
  read: ApprovalRead
}) {
  const now = useNow()
  const renders = useRef(0)
  renders.current += 1
  const state = idleQueue(works, canDoNow(asks, items, read, now))
  if (state.line == null) return null
  const waited = state.tone === 'amber' ? idleForText(works, now) : null
  return (
    <span data-idle-line="" data-idle-renders={renders.current} className="text-sm">
      {state.tone === 'red' ? (
        <a href="#needs-you" className="text-destructive underline" onClick={event => event.stopPropagation()}>
          {state.line}
        </a>
      ) : (
        <span className={state.tone === 'green' ? 'text-emerald-700' : state.tone === 'amber' ? 'text-warning' : ''}>{state.line}</span>
      )}
      {waited != null ? <span data-idle-for="" className="text-warning">{` ${waited}`}</span> : null}
    </span>
  )
}

function RunningPicture({
  doc,
  asks,
  items,
  read,
}: {
  doc: ProgressDocument
  asks: readonly AskSource[]
  items: ApprovalItem[] | undefined
  read: ApprovalRead
}) {
  const renders = useRef(0)
  renders.current += 1
  const openSection = useOpenSection()
  const rows = runningRows(doc.works ?? [])
    .map((row, index) => ({ row, index }))
    .sort((a, b) => runningRank(a.row.step) - runningRank(b.row.step) || a.index - b.index)
    .map(entry => entry.row)
  return (
    <button
      type="button"
      data-summary-block="running"
      data-summary-renders={renders.current}
      className="flex min-h-11 w-full min-w-0 flex-col gap-1 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-3 py-2 text-left"
      onClick={() => openSection('sec-running')}
    >
      <span className="flex min-h-11 items-center gap-2 text-sm font-medium">
        Running now
        <RunPill works={doc.works} asks={asks} items={items} read={read} count={rows.length} />
      </span>
      {rows.length === 0 ? (
        <IdleLine works={doc.works} asks={asks} items={items} read={read} />
      ) : (
        <ul className="m-0 flex w-full min-w-0 list-none flex-col p-0">
          {rows.map(row => (
            <RunningRowView key={`${row.workIndex}-${row.stepIndex}`} row={row} />
          ))}
        </ul>
      )}
    </button>
  )
}

function PoolTick({ pool }: { pool: ProgressPool }) {
  if (!usedIsDisplayable(pool.used)) {
    return (
      <span data-pool={pool.id ?? ''} className="inline-flex w-16 min-w-0 flex-col text-[10px]">
        <span>Unknown</span>
      </span>
    )
  }
  const used = barPercent(pool.used) ?? 0
  const elapsed = barPercent(pool.elapsed_pct)
  return (
    <span data-pool={pool.id ?? ''} className="inline-flex w-16 min-w-0 flex-col gap-0.5">
      <span className="relative block h-2 overflow-hidden rounded bg-foreground/10">
        <span className="absolute inset-y-0 left-0 bg-emerald-500" style={{ width: `${Math.min(100, used)}%` }} />
        {elapsed != null ? (
          <span data-window-tick={elapsed} className="absolute inset-y-0 w-0.5 bg-foreground" style={{ left: `${Math.min(100, elapsed)}%` }} />
        ) : null}
      </span>
      <span className="truncate text-[10px] text-muted-foreground">
        <AgentText value={pool.label ?? pool.id} />
      </span>
    </span>
  )
}

function FreshGauge({ env, at }: { env: AgentProgressResponse | undefined; at: number | undefined }) {
  const now = useNow()
  const skewed = generationIsSkewed(at, env?.received_at ?? '', now)
  const tone = typeof at !== 'number' || !(at > 0) ? 'unknown' : skewed ? 'stale' : 'ok'
  return (
    <div data-gauge="freshness" data-fresh={tone} className="flex min-h-11 min-w-0 flex-col justify-center gap-0.5 text-xs">
      <span className="text-muted-foreground">This page</span>
      <span className={tone === 'stale' ? 'text-warning' : ''}>
        <Tick text={clock => (typeof at === 'number' && at > 0 ? computedAgeLine(at, clock) : 'Unknown')} />
      </span>
    </div>
  )
}

function CoordGauge({ doc }: { doc: ProgressDocument }) {
  const now = useNow()
  const openSection = useOpenSection()
  const at = doc.coordinator?.at
  const known = typeof at === 'number' && at > 0
  const stale = known && ageSeconds(at, now) > COORDINATOR_STALE_SECONDS
  const tone = !known ? 'unknown' : stale ? 'stale' : 'ok'
  return (
    <button type="button" data-gauge="coordinator" data-coord={tone} className="flex min-h-11 min-w-0 flex-col justify-center gap-0.5 text-left text-xs" onClick={() => openSection('sec-coord')}>
      <span className="text-muted-foreground">Coordinator</span>
      <span className="inline-flex items-center gap-1">
        <i className={tone === 'ok' ? 'inline-block size-3 rounded-full bg-emerald-500' : tone === 'stale' ? 'inline-block size-3 rounded-full bg-amber-500' : 'inline-block size-3 rounded-full bg-foreground/20'} />
        {known ? <Tick text={clock => `${formatDuration(ageSeconds(at, clock))} ago`} /> : <span>Unknown</span>}
      </span>
    </button>
  )
}

function TroubleGauge({
  works,
  asks,
  items,
  read,
}: {
  works: ProgressWork[] | undefined
  asks: readonly AskSource[]
  items: ApprovalItem[] | undefined
  read: ApprovalRead
}) {
  const now = useNow()
  const renders = useRef(0)
  renders.current += 1
  const openSection = useOpenSection()
  const trouble = troubleTone(works, now)
  const idle = idleQueue(works, canDoNow(asks, items, read, now))
  const doneSteps = finishedStepCount(works)
  const whose =
    idle.tone === 'amber'
      ? doneSteps > 0
        ? `${doneSteps} steps finished, waiting for the coordinator's check — nothing for you`
        : 'Waiting for the coordinator — nothing for you'
      : null
  const light = (name: 'green' | 'amber' | 'red', on: boolean, className: string) => (
    <i data-light={name} data-on={on ? 'true' : 'false'} className={`inline-block size-3.5 rounded-full ${on ? className : 'bg-foreground/15'}`} />
  )
  return (
    <button
      type="button"
      data-gauge="trouble"
      data-trouble={trouble}
      data-trouble-renders={renders.current}
      className="flex min-h-11 min-w-0 flex-col justify-center gap-1 text-left text-xs"
      onClick={() => openSection('sec-running')}
    >
      <span className="text-muted-foreground">Trouble</span>
      <span className="inline-flex gap-1">
        {light('green', trouble === 'green', 'bg-emerald-500')}
        {light('amber', trouble === 'amber', 'bg-amber-500')}
        {light('red', trouble === 'red', 'bg-destructive')}
      </span>
      {whose != null ? <span data-trouble-whose="">{whose}</span> : null}
    </button>
  )
}

function Gauges({
  doc,
  env,
  asks,
  items,
  read,
}: {
  doc: ProgressDocument
  env: AgentProgressResponse | undefined
  asks: readonly AskSource[]
  items: ApprovalItem[] | undefined
  read: ApprovalRead
}) {
  const renders = useRef(0)
  renders.current += 1
  const openSection = useOpenSection()
  const pools = (doc.usage?.pools ?? []).filter(pool => pool.verdict !== 'sublimit')
  const done = doc.recent?.length ?? 0
  return (
    <div data-summary-block="gauges" data-summary-renders={renders.current} className="grid w-full min-w-0 grid-cols-2 gap-2 min-[821px]:grid-cols-5">
      <TroubleGauge works={doc.works} asks={asks} items={items} read={read} />
      <button type="button" data-gauge="quota" data-summary-renders={renders.current} className="flex min-h-11 min-w-0 flex-col justify-center gap-1 text-left text-xs" onClick={() => openSection('sec-quota')}>
        <span className="text-muted-foreground">Quota</span>
        {pools.length === 0 ? (
          <span className="text-muted-foreground">No pools</span>
        ) : (
          <span className="flex min-w-0 flex-wrap gap-2">
            {pools.map((pool, index) => (
              <PoolTick key={`${pool.id ?? pool.label ?? 'pool'}-${index}`} pool={pool} />
            ))}
          </span>
        )}
      </button>
      <CoordGauge doc={doc} />
      <button type="button" data-gauge="done" data-summary-renders={renders.current} className="flex min-h-11 min-w-0 flex-col justify-center gap-0.5 text-left text-xs" onClick={() => openSection('sec-done')}>
        <span className="text-muted-foreground">Done today</span>
        <span data-done-count={done} className="text-base font-medium">{done}</span>
      </button>
      <FreshGauge env={env} at={doc.at} />
    </div>
  )
}

/** The Owner's first screen: what needs him, the plan, what is running, then small gauges. */
export function ProgressSummary({
  doc,
  env,
  documentRead = { kind: 'ok' },
}: {
  doc: ProgressDocument
  env?: AgentProgressResponse
  documentRead?: DocumentRead
}) {
  const paints = useRef(0)
  paints.current += 1
  const gathered = asksFromDocument(doc.owner_items, doc.standing_items, doc.owner_now, doc.owner_all)
  const enabled = gathered.asks.some(ask => isApprovalId(ask.approval?.id))
  const list = useProgressApprovals(enabled)
  const read = approvalRead(enabled, list.isSuccess, list.isError, list.isPending)
  return (
    <div data-testid="progress-summary" data-paints={paints.current} className="flex w-full min-w-0 max-w-full flex-col gap-3 overflow-x-hidden">
      <NeedsYouBlock
        asks={gathered.asks}
        items={list.data}
        read={read}
        listUnreadable={gathered.listUnreadable}
        later={laterRequestCount(doc.roadmap)}
        documentRead={documentRead}
      />
      <div data-summary-block="pictures" className="grid w-full min-w-0 grid-cols-1 gap-3 min-[821px]:grid-cols-2">
        <PlanPicture doc={doc} />
        <RunningPicture doc={doc} asks={gathered.asks} items={list.data} read={read} />
      </div>
      <Gauges doc={doc} env={env} asks={gathered.asks} items={list.data} read={read} />
    </div>
  )
}

export function workIsHot(work: ProgressWork): boolean {
  return (work.steps ?? []).some(step => {
    if (step.state === 'done') return false
    if (step.needs_owner === true) return true
    return step.state === 'running' || step.state === 'slow' || step.state === 'quiet' || step.state === 'dead' || step.state === 'failed' || step.state === 'gap'
  })
}
