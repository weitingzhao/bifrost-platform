import { useQuery } from '@tanstack/react-query'
import { fetchTaskDelivery, taskStages, type DeliveryRead, type StageReading, type TaskStage } from '@/api/deliveryStages'
import { clockText } from './deliveryStripModel'

/** One task's delivery is read again at most once a minute, however many cards and rows show it. */
const DELIVERY_POLL_MS = 60_000

/** The four stages the strip shows, in order; planned and verified are told elsewhere on the card (state, Review). */
const SHOWN: [TaskStage, string][] = [['handed_in', 'Handed in'], ['on_main', 'On main'], ['on_stg', 'STG'], ['on_prod', 'PROD']]

function Stage({ label, reading }: { label: string; reading: StageReading }) {
  return <span data-stage={reading.stage} data-stage-state={reading.state} title={reading.state === 'unknown' ? reading.reason : undefined}>
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
