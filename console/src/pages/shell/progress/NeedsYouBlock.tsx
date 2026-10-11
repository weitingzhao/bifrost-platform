import { useRef, useState, type ReactNode } from 'react'
import { isAwaitingDecision, type ApprovalItem } from '@/api/approvals'
import { formatLocalTime, receivedIsStale, waitingLine, type AgentProgressResponse, type OwnerNow } from '@/api/agentProgress'
import { isApprovalId } from '@/api/agentProgressExtra'
import { Tick, useNow } from '@/pages/shell/progress/progressClock'
import { AgentText } from '@/pages/shell/progress/AgentText'
import { DocumentActions, RequestCard } from '@/pages/shell/progress/ProgressNeedsYou'
import {
  agentText,
  askAct,
  bucketAsks,
  deadlineMarker,
  needsYouTotal,
  orderedAsks,
  requestAgentLines,
  requestItem,
  requestLinked,
  turnMarker,
  type ApprovalRead,
  type AskSource,
  type DocumentRead,
} from '@/pages/shell/progress/progressSummaryModel'

function requestHeadline(item: Pick<ApprovalItem, 'number' | 'summary'>): string {
  const summary = item.summary?.trim() ?? ''
  const number = typeof item.number === 'number' && Number.isFinite(item.number) ? item.number : null
  if (number != null && summary !== '') return `Request #${number} — ${summary}`
  if (number != null) return `Request #${number}`
  if (summary !== '') return `Request — ${summary}`
  return 'Request'
}

function requestQuiet(item: ApprovalItem | undefined, nowMs: number): boolean {
  if (item == null) return false
  if (item.status !== 'pending') return true
  return !isAwaitingDecision(item, nowMs)
}

function Missing({ field, amber }: { field: string; amber?: boolean }) {
  return (
    <span data-missing={field} className={amber ? 'text-warning' : ''}>
      Not written
    </span>
  )
}

function AgentBits({ lines }: { lines: string[] }) {
  if (lines.length === 0) return null
  return (
    <span data-agent-note="" data-agent-text="" className="block min-w-0">
      <span className="text-muted-foreground">Agent&apos;s note: </span>
      {lines.map((line, index) => (
        <span key={index} data-agent-text="" className="break-words">
          {line}
        </span>
      ))}
    </span>
  )
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div data-fact={label} className="flex min-w-0 flex-col gap-0.5">
      <span className="text-muted-foreground">{label}</span>
      <div className="min-w-0">{children}</div>
    </div>
  )
}

function ownerEntry(ask: AskSource): OwnerNow {
  const lines = requestAgentLines(ask)
  return {
    name: ask.name,
    work: ask.work,
    since: ask.since,
    urgency: ask.urgency ?? (lines.length > 0 ? lines.join(' ') : undefined),
    actions: ask.actions,
    approval: ask.approval,
  }
}

function Latest({
  ask,
  items,
  read,
  linked,
}: {
  ask: AskSource
  items: ApprovalItem[] | undefined
  read: ApprovalRead
  linked: boolean
}) {
  const now = useNow()
  const deadline = deadlineMarker(ask, items, read, now)
  const latest = agentText(ask.deadline_text)
  if (linked) {
    if (deadline === 'none' || deadline === 'unknown') return <span>Unknown</span>
    return (
      <span data-deadline="by" data-urgent={deadline.amber ? 'true' : 'false'} className={deadline.amber ? 'text-warning' : ''}>
        {deadline.text}
      </span>
    )
  }
  if (latest !== '') return <AgentBits lines={[latest]} />
  if (typeof ask.deadline === 'number' && deadline !== 'none' && deadline !== 'unknown') {
    return (
      <span data-deadline="by" data-urgent={deadline.amber ? 'true' : 'false'} className={deadline.amber ? 'text-warning' : ''}>
        {deadline.text}
      </span>
    )
  }
  if (deadline === 'unknown') return <span>Unknown</span>
  return <Missing field="deadline" />
}

function actionHasWords(action: { text?: string; link?: string; link_text?: string }): boolean {
  return agentText(action.text) !== '' || agentText(action.link_text) !== '' || agentText(action.link) !== ''
}

