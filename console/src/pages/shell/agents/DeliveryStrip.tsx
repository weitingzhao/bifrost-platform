import { useQuery } from '@tanstack/react-query'
import { commitStages, deliverySettled, fetchCommitDelivery, fetchTaskDelivery, taskStages, type DeliveryRead, type StageReading } from '@/api/deliveryStages'
import { clockText } from './deliveryStripModel'

/** One task's delivery is read again at most once a minute, however many cards and rows show it. */
const DELIVERY_POLL_MS = 60_000

type Shown = [StageReading['stage'], string][]
/** The four stages the strip shows, in order; planned and verified are told elsewhere on the card (state, Review). */
const SHOWN: Shown = [['handed_in', 'Handed in'], ['on_main', 'On main'], ['on_stg', 'STG'], ['on_prod', 'PROD']]
/** A commit's strip also shows when its CI went green: the evidence an external work's hand-in rests on. */
const COMMIT_SHOWN: Shown = [['handed_in', 'Handed in'], ['ci', 'CI'], ['on_main', 'On main'], ['on_stg', 'STG'], ['on_prod', 'PROD']]
/** How many of one work's commits get a strip; the rest are counted, not read. */
const COMMITS_SHOWN = 3

function Stage({ label, reading }: { label: string; reading: StageReading }) {
  return <span data-stage={reading.stage} data-stage-state={reading.state} title={reading.state === 'reached' ? undefined : reading.reason}>
    {label}{' '}
    {reading.state === 'reached' ? <time dateTime={reading.at} title={reading.at}>{clockText(reading.at)}</time>
      : reading.state === 'unknown' ? 'Unknown' : '—'}
  </span>
}

/** The strip as a read gives it: 'Handed in HH:MM → On main HH:MM → STG HH:MM → PROD —'. */
export function StageStrip({ task, read }: { task: string; read: DeliveryRead }) {
  const stages = new Map(taskStages(task, read).map(s => [s.stage, s]))
  return <span data-delivery-strip={task} className="block text-xs text-muted-foreground [overflow-wrap:anywhere]">
    {SHOWN.map(([stage, label], i) => <span key={stage}>{i > 0 && ' → '}<Stage label={label} reading={stages.get(stage)!} /></span>)}
  </span>
}

/** A commit's strip as a read gives it: 'abc1234 · Handed in HH:MM → CI HH:MM → On main Unknown → STG HH:MM → PROD —'. */
export function CommitStageStrip({ commit, read }: { commit: string; read: DeliveryRead }) {
  const stages = new Map(commitStages(read).map(s => [s.stage, s]))
  return <span data-delivery-strip={commit} className="block text-xs text-muted-foreground [overflow-wrap:anywhere]">
    <code title={commit}>{commit.slice(0, 8)}</code> · {COMMIT_SHOWN.map(([stage, label], i) => <span key={stage}>{i > 0 && ' → '}<Stage label={label} reading={stages.get(stage)!} /></span>)}
  </span>
}

/**
 * A task's stage strip on its line (step card, Live row): GET /delivery?task=, one read per task shared by every place
 * that shows it. A failed read shows each stage Unknown, with why on hover; a task that handed no change in (404) none
 * reached.
 */
export function DeliveryStrip({ task }: { task: string }) {
  const q = useQuery({ queryKey: ['agentrt-delivery', task], queryFn: ({ signal }) => fetchTaskDelivery(task, signal),
    staleTime: DELIVERY_POLL_MS, refetchInterval: DELIVERY_POLL_MS, retry: false })
  if (q.data) return <StageStrip task={task} read={q.data} />
  if (q.isError) return <StageStrip task={task} read={{ state: 'unknown', reason: q.error.message }} />
  return <span data-delivery-strip={task} className="block text-xs text-muted-foreground">Delivery · reading…</span>
}

/**
 * The stage strip of a commit an external work handed in: GET /delivery?commit=, one read per commit shared by every
 * place that shows it, read again each minute until the commit is on PROD. A failed read shows each stage Unknown,
 * with why on hover; a commit the platform cannot name (404) none reached.
 */
export function CommitDeliveryStrip({ commit }: { commit: string }) {
  const q = useQuery({ queryKey: ['agentrt-delivery-commit', commit], queryFn: ({ signal }) => fetchCommitDelivery(commit, signal),
    staleTime: DELIVERY_POLL_MS, refetchInterval: query => deliverySettled(query.state.data) ? false : DELIVERY_POLL_MS, retry: false })
  if (q.data) return <CommitStageStrip commit={commit} read={q.data} />
  if (q.isError) return <CommitStageStrip commit={commit} read={{ state: 'unknown', reason: q.error.message }} />
  return <span data-delivery-strip={commit} className="block text-xs text-muted-foreground">Delivery · reading…</span>
}

/** The strips of an external work's commits (the commit_shas its heartbeats and its submitted event named): the first few, the rest counted. */
export function WorkDeliveryStrips({ commits }: { commits: string[] | undefined }) {
  if (!commits?.length) return null
  return <>
    {commits.slice(0, COMMITS_SHOWN).map(sha => <CommitDeliveryStrip key={sha} commit={sha} />)}
    {commits.length > COMMITS_SHOWN && <span className="block text-xs text-muted-foreground">Delivery of {commits.length - COMMITS_SHOWN} more commits not shown</span>}
  </>
}
