import { useState, type ReactNode } from 'react'
import { readApprovalToken } from '@/api/approvals'
import { operatorToken } from '@/api/client'
import { OpsSection } from '@/components/layout/OpsSection'
import { SystemVerdictLine } from '@/components/shell/SystemVerdictLine'
import { ApprovalRow } from '@/pages/shell/needs-you/ApprovalRow'
import { ApprovalTokenField } from '@/pages/shell/needs-you/ApprovalTokenField'
import { needsYouCountText } from '@/pages/shell/needs-you/needsYouModel'
import { useNeedsYou } from '@/pages/shell/needs-you/useNeedsYou'

function Group({
  label,
  count,
  children,
}: {
  label: string
  count: string
  children: ReactNode
}) {
  return (
    <div role="region" aria-label={label}>
      <OpsSection title={`${label} · ${count}`} bodyPadding="compact">
        {children}
      </OpsSection>
    </div>
  )
}

function Note({ children }: { children: ReactNode }) {
  return <p className="m-0 text-sm text-muted-foreground">{children}</p>
}

/**
 * Home. Lists what waits for the Owner — approve, decide, sign off — and links each
 * request to its page. No approve or reject here.
 */
export function NeedsYouPage() {
  const { count, toApprove, query } = useNeedsYou()
  const [approvalToken, setApprovalToken] = useState(() => readApprovalToken())
  const hasReadToken = operatorToken() !== '' || approvalToken !== ''
  const now = Date.now()

  let approve: ReactNode
  if (count.state === 'loading') {
    approve = <Note>Loading…</Note>
  } else if (count.state === 'unknown') {
    approve = (
      <div className="flex w-full min-w-0 flex-col gap-2">
        <Note>
          {hasReadToken
            ? `Could not read approvals: ${count.reason}`
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

  return (
    <div data-testid="needs-you-page" className="flex w-full min-w-0 max-w-3xl flex-col gap-4">
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
      <Group label="Approve" count={needsYouCountText(count)}>
        {approve}
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
