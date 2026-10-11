import { useState, type ReactNode } from 'react'
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
import { RequestChipLink } from '@/pages/shell/progress/ProgressNeedsYou'
import { Tick, useNow } from '@/pages/shell/progress/progressClock'
import {
  earlierStepsLabel,
  editDelta,
  eventsNewestFirst,
  flowSteps,
  groupParallel,
  lastOutLine,
  nextRow,
  nowRow,
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

function Fold({ summary, children }: { summary: string; children: ReactNode }) {
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
      return 'warning'
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
        <code className="block whitespace-pre-wrap break-all font-mono text-xs">{event.text ?? ''}</code>
        {status != null ? <span>{status}</span> : null}
        {out != null ? <span className="break-all text-muted-foreground">{out}</span> : null}
      </>
    )
  }
  if (event.kind === 'edit') {
    const delta = editDelta(event)
    return (
      <>
        <span className="break-all">{event.text}</span>
        {delta != null ? <span>{delta}</span> : null}
      </>
    )
  }
  return <span className="break-words">{event.text}</span>
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

function StepBar({ step }: { step: ProgressStep }) {
  const now = useNow()
  const pct = Math.round(stepBarPercent(step, now))
  return (
    <span className="mt-1 block h-1 w-full max-w-full overflow-hidden rounded bg-foreground/10" aria-hidden="true">
      <span className="block h-full bg-foreground/70" style={{ width: `${pct}%` }} data-step-bar={pct} />
    </span>
  )
}

function StepCard({ step }: { step: ProgressStep }) {
  const label = stepStateLabel(step.state, step.needs_owner)
  const done = step.state === 'done'
  const waits = step.needs_owner === true
  const expectation = done || waits ? null : expectationLine(step)
  const took = tookLine(step)
  const who = step.who?.trim() ?? ''
  const waitsFor = step.waits_for?.trim() ?? ''
  return (
    <li
      aria-label={step.name ?? 'Step'}
      className="flex min-w-0 max-w-full flex-col gap-1 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-2 py-1.5"
    >
      <span className="flex min-w-0 flex-wrap items-baseline gap-2">
        <span className="break-words text-sm font-medium text-foreground">{step.name}</span>
        <RequestChipLink approval={step.approval} />
      </span>
      <span className="flex min-w-0 flex-wrap items-center gap-1">
        {who !== '' ? (
          <DenseTag variant="category" size="pill">
            {who}
          </DenseTag>
        ) : null}
        <DenseTag variant={stateVariant(label)} size="pill">
          {label}
        </DenseTag>
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
      {waitsFor !== '' ? <span className="break-words text-sm text-muted-foreground">waits for: {waitsFor}</span> : null}
      <StepBar step={step} />
      {done ? (
        <Fold summary="What it did last">
          <EventList events={step.events} />
        </Fold>
      ) : null}
    </li>
  )
}

function StepFlow({ steps, label }: { steps: ProgressStep[]; label: string }) {
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
                <StepCard key={`${step.name ?? 'step'}-${stepIndex}`} step={step} />
              ))}
            </ul>
          </li>
        ) : (
          group.steps.map((step, stepIndex) => <StepCard key={`${step.name ?? 'step'}-${stepIndex}`} step={step} />)
        ),
      )}
    </ul>
  )
}

function LivePanel({ step }: { step: ProgressStep }) {
  return (
    <section aria-label={`Live ${step.name ?? 'step'}`} className="flex w-full min-w-0 flex-col gap-1">
      <h4 className="m-0 text-sm font-medium text-foreground">Live</h4>
      <EventList events={step.events} />
    </section>
  )
}

function YourTurn({ work }: { work: ProgressWork }) {
  const name = work.owner?.name?.trim() ?? ''
  if (name === '') return null
  if (work.sure === false) {
    return <p className="m-0 break-words text-sm text-foreground">Your turn: {name} · cannot be estimated</p>
  }
  return (
    <p className="m-0 break-words text-sm text-foreground">
      Your turn: {name}
      <Tick text={now => yourTurnWhen(work.owner?.at, now)} />
    </p>
  )
}

function WorkBody({ work, foldDone }: { work: ProgressWork; foldDone: boolean }) {
  const { done, flow } = flowSteps(work, foldDone)
  const notes = (work.steps ?? []).map(activeStepText).filter(text => text !== '')
  const nowText = nowRow(work)
  const next = nextRow(work)
  const risk = work.risk?.trim() ?? ''
  const live = flow.filter(showLive)
  return (
    <>
      <h3 className="m-0 break-words text-sm font-medium text-foreground">{work.title}</h3>
      {work.goal?.trim() ? <p className="m-0 break-words text-sm text-foreground">{work.goal}</p> : null}
      {typeof work.round === 'number' ? <p className="m-0 text-sm text-muted-foreground">Round {work.round}</p> : null}
      {done.length > 0 ? (
        <Fold summary={earlierStepsLabel(done.length)}>
          <StepFlow steps={done} label="Earlier steps" />
        </Fold>
      ) : null}
      <StepFlow steps={flow} label="Steps" />
      {nowText != null ? <p className="m-0 break-words text-sm text-foreground">{nowText}</p> : null}
      {next != null ? <p className="m-0 break-words text-sm text-foreground">{next}</p> : null}
      <YourTurn work={work} />
      {notes.map(note => (
        <p key={note} className="m-0 break-words text-sm text-muted-foreground">
          {note}
        </p>
      ))}
      {risk !== '' ? <p className="m-0 break-words text-sm text-foreground">Risk: {risk}</p> : null}
      {live.map((step, index) => (
        <LivePanel key={`${step.name ?? 'live'}-${index}`} step={step} />
      ))}
    </>
  )
}

export function WorkCard({ work, index }: { work: ProgressWork; index: number }) {
  return (
    <article
      id={workCardId(index)}
      className="flex w-full min-w-0 flex-col gap-2 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-3 py-2.5"
    >
      <WorkBody work={work} foldDone />
    </article>
  )
}

function finishedLine(work: ProgressWork): string {
  const title = work.title?.trim() || workShortName(work) || 'Finished work'
  const at = workFinishedAt(work)
  if (!(at > 0)) return title
  return `${title} · ${formatLocalTime(at)}`
}

export function FinishedWorks({ works }: { works: readonly ProgressWork[] }) {
  if (works.length === 0) return null
  return (
    <section aria-label="Finished" className="w-full min-w-0">
      <Fold summary={`Finished · ${works.length}`}>
        <ul className="m-0 flex w-full min-w-0 list-none flex-col gap-2 p-0">
          {works.map((work, index) => (
            <li key={`${work.title ?? 'finished'}-${index}`} className="min-w-0">
              <Fold summary={finishedLine(work)}>
                <StepFlow steps={work.steps ?? []} label="Steps" />
              </Fold>
            </li>
          ))}
        </ul>
      </Fold>
    </section>
  )
}
