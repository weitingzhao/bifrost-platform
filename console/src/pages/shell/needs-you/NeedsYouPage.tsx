import { useQuery } from '@tanstack/react-query'
import { useEffect, type ReactNode } from 'react'
import { clearLegacyApprovalToken } from '@/api/approvals'
import { fetchLineage } from '@/api/lineage'
import { approvalsReadFailure } from '@/pages/approvalsReadNotice'
import { OpsSection } from '@/components/layout/OpsSection'
import { HostHeartbeatRow } from '@/components/shell/HostHeartbeatRow'
import { AnswerGroup } from '@/pages/shell/needs-you/AnswerGroup'
import { questionHref } from '@/lib/shell/consoleRoutes'
import { ApprovalRow } from '@/pages/shell/needs-you/ApprovalRow'
import { SilentThreadRow } from '@/pages/shell/needs-you/SilentThreadRow'
import { needsYouCountText, type NeedsYouCount } from '@/pages/shell/needs-you/needsYouModel'
import {
  DRILL_LINE,
  EARLIER_NOTIFICATIONS,
  TERMINAL_NOT_RUN,
  checkLines,
  type HandledRow,
} from '@/pages/shell/needs-you/needsYouGroups'
import { resolveSilentThread, SILENT_THREAD_EXPLANATION } from '@/pages/shell/needs-you/silentThread'
import { useNeedsYou } from '@/pages/shell/needs-you/useNeedsYou'
import { approvalPart } from '@/pages/shell/progress/progressPart'

function Group({
  label,
  count,
  description,
  children,
  collapsed = false,
}: {
  collapsed?: boolean
  label: string
  count: string
  description?: string
  children: ReactNode
}) {
  return (
    <div role="region" aria-label={label}>
      <OpsSection title={`${label} · ${count}`} description={description} collapsible={collapsed} defaultCollapsed={collapsed} bodyPadding="compact">
        {children}
      </OpsSection>
    </div>
  )
}

function Note({ children }: { children: ReactNode }) {
  return <p className="m-0 text-sm text-muted-foreground">{children}</p>
}

function ApprovalList({ children }: { children: ReactNode }) {
  return <ul className="m-0 flex w-full min-w-0 flex-col gap-2 p-0">{children}</ul>
}

function HandledRows({ rows }: { rows: HandledRow[] }) {
  if (rows.length === 0) return null
  return (
    <ul className="m-0 flex w-full min-w-0 flex-col gap-2 p-0">
      {rows.map(row =>
        row.kind === 'approval' ? (
          <ApprovalRow key={row.item.id} item={row.item} lines={[row.line]} />
        ) : row.kind === 'question' ? (
          <li key={row.id} className="list-none min-w-0 break-words text-sm text-foreground">
            <a href={questionHref(row.id)} className="text-inherit no-underline">
              {row.line}
            </a>
          </li>
        ) : (
          <li
            key={`${row.thread.vendor}/${row.thread.thread}`}
            data-agent-thread={row.thread.thread}
            className="list-none min-w-0 break-words text-sm text-foreground"
          >
            {row.line}
          </li>
        ),
      )}
    </ul>
  )
}

function RecentlyHandled({ count, rows }: { count: NeedsYouCount; rows: HandledRow[] }) {
  return (
    <OpsSection
      id="recently-handled"
      title={`Recently handled · ${needsYouCountText(count)}`}
      collapsible
      defaultCollapsed
      bodyPadding="compact"
      className="min-w-0 max-w-full"
    >
      {count.state === 'unknown' ? <Note>{`Could not read handled requests: ${count.reason}`}</Note> : null}
      <HandledRows rows={rows} />
    </OpsSection>
  )
}

/**
 * For you's waiting section (it was the Needs you page; W-31 2.2b). Lists what waits for the
 * Owner: decide, run in a terminal, check, questions, agent cards and silent threads. Recently
 * handled keeps a notification findable after the item has closed. No approve or reject here.
 * For you carries the one count and the verdict above it, so this section shows neither.
 */
