import { Fragment, useRef, useState, type ReactNode } from 'react'
import { DenseTag } from '@bifrost/ui'
import {
  activeStepText,
  expectationLine,
  formatLocalTime,
  lastOutputLine,
  stepStateLabel,
  type ProgressStep,
  type ProgressWork,
} from '@/api/agentProgress'
import type { ProgressEvent } from '@/api/agentProgressBoard'
import { AgentText } from '@/pages/shell/progress/AgentText'
import { RequestChipLink } from '@/pages/shell/progress/ProgressNeedsYou'
import { agentText } from '@/pages/shell/progress/progressSummaryModel'
import { SectionFold } from '@/pages/shell/progress/progressFold'
import { Tick } from '@/pages/shell/progress/progressClock'
import {
  earlierStepsLabel,
  editDelta,
  eventsNewestFirst,
  flowSteps,
  groupParallel,
  isNowStep,
  lastOutLine,
  runResultLine,
  showLive,
  stepBarPercent,
  stepPaceLine,
  tookLine,
  workCardId,
  workFinishedAt,
  workShortName,
  yourTurnWhen,
} from '@/pages/shell/progress/progressModel'

function Fold({ summary, children }: { summary: ReactNode; children: ReactNode }) {
  const [open, setOpen] = useState(false)
  return (
    <details open={open} className="min-w-0 max-w-full">
      <summary
        className="cursor-pointer break-words text-sm text-foreground"
        onClick={event => {
          event.preventDefault()
          event.stopPropagation()
          setOpen(value => !value)
        }}
      >
        {summary}
      </summary>
      {open ? <div className="mt-2 min-w-0">{children}</div> : null}
    </details>
  )
}

function stateVariant(label: string): 'success' | 'warning' | 'danger' | 'neutral' | 'info' | 'category' {
  switch (label) {
    case 'running':
      return 'info'
    case 'slower than usual':
    case 'may be stuck':
    case 'waiting for you':
    case 'waiting for the coordinator':
      return 'warning'
    case 'not your turn yet':
      return 'neutral'
    case 'process gone':
    case 'failed':
      return 'danger'
    case 'done':
      return 'success'
    case 'not started':
      return 'neutral'
    default:
      return 'category'
  }
}

const KIND_PATH: Record<string, string> = {
  say: 'M2 3h12v7H6l-3 3V3z',
  think: 'M8 2a3.5 3.5 0 1 1 0 7 3.5 3.5 0 0 1 0-7zM6 12h4',
  run: 'M3 2h10v12H3zM5 5h6M5 8h6M5 11h4',
  edit: 'M2 13l3-1 8-8-2-2-8 8z',
  read: 'M2 3h5v10H2zM9 3h5v10H9z',
  search: 'M7 2a4 4 0 1 1 0 8 4 4 0 0 1 0-8zM10 10l4 4',
  line: 'M2 8h12',
}

function KindIcon({ kind }: { kind: string }) {
  return (
    <svg aria-hidden="true" viewBox="0 0 16 16" className="inline-block size-4 shrink-0" data-kind={kind}>
      <path d={KIND_PATH[kind] ?? KIND_PATH.line} fill="currentColor" />
    </svg>
  )
}

function EventBody({ event }: { event: ProgressEvent }) {
  if (event.kind === 'run') {
    const status = runResultLine(event)
    const out = lastOutLine(event.out)
    return (
      <>
        <code data-agent-text="" className="block whitespace-pre-wrap break-all font-mono text-xs">
          {agentText(event.text)}
        </code>
        {status != null ? <span>{status}</span> : null}
        {out != null ? (
          <span data-agent-text="" className="break-all text-muted-foreground">
            {agentText(out)}
          </span>
        ) : null}
      </>
    )
  }
  if (event.kind === 'edit') {
    const delta = editDelta(event)
    return (
      <>
        <span className="break-all">
          <AgentText value={event.text} />
        </span>
        {delta != null ? <span>{delta}</span> : null}
      </>
    )
  }
  return (
    <span className="break-words">
      <AgentText value={event.text} />
    </span>
  )
}