function HowTo({ actions }: { actions: AskSource['actions'] }) {
  const written = (actions ?? []).filter(actionHasWords)
  if (written.length === 0) {
    return (
      <span data-agent-note="" data-agent-text="" className="block min-w-0">
        <span className="text-muted-foreground">Agent&apos;s note: </span>
        <span data-agent-text="" data-missing="actions">Not written</span>
      </span>
    )
  }
  return (
    <span data-agent-note="" data-agent-text="" className="block min-w-0">
      <span className="text-muted-foreground">Agent&apos;s note: </span>
      <DocumentActions actions={written} />
    </span>
  )
}

function AskBody({
  ask,
  items,
  read,
}: {
  ask: AskSource
  items: ApprovalItem[] | undefined
  read: ApprovalRead
}) {
  const linked = requestLinked(ask)
  const item = requestItem(ask, items, read)
  const advice = agentText(ask.advice)
  const where = agentText(ask.where)
  const reply = agentText(ask.reply)
  const takes = agentText(ask.takes)
  const before = agentText(ask.not_before)
  const blockedLines = [agentText(ask.blocks_text), agentText(ask.blocks)].filter((line, index, all) => line !== '' && all.indexOf(line) === index)
  const badId = ask.approval != null && !isApprovalId(ask.approval.id)
  return (
    <div className="flex min-w-0 flex-col gap-2 pb-2">
      <Fact label="Recommended">{advice !== '' ? <AgentBits lines={[advice]} /> : <Missing field="advice" />}</Fact>
      <Fact label="Where">
        {linked ? <span>The request card, here</span> : where !== '' ? <AgentBits lines={[where]} /> : <Missing field="where" amber />}
      </Fact>
      <Fact label="How to do it">
        {linked ? <RequestCard entry={ownerEntry(ask)} item={item} unreadable={read === 'unreadable'} /> : <HowTo actions={ask.actions} />}
      </Fact>
      <Fact label="How to reply">
        {linked ? (
          <span>No reply needed — this row closes when the request is decided</span>
        ) : reply !== '' ? (
          <AgentBits lines={[reply]} />
        ) : (
          <Missing field="reply" amber />
        )}
      </Fact>
      <Fact label="After">{linked ? <span>Unknown</span> : takes !== '' ? <AgentBits lines={[takes]} /> : <Missing field="takes" />}</Fact>
      {!linked && before !== '' ? <AgentBits lines={[before]} /> : null}
      <Fact label="Latest">
        <Latest ask={ask} items={items} read={read} linked={linked} />
      </Fact>
      <Fact label="If not done">
        {linked ? <span data-holds="no">Unknown</span> : blockedLines.length > 0 ? <AgentBits lines={blockedLines} /> : <Missing field="blocks" />}
      </Fact>
      {!linked && agentText(ask.name) !== '' ? (
        <span className="break-words font-medium">
          <AgentText value={ask.name} />
        </span>
      ) : null}
      {badId ? <span className="text-sm text-muted-foreground">request not shown</span> : null}
      {!linked && typeof ask.since === 'number' ? <Tick className="text-sm text-muted-foreground" text={clock => waitingLine(ask.since ?? 0, clock)} /> : null}
      {!linked && agentText(ask.urgency) !== '' ? <AgentBits lines={[agentText(ask.urgency)]} /> : null}
    </div>
  )
}

