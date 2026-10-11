import { createContext, memo, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { DenseTag, StatusLamp } from '@bifrost/ui'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  actionLink,
  activeStepText,
  AGENT_PROGRESS_POLL_MS,
  computedAgeLine,
  coordinatorLine,
  elapsedLine,
  expectationLine,
  fetchAgentProgress,
  generationIsSkewed,
  lastOutputLine,
  nextLine,
  ownerNextLine,
  publishedAgeSeconds,
  receivedIsStale,
  skewSentence,
  staleSentence,
  stepStateLabel,
  waitingLine,
  type AgentProgressResponse,
  type ProgressAction,
  type ProgressDocument,
  type ProgressStep,
  type ProgressWork,
} from '@/api/agentProgress'

/**
 * Interim bridge (DESIGN §20.4): the coordinating thread publishes the
 * document its local board already computes. Later the runtime computes it
 * from events. Duration texts tick each second; the rest of the board does not.
 */
const NowContext = createContext(0)

function NowClock({ children }: { children: ReactNode }) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [])
  return <NowContext.Provider value={now}>{children}</NowContext.Provider>
}

function useNow(): number {
  return useContext(NowContext)
}

function Tick({ text, className }: { text: (nowMs: number) => string | null; className?: string }) {
  const now = useNow()
  const value = text(now)
  if (value == null || value === '') return null
  if (className == null) return <>{value}</>
  return <span className={className}>{value}</span>
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
  const at = doc.at
  return (
    <div className="flex w-full min-w-0 items-start gap-2">
      <StatusLamp value={lampFor(doc.level)} />
      <p className="m-0 min-w-0 break-words text-sm font-medium text-foreground">
        <span data-level={doc.level ?? ''}>{doc.verdict ?? ''}</span>
        {typeof at === 'number' ? (
          <>
            {' '}
            <Tick className="font-normal text-muted-foreground" text={now => computedAgeLine(at, now)} />
          </>
        ) : null}
      </p>
    </div>
  )
}

function ActionBody({ action }: { action: ProgressAction }) {
  const view = actionLink(action)
  if (view.kind === 'anchor') {
    return (
      <a
        href={view.href}
        rel="noopener noreferrer"
        {...(view.external ? { target: '_blank' } : {})}
        className="underline"
      >
        {view.label}
      </a>
    )
  }
  if (view.kind === 'blocked') {
    const note = view.host !== '' ? `link not shown: ${view.host}` : 'link not shown'
    return (
      <>
        <span>{view.text}</span>
        <span className="mt-0.5 block text-xs text-muted-foreground">{note}</span>
      </>
    )
  }
  return <>{view.text}</>
}

function OwnerSection({ doc }: { doc: ProgressDocument }) {
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
              <Tick className="text-sm text-muted-foreground" text={now => waitingLine(entry.since ?? 0, now)} />
              <ol className="m-0 list-decimal pl-5 text-sm text-foreground">
                {(entry.actions ?? []).map((action, actionIndex) => (
                  <li key={actionIndex} className={action.done ? 'break-words line-through' : 'break-words'}>
                    <ActionBody action={action} />
                  </li>
                ))}
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

function StepCard({ step }: { step: ProgressStep }) {
  const label = stepStateLabel(step.state, step.needs_owner)
  const expectation = expectationLine(step)
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
      <Tick className="break-words text-sm text-muted-foreground" text={now => elapsedLine(step, now)} />
      {expectation != null ? <span className="break-words text-sm text-muted-foreground">{expectation}</span> : null}
      <Tick className="break-words text-sm text-muted-foreground" text={now => lastOutputLine(step, now)} />
    </li>
  )
}

function WorkCard({ work }: { work: ProgressWork }) {
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
          <StepCard key={`${step.name ?? 'step'}-${index}`} step={step} />
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

const WorkDocument = memo(function WorkDocument({ doc }: { doc: ProgressDocument }) {
  const paints = useRef(0)
  paints.current += 1
  const coordinator = doc.coordinator
  return (
    <div data-testid="agent-work-document" data-paints={paints.current} className="flex w-full min-w-0 flex-col gap-4">
      <Verdict doc={doc} />
      <OwnerSection doc={doc} />
      {coordinator != null ? (
        <p className="m-0 break-words text-sm text-foreground">
          <span className="font-medium">Coordinator: </span>
          <Tick text={now => coordinatorLine(coordinator, now)} />
        </p>
      ) : null}
      {(doc.works ?? []).map((work, index) => (
        <WorkCard key={`${work.title ?? 'work'}-${index}`} work={work} />
      ))}
      <RecentBlock doc={doc} />
    </div>
  )
})

function Freshness({
  failed,
  error,
  env,
}: {
  failed: boolean
  error: unknown
  env: AgentProgressResponse | undefined
}) {
  const now = useNow()
  const doc = env?.document ?? null
  const age = publishedAgeSeconds(env?.received_at ?? '', env?.age_seconds ?? 0, now)
  const stale = env?.stale === true || receivedIsStale(env?.received_at ?? '', env?.age_seconds ?? 0, now)
  const skewed = generationIsSkewed(doc?.at, env?.received_at ?? '', now)
  const dim = failed || stale || skewed
  return (
    <>
      {failed ? (
        <p className="m-0 break-words text-sm text-foreground">Progress: Unknown — {failureReason(error)}</p>
      ) : null}
      {stale ? <p className="m-0 break-words text-sm text-foreground">{staleSentence(age)}</p> : null}
      {skewed && typeof doc?.at === 'number' ? (
        <p className="m-0 break-words text-sm text-foreground">{skewSentence(doc.at, now)}</p>
      ) : null}
      {doc == null ? (
        failed || stale || skewed ? null : (
          <p className="m-0 text-sm text-muted-foreground">No progress has been published.</p>
        )
      ) : (
        <section
          aria-label={dim ? 'Last known' : undefined}
          className={
            dim
              ? 'flex w-full min-w-0 flex-col gap-4 opacity-60'
              : 'flex w-full min-w-0 flex-col gap-4'
          }
        >
          {dim ? <p className="m-0 text-sm font-medium text-foreground">Last known</p> : null}
          <WorkDocument doc={doc} />
        </section>
      )}
    </>
  )
}

const ProgressBody = memo(function ProgressBody({
  pending,
  failed,
  error,
  env,
}: {
  pending: boolean
  failed: boolean
  error: unknown
  env: AgentProgressResponse | undefined
}) {
  if (pending) return <p className="m-0 text-sm text-muted-foreground">Loading…</p>
  return (
    <div className="flex w-full min-w-0 max-w-full flex-col gap-4 overflow-x-hidden [overflow-wrap:anywhere]">
      <Freshness failed={failed} error={error} env={env} />
    </div>
  )
})

export function AgentWorkBoard() {
  const query = useQuery({
    queryKey: ['agent-progress'],
    queryFn: fetchAgentProgress,
    refetchInterval: AGENT_PROGRESS_POLL_MS,
    retry: false,
  })

  return (
    <OpsSection title="Agent work" bodyPadding="compact" className="min-w-0 max-w-full overflow-x-hidden">
      <NowClock>
        <ProgressBody pending={query.isPending} failed={query.isError} error={query.error} env={query.data} />
      </NowClock>
    </OpsSection>
  )
}