function EventList({ events }: { events: ProgressEvent[] | undefined }) {
  const rows = eventsNewestFirst(events ?? [])
  if (rows.length === 0) return <p className="m-0 text-sm text-muted-foreground">No events yet.</p>
  return (
    <ol className="m-0 flex w-full min-w-0 list-none flex-col gap-2 p-0">
      {rows.map((event, index) => (
        <li key={index} data-kind={event.kind ?? ''} className="flex min-w-0 flex-col gap-0.5 text-sm text-foreground">
          <span className="flex min-w-0 items-center gap-1 text-muted-foreground">
            {typeof event.at === 'number' && event.at > 0 ? <time>{formatLocalTime(event.at)}</time> : null}
            <KindIcon kind={event.kind ?? 'line'} />
          </span>
          <EventBody event={event} />
        </li>
      ))}
    </ol>
  )
}

const PACE_BAR = new Set(['running', 'slow', 'quiet'])

function StepBar({ step, atSec }: { step: ProgressStep; atSec: number | undefined }) {
  const renders = useRef(0)
  renders.current += 1
  if (!PACE_BAR.has(step.state ?? '')) return null
  const basis = typeof atSec === 'number' && atSec > 0 ? atSec * 1000 : 0
  const pct = Math.round(stepBarPercent(step, basis))
  return (
    <span
      className="mt-1 block h-1 w-full max-w-full overflow-hidden rounded bg-foreground/10"
      aria-hidden="true"
      data-bar-renders={renders.current}
    >
      <span className="block h-full bg-foreground/70" style={{ width: `${pct}%` }} data-step-bar={pct} />
    </span>
  )
}

function StepCard({ step, atSec }: { step: ProgressStep; atSec: number | undefined }) {
  const renders = useRef(0)
  renders.current += 1
  const label = stepStateLabel(step.state, step.needs_owner)
  const follow = step.needs_owner === true && step.state === 'waiting' ? (step.label?.trim() ?? '') : ''
  const done = step.state === 'done'
  const waits = step.needs_owner === true
  const paced = step.state === 'running' || step.state === 'slow' || step.state === 'quiet' || step.state === 'dead' || step.state === 'failed'
  const expectation = done || waits || !paced ? null : expectationLine(step)
  const took = tookLine(step)
  const who = step.who?.trim() ?? ''
  const waitsFor = step.waits_for?.trim() ?? ''
  return (
    <li
      aria-label={agentText(step.name) || 'Step'}
      data-renders={renders.current}
      className="flex min-w-0 max-w-full flex-col gap-1 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-2 py-1.5"
    >
      <span className="flex min-w-0 flex-wrap items-baseline gap-2">
        <span className="break-words text-sm font-medium text-foreground">
          <AgentText value={step.name} />
        </span>
        <RequestChipLink approval={step.approval} />
      </span>
      <span className="flex min-w-0 flex-wrap items-center gap-1">
        {who !== '' ? (
          <DenseTag variant="category" size="pill">
            <AgentText value={step.who} />
          </DenseTag>
        ) : null}
        <DenseTag variant={stateVariant(label)} size="pill">
          {label}
        </DenseTag>
        {follow !== '' ? (
          <span className="break-words text-sm text-foreground">
            <AgentText value={step.label} />
          </span>
        ) : null}
      </span>
      {done ? (
        took != null ? <span className="break-words text-sm text-muted-foreground">{took}</span> : null
      ) : (
        <>
          <Tick className="break-words text-sm text-muted-foreground" text={now => stepPaceLine(step, now)} />
          {expectation != null ? <span className="break-words text-sm text-muted-foreground">{expectation}</span> : null}
          <Tick className="break-words text-sm text-muted-foreground" text={now => lastOutputLine(step, now)} />
        </>
      )}
      {waitsFor !== '' ? (
        <span data-agent-text="" className="break-words text-sm text-muted-foreground">
          {'waits for: '}
          {agentText(step.waits_for)}
        </span>
      ) : null}
      <StepBar step={step} atSec={atSec} />
      {done ? (
        <Fold summary="What it did last">
          <EventList events={step.events} />
        </Fold>
      ) : null}
    </li>
  )
}