function AskLine({
  ask,
  items,
  read,
  allowGreen,
}: {
  ask: AskSource
  items: ApprovalItem[] | undefined
  read: ApprovalRead
  allowGreen: boolean
}) {
  const now = useNow()
  const act = ask.unreadable ? 'unreadable' : askAct(ask, items, read, now)
  const turn = ask.unreadable ? { text: 'Could not read this item', tone: 'neutral' as const } : turnMarker(ask, act === 'unreadable' ? 'unknown' : act)
  const [forced, setForced] = useState<boolean | null>(null)
  const open = forced ?? act === 'now'
  const linked = requestLinked(ask)
  const item = requestItem(ask, items, read)
  const quiet = linked && requestQuiet(item, now)
  const dot =
    allowGreen && turn.tone === 'now'
      ? 'inline-block size-2.5 shrink-0 rounded-full bg-emerald-500'
      : 'inline-block size-2.5 shrink-0 rounded-full border border-current'
  const name = agentText(ask.name)
  return (
    <li
      data-ask={act === 'unreadable' ? 'unreadable' : act}
      data-request-quiet={linked ? (quiet ? 'true' : 'false') : undefined}
      className={
        quiet
          ? 'flex min-w-0 flex-col gap-1 border-t border-[var(--card-border)] py-1 text-sm text-muted-foreground opacity-60 first:border-t-0'
          : 'flex min-w-0 flex-col gap-1 border-t border-[var(--card-border)] py-1 text-sm first:border-t-0'
      }
    >
      <button type="button" aria-expanded={open} className="flex min-h-11 w-full min-w-0 flex-wrap items-center gap-2 text-left" onClick={() => setForced(!open)}>
        <i data-turn-dot={turn.tone} className={dot} />
        <span>{turn.text}</span>
        {!open && name !== '' ? (
          <span className="min-w-0 truncate font-medium">
            <AgentText value={ask.name} />
          </span>
        ) : null}
        {!open && item != null ? <span className="min-w-0 truncate">{requestHeadline(item)}</span> : null}
      </button>
      {linked && read === 'unreadable' ? (
        <>
          <span>Could not read the request&apos;s state</span>
          <a href="#needs-you" className="inline-flex min-h-11 items-center text-sm underline">
            Open Needs you
          </a>
        </>
      ) : null}
      {linked && read === 'ready' && isApprovalId(ask.approval?.id) && item == null ? (
        <span>The request this step refers to was not found</span>
      ) : null}
      {open ? <AskBody ask={ask} items={items} read={read} /> : null}
    </li>
  )
}

function Group({
  title,
  asks,
  items,
  read,
  allowGreen,
}: {
  title: string
  asks: readonly AskSource[]
  items: ApprovalItem[] | undefined
  read: ApprovalRead
  allowGreen: boolean
}) {
  if (asks.length === 0) return null
  return (
    <div data-needs-group={title} className="flex min-w-0 flex-col">
      <h3 className="m-0 text-sm font-medium text-foreground">{title}</h3>
      <ul className="m-0 flex w-full min-w-0 list-none flex-col p-0">
        {asks.map((ask, index) => (
          <AskLine key={`${ask.name ?? 'ask'}-${ask.index}-${index}`} ask={ask} items={items} read={read} allowGreen={allowGreen} />
        ))}
      </ul>
    </div>
  )
}

function asOfText(at: number | null): string {
  if (at == null) return 'Unknown'
  const clock = formatLocalTime(at)
  return clock === '' ? 'Unknown' : clock
}

/** Stale is a clock fact. It is decided here so a tick does not repaint the whole board. */
function shownRead(documentRead: DocumentRead, env: AgentProgressResponse | undefined, nowMs: number): DocumentRead {
  if (documentRead.kind !== 'ok') return documentRead
  const stale = env?.stale === true || (env != null && receivedIsStale(env.received_at, env.age_seconds ?? 0, nowMs))
  if (!stale) return documentRead
  const at = env?.document?.at
  return { kind: 'stale', lastGoodAt: typeof at === 'number' && at > 0 ? at : null }
}