export function NeedsYouPage() {
  const {
    count,
    runtimeItems, runtimeCount, runtimeQuery,
    openCount,
    silentCount,
    silentOffline,
    recentCount,
    decide,
    terminal,
    check,
    drills,
    recent,
    silent,
    lost,
    capacityText,
    now,
    questions,
    openQuestions,
    questionsLoading,
    questionsUnreadable,
    questionsStale,
    questionsReason,
    query,
  } = useNeedsYou()
  const lineageQ = useQuery({
    queryKey: ['lineage', 'needs-you'],
    queryFn: () => fetchLineage(14),
    staleTime: 60_000,
    retry: false,
  })
  useEffect(() => {
    clearLegacyApprovalToken()
  }, [])
  const showRecent =
    recentCount.state === 'unknown' || (recentCount.state === 'known' && recentCount.count > 0)

  let openBody: ReactNode = null
  if (openCount.state === 'loading') {
    openBody = <Note>Loading…</Note>
  } else if (openCount.state === 'unknown') {
    openBody = (
      <div className="flex w-full min-w-0 flex-col gap-2">
        <Note>
          {query.isPaused ? 'Could not read approvals — offline.' : approvalsReadFailure(query.errorUpdatedAt)}
        </Note>
      </div>
    )
  }

  let stalled: ReactNode
  if (silentCount.state === 'loading') {
    stalled = <Note>Loading…</Note>
  } else if (silentCount.state === 'unknown') {
    // Offline, the reason is already the whole sentence (review 352 C1).
    stalled = <Note>{silentOffline ? silentCount.reason : `Could not read agent threads: ${silentCount.reason}`}</Note>
  } else if (silent.length === 0 && lost.length === 0 && capacityText === '') {
    stalled = <Note>No agent thread has gone silent mid-turn.</Note>
  } else {
    stalled = (
      <ul className="m-0 flex w-full min-w-0 flex-col gap-2 p-0">
        {capacityText !== '' ? (
          <li className="text-sm text-foreground" data-monitoring-full>
            {capacityText}
          </li>
        ) : null}
        {lost.map(host => (
          <HostHeartbeatRow key={host.host} host={host} />
        ))}
        {silent.map(t => (
          <SilentThreadRow
            key={`${t.vendor}/${t.thread}`}
            thread={t}
            match={resolveSilentThread(t.thread, lineageQ.data?.threads)}
          />
        ))}
      </ul>
    )
  }

  return (
    <div data-testid="needs-you-page" data-needs-you-count={needsYouCountText(count)} className="flex w-full min-w-0 max-w-3xl flex-col gap-4 overflow-x-clip">
      {count.state === 'known' && count.count === 0 ? (
        <p className="m-0 text-sm text-muted-foreground">Nothing is waiting in these lists.</p>
      ) : null}
      {count.state === 'known' && count.count === 0 && recentCount.state === 'known' && recentCount.count > 0 ? (
        <p className="m-0 text-sm text-muted-foreground">{EARLIER_NOTIFICATIONS}</p>
      ) : null}
      {openBody}
      {decide.length > 0 ? (
        <Group label="Decide" count={String(decide.length)}>
          <ApprovalList>
            {decide.map(item => (
              <ApprovalRow
                key={item.id}
                item={item}
                now={now}
                partOf={item.status === 'pending' ? approvalPart(item) : null}
                planSteps
              />
            ))}
          </ApprovalList>
        </Group>
      ) : null}
      {terminal.length > 0 ? (
        <Group label="Run in a terminal" count={String(terminal.length)}>
          <ApprovalList>
            {terminal.map(item => (
              <ApprovalRow key={item.id} item={item} now={now} lines={[TERMINAL_NOT_RUN]} planSteps />
            ))}
          </ApprovalList>
        </Group>
      ) : null}
      {check.length > 0 ? (
        <Group label="Check" count={String(check.length)}>
          <ApprovalList>
            {check.map(item => (
              <ApprovalRow key={item.id} item={item} now={now} lines={checkLines(item.error)} planSteps />
            ))}
          </ApprovalList>
        </Group>
      ) : null}
      {drills.length > 0 ? (
        // Drills (v4 §20.3 rule 8): shown as they would be, not counted; nothing runs and there is nothing to approve.
        <Group label="Drills" count={String(drills.length)}>
          <Note>Not counted: a drill shows what a request looks like. Nothing runs, and it cannot be approved.</Note>
          <ApprovalList>
            {drills.map(item => (
              <ApprovalRow key={item.id} item={item} now={now} lines={[DRILL_LINE]} />
            ))}
          </ApprovalList>
        </Group>
      ) : null}
      {questionsLoading ? (
        <p className="m-0 text-sm text-muted-foreground" data-questions-state="loading">
          Loading…
        </p>
      ) : null}
      {questionsUnreadable ? (
        <p className="m-0 break-words text-sm text-destructive" data-questions-state="unreadable">
          {`Could not read questions: ${questionsReason}`}
        </p>
      ) : null}
      {questionsStale ? (
        <p className="m-0 break-words text-sm text-destructive" data-questions-state="stale">
          {`Could not refresh questions: ${questionsReason}`}
        </p>
      ) : null}
      {!questionsLoading && !questionsUnreadable ? (
        <AnswerGroup open={openQuestions} questions={questions} threads={lineageQ.data?.threads} />
      ) : null}
      <Group label="Agent runtime" count={needsYouCountText(runtimeCount)}>
        <Note>Generated: {runtimeQuery.data?.generated_at || 'Unknown'}</Note>
        {runtimeCount.state === 'loading' && <Note>Loading…</Note>}
        {runtimeCount.state === 'unknown' && <Note>Could not load agent runtime: {runtimeCount.reason}. Count is incomplete.</Note>}
        {runtimeCount.state === 'known' && runtimeItems.length === 0 && <Note>No requests.</Note>}
        <ul className="m-0 flex flex-col gap-2 p-0">{runtimeItems.map(item => <li key={item.id} className="list-none text-sm">
          <strong>{item.title}</strong>{item.kind && ` · ${item.kind}`} {item.since && <time>{item.since}</time>}
          <p><a className="underline" href={item.href}>{item.instruction}</a></p>
        </li>)}</ul>
      </Group>
      <Group label="Silent threads" count={needsYouCountText(silentCount)} collapsed description={SILENT_THREAD_EXPLANATION}>
        <Note>Claude sessions that went quiet.</Note>
        {stalled}
      </Group>
      <Group label="Sign off" count="—">
        <Note>Not connected yet.</Note>
      </Group>
      <Note>
        Closed requests, audit, and patrol runs are in{' '}
        <a href="#records?tab=closed" className="underline">
          Records
        </a>
        .
      </Note>
      {showRecent ? <RecentlyHandled count={recentCount} rows={recent} /> : null}
    </div>
  )
}