function StepFlow({ steps, label, atSec }: { steps: ProgressStep[]; label: string; atSec: number | undefined }) {
  const groups = groupParallel(steps)
  return (
    <ul aria-label={label} className="m-0 flex w-full min-w-0 list-none flex-wrap gap-2 p-0">
      {groups.map((group, index) =>
        group.parallel ? (
          <li
            key={`parallel-${index}`}
            className="flex w-full min-w-0 basis-full flex-col gap-2 rounded-[var(--card-radius)] border border-dashed border-[var(--card-border)] px-2 py-1.5"
          >
            <span className="text-sm text-muted-foreground">at the same time</span>
            <ul className="m-0 flex w-full min-w-0 list-none flex-col gap-2 p-0">
              {group.steps.map((step, stepIndex) => (
                <StepCard key={`${step.name ?? 'step'}-${stepIndex}`} step={step} atSec={atSec} />
              ))}
            </ul>
          </li>
        ) : (
          group.steps.map((step, stepIndex) => (
            <StepCard key={`${step.name ?? 'step'}-${stepIndex}`} step={step} atSec={atSec} />
          ))
        ),
      )}
    </ul>
  )
}

function LivePanel({ step }: { step: ProgressStep }) {
  return (
    <section aria-label={`Live ${agentText(step.name) || 'step'}`} className="flex w-full min-w-0 flex-col gap-1">
      <h4 className="m-0 text-sm font-medium text-foreground">Live</h4>
      <EventList events={step.events} />
    </section>
  )
}

function YourTurn({ work }: { work: ProgressWork }) {
  if (agentText(work.owner?.name) === '') return null
  if (work.sure === false) {
    return (
      <p data-agent-text="" className="m-0 break-words text-sm text-foreground">
        {'Your turn: '}
        {agentText(work.owner?.name)}
        {' · cannot be estimated'}
      </p>
    )
  }
  return (
    <p data-agent-text="" className="m-0 break-words text-sm text-foreground">
      {'Your turn: '}
      {agentText(work.owner?.name)}
      <Tick text={now => yourTurnWhen(work.owner?.at, now)} />
    </p>
  )
}

function NowLine({ work }: { work: ProgressWork }) {
  const shown = (work.steps ?? []).filter(step => isNowStep(step) && (agentText(step.name) !== '' || agentText(step.who) !== ''))
  if (shown.length === 0) return null
  return (
    <p data-agent-text="" className="m-0 break-words text-sm text-foreground">
      {'Now: '}
      {shown.map((step, index) => (
        <Fragment key={index}>
          {index > 0 ? '; ' : null}
          {agentText(step.name)}
          {agentText(step.name) !== '' && agentText(step.who) !== '' ? ' · ' : null}
          {agentText(step.who)}
        </Fragment>
      ))}
    </p>
  )
}

function NextLine({ work }: { work: ProgressWork }) {
  const steps = work.steps ?? []
  let lastNow = -1
  steps.forEach((step, index) => {
    if (isNowStep(step)) lastNow = index
  })
  const upcoming = steps.slice(lastNow + 1).find(step => step.state === 'waiting' && agentText(step.name) !== '')
  const name = upcoming?.name ?? work.next?.name
  const who = upcoming != null ? upcoming.who : work.next?.who
  const needs = upcoming != null ? upcoming.needs_owner === true : work.next?.needs_owner === true
  if (agentText(name) === '') return null
  return (
    <p data-agent-text="" className="m-0 break-words text-sm text-foreground">
      {'Next: '}
      {agentText(name)}
      {agentText(who) !== '' ? ' · ' : null}
      {agentText(who) !== '' ? agentText(who) : null}
      {needs ? ' — this one needs you' : null}
    </p>
  )
}

function StepNote({ step }: { step: ProgressStep }) {
  if (activeStepText(step) === '') return null
  const body = agentText(step.note) !== '' ? step.note : step.doing
  if (agentText(body) === '') return null
  return (
    <p data-agent-text="" className="m-0 break-words text-sm text-muted-foreground">
      {agentText(step.name) !== '' ? agentText(step.name) : null}
      {agentText(step.name) !== '' ? ': ' : null}
      {agentText(body)}
    </p>
  )
}