function NeedsYouLive({
  asks,
  items,
  read,
  listUnreadable,
  later,
  documentRead,
  env,
}: {
  asks: AskSource[]
  items: ApprovalItem[] | undefined
  read: ApprovalRead
  listUnreadable: boolean
  later: number | null
  documentRead: DocumentRead
  env?: AgentProgressResponse
}) {
  const now = useNow()
  const shown = shownRead(documentRead, env, now)
  if (shown.kind === 'loading') return <p className="m-0 text-sm text-muted-foreground">Loading…</p>
  const current = shown.kind === 'ok'
  const bothOk = current && read === 'ready'
  const ordered = current ? orderedAsks(asks, items, read, now) : []
  const buckets = bucketAsks(ordered, items, read, now)
  const counted = current ? needsYouTotal(buckets) : { n: 0, a: 0, b: 0, c: 0 }
  const { n, a, b, c } = counted
  const empty = bothOk && ordered.length === 0 && !listUnreadable
  const asOf = shown.kind === 'ok' ? '' : asOfText(shown.lastGoodAt)
  return (
    <>
      <h2 className="m-0 flex min-h-11 flex-wrap items-center gap-2 text-sm font-medium">
        Needs you
        <span data-owner-count="" data-needs-total={n} data-now-count={a} className="rounded-full bg-foreground/10 px-2">
          {n}
        </span>
      </h2>
      <p className="m-0 text-sm text-muted-foreground">
        Can do now <span data-owner-count="" data-can-now={a}>{a}</span>
        {' · '}
        Not your turn yet <span data-owner-count="" data-not-yet={b}>{b}</span>
        {c > 0 ? (
          <>
            {' · '}
            Unknown <span data-owner-count="" data-needs-unknown={c}>{c}</span>
          </>
        ) : null}
      </p>
      {current && listUnreadable ? <span className="text-sm">Could not read this item</span> : null}
      {empty ? <span className="text-sm text-muted-foreground">Nothing needs you now, and nothing is queued for you.</span> : null}
      {shown.kind === 'ok' ? (
        <>
          <Group title="Can do now" asks={buckets.now} items={items} read={read} allowGreen={bothOk} />
          <Group title="Not your turn yet" asks={buckets.later} items={items} read={read} allowGreen={bothOk} />
          <Group title="Unknown" asks={buckets.unknown} items={items} read={read} allowGreen={bothOk} />
          <Group title="Expired" asks={buckets.expired} items={items} read={read} allowGreen={bothOk} />
          {ordered.length > 0 ? (
            <p className="m-0 text-sm text-muted-foreground">
              Three places to act: reply in the thread · decide in Needs you · paste a command in your terminal. Each row says which.
            </p>
          ) : null}
          {later != null ? (
            <p className="m-0 text-sm text-muted-foreground">
              Later: about <span data-owner-count="">{later}</span> more requests to you over this step, most of them a release to approve
            </p>
          ) : null}
        </>
      ) : (
        <>
          <p className="m-0 text-sm text-foreground">Unknown — the progress document could not be read</p>
          <p className="m-0 text-sm text-muted-foreground">Last good read: {asOf}</p>
          <a href="#needs-you" className="inline-flex min-h-11 items-center text-sm underline">
            Requests waiting for you are listed there
          </a>
          {asks.length > 0 ? (
            <ul className="m-0 flex w-full min-w-0 list-none flex-col p-0">
              {asks.map((ask, index) => (
                <li key={`${ask.name ?? 'ask'}-${ask.index}-${index}`} data-historical="" className="flex min-w-0 flex-col gap-1 border-t border-[var(--card-border)] py-1 text-sm first:border-t-0">
                  <span className="text-muted-foreground">as of {asOf}</span>
                  {agentText(ask.name) !== '' ? <AgentText value={ask.name} /> : <Missing field="name" />}
                </li>
              ))}
            </ul>
          ) : null}
        </>
      )}
    </>
  )
}

/** The only Needs you block. Its shell does not subscribe to the clock. */
export function NeedsYouBlock({
  asks,
  items,
  read,
  listUnreadable,
  later,
  documentRead = { kind: 'ok' },
  env,
}: {
  asks: AskSource[]
  items: ApprovalItem[] | undefined
  read: ApprovalRead
  listUnreadable: boolean
  later: number | null
  documentRead?: DocumentRead
  env?: AgentProgressResponse
}) {
  const renders = useRef(0)
  renders.current += 1
  return (
    <section
      id="needs-you"
      aria-label="Needs you"
      data-summary-block="needs-you"
      data-summary-renders={renders.current}
      className="flex w-full min-w-0 flex-col gap-2 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-3 py-2 text-left"
    >
      <NeedsYouLive asks={asks} items={items} read={read} listUnreadable={listUnreadable} later={later} documentRead={documentRead} env={env} />
    </section>
  )
}
