import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { SegmentControl } from '@bifrost/ui'
import { fetchApprovalList, recentClosed } from '@/api/approvals'
import { fetchAudit } from '@/api/cluster'
import { formatShellHash } from '@/lib/shell/consoleRoutes'
import { AuditPage } from '@/pages/AuditPage'
import { AutonomousSkillsPage } from '@/pages/AutonomousSkillsPage'
import { ApprovalRow } from '@/pages/shell/needs-you/ApprovalRow'
import { RECORDS_TABS, recordsTabFromHash, type RecordsTab } from '@/pages/shell/records/recordsTabs'


function ClosedRequests() {
  const q = useQuery({
    queryKey: ['approvals', 'all'],
    queryFn: () => fetchApprovalList('all'),
    retry: false,
  })
  if (q.isLoading) return <p className="m-0 text-sm text-muted-foreground">Loading…</p>
  if (q.error != null) {
    return (
      <p className="m-0 text-sm text-destructive">
        {q.error instanceof Error ? q.error.message : 'Could not load approvals'}
      </p>
    )
  }
  const closed = recentClosed(q.data ?? [])
  if (closed.length === 0) return <p className="m-0 text-sm text-muted-foreground">No closed requests.</p>
  return (
    <ul aria-label="Closed requests" className="m-0 flex w-full min-w-0 max-w-3xl flex-col gap-2 p-0">
      {closed.map(item => (
        <ApprovalRow key={item.id} item={item} />
      ))}
    </ul>
  )
}

function Audit() {
  const audit = useQuery({
    queryKey: ['audit', 'records'],
    queryFn: fetchAudit,
    retry: false,
  })
  const message = audit.error instanceof Error ? audit.error.message : null
  return (
    <div className="flex w-full min-w-0 flex-col gap-3">
      {message != null ? <p className="m-0 text-sm text-destructive">{message}</p> : null}
      <AuditPage records={audit.data?.records ?? []} isLoading={audit.isLoading} />
    </div>
  )
}

/** Records: what has already happened. Not counted anywhere; the tab is in the address. */
export function RecordsPage() {
  const [tab, setTab] = useState<RecordsTab>(() => recordsTabFromHash(window.location.hash))

  useEffect(() => {
    const sync = () => setTab(recordsTabFromHash(window.location.hash))
    window.addEventListener('hashchange', sync)
    return () => window.removeEventListener('hashchange', sync)
  }, [])

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <SegmentControl
        ariaLabel="Records sections"
        value={tab}
        options={[...RECORDS_TABS]}
        onChange={value => {
          const next = value as RecordsTab
          setTab(next)
          window.location.hash = formatShellHash('records', `tab=${next}`)
        }}
      />
      {tab === 'closed' ? <ClosedRequests /> : null}
      {tab === 'audit' ? <Audit /> : null}
      {tab === 'patrol' ? <AutonomousSkillsPage /> : null}
    </div>
  )
}
