import { useQuery } from '@tanstack/react-query'
import { fetchApprovalList, isAwaitingDecision, type ApprovalItem } from '@/api/approvals'
import {
  actionLink,
  AGENT_PROGRESS_POLL_MS,
  waitingLine,
  type OwnerNow,
  type ProgressAction,
  type ProgressDocument,
} from '@/api/agentProgress'
import {
  approvalLiveState,
  isApprovalId,
  openRequestLabel,
  requestChipLabel,
  type ProgressApproval,
} from '@/api/agentProgressExtra'
import { approvalHref } from '@/lib/shell/consoleRoutes'
import { Tick, useNow } from '@/pages/shell/progress/progressClock'
import { boundDocumentText } from '@/pages/shell/progress/progressPart'

const NAME_MAX = 120
const URGENCY_MAX = 200
const ACTION_MAX = 200
const ACTION_CAP = 6

function ActionBody({ action }: { action: ProgressAction }) {
  const view = actionLink(action)
  if (view.kind === 'anchor') {
    return (
      <a href={view.href} rel="noopener noreferrer" {...(view.external ? { target: '_blank' } : {})} className="underline">
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

function shownAction(action: ProgressAction): ProgressAction {
  const linkText = action.link_text
  return {
    ...action,
    text: boundDocumentText(action.text ?? '', ACTION_MAX),
    link_text: linkText == null ? linkText : boundDocumentText(linkText, ACTION_MAX),
  }
}

function ActionItems({ actions, start }: { actions: readonly ProgressAction[]; start?: number }) {
  return (
    <ol start={start} className="m-0 list-decimal pl-5 text-sm text-foreground">
      {actions.map((action, actionIndex) => (
        <li key={actionIndex} className={action.done ? 'break-words text-muted-foreground line-through' : 'break-words'}>
          {action.done ? (
            <input
              type="checkbox"
              checked
              disabled
              aria-label="Done"
              onChange={() => undefined}
              className="mr-1 align-middle"
            />
          ) : null}
          <ActionBody action={action} />
        </li>
      ))}
    </ol>
  )
}

function ActionList({ actions }: { actions: readonly ProgressAction[] }) {
  if (actions.length === 0) return null
  const clean = actions.map(shownAction)
  const shown = clean.slice(0, ACTION_CAP)
  const rest = clean.slice(ACTION_CAP)
  return (
    <>
      <ActionItems actions={shown} />
      {rest.length > 0 ? (
        <details className="min-w-0">
          <summary className="cursor-pointer text-sm text-muted-foreground">{`${rest.length} more`}</summary>
          <ActionItems actions={rest} start={ACTION_CAP + 1} />
        </details>
      ) : null}
    </>
  )
}

/** The approvals list this page already loads. Same cadence as the progress document. */
function useProgressApprovals(enabled: boolean) {
  return useQuery({
    queryKey: ['approvals', 'all'],
    queryFn: () => fetchApprovalList('all'),
    enabled,
    retry: false,
    refetchInterval: AGENT_PROGRESS_POLL_MS,
    refetchOnWindowFocus: true,
  })
}

function validOwnerIds(doc: ProgressDocument): string[] {
  const ids: string[] = []
  for (const entry of doc.owner_now ?? []) {
    const id = entry.approval?.id
    if (isApprovalId(id) && !ids.includes(id)) ids.push(id)
  }
  return ids
}

/** Number and summary from the approvals list. The document's copies are not used. */
function requestHeadline(item: Pick<ApprovalItem, 'number' | 'summary'>): string {
  const summary = item.summary?.trim() ?? ''
  const number = typeof item.number === 'number' && Number.isFinite(item.number) ? item.number : null
  if (number != null && summary !== '') return `Request #${number} — ${summary}`
  if (number != null) return `Request #${number}`
  if (summary !== '') return `Request — ${summary}`
  return 'Request'
}

function liveStatus(item: ApprovalItem, nowMs: number): string {
  if (item.status === 'pending' && !isAwaitingDecision(item, nowMs)) return 'expired'
  return item.status
}

function stateWords(status: string): string {
  return approvalLiveState(status) ?? 'Unknown'
}

/**
 * The same small link the Needs you row uses. The number is the real request's,
 * and only when that id is in the approvals list.
 */
export function RequestChipLink({ approval }: { approval: ProgressApproval | undefined }) {
  const id = approval?.id
  const list = useProgressApprovals(isApprovalId(id))
  if (!isApprovalId(id) || !list.isSuccess || list.data == null) return null
  const item = list.data.find(row => row.id === id)
  if (item == null) return null
  return (
    <a href={approvalHref(id)} className="text-sm text-foreground underline">
      {requestChipLabel(item.number)}
    </a>
  )
}

function AgentNote({
  entry,
  showTimer,
  showAsk,
}: {
  entry: OwnerNow
  showTimer: boolean
  showAsk: boolean
}) {
  const work = boundDocumentText(entry.work ?? '', NAME_MAX)
  const name = boundDocumentText(entry.name ?? '', NAME_MAX)
  const urgency = showAsk ? boundDocumentText(entry.urgency ?? '', URGENCY_MAX) : ''
  const actions = showAsk ? (entry.actions ?? []) : []
  if (work === '' && name === '' && urgency === '' && actions.length === 0 && !showTimer) return null
  return (
    <div className="mt-1 flex min-w-0 flex-col gap-1 border-t border-[var(--card-border)] pt-1">
      <span className="text-sm text-muted-foreground">Agent's note:</span>
      {work !== '' ? <span className="break-words text-sm text-foreground">{work}</span> : null}
      {name !== '' ? <span className="break-words text-sm text-foreground">{name}</span> : null}
      {showTimer ? <Tick className="text-sm text-muted-foreground" text={now => waitingLine(entry.since ?? 0, now)} /> : null}
      {urgency !== '' ? <span className="break-words text-sm text-muted-foreground">{urgency}</span> : null}
      <ActionList actions={actions} />
    </div>
  )
}

function OpenNeedsYou() {
  return (
    <a href="#needs-you" className="w-fit max-w-full text-sm text-foreground underline">
      Open Needs you
    </a>
  )
}

function PlainCard({ entry }: { entry: OwnerNow }) {
  const work = boundDocumentText(entry.work ?? '', NAME_MAX)
  const name = boundDocumentText(entry.name ?? '', NAME_MAX)
  const urgency = boundDocumentText(entry.urgency ?? '', URGENCY_MAX)
  return (
    <>
      {work !== '' ? <span className="break-words text-sm font-medium text-foreground">{work}</span> : null}
      {name !== '' ? <span className="break-words text-sm text-foreground">{name}</span> : null}
      <Tick className="text-sm text-muted-foreground" text={now => waitingLine(entry.since ?? 0, now)} />
      {urgency !== '' ? <span className="break-words text-sm text-muted-foreground">{urgency}</span> : null}
      <ActionList actions={entry.actions ?? []} />
    </>
  )
}

function RequestCard({
  entry,
  item,
  unreadable,
}: {
  entry: OwnerNow
  item: ApprovalItem | undefined
  unreadable: boolean
}) {
  const now = useNow()
  if (unreadable) {
    return (
      <>
        <p className="m-0 text-sm text-foreground">Could not read the request's state</p>
        <OpenNeedsYou />
        <AgentNote entry={entry} showTimer={false} showAsk={false} />
      </>
    )
  }
  if (item == null) {
    return (
      <>
        <p className="m-0 text-sm text-muted-foreground">The request this step refers to was not found</p>
        <AgentNote entry={entry} showTimer showAsk />
      </>
    )
  }
  const status = liveStatus(item, now)
  const asking = status === 'pending'
  return (
    <>
      <p data-request-line="" className="m-0 break-words text-sm font-medium text-foreground">
        {requestHeadline(item)}
      </p>
      <p className="m-0 text-sm text-foreground">{stateWords(status)}</p>
      {asking ? (
        <a
          href={approvalHref(item.id)}
          className="inline-flex w-fit max-w-full items-center rounded-[var(--control-radius)] bg-primary px-3 py-1.5 text-sm font-medium text-primary-foreground no-underline"
        >
          {openRequestLabel(item.number)}
        </a>
      ) : null}
      {asking ? <AgentNote entry={entry} showTimer showAsk /> : null}
    </>
  )
}

function OwnerCard({
  entry,
  items,
  readable,
}: {
  entry: OwnerNow
  items: ApprovalItem[] | undefined
  readable: boolean
}) {
  const approval = entry.approval
  if (approval == null) return <PlainCard entry={entry} />
  if (!isApprovalId(approval.id)) {
    return (
      <>
        <p className="m-0 text-sm text-muted-foreground">request not shown</p>
        <AgentNote entry={entry} showTimer showAsk />
      </>
    )
  }
  const item = readable ? items?.find(row => row.id === approval.id) : undefined
  return <RequestCard entry={entry} item={item} unreadable={!readable} />
}

/**
 * Things waiting for the Owner right now. The request line is the approvals
 * list. What the progress document says sits under "Agent's note:".
 */
function requestIsQuiet(entry: OwnerNow, items: ApprovalItem[] | undefined, readable: boolean, nowMs: number): boolean {
  if (!readable || !isApprovalId(entry.approval?.id)) return false
  const item = items?.find(row => row.id === entry.approval?.id)
  if (item == null) return false
  return liveStatus(item, nowMs) !== 'pending'
}

export function NeedsYouNow({ doc }: { doc: ProgressDocument }) {
  const now = useNow()
  const entries = doc.owner_now ?? []
  const ids = validOwnerIds(doc)
  const list = useProgressApprovals(ids.length > 0)
  if (entries.length === 0) return null
  const readable = list.isSuccess && list.data != null
  return (
    <section aria-label="Needs you now" className="flex w-full min-w-0 flex-col gap-2">
      <h3 className="m-0 text-sm font-medium text-foreground">Needs you now</h3>
      <ul className="m-0 flex w-full min-w-0 list-none flex-col gap-2 p-0">
        {entries.map((entry, index) => {
          const quiet = requestIsQuiet(entry, list.data, readable, now)
          return (
            <li
              key={`${entry.name ?? 'item'}-${index}`}
              data-request-quiet={entry.approval == null ? undefined : quiet ? 'true' : 'false'}
              className={
                quiet
                  ? 'flex w-full min-w-0 flex-col gap-1 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-3 py-2.5 text-muted-foreground opacity-60'
                  : 'flex w-full min-w-0 flex-col gap-1 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-3 py-2.5'
              }
            >
              <OwnerCard entry={entry} items={list.data} readable={readable} />
            </li>
          )
        })}
      </ul>
    </section>
  )
}
