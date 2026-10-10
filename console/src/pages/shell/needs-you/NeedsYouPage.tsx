import { useQuery } from '@tanstack/react-query'
import { useState, type ReactNode } from 'react'
import { fetchLineage } from '@/api/lineage'
import { readApprovalToken } from '@/api/approvals'
import { operatorToken } from '@/api/client'
import { OpsSection } from '@/components/layout/OpsSection'
import { HostHeartbeatRow } from '@/components/shell/HostHeartbeatRow'
import { SystemVerdictLine } from '@/components/shell/SystemVerdictLine'
import { ApprovalRow } from '@/pages/shell/needs-you/ApprovalRow'
import { ApprovalTokenField } from '@/pages/shell/needs-you/ApprovalTokenField'
import { SilentThreadRow } from '@/pages/shell/needs-you/SilentThreadRow'
import { needsYouCountText } from '@/pages/shell/needs-you/needsYouModel'
import { resolveSilentThread, SILENT_THREAD_EXPLANATION } from '@/pages/shell/needs-you/silentThread'
import { useNeedsYou } from '@/pages/shell/needs-you/useNeedsYou'

function Group({
  label,
  count,
  description,
  children,
}: {
  label: string
  count: string
  description?: string
  children: ReactNode
}) {
  return (
    <div role="region" aria-label={label}>
      <OpsSection title={`${label} · ${count}`} description={description} bodyPadding="compact">
        {children}
      </OpsSection>
    </div>
  )
}

function Note({ children }: { children: ReactNode }) {
  return <p className="m-0 text-sm text-muted-foreground">{children}</p>
}

/**
 * Home. Lists what waits for the Owner — approve, silent agent threads, decide, sign
 * off — and links each request to its page. No approve or reject here.
 */
export function NeedsYouPage() {
  const { count, approveCount, silentCount, toApprove, silent, lost, capacityText, query } = useNeedsYou()
  const lineageQ = useQuery({
    queryKey: ['lineage', 'needs-you'],
    queryFn: () => fetchLineage(14),
    staleTime: 60_000,
    retry: false,
  })
  const [approvalToken, setApprovalToken] = useState(() => readApprovalToken())
  const hasReadToken = operatorToken() !== '' || approvalToken !== ''
  const now = Date.now()

  let approve: ReactNode
  if (approveCount.state === 'loading') {
    approve = <Note>Loading…</Note>
  } else if (approveCount.state === 'unknown') {
    approve = (
      <div className="flex w-full min-w-0 flex-col gap-2">
        <Note>
          {hasReadToken
            ? `Could not read approvals: ${approveCount.reason}`
            : 'This device has no approval token. Save it once to see what waits for you here.'}
        </Note>
        <ApprovalTokenField
          token={approvalToken}
          onSaved={next => {
            setApprovalToken(next)
            void query.refetch()
          }}
        />
      </div>
    )
  } else if (toApprove.length === 0) {
    approve = <Note>Nothing to approve.</Note>
  } else {
    approve = (
      <ul className="m-0 flex w-full min-w-0 flex-col gap-2 p-0">
        {toApprove.map(item => (
          <ApprovalRow key={item.id} item={item} now={now} />
        ))}
      </ul>
    )
  }

  let stalled: ReactNode
  if (silentCount.state === 'loading') {
    stalled = <Note>Loading…</Note>
  } else if (silentCount.state === 'unknown') {
    stalled = <Note>{`Could not read agent threads: ${silentCount.reason}`}</Note>
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
    <div data-testid="needs-you-page" className="flex w-full min-w-0 max-w-3xl flex-col gap-4 overflow-x-clip">
      <SystemVerdictLine link />
      <p className="m-0 text-base font-medium text-foreground" data-needs-you-count={needsYouCountText(count)}>
        {count.state === 'known' && count.count === 0
          ? 'Nothing needs you right now'
          : count.state === 'known'
            ? `${count.count} waiting for you`
            : count.state === 'unknown'
              ? 'Needs you: Unknown'
              : 'Checking what needs you…'}
      </p>
      <Group label="Approve" count={needsYouCountText(approveCount)}>
        {approve}
      </Group>
      <Group
        label="Silent threads"
        count={needsYouCountText(silentCount)}
        description={SILENT_THREAD_EXPLANATION}
      >
        {stalled}
      </Group>
      <Group label="Decide" count="—">
        <Note>Not connected yet.</Note>
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
    </div>
  )
}
