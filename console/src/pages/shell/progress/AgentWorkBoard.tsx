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
import { FinishedWorks, IdleWorkLine, WorkCard } from '@/pages/shell/progress/ProgressWorks'
import { ProgressPlan } from '@/pages/shell/progress/ProgressPlan'
import { ProgressRunning } from '@/pages/shell/progress/ProgressRunning'
import { ProgressSummary, workIsHot } from '@/pages/shell/progress/ProgressSummary'
import { ProgressUsageBlock } from '@/pages/shell/progress/ProgressUsage'
import { FoldProvider, SectionFold } from '@/pages/shell/progress/progressFold'
import { NowClock, Tick, useNow } from '@/pages/shell/progress/progressClock'
import { runningRows, splitWorks } from '@/pages/shell/progress/progressModel'
import { hasNeedsYou } from '@/pages/shell/progress/progressSummaryModel'

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
    </div>
  )
}

function NextTime({ doc }: { doc: ProgressDocument }) {
  if (hasNeedsYou(doc.owner_items, doc.standing_items, doc.owner_now)) return null
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

function needsYouOpen(doc: ProgressDocument): boolean {
  return hasNeedsYou(doc.owner_items, doc.standing_items, doc.owner_now)
}

const WorkDocument = memo(function WorkDocument({
  doc,
  env,
}: {
  doc: ProgressDocument
  env?: AgentProgressResponse
}) {
  const paints = useRef(0)
  paints.current += 1
  const { active, finished } = splitWorks(doc.works ?? [])
  const hot = active.filter(workIsHot)
  const idle = active.filter(work => !workIsHot(work))
  const running = runningRows(doc.works ?? []).length
  const coord = doc.coordinator?.doing?.trim() || doc.coordinator?.state?.trim() || ''
  const needsDigest = needsYouOpen(doc) ? 'Open' : 'Nothing now'
  return (
    <div data-testid="agent-work-document" data-paints={paints.current} className="flex w-full min-w-0 flex-col gap-4">
      <Verdict doc={doc} />
      <ProgressSummary doc={doc} env={env} />
      <SectionFold id="sec-needs-you" title="Needs you" digest={needsDigest} defaultOpen={needsYouOpen(doc)}>
        <NeedsYouNow doc={doc} />
      </SectionFold>
      <NextTime doc={doc} />
      <SectionFold id="sec-running" title="Running now" digest={running > 0 ? `${running} steps` : 'Nothing running'} defaultOpen={running > 0}>
        <ProgressRunning works={active} />
      </SectionFold>
      <SectionFold id="sec-coord" title="Coordinator" digest={coord === '' ? '' : coord.slice(0, 48)} defaultOpen={false}>
        <CoordinatorLine coordinator={doc.coordinator} />
      </SectionFold>
      {doc.roadmap != null ? (
        <SectionFold id="sec-plan" title="Plan" digest={`${planDigest(doc)}`} defaultOpen={false}>
          <ProgressPlan roadmap={doc.roadmap} />
        </SectionFold>
      ) : null}
      {doc.usage != null ? (
        <SectionFold id="sec-quota" title="Quota" digest={doc.usage.pools != null ? `${doc.usage.pools.length} pools` : 'Usage'} defaultOpen={false}>
          <ProgressUsageBlock usage={doc.usage} />
        </SectionFold>
      ) : null}
      <SectionFold
        id="sec-works"
        title="Every work"
        digest={`${hot.length} underway · ${idle.length} quiet`}
        defaultOpen={hot.length > 0}
      >
        {hot.map(work => {
          const index = (doc.works ?? []).indexOf(work)
          return <WorkCard key={`${work.title ?? 'work'}-${index}`} work={work} index={index} at={doc.at} />
        })}
        {idle.map(work => {
          const index = (doc.works ?? []).indexOf(work)
          return <IdleWorkLine key={`${work.title ?? 'idle'}-${index}`} work={work} index={index} at={doc.at} />
        })}
      </SectionFold>
      <ProgressActivity feed={doc.feed} />
      <ProgressDoneToday recent={doc.recent} />
      <FinishedWorks works={finished} at={doc.at} />
    </div>
  )
})

function planDigest(doc: ProgressDocument): string {
  const counts = doc.roadmap != null ? { done: 0, doing: 0, todo: 0 } : null
  if (counts == null || doc.roadmap == null) return ''
  for (const group of doc.roadmap.groups ?? []) {
    if (group.state === 'done' || group.state === 'doing' || group.state === 'todo') {
      counts[group.state] += group.items?.length ?? 0
    }
  }
  return `${counts.done} live · ${counts.doing} in progress · ${counts.todo} not started`
}

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
          <WorkDocument doc={doc} env={env} />
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
      <FoldProvider>
        <NowClock>
          <ProgressBody pending={query.isPending} failed={query.isError} error={query.error} env={query.data} />
        </NowClock>
      </FoldProvider>
    </OpsSection>
  )
}
