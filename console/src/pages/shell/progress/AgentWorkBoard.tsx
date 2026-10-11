import { memo, useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { StatusLamp } from '@bifrost/ui'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  ageSeconds,
  AGENT_PROGRESS_POLL_MS,
  computedAgeLine,
  coordinatorLine,
  fetchAgentProgress,
  formatDuration,
  generationIsSkewed,
  ownerNextLine,
  publishedAgeSeconds,
  receivedIsStale,
  skewSentence,
  staleSentence,
  type AgentProgressResponse,
  type ProgressCoordinator,
  type ProgressDocument,
} from '@/api/agentProgress'
import { ProgressActivity, ProgressDoneToday } from '@/pages/shell/progress/ProgressActivity'
import { NeedsYouNow } from '@/pages/shell/progress/ProgressNeedsYou'
import { FinishedWorks, WorkCard } from '@/pages/shell/progress/ProgressWorks'
import { ProgressPlan } from '@/pages/shell/progress/ProgressPlan'
import { ProgressRunning } from '@/pages/shell/progress/ProgressRunning'
import { ProgressUsageBlock } from '@/pages/shell/progress/ProgressUsage'
import { NowClock, Tick, useNow } from '@/pages/shell/progress/progressClock'
import { queuedNextLine, splitWorks } from '@/pages/shell/progress/progressModel'

/**
 * Interim bridge (DESIGN §20.4): the coordinating thread publishes the
 * document its local board already computes. Later the runtime computes it
 * from events. Duration texts tick each second; the rest of the board does not.
 */
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

function Verdict({ doc }: { doc: ProgressDocument }) {
  const at = doc.at
  const next = queuedNextLine(doc.works, doc.queue)
  return (
    <div className="flex w-full min-w-0 flex-col gap-1">
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
      {next != null ? <p className="m-0 break-words text-sm text-foreground">{next}</p> : null}
    </div>
  )
}

function NextTime({ doc }: { doc: ProgressDocument }) {
  if ((doc.owner_now ?? []).length > 0) return null
  return (
    <section aria-label="Next time you are needed" className="flex w-full min-w-0 flex-col gap-2">
      <h3 className="m-0 text-sm font-medium text-foreground">Next time you are needed</h3>
      {doc.owner_next != null && ownerNextLine(doc.owner_next) !== '' ? (
        <p className="m-0 break-words text-sm text-foreground">{ownerNextLine(doc.owner_next)}</p>
      ) : (
        <p className="m-0 text-sm text-muted-foreground">Nothing is waiting for you.</p>
      )}
    </section>
  )
}

const COORDINATOR_STALE_SECONDS = 20 * 60

function CoordinatorAge({ at, lead }: { at: number | undefined; lead: boolean }) {
  const now = useNow()
  if (typeof at !== 'number' || !(at > 0)) return null
  const age = ageSeconds(at, now)
  const stale = age > COORDINATOR_STALE_SECONDS
  const text = stale ? `updated ${formatDuration(age)} ago — may be stale` : `updated ${formatDuration(age)} ago`
  return (
    <span
      data-stale={stale ? 'true' : 'false'}
      className={stale ? 'font-medium text-destructive' : 'text-muted-foreground'}
    >
      {lead ? ` · ${text}` : text}
    </span>
  )
}

function CoordinatorLine({ coordinator }: { coordinator: ProgressCoordinator | undefined }) {
  if (coordinator == null) return null
  const doing = coordinator.doing?.trim() ?? ''
  const next = coordinator.next?.trim() ?? ''
  if (doing === '' && next === '') {
    const legacy = coordinatorLine(coordinator, Date.now())
    if (legacy === '' && !((coordinator.at ?? 0) > 0)) return null
    return (
      <p className="m-0 break-words text-sm text-foreground">
        <span className="font-medium">Coordinator: </span>
        <Tick text={now => coordinatorLine(coordinator, now)} />
      </p>
    )
  }
  return (
    <p className="m-0 break-words text-sm text-foreground">
      {doing !== '' ? <span>Coordinator: {doing}</span> : null}
      {next !== '' ? <span>{doing !== '' ? ' · ' : ''}Then: {next}</span> : null}
      <CoordinatorAge at={coordinator.at} lead={doing !== '' || next !== ''} />
    </p>
  )
}

const WorkDocument = memo(function WorkDocument({ doc }: { doc: ProgressDocument }) {
  const paints = useRef(0)
  paints.current += 1
  const { active, finished } = splitWorks(doc.works ?? [])
  return (
    <div data-testid="agent-work-document" data-paints={paints.current} className="flex w-full min-w-0 flex-col gap-4">
      <Verdict doc={doc} />
      <NeedsYouNow doc={doc} />
      <CoordinatorLine coordinator={doc.coordinator} />
      <NextTime doc={doc} />
      <ProgressPlan roadmap={doc.roadmap} />
      <ProgressUsageBlock usage={doc.usage} />
      <ProgressRunning works={active} />
      {active.map((work, index) => (
        <WorkCard key={`${work.title ?? 'work'}-${index}`} work={work} index={index} at={doc.at} />
      ))}
      <ProgressActivity feed={doc.feed} />
      <ProgressDoneToday recent={doc.recent} />
      <FinishedWorks works={finished} at={doc.at} />
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
