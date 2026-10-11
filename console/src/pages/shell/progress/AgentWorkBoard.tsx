import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { DenseTag, StatusLamp } from '@bifrost/ui'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  AGENT_PROGRESS_POLL_MS,
  activeStepText,
  coordinatorLine,
  elapsedLine,
  expectationLine,
  fetchAgentProgress,
  lastOutputLine,
  nextLine,
  ownerNextLine,
  publishedAgeSeconds,
  safeLink,
  staleSentence,
  stepStateLabel,
  waitingLine,
  type ProgressDocument,
  type ProgressStep,
  type ProgressWork,
} from '@/api/agentProgress'

/**
 * Interim bridge (DESIGN §20.4): the coordinating thread publishes the
 * document its local board already computes. Later the runtime computes it
 * from events. Durations tick from the timestamps; the read is every 15s.
 */
function useNow(): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [])
  return now
}

function failureReason(err: unknown): string {
  if (err instanceof Error && err.message.trim() !== '') return err.message
  return 'request failed'
}

function lampFor(level: string | undefined): 'ok' | 'degraded' | 'fail' | 'unknown' {
  switch (level) {
    case 'run':
      return 'ok'
    case 'slow':
      return 'degraded'
    case 'bad':
      return 'fail'
    default:
      return 'unknown'
  }
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

function Verdict({ doc }: { doc: ProgressDocument }) {
  return (
    <div className="flex w-full min-w-0 items-start gap-2">
      <StatusLamp value={lampFor(doc.level)} />
      <p className="m-0 break-words text-sm font-medium text-foreground" data-level={doc.level ?? ''}>
        {doc.verdict ?? ''}
      </p>
    </div>
  )
}

function OwnerSection({ doc, nowMs }: { doc: ProgressDocument; nowMs: number }) {
  const nowItems = doc.owner_now ?? []
  return (
    <section aria-label="Next time you are needed" className="flex w-full min-w-0 flex-col gap-2">
      <h3 className="m-0 text-sm font-medium text-foreground">Next time you are needed</h3>
      {nowItems.length > 0 ? (
        <ul className="m-0 flex w-full min-w-0 list-none flex-col gap-2 p-0">
          {nowItems.map((entry, index) => (
            <li
              key={`${entry.name ?? 'item'}-${index}`}
              className="flex w-full min-w-0 flex-col gap-1 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-3 py-2.5"
            >
              <span className="break-words text-sm font-medium text-foreground">{entry.name}</span>
              {entry.urgency?.trim() ? (
                <span className="break-words text-sm text-muted-foreground">{entry.urgency}</span>
              ) : null}
              <span className="text-sm text-muted-foreground">{waitingLine(entry.since ?? 0, nowMs)}</span>
              <ol className="m-0 list-decimal pl-5 text-sm text-foreground">
                {(entry.actions ?? []).map((action, actionIndex) => {
                  const href = safeLink(action.link)
                  const linkText = action.link_text?.trim() || action.text || href
                  return (
                    <li key={actionIndex} className={action.done ? 'break-words line-through' : 'break-words'}>
                      {href !== '' ? (
                        <a href={href} target="_blank" rel="noopener noreferrer" className="underline">
                          {linkText}
                        </a>
                      ) : (
                        action.text
                      )}
                    </li>
                  )
                })}
              </ol>
            </li>
          ))}
        </ul>
      ) : doc.owner_next != null && ownerNextLine(doc.owner_next) !== '' ? (
        <p className="m-0 break-words text-sm text-foreground">{ownerNextLine(doc.owner_next)}</p>
      ) : (
        <p className="m-0 text-sm text-muted-foreground">Nothing is waiting for you.</p>
      )}
    </section>
  )
}

function StepCard({ step, nowMs }: { step: ProgressStep; nowMs: number }) {
  const label = stepStateLabel(step.state, step.needs_owner)
  const elapsed = elapsedLine(step, nowMs)
  const expectation = expectationLine(step)
  const last = lastOutputLine(step, nowMs)
  const who = step.who?.trim() ?? ''
  return (
    <li
      aria-label={step.name ?? 'Step'}
      className="flex min-w-0 max-w-full flex-col gap-1 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-2 py-1.5"
    >
      <span className="break-words text-sm font-medium text-foreground">{step.name}</span>
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
      {elapsed != null ? <span className="break-words text-sm text-muted-foreground">{elapsed}</span> : null}
      {expectation != null ? <span className="break-words text-sm text-muted-foreground">{expectation}</span> : null}
      {last != null ? <span className="break-words text-sm text-muted-foreground">{last}</span> : null}
    </li>
  )
}

function WorkCard({ work, nowMs }: { work: ProgressWork; nowMs: number }) {
  const steps = work.steps ?? []
  const notes = steps.map(activeStepText).filter(text => text !== '')
  const next = nextLine(work.next)
  const risk = work.risk?.trim() ?? ''
  return (
    <article className="flex w-full min-w-0 flex-col gap-2 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-3 py-2.5">
      <h3 className="m-0 break-words text-sm font-medium text-foreground">{work.title}</h3>
      {work.goal?.trim() ? <p className="m-0 break-words text-sm text-foreground">{work.goal}</p> : null}
      {typeof work.round === 'number' ? (
        <p className="m-0 text-sm text-muted-foreground">Round {work.round}</p>
      ) : null}
      <ul aria-label="Steps" className="m-0 flex w-full min-w-0 list-none flex-wrap gap-2 p-0">
        {steps.map((step, index) => (
          <StepCard key={`${step.name ?? 'step'}-${index}`} step={step} nowMs={nowMs} />
        ))}
      </ul>
      {notes.map(note => (
        <p key={note} className="m-0 break-words text-sm text-muted-foreground">
          {note}
        </p>
      ))}
      {next !== '' ? <p className="m-0 break-words text-sm text-foreground">{next}</p> : null}
      {risk !== '' ? <p className="m-0 break-words text-sm text-foreground">Risk: {risk}</p> : null}
    </article>
  )
}

function RecentBlock({ doc }: { doc: ProgressDocument }) {
  const recent = doc.recent ?? []
  const feed = [...(doc.feed ?? [])].sort((a, b) => (b.at ?? 0) - (a.at ?? 0)).slice(0, 15)
  return (
    <details className="w-full min-w-0">
      <summary className="cursor-pointer text-sm font-medium text-foreground">Recent</summary>
      {recent.length === 0 && feed.length === 0 ? (
        <p className="m-0 mt-2 text-sm text-muted-foreground">Nothing recent.</p>
      ) : null}
      {recent.length > 0 ? (
        <ul aria-label="Recent lines" className="m-0 mt-2 flex list-none flex-col gap-1 p-0">
          {recent.map((line, index) => (
            <li key={index} className="break-words text-sm text-foreground">
              {line}
            </li>
          ))}
        </ul>
      ) : null}
      {feed.length > 0 ? (
        <ul aria-label="Feed" className="m-0 mt-2 flex list-none flex-col gap-1 p-0">
          {feed.map((item, index) => (
            <li key={index} className="break-words text-sm text-muted-foreground">
              {[item.work, item.step, item.who, item.text].filter(part => (part ?? '').trim() !== '').join(' · ')}
            </li>
          ))}
        </ul>
      ) : null}
    </details>
  )
}

function Board({ doc, nowMs }: { doc: ProgressDocument; nowMs: number }) {
  const coordinator = doc.coordinator
  const line = coordinator != null ? coordinatorLine(coordinator, nowMs) : ''
  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <Verdict doc={doc} />
      <OwnerSection doc={doc} nowMs={nowMs} />
      {line !== '' ? (
        <p className="m-0 break-words text-sm text-foreground">
          <span className="font-medium">Coordinator: </span>
          {line}
        </p>
      ) : null}
      {(doc.works ?? []).map((work, index) => (
        <WorkCard key={`${work.title ?? 'work'}-${index}`} work={work} nowMs={nowMs} />
      ))}
      <RecentBlock doc={doc} />
    </div>
  )
}