function WorkBody({ work, foldDone, atSec }: { work: ProgressWork; foldDone: boolean; atSec: number | undefined }) {
  const { done, flow } = flowSteps(work, foldDone)
  const live = flow.filter(showLive)
  return (
    <>
      <h3 className="m-0 break-words text-sm font-medium text-foreground">
        <AgentText value={work.title} />
      </h3>
      {agentText(work.goal) !== '' ? (
        <p className="m-0 break-words text-sm text-foreground">
          <AgentText value={work.goal} />
        </p>
      ) : null}
      {typeof work.round === 'number' ? <p className="m-0 text-sm text-muted-foreground">Round {work.round}</p> : null}
      {done.length > 0 || work.earlier_done != null ? (
        <Fold summary={doneFoldSummary(done.length, work.earlier_done)}>
          {done.length > 0 ? <StepFlow steps={done} label="Earlier steps" atSec={atSec} /> : <p className="m-0 text-sm text-muted-foreground">Left out of this document.</p>}
        </Fold>
      ) : null}
      <StepFlow steps={flow} label="Steps" atSec={atSec} />
      <NowLine work={work} />
      <NextLine work={work} />
      <YourTurn work={work} />
      {(work.steps ?? []).map((step, index) => (
        <StepNote key={index} step={step} />
      ))}
      {agentText(work.risk) !== '' ? (
        <p data-agent-text="" className="m-0 break-words text-sm text-foreground">
          {'Risk: '}
          {agentText(work.risk)}
        </p>
      ) : null}
      {live.map((step, index) => (
        <LivePanel key={`${step.name ?? 'live'}-${index}`} step={step} />
      ))}
    </>
  )
}

export function WorkCard({ work, index, at }: { work: ProgressWork; index: number; at?: number }) {
  return (
    <article
      id={workCardId(index)}
      className="flex w-full min-w-0 flex-col gap-2 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-3 py-2.5"
    >
      <WorkBody work={work} foldDone atSec={at} />
    </article>
  )
}

function doneFoldSummary(doneCount: number, earlier: ProgressWork['earlier_done']): string {
  const base = doneCount > 0 ? earlierStepsLabel(doneCount) : ''
  if (earlier === 'unknown') return base === '' ? 'Older steps Unknown' : `${base} · older steps Unknown`
  if (typeof earlier === 'number' && earlier > 0) {
    const older = earlier === 1 ? '1 older step not listed' : `${earlier} older steps not listed`
    return base === '' ? older : `${base} · ${older}`
  }
  return base
}

function FinishedSummary({ work }: { work: ProgressWork }) {
  const title = agentText(work.title) || agentText(workShortName(work))
  const at = workFinishedAt(work)
  return (
    <>
      {title !== '' ? <AgentText value={work.title || work.short} /> : 'Finished work'}
      {at > 0 ? ` · ${formatLocalTime(at)}` : null}
    </>
  )
}

/** A work with nothing running and nothing waiting on the Owner, drawn as one line. */
export function IdleWorkLine({ work, index, at }: { work: ProgressWork; index: number; at?: number }) {
  const name = agentText(workShortName(work))
  return (
    <SectionFold
      id={`sec-work-${index}`}
      title={name !== '' ? <AgentText value={work.short || work.title} /> : 'Work'}
      digest={agentText(work.risk) !== '' ? <AgentText value={work.risk} /> : 'Nothing running'}
      defaultOpen={false}
    >
      <WorkCard work={work} index={index} at={at} />
    </SectionFold>
  )
}

export function FinishedWorks({ works, at }: { works: readonly ProgressWork[]; at?: number }) {
  if (works.length === 0) return null
  return (
    <section aria-label="Finished" className="w-full min-w-0">
      <Fold summary={`Finished · ${works.length}`}>
        <ul className="m-0 flex w-full min-w-0 list-none flex-col gap-2 p-0">
          {works.map((work, index) => (
            <li key={`${work.title ?? 'finished'}-${index}`} className="min-w-0">
              <Fold summary={<FinishedSummary work={work} />}>
                <StepFlow steps={work.steps ?? []} label="Steps" atSec={at} />
              </Fold>
            </li>
          ))}
        </ul>
      </Fold>
    </section>
  )
}
