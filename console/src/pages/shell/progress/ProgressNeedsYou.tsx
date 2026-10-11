import { useQuery } from '@tanstack/react-query'
import { fetchApprovalList, type ApprovalItem } from '@/api/approvals'
import {
  actionLink,
  waitingLine,
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
import { Tick } from '@/pages/shell/progress/progressClock'

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

function validIds(doc: ProgressDocument): string[] {
  const ids: string[] = []
  for (const entry of doc.owner_now ?? []) {
    const id = entry.approval?.id
    if (isApprovalId(id)) ids.push(id)
  }
  return ids
}

/** The same small link the Needs you row uses, from the document's id only. */
export function RequestChipLink({ approval }: { approval: ProgressApproval | undefined }) {
  if (approval == null || !isApprovalId(approval.id)) return null
  return (
    <a href={approvalHref(approval.id)} className="text-sm text-foreground underline">
      {requestChipLabel(approval.number)}
    </a>
  )
}

function RequestAction({
  approval,
  items,
  readable,
}: {
  approval: ProgressApproval | undefined
  items: ApprovalItem[] | undefined
  readable: boolean
}) {
  if (approval == null) return null
  if (!isApprovalId(approval.id)) {
    return <p className="m-0 text-sm text-muted-foreground">request not shown</p>
  }
  const status = readable ? (items?.find(item => item.id === approval.id)?.status ?? 'unknown') : undefined
  const state = readable ? approvalLiveState(status) : null
  const asking = !readable || status === 'pending'
  return (
    <>
      {state != null ? <p className="m-0 text-sm text-foreground">{state}</p> : null}
      {asking ? (
        <a
          href={approvalHref(approval.id)}
          className="inline-flex w-fit max-w-full items-center rounded-[var(--control-radius)] bg-primary px-3 py-1.5 text-sm font-medium text-primary-foreground no-underline"
        >
          {openRequestLabel(approval.number)}
        </a>
      ) : null}
    </>
  )
}

/**
 * Things waiting for the Owner right now. Each card opens the request it is
 * about, inside the Console, when the published id is one this Console issued.
 */
export function NeedsYouNow({ doc }: { doc: ProgressDocument }) {
  const entries = doc.owner_now ?? []
  const ids = validIds(doc)
  const list = useQuery({
    queryKey: ['approvals', 'all'],
    queryFn: () => fetchApprovalList('all'),
    enabled: ids.length > 0,
    retry: false,
  })
  if (entries.length === 0) return null
  const readable = list.isSuccess && list.data != null
  return (
    <section aria-label="Needs you now" className="flex w-full min-w-0 flex-col gap-2">
      <h3 className="m-0 text-sm font-medium text-foreground">Needs you now</h3>
      <ul className="m-0 flex w-full min-w-0 list-none flex-col gap-2 p-0">
        {entries.map((entry, index) => {
          const status = readable
            ? (list.data?.find(item => item.id === entry.approval?.id)?.status ?? 'unknown')
            : undefined
          const quiet = readable && isApprovalId(entry.approval?.id) && status !== 'pending'
          return (
            <li
              key={`${entry.name ?? 'item'}-${index}`}
              data-request-quiet={quiet ? 'true' : 'false'}
              className={
                quiet
                  ? 'flex w-full min-w-0 flex-col gap-1 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-3 py-2.5 text-muted-foreground opacity-60'
                  : 'flex w-full min-w-0 flex-col gap-1 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-3 py-2.5'
              }
            >
              {entry.work?.trim() ? (
                <span className="break-words text-sm font-medium text-foreground">{entry.work}</span>
              ) : null}
              {entry.name?.trim() ? <span className="break-words text-sm text-foreground">{entry.name}</span> : null}
              <Tick className="text-sm text-muted-foreground" text={now => waitingLine(entry.since ?? 0, now)} />
              {entry.urgency?.trim() ? (
                <span className="break-words text-sm text-muted-foreground">{entry.urgency}</span>
              ) : null}
              <ol className="m-0 list-decimal pl-5 text-sm text-foreground">
                {(entry.actions ?? []).map((action, actionIndex) => (
                  <li
                    key={actionIndex}
                    className={
                      action.done ? 'break-words text-muted-foreground line-through' : 'break-words'
                    }
                  >
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
              <RequestAction approval={entry.approval} items={list.data} readable={readable} />
            </li>
          )
        })}
      </ul>
    </section>
  )
}