export function AgentWorkBoard() {
  const nowMs = useNow()
  const query = useQuery({
    queryKey: ['agent-progress'],
    queryFn: fetchAgentProgress,
    refetchInterval: AGENT_PROGRESS_POLL_MS,
    retry: false,
  })

  let body
  if (query.isPending) {
    body = <p className="m-0 text-sm text-muted-foreground">Loading…</p>
  } else {
    const failed = query.isError
    const env = query.data
    const doc = env?.document ?? null
    const dim = failed || env?.stale === true
    const age = publishedAgeSeconds(env?.received_at ?? '', env?.age_seconds ?? 0, nowMs)
    body = (
      <div className="flex w-full min-w-0 max-w-full flex-col gap-4 overflow-x-hidden [overflow-wrap:anywhere]">
        {failed ? (
          <p className="m-0 break-words text-sm text-foreground">Progress: Unknown — {failureReason(query.error)}</p>
        ) : env?.stale ? (
          <p className="m-0 break-words text-sm text-foreground">{staleSentence(age)}</p>
        ) : null}
        {doc == null ? (
          failed || env?.stale ? null : (
            <p className="m-0 text-sm text-muted-foreground">No progress has been published.</p>
          )
        ) : dim ? (
          <section aria-label="Last known" className="flex w-full min-w-0 flex-col gap-4 opacity-60">
            <p className="m-0 text-sm font-medium text-foreground">Last known</p>
            <Board doc={doc} nowMs={nowMs} />
          </section>
        ) : (
          <Board doc={doc} nowMs={nowMs} />
        )}
      </div>
    )
  }

  return (
    <OpsSection title="Agent work" bodyPadding="compact" className="min-w-0 max-w-full overflow-x-hidden">
      {body}
    </OpsSection>
  )
}
